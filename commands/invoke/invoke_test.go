// This test is in-package rather than in invoke_test, for two reasons. What
// New produces is a Command whose fields are unexported, and there is no
// exported surface that shows what it parsed short of running the command
// against a real API; and transcribe is unexported too, being the one piece of
// behaviour this package still owns now that the loop lives in core.
package invoke

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/agents"
	"github.com/ksahli/compadre/internal/core/tools/definitions"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/core/usage"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		instructions string
		input        string
		exchange     string
		store        string
		model        string
		tokens       int64
		retries      int
		directory    string
		turns        int
		usage        bool
	}{
		{
			name:      "a message on its own",
			arguments: []string{"-message", "why is the sky blue?"},
			input:     "why is the sky blue?",
		},
		{
			name:         "instructions alongside it",
			arguments:    []string{"-instructions", "answer in one sentence", "-message", "hello"},
			instructions: "answer in one sentence",
			input:        "hello",
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
			// A message that looks like a flag is still a message: the
			// value belongs to -message, not to the flag set.
			name:      "a message that looks like a flag",
			arguments: []string{"-message", "-instructions"},
			input:     "-instructions",
		},
		{
			name:      "a model of the caller's choosing",
			arguments: []string{"-model", "claude-opus-5", "-message", "hello"},
			input:     "hello",
			model:     "claude-opus-5",
		},
		{
			name:      "a ceiling on one reply",
			arguments: []string{"-max-tokens", "4096", "-message", "hello"},
			input:     "hello",
			tokens:    4096,
		},
		{
			name:      "retries of a request the API turned away",
			arguments: []string{"-max-retries", "5", "-message", "hello"},
			input:     "hello",
			retries:   5,
		},
		{
			name:      "a workspace somewhere other than here",
			arguments: []string{"-workspace", "/tmp", "-message", "hello"},
			input:     "hello",
			directory: "/tmp",
		},
		{
			name:      "a ceiling on the turns",
			arguments: []string{"-max-turns", "25", "-message", "hello"},
			input:     "hello",
			turns:     25,
		},
		{
			// Zero is not a bound of zero: it is the absence the
			// defaults are reached for at Execute, and passing it
			// outright is the same as passing nothing.
			name:      "a ceiling explicitly left to the defaults",
			arguments: []string{"-max-tokens", "0", "-max-turns", "0", "-max-retries", "0", "-message", "hello"},
			input:     "hello",
		},
		{
			name:         "all five bounds at once",
			arguments:    []string{"-model", "claude-opus-5", "-max-tokens", "4096", "-max-retries", "5", "-workspace", "/tmp", "-max-turns", "25", "-instructions", "be brief", "-message", "hello"},
			instructions: "be brief",
			input:        "hello",
			model:        "claude-opus-5",
			tokens:       4096,
			retries:      5,
			directory:    "/tmp",
			turns:        25,
		},
		{
			// Not a bound: a question of what is said about the
			// run, and off unless it is asked for.
			name:      "what each turn cost, asked for",
			arguments: []string{"-usage", "-message", "hello"},
			input:     "hello",
			usage:     true,
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
			if command.model != c.model {
				t.Errorf("model = %q, want %q", command.model, c.model)
			}
			if command.tokens != c.tokens {
				t.Errorf("tokens = %d, want %d", command.tokens, c.tokens)
			}
			if command.retries != c.retries {
				t.Errorf("retries = %d, want %d", command.retries, c.retries)
			}
			if command.directory != c.directory {
				t.Errorf("directory = %q, want %q", command.directory, c.directory)
			}
			if command.turns != c.turns {
				t.Errorf("turns = %d, want %d", command.turns, c.turns)
			}
			if command.usage != c.usage {
				t.Errorf("usage = %v, want %v", command.usage, c.usage)
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
			for _, want := range []string{"-instructions", "-message", "-exchange", "-store", "-model", "-max-tokens", "-max-retries", "-workspace", "-max-turns", "-usage"} {
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
		{"a ceiling that is not a number", []string{"-max-tokens", "plenty", "-message", "hello"}},
		{"turns that are not a number", []string{"-max-turns", "lots", "-message", "hello"}},
		{"retries that are not a number", []string{"-max-retries", "often", "-message", "hello"}},
		// These three parse as numbers and are refused here rather
		// than by the flag set: -3 is a bound nobody could have
		// meant, and running with a different one would be an
		// answer to a question nobody asked.
		{"a ceiling below zero", []string{"-max-tokens", "-3", "-message", "hello"}},
		{"turns below zero", []string{"-max-turns", "-3", "-message", "hello"}},
		{"retries below zero", []string{"-max-retries", "-3", "-message", "hello"}},
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

// TestNewRequiresAMessage pins what separates this command from interact. A
// turn is what invoke is for, so a run with nothing to say is refused at
// parsing rather than sent as an empty one — and blank is nothing, the same
// way it is at interact's prompt. The error points at the command that reads
// its turns from stdin, since a caller who gave none may have wanted that one.
func TestNewRequiresAMessage(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"no arguments at all", nil},
		{"every bound but a turn", []string{"-model", "claude-opus-5", "-usage"}},
		{"an explicitly empty message", []string{"-message", ""}},
		{"a message of only spaces", []string{"-message", "   \t "}},
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
			for _, want := range []string{"-message", "interact"} {
				if got := err.Error(); !strings.Contains(got, want) {
					t.Errorf("New() error = %q, want it to name %q", got, want)
				}
			}
		})
	}
}

