// This test is in-package rather than in invoke_test, for two reasons. What
// New produces is a Command whose fields are unexported, and there is no
// exported surface that shows what it parsed short of running the command
// against a real API; and transcribe is unexported too, being the one piece of
// behaviour this package still owns now that the loop lives in core.
package invoke

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		instructions string
		input        string
		exchange     string
		store        string
	}{
		{
			name:      "no arguments leaves both empty",
			arguments: nil,
		},
		{
			name:      "a message on its own",
			arguments: []string{"-message", "why is the sky blue?"},
			input:     "why is the sky blue?",
		},
		{
			name:         "instructions on their own",
			arguments:    []string{"-instructions", "answer in one sentence"},
			instructions: "answer in one sentence",
		},
		{
			name:         "both, in either order",
			arguments:    []string{"-message", "hello", "-instructions", "be brief"},
			instructions: "be brief",
			input:        "hello",
		},
		{
			name:      "a record kept somewhere named",
			arguments: []string{"-message", "hello", "-store", "/tmp/exchanges.db"},
			input:     "hello",
			store:     "/tmp/exchanges.db",
		},
		{
			name:      "an exchange to continue",
			arguments: []string{"-exchange", "7", "-message", "and at sunset?"},
			input:     "and at sunset?",
			exchange:  "7",
		},
		{
			name:      "an explicitly empty message",
			arguments: []string{"-message", ""},
			input:     "",
		},
		{
			// A message that looks like a flag is still a message: the
			// value belongs to -message, not to the flag set.
			name:      "a message that looks like a flag",
			arguments: []string{"-message", "-instructions"},
			input:     "-instructions",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := New(c.arguments)
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if command == nil {
				t.Fatal("New() = nil, want a command")
			}
			if command.instructions != c.instructions {
				t.Errorf("instructions = %q, want %q", command.instructions, c.instructions)
			}
			if command.input != c.input {
				t.Errorf("input = %q, want %q", command.input, c.input)
			}
			if command.exchange != c.exchange {
				t.Errorf("exchange = %q, want %q", command.exchange, c.exchange)
			}
			if command.store != c.store {
				t.Errorf("store = %q, want %q", command.store, c.store)
			}
		})
	}
}

// TestNewReportsHelpAsItsUsage pins the one Parse error that is not a mistake.
// The binary's own help points a caller here, so what comes back has to be the
// flags this command takes and not the flag package's word for the request.
func TestNewReportsHelpAsItsUsage(t *testing.T) {
	for _, argument := range []string{"-h", "-help"} {
		t.Run(argument, func(t *testing.T) {
			command, err := New([]string{argument})
			if err == nil {
				t.Fatal("New() error = nil, want the usage")
			}
			if command != nil {
				t.Errorf("New() = %v, want nil on error", command)
			}
			for _, want := range []string{"-instructions", "-message", "-exchange", "-store"} {
				if got := err.Error(); !strings.Contains(got, want) {
					t.Errorf("New() error = %q, want it to name %q", got, want)
				}
			}
		})
	}
}

// TestNewRejectsUnparseableArguments pins the half the flag set used to take
// out of the caller's hands: an argument it cannot make sense of comes back as
// an error, rather than ending the process from inside the parser.
func TestNewRejectsUnparseableArguments(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"a flag nothing defines", []string{"-nope"}},
		{"a flag missing its value", []string{"-message"}},
		{"a positional where a flag belongs", []string{"-instructions", "be brief", "-nope", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := New(c.arguments)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if command != nil {
				t.Errorf("New() = %v, want nil on error", command)
			}
		})
	}
}

// TestExecuteRejectsAnEmptyMessage pins what happens before a provider is
// ever built: an exchange with nothing to open it is refused here, where the
// caller can be told what to pass, rather than by the API in its own words.
func TestExecuteRejectsAnEmptyMessage(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"no message at all", ""},
		{"a message that is only spaces", "   "},
		{"a message that is only a newline", "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command := &Command{input: c.input}

			err := command.Execute(context.Background())
			if err == nil {
				t.Fatal("Execute() error = nil, want an error")
			}
			if got := err.Error(); !strings.Contains(got, "-message") {
				t.Errorf("Execute() error = %q, want it to name %q", got, "-message")
			}
		})
	}
}

// TestExecuteRejectsInstructionsOnAResumedExchange pins the other thing
// settled before a provider is built. An exchange that already exists was
// opened with instructions of its own, and a run that passes more is asking
// for something this command cannot do — so it is told, rather than having the
// flag quietly dropped.
func TestExecuteRejectsInstructionsOnAResumedExchange(t *testing.T) {
	command := &Command{input: "and at sunset?", exchange: "7", instructions: "be brief"}

	err := command.Execute(context.Background())
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}
	for _, want := range []string{"-instructions", "-exchange", "7"} {
		if got := err.Error(); !strings.Contains(got, want) {
			t.Errorf("Execute() error = %q, want it to name %q", got, want)
		}
	}
}

// filed is a store with one exchange in it, or an error where an exchange
// would be. Save is not reached from open, which is the only thing these tests
// call, so it says so rather than pretending to keep anything.
type filed struct {
	exchange Exchange
	err      error
	asked    []string
}

func (f *filed) Save(_ Context, _ Exchange) (Exchange, error) {
	return Exchange{}, errors.New("not asked for here")
}

func (f *filed) Load(_ Context, id string) (Exchange, error) {
	f.asked = append(f.asked, id)
	if f.err != nil {
		return Exchange{}, f.err
	}
	return f.exchange, nil
}

// said renders a thread as one string per turn, so a test can say what it
// expects to be there without reaching into every content block.
func said(thread threads.Type) []string {
	turns := []string{}
	for _, message := range thread.Messages() {
		for _, content := range message.Content() {
			if text, ok := content.Text(); ok {
				turns = append(turns, message.Role()+": "+text)
			}
		}
	}
	return turns
}

// TestOpen pins the two shapes an exchange arrives in. Without -exchange it is
// a new one, carrying the instructions this run was given and the message as
// its only turn. With one it is what the store had, the message folded onto
// the end of it — and the id comes back with it, which is what makes every
// later save an append rather than a second conversation.
func TestOpen(t *testing.T) {
	stored := exchanges.New("7", threads.New("be brief",
		messages.New(roles.User, messages.Text("why is the sky blue?")),
		messages.New(roles.Assistant, messages.Text("rayleigh scattering")),
	))

	cases := []struct {
		name         string
		command      *Command
		id           string
		instructions string
		said         []string
	}{
		{
			name:         "an exchange nobody has opened yet",
			command:      &Command{input: "why is the sky blue?", instructions: "be brief"},
			id:           "",
			instructions: "be brief",
			said:         []string{"User: why is the sky blue?"},
		},
		{
			name:         "an exchange that was already filed",
			command:      &Command{input: "and at sunset?", exchange: "7"},
			id:           "7",
			instructions: "be brief",
			said: []string{
				"User: why is the sky blue?",
				"Assistant: rayleigh scattering",
				"User: and at sunset?",
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			store := &filed{exchange: stored}

			opened, err := c.command.open(context.Background(), store)
			if err != nil {
				t.Fatalf("open() error = %v, want nil", err)
			}
			if got := opened.ID(); got != c.id {
				t.Errorf("open().ID() = %q, want %q", got, c.id)
			}
			if got := opened.Thread().Instructions(); got != c.instructions {
				t.Errorf("open().Thread().Instructions() = %q, want %q", got, c.instructions)
			}
			if got := said(opened.Thread()); !slices.Equal(got, c.said) {
				t.Errorf("open() said %v, want %v", got, c.said)
			}
		})
	}
}

// TestOpenLeavesTheFiledExchangeAlone pins the immutability the append rests
// on: what the store handed over is not the thing that grew.
func TestOpenLeavesTheFiledExchangeAlone(t *testing.T) {
	stored := exchanges.New("7", threads.New("",
		messages.New(roles.User, messages.Text("why is the sky blue?")),
	))
	store := &filed{exchange: stored}
	command := &Command{input: "and at sunset?", exchange: "7"}

	if _, err := command.open(context.Background(), store); err != nil {
		t.Fatalf("open() error = %v, want nil", err)
	}

	want := []string{"User: why is the sky blue?"}
	if got := said(stored.Thread()); !slices.Equal(got, want) {
		t.Errorf("the filed exchange said %v, want %v", got, want)
	}
}

// TestOpenOfAnExchangeThatIsNotThere pins that an id nothing was filed under
// ends the run. Opening a fresh exchange instead would answer a question the
// caller did not ask, under an id they did not choose.
func TestOpenOfAnExchangeThatIsNotThere(t *testing.T) {
	store := &filed{err: errors.New("no exchange is filed under '404'")}
	command := &Command{input: "and at sunset?", exchange: "404"}

	opened, err := command.open(context.Background(), store)
	if err == nil {
		t.Fatal("open() error = nil, want an error")
	}
	if got := opened.ID(); got != "" {
		t.Errorf("open() = an exchange filed under %q, want none", got)
	}
	if want := []string{"404"}; !slices.Equal(store.asked, want) {
		t.Errorf("the store was asked for %v, want %v", store.asked, want)
	}
}