// TestExecuteRejectsInstructionsOnAResumedExchange pins the one thing settled
// before a provider is ever built. An exchange that already exists was opened
// with instructions of its own, and a run that passes more is asking for
// something this command cannot do — so it is told, rather than having the
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

func (f *filed) Save(_ context.Context, _ Exchange) (Exchange, error) {
	return Exchange{}, errors.New("not asked for here")
}

func (f *filed) Load(_ context.Context, id string) (Exchange, error) {
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
				turns = append(turns, message.Role().String()+": "+text)
			}
		}
	}
	return turns
}

// TestOpen pins the two shapes an exchange arrives in. Without -exchange it is
// a new one, carrying the instructions this run was given and no turns at all.
// With one it is what the store had, whole — and the id comes back with it,
// which is what makes every later save an append rather than a second
// conversation.
//
// The message is not folded in here, in either shape. An exchange is opened
// before the turn is folded onto it, which is what keeps the two shapes from
// reaching any further in than this function.
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
			said:         []string{},
		},
		{
			name:         "an exchange that was already filed",
			command:      &Command{input: "and at sunset?", exchange: "7"},
			id:           "7",
			instructions: "be brief",
			said: []string{
				"User: why is the sky blue?",
				"Assistant: rayleigh scattering",
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

	out, errs := &strings.Builder{}, &strings.Builder{}
	transcribe(out, errs, thread, 0, false)

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
			out, errs := &strings.Builder{}, &strings.Builder{}
			transcribe(out, errs, c.thread, 0, true)

			if got := errs.String(); got != "" {
				t.Errorf("transcribe() said %q about the run, want nothing", got)
			}
			if got := out.String(); got != "" {
				t.Errorf("transcribe() printed %q, want nothing", got)
			}
		})
	}
}

// TestTranscribeSaysWhatTheTurnCost pins the count and the stream it goes on.
// It is this program accounting for the run rather than something the model
// said, so it belongs where the prompt and the exchange id go — which is what
// lets the answer still be piped somewhere on its own.
func TestTranscribeSaysWhatTheTurnCost(t *testing.T) {
	thread := threads.New("",
		messages.New(roles.User, messages.Text("hello")),
		messages.New(roles.Assistant, messages.Text("hola")).With(usage.New(1204, 318)),
	)

	out, errs := &strings.Builder{}, &strings.Builder{}
	transcribe(out, errs, thread, 0, true)

	if got, want := out.String(), "hola\n"; got != want {
		t.Errorf("transcribe() printed %q, want %q", got, want)
	}
	if got, want := errs.String(), "tokens: 1204 in, 318 out\n"; got != want {
		t.Errorf("transcribe() said %q about the run, want %q", got, want)
	}
}

// TestTranscribeIsQuietAboutTheCount pins the two ways a count goes unsaid: a
// caller who did not ask for one, and a turn nobody counted. The second is the
// one worth having — a turn measured at nothing and a turn nobody measured are
// different facts, and reporting the second as "0 in, 0 out" would state a
// number nobody took.
func TestTranscribeIsQuietAboutTheCount(t *testing.T) {
	cases := []struct {
		name   string
		thread threads.Type
		counts bool
	}{
		{
			name: "a count nobody asked for",
			thread: threads.New("",
				messages.New(roles.Assistant, messages.Text("hola")).With(usage.New(1204, 318))),
			counts: false,
		},
		{
			name: "a turn nobody counted",
			thread: threads.New("",
				messages.New(roles.Assistant, messages.Text("hola"))),
			counts: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, errs := &strings.Builder{}, &strings.Builder{}
			transcribe(out, errs, c.thread, 0, c.counts)

			if got, want := out.String(), "hola\n"; got != want {
				t.Errorf("transcribe() printed %q, want %q", got, want)
			}
			if got := errs.String(); got != "" {
				t.Errorf("transcribe() said %q about the run, want nothing", got)
			}
		})
	}
}

// A turn counted at nothing is a turn somebody counted, and is said.
func TestTranscribeSaysACountOfNothing(t *testing.T) {
	thread := threads.New("",
		messages.New(roles.Assistant, messages.Text("hola")).With(usage.New(0, 0)))

	out, errs := &strings.Builder{}, &strings.Builder{}
	transcribe(out, errs, thread, 0, true)

	if got, want := errs.String(), "tokens: 0 in, 0 out\n"; got != want {
		t.Errorf("transcribe() said %q about the run, want %q", got, want)
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
			out, errs := &strings.Builder{}, &strings.Builder{}
			transcribe(out, errs, thread, c.from, false)

			if got := errs.String(); got != "" {
				t.Errorf("transcribe() said %q about the run, want nothing", got)
			}
			if got := out.String(); got != c.want {
				t.Errorf("transcribe() printed %q, want %q", got, c.want)
			}
		})
	}
}

// TestWorkspace pins the root the file tools are held inside: what -workspace
// named, or where the command was run if it named nothing. Both come back
// resolved, which is what lets every later containment check compare two paths
// nothing can still redirect.
func TestWorkspace(t *testing.T) {
	directory := t.TempDir()

	// The temporary directory is itself reached through a symlink on some
	// platforms, so what to expect is what resolving says rather than what
	// t.TempDir handed over.
	resolved, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatalf("resolving the temporary directory: %v", err)
	}

	t.Run("a directory the caller named", func(t *testing.T) {
		root, err := workspace(directory)
		if err != nil {
			t.Fatalf("workspace() error = %v, want nil", err)
		}
		if root != resolved {
			t.Errorf("workspace() = %q, want %q", root, resolved)
		}
	})

	t.Run("a link to one, followed", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "elsewhere")
		if err := os.Symlink(directory, link); err != nil {
			t.Fatalf("linking to the temporary directory: %v", err)
		}

		root, err := workspace(link)
		if err != nil {
			t.Fatalf("workspace() error = %v, want nil", err)
		}
		if root != resolved {
			t.Errorf("workspace() = %q, want %q", root, resolved)
		}
	})

	t.Run("nothing named is where the command was run", func(t *testing.T) {
		here, err := os.Getwd()
		if err != nil {
			t.Fatalf("finding the working directory: %v", err)
		}
		here, err = filepath.EvalSymlinks(here)
		if err != nil {
			t.Fatalf("resolving the working directory: %v", err)
		}

		root, err := workspace("")
		if err != nil {
			t.Fatalf("workspace() error = %v, want nil", err)
		}
		if root != here {
			t.Errorf("workspace() = %q, want %q", root, here)
		}
	})
}