// TestTranscribe pins what a reader is shown, which is the half of the
// exchange that is any of their business. The loop hands back the whole
// thing — the turn that opened it, every call the model made and every answer
// it got — and only what the model said in words comes out of here.
func TestTranscribe(t *testing.T) {
	thread := threads.New("be brief",
		messages.New(roles.User, messages.Text("hello")),
		messages.New(roles.Assistant,
			messages.Text("let me look"),
			messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`))),
		),
		messages.New(roles.User, messages.Result(tools.Success("toolu_1", "the answer"))),
		messages.New(roles.Assistant, messages.Text("hola")),
	)

	out := &strings.Builder{}
	transcribe(out, thread, 0)

	if got, want := out.String(), "let me look\nhola\n"; got != want {
		t.Errorf("transcribe() printed %q, want %q", got, want)
	}
}

// TestTranscribeAnExchangeWithNothingSaid pins the quiet cases. A thread the
// loop never got an answer into prints nothing at all, rather than a blank
// line standing in for one — which is what makes it safe to call on a run that
// failed before the model ever spoke.
func TestTranscribeAnExchangeWithNothingSaid(t *testing.T) {
	cases := []struct {
		name   string
		thread threads.Type
	}{
		{
			"a run that failed on its first turn",
			threads.New("", messages.New(roles.User, messages.Text("hello"))),
		},
		{
			"an exchange with no turns in it at all",
			threads.New(""),
		},
		{
			"an assistant turn that only asked for a tool",
			threads.New("",
				messages.New(roles.User, messages.Text("hello")),
				messages.New(roles.Assistant,
					messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`)))),
			),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &strings.Builder{}
			transcribe(out, c.thread, 0)

			if got := out.String(); got != "" {
				t.Errorf("transcribe() printed %q, want nothing", got)
			}
		})
	}
}

// TestTranscribeFromWhereTheRunBegan pins what a resumed exchange prints. The
// thread handed back holds everything that was ever said in it, and only what
// was said this run is the answer to what was just asked — the rest the caller
// has already read once.
func TestTranscribeFromWhereTheRunBegan(t *testing.T) {
	thread := threads.New("",
		messages.New(roles.User, messages.Text("why is the sky blue?")),
		messages.New(roles.Assistant, messages.Text("rayleigh scattering")),
		messages.New(roles.User, messages.Text("and at sunset?")),
		messages.New(roles.Assistant, messages.Text("the light travels further")),
	)

	cases := []struct {
		name string
		from int
		want string
	}{
		{"from the beginning", 0, "rayleigh scattering\nthe light travels further\n"},
		{"from where this run began", 3, "the light travels further\n"},
		{"from the end", 4, ""},
		{
			// A thread only grows, so this cannot happen; it prints
			// nothing rather than reaching past the end if it does.
			name: "from past the end",
			from: 9,
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &strings.Builder{}
			transcribe(out, thread, c.from)

			if got := out.String(); got != c.want {
				t.Errorf("transcribe() printed %q, want %q", got, c.want)
			}
		})
	}
}

// TestRecord pins where the exchange is written. A path given on the command
// line is taken as it is; without one, the record goes to a database of this
// program's own under the user's config directory rather than into whatever
// directory the caller happened to be standing in.
func TestRecord(t *testing.T) {
	t.Run("a path that was named", func(t *testing.T) {
		want := filepath.Join(t.TempDir(), "somewhere.db")
		command := &Command{input: "hello", store: want}

		got, err := command.record()
		if err != nil {
			t.Fatalf("record() error = %v, want nil", err)
		}
		if got != want {
			t.Errorf("record() = %q, want %q", got, want)
		}
	})

	t.Run("no path at all", func(t *testing.T) {
		// UserConfigDir reads the environment, so the test says what
		// it should find there rather than writing to the real one.
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		command := &Command{input: "hello"}

		got, err := command.record()
		if err != nil {
			t.Fatalf("record() error = %v, want nil", err)
		}

		config, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("UserConfigDir() error = %v, want nil", err)
		}
		if want := filepath.Join(config, "compadre", "exchanges.db"); got != want {
			t.Errorf("record() = %q, want %q", got, want)
		}
		// The directory is made on the way, so that opening the store
		// is not the thing that discovers it is not there.
		if _, err := os.Stat(filepath.Dir(got)); err != nil {
			t.Errorf("the directory for the record was not made: %v", err)
		}
	})
}