// TestWorkspaceRejectsWhatIsNotOne covers the two arguments that cannot be a
// root. Both are refused rather than worked around: the tools would hold the
// model inside a file perfectly well and answer every request with nothing,
// and a run whose every listing is empty for a reason nobody is told is worse
// than being told what was wrong with the argument.
func TestWorkspaceRejectsWhatIsNotOne(t *testing.T) {
	file := filepath.Join(t.TempDir(), "notadirectory")
	if err := os.WriteFile(file, []byte("hello"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}

	cases := []struct {
		name      string
		directory string
	}{
		{"a path that is not there", filepath.Join(t.TempDir(), "missing")},
		{"a file rather than a directory", file},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, err := workspace(c.directory)
			if err == nil {
				t.Fatal("workspace() error = nil, want an error")
			}
			if root != "" {
				t.Errorf("workspace() = %q, want it empty on error", root)
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

// model is a provider with its replies handed to it, one turn's worth at a
// time, so that a session can be run several turns deep without a service
// behind it. It keeps every thread it was given, which is how a case says what
// the second turn knew about the first.
type model struct {
	replies [][]messages.Type
	err     error
	threads []threads.Type
}

func (m *model) Invoke(_ context.Context, thread threads.Type, _ tools.Registry) ([]messages.Type, error) {
	m.threads = append(m.threads, thread)
	if m.err != nil {
		return nil, m.err
	}
	if len(m.replies) == 0 {
		return nil, errors.New("asked for more turns than this model has")
	}
	reply := m.replies[0]
	m.replies = m.replies[1:]
	return reply, nil
}

// answers is a provider that says each of these once, a turn apiece.
func answers(said ...string) *model {
	replies := [][]messages.Type{}
	for _, text := range said {
		replies = append(replies, []messages.Type{
			messages.New(roles.Assistant, messages.Text(text)),
		})
	}
	return &model{replies: replies}
}

// counted is [answers] with a price on every turn, for the cases where what a
// turn cost is the thing under test.
func counted(input, output int64, said ...string) *model {
	provider := answers(said...)
	for turn, reply := range provider.replies {
		for at, message := range reply {
			provider.replies[turn][at] = message.With(usage.New(input, output))
		}
	}
	return provider
}

// recorder stands in for a store. It files the first exchange it sees under an
// id of its own, so that carrying one id across a whole session can be pinned,
// and counts its writes so that a case can say the record was kept as the
// session went rather than at the end of it.
type recorder struct {
	id     string
	writes int
}

func (r *recorder) Save(_ context.Context, exchange Exchange) (Exchange, error) {
	r.writes++
	if exchange.ID() == "" {
		exchange = exchanges.New(r.id, exchange.Thread())
	}
	return exchange, nil
}

func (r *recorder) Load(context.Context, string) (Exchange, error) {
	return Exchange{}, errors.New("not asked for here")
}

// turn builds a command whose two streams a test holds both ends of. There is
// no third one: the turn arrives as a flag, so nothing here reads.
func turn(said string) (*Command, *bytes.Buffer, *bytes.Buffer) {
	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	return &Command{input: said, out: out, errs: errs}, out, errs
}

// TestConverse pins the whole of what this command does once the wiring is
// built: one turn is folded onto the exchange, the loop runs it to its end,
// what the model said goes to stdout, and the exchange comes back carrying
// both halves under the id the store minted.
func TestConverse(t *testing.T) {
	command, out, errs := turn("why is the sky blue?")
	provider := answers("rayleigh scattering")
	store := &recorder{id: "7"}
	agent := agents.New(provider, definitions.New(), store, agents.Turns)
	opening := exchanges.Open(threads.New("be brief"))

	finished, err := command.converse(context.Background(), agent, opening.With(opening.Thread().Append(ask(command.input))))
	if err != nil {
		t.Fatalf("converse() error = %v, want nil", err)
	}

	if got, want := len(provider.threads), 1; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("converse().ID() = %q, want %q", got, want)
	}
	whole := []string{
		"User: why is the sky blue?",
		"Assistant: rayleigh scattering",
	}
	if got := said(finished.Thread()); !slices.Equal(got, whole) {
		t.Errorf("converse() said %v, want %v", got, whole)
	}

	// The invariant a pipe rests on: what the model said is the whole of
	// stdout, and this command said nothing about itself unasked.
	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("converse() printed %q to stdout, want %q", got, want)
	}
	if got := errs.String(); got != "" {
		t.Errorf("converse() printed %q to stderr, want nothing", got)
	}
}

// TestConversePrintsWhatATurnGotAsFarAsSaying pins why the exchange comes back
// from a run that failed rather than being dropped. A turn that went wrong
// part way through still said something, and reporting the failure by throwing
// that away would lose the answer to report the loss.
func TestConversePrintsWhatATurnGotAsFarAsSaying(t *testing.T) {
	command, out, _ := turn("why is the sky blue?")
	failing := &model{err: errors.New("the model is out")}
	agent := agents.New(failing, definitions.New(), &recorder{id: "7"}, agents.Turns)
	opening := exchanges.Open(threads.New(""))

	finished, err := command.converse(context.Background(), agent, opening.With(opening.Thread().Append(ask(command.input))))
	if err == nil {
		t.Fatal("converse() error = nil, want an error")
	}
	// The question is still in the exchange that came back, and still on
	// its way to the record, which is what makes -exchange a way to pick
	// this up rather than a way to find nothing.
	if got, want := said(finished.Thread()), []string{"User: why is the sky blue?"}; !slices.Equal(got, want) {
		t.Errorf("converse() said %v, want %v", got, want)
	}
	if got := out.String(); got != "" {
		t.Errorf("converse() printed %q, want nothing", got)
	}
}

// TestConverseSaysWhatTheTurnCost pins the count on the stream it belongs on.
// It is this program accounting for the run rather than something the model
// said, so it goes to stderr and the answer is left alone on stdout.
func TestConverseSaysWhatTheTurnCost(t *testing.T) {
	command, out, errs := turn("why is the sky blue?")
	command.usage = true
	agent := agents.New(counted(1204, 318, "rayleigh scattering"), definitions.New(), &recorder{id: "7"}, agents.Turns)
	opening := exchanges.Open(threads.New(""))

	if _, err := command.converse(context.Background(), agent, opening.With(opening.Thread().Append(ask(command.input)))); err != nil {
		t.Fatalf("converse() error = %v, want nil", err)
	}

	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("converse() printed %q to stdout, want %q", got, want)
	}
	if got, want := errs.String(), "tokens: 1204 in, 318 out\n"; got != want {
		t.Errorf("converse() printed %q to stderr, want %q", got, want)
	}
}

// And a run that was not asked for the count says nothing about it, even
// though the provider counted the turn.
func TestConverseIsQuietAboutTheCountUnasked(t *testing.T) {
	command, _, errs := turn("why is the sky blue?")
	agent := agents.New(counted(1204, 318, "rayleigh scattering"), definitions.New(), &recorder{id: "7"}, agents.Turns)
	opening := exchanges.Open(threads.New(""))

	if _, err := command.converse(context.Background(), agent, opening.With(opening.Thread().Append(ask(command.input)))); err != nil {
		t.Fatalf("converse() error = %v, want nil", err)
	}
	if got := errs.String(); got != "" {
		t.Errorf("converse() printed %q to stderr, want nothing", got)
	}
}
