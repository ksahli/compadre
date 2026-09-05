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
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/agents"
	"github.com/ksahli/compadre/internal/core/tools/definitions"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/failures"
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
			// Every bound left at its zero value, which is how an
			// absent flag says "whatever was true before there was
			// one to say otherwise".
			name:      "no arguments leaves them all empty",
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
			// No message is not a mistake to be caught at parsing:
			// it is the run that reads its turns from stdin.
			name:      "a store and no message",
			arguments: []string{"-store", "/tmp/exchanges.db"},
			store:     "/tmp/exchanges.db",
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
			arguments: []string{"-max-tokens", "0", "-max-turns", "0", "-max-retries", "0"},
		},
		{
			name:         "all five bounds at once",
			arguments:    []string{"-model", "claude-opus-5", "-max-tokens", "4096", "-max-retries", "5", "-workspace", "/tmp", "-max-turns", "25", "-instructions", "be brief"},
			instructions: "be brief",
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
		{"a ceiling that is not a number", []string{"-max-tokens", "plenty"}},
		{"turns that are not a number", []string{"-max-turns", "lots"}},
		{"retries that are not a number", []string{"-max-retries", "often"}},
		// These two parse as numbers and are refused here rather
		// than by the flag set: -3 is a bound nobody could have
		// meant, and running with a different one would be an
		// answer to a question nobody asked.
		{"a ceiling below zero", []string{"-max-tokens", "-3"}},
		{"turns below zero", []string{"-max-turns", "-3"}},
		{"retries below zero", []string{"-max-retries", "-3"}},
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
// before anything knows whether this run has one turn to put in it or a
// session's worth, and both fold their own on: that is what lets one function
// serve the two.
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
		{
			// A session opens the same exchange as a single
			// message does; having nothing to say yet is not a
			// different shape.
			name:         "a session with nothing said in it yet",
			command:      &Command{instructions: "be brief"},
			id:           "",
			instructions: "be brief",
			said:         []string{},
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

func (m *model) Invoke(_ Context, thread threads.Type, _ tools.Registry) ([]messages.Type, error) {
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

func (r *recorder) Save(_ Context, exchange Exchange) (Exchange, error) {
	r.writes++
	if exchange.ID() == "" {
		exchange = exchanges.New(r.id, exchange.Thread())
	}
	return exchange, nil
}

func (r *recorder) Load(Context, string) (Exchange, error) {
	return Exchange{}, errors.New("not asked for here")
}

// session builds a command whose streams a test holds both ends of, and the
// buffers behind two of them. The third is what the session reads its turns
// from, one per line.
func session(turns string) (*Command, *bytes.Buffer, *bytes.Buffer) {
	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	return &Command{in: strings.NewReader(turns), out: out, errs: errs}, out, errs
}

// TestSession pins the shape of the thing: every line is a turn, and each is
// answered in the exchange the last one grew rather than in one of its own.
// The second thread the model is handed is the proof — it carries the first
// question and its answer, which is what makes this a conversation and not two
// runs sharing a process.
func TestSession(t *testing.T) {
	command, out, _ := session("why is the sky blue?\nand at sunset?\n")
	provider := answers("rayleigh scattering", "the light travels further")
	store := &recorder{id: "7"}
	agent := agents.New(provider, definitions.New(), store, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("be brief")), nil)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got, want := len(provider.threads), 2; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	first := []string{"User: why is the sky blue?"}
	if got := said(provider.threads[0]); !slices.Equal(got, first) {
		t.Errorf("the first turn was handed %v, want %v", got, first)
	}
	second := []string{
		"User: why is the sky blue?",
		"Assistant: rayleigh scattering",
		"User: and at sunset?",
	}
	if got := said(provider.threads[1]); !slices.Equal(got, second) {
		t.Errorf("the second turn was handed %v, want %v", got, second)
	}

	// One exchange, under one id, holding everything said in it.
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
	whole := append(second, "Assistant: the light travels further")
	if got := said(finished.Thread()); !slices.Equal(got, whole) {
		t.Errorf("session() said %v, want %v", got, whole)
	}

	if got, want := out.String(), "rayleigh scattering\nthe light travels further\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}

	// The record was kept as the session went, not at the end of it: two
	// writes a turn, the question before the model is asked anything and
	// the answer once it has been. A session interrupted after the first
	// turn has that turn on disk.
	if got, want := store.writes, 4; got != want {
		t.Errorf("the record was written to %d times, want %d", got, want)
	}
}

// TestSessionKeepsTheStreamsApart pins the invariant a pipe rests on. What the
// model said goes to stdout and nothing else does; the prompt is this program
// talking about itself, so it goes where the id line goes.
func TestSessionKeepsTheStreamsApart(t *testing.T) {
	command, out, errs := session("why is the sky blue?\n")
	agent := agents.New(answers("rayleigh scattering"), definitions.New(), &recorder{id: "7"}, agents.Turns)

	if _, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil); err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("session() printed %q to stdout, want %q", got, want)
	}
	// One prompt before the turn and one before the read that finds the
	// end: a session asks again until it is told there is nothing more.
	if got, want := errs.String(), prompt+prompt; got != want {
		t.Errorf("session() printed %q to stderr, want %q", got, want)
	}
}

// TestSessionSaysWhatEachTurnCost pins the count over a whole session: one
// line per turn the model took, on the stream this program talks about itself
// on, with the answer left alone on the other one. The prompts and the counts
// interleave because that is the order they happened in.
func TestSessionSaysWhatEachTurnCost(t *testing.T) {
	command, out, errs := session("why is the sky blue?\nand at sunset?\n")
	command.usage = true
	provider := counted(1204, 318, "rayleigh scattering", "the light travels further")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	if _, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil); err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got, want := out.String(), "rayleigh scattering\nthe light travels further\n"; got != want {
		t.Errorf("session() printed %q to stdout, want %q", got, want)
	}

	count := "tokens: 1204 in, 318 out\n"
	if got, want := errs.String(), prompt+count+prompt+count+prompt; got != want {
		t.Errorf("session() printed %q to stderr, want %q", got, want)
	}
}

// And a session that was not asked for the counts says nothing about them,
// even though the provider counted every turn.
func TestSessionIsQuietAboutTheCountUnasked(t *testing.T) {
	command, out, errs := session("why is the sky blue?\n")
	provider := counted(1204, 318, "rayleigh scattering")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	if _, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil); err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("session() printed %q to stdout, want %q", got, want)
	}
	if got, want := errs.String(), prompt+prompt; got != want {
		t.Errorf("session() printed %q to stderr, want %q", got, want)
	}
}

// TestSessionDoesNotSendABlankLine pins the one line that is not a turn. An
// exchange with nothing to carry it on is a request that would come back
// refused, and at a prompt the answer to that is to ask again rather than to
// spend it finding out.
func TestSessionDoesNotSendABlankLine(t *testing.T) {
	command, out, errs := session("\n   \n\t\n")
	provider := answers("rayleigh scattering")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got := len(provider.threads); got != 0 {
		t.Errorf("the model was asked %d times, want 0", got)
	}
	if got := out.String(); got != "" {
		t.Errorf("session() printed %q, want nothing", got)
	}
	// Nothing was ever said, so nothing was ever filed: the id is minted
	// by the first save, and there was none.
	if got := finished.ID(); got != "" {
		t.Errorf("session().ID() = %q, want none", got)
	}
	// Four prompts: one for each blank line, and one for the end.
	if got, want := strings.Count(errs.String(), prompt), 4; got != want {
		t.Errorf("session() prompted %d times, want %d", got, want)
	}
}

// TestSessionEndsWhenThereIsNothingLeftToRead pins the ordinary way out. Stdin
// running out is a session that said everything it had to say, not one that
// went wrong, so it comes back with no error and the exchange as it grew.
func TestSessionEndsWhenThereIsNothingLeftToRead(t *testing.T) {
	command, _, _ := session("")
	provider := answers("rayleigh scattering")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)
	opening := exchanges.Open(threads.New("be brief"))

	finished, err := command.session(context.Background(), agent, opening, nil)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}
	if got := len(provider.threads); got != 0 {
		t.Errorf("the model was asked %d times, want 0", got)
	}
	if got, want := finished.Thread().Instructions(), "be brief"; got != want {
		t.Errorf("session() came back with instructions %q, want %q", got, want)
	}
}

// TestSessionEndsOnACancelledContext pins the other way out, and the reason
// the read is not a plain one. An interrupt at the prompt has nothing in
// flight to spoil, so it ends the session as quietly as running out of input
// does — and it has to be noticed while the session is waiting, or a caller
// who typed nothing more would have no way out at all.
func TestSessionEndsOnACancelledContext(t *testing.T) {
	command, out, _ := session("why is the sky blue?\n")
	provider := answers("rayleigh scattering")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	finished, err := command.session(ctx, agent, exchanges.Open(threads.New("")), nil)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}
	if got := len(provider.threads); got != 0 {
		t.Errorf("the model was asked %d times, want 0", got)
	}
	if got := finished.ID(); got != "" {
		t.Errorf("session().ID() = %q, want none", got)
	}
	if got := out.String(); got != "" {
		t.Errorf("session() printed %q, want nothing", got)
	}
}

// TestSessionCarriesOnAfterATurnThatFailed pins the point of the change. A
// turn that failed is a turn that did not happen, not a session that is over:
// what went wrong is said at the prompt and the next turn is asked for, the
// same way an interrupted one is. Three turns go in, the second fails, and the
// third is answered — which is the whole claim.
func TestSessionCarriesOnAfterATurnThatFailed(t *testing.T) {
	command, out, errs := session("why is the sky blue?\nand at sunset?\nand at noon?\n")
	failing := &stumbling{model: answers("rayleigh scattering", "the sun is overhead"), on: 2, err: errors.New("the model is out")}
	agent := agents.New(failing, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}
	// All three lines were read, and the third was answered.
	if got, want := len(failing.model.threads), 3; got != want {
		t.Errorf("the model was asked %d times, want %d", got, want)
	}
	if got, want := out.String(), "rayleigh scattering\nthe sun is overhead\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}
	// What went wrong is said where this program says everything about
	// itself, so a session driven by a pipe does not find it in the answer.
	if got, want := errs.String(), "the model is out"; !strings.Contains(got, want) {
		t.Errorf("session() said %q on stderr, want it to name %q", got, want)
	}
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
}

// TestSessionEndsOnASettledFailure pins the exception. Coming back to the
// prompt is only worth doing when the next turn could go differently, and a
// failure the core has marked settled says it cannot: the session ends and the
// error comes back, so the id is printed and the exchange is picked up with
// -exchange once whatever it was has been put right.
func TestSessionEndsOnASettledFailure(t *testing.T) {
	command, out, _ := session("why is the sky blue?\nand at sunset?\nand at noon?\n")
	settled := fmt.Errorf("%w: the API would not accept the credentials", failures.ErrSettled)
	failing := &turning{model: answers("rayleigh scattering"), err: settled}
	agent := agents.New(failing, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil)
	if err == nil {
		t.Fatal("session() error = nil, want an error")
	}
	if !errors.Is(err, failures.ErrSettled) {
		t.Errorf("session() error = %v, want it to be settled", err)
	}
	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}
	// The third line is never read: the session stopped on the second.
	if got, want := len(failing.model.threads), 2; got != want {
		t.Errorf("the model was asked %d times, want %d", got, want)
	}
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
}

// TestSessionEndsOnAStoreThatCannotWrite pins the other settled one, and the
// reason it is settled at all. A session that came back to the prompt from
// this would go on answering while nothing was being written down, which is a
// session quietly losing the turns it is still taking.
func TestSessionEndsOnAStoreThatCannotWrite(t *testing.T) {
	command, out, _ := session("why is the sky blue?\nand at sunset?\n")
	provider := answers("rayleigh scattering")
	agent := agents.New(provider, definitions.New(), wedged{}, agents.Turns)

	_, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil)
	if err == nil {
		t.Fatal("session() error = nil, want an error")
	}
	if !errors.Is(err, failures.ErrSettled) {
		t.Errorf("session() error = %v, want it to be settled", err)
	}
	if got, want := err.Error(), "could not keep the record"; !strings.Contains(got, want) {
		t.Errorf("session() error = %q, want it to name %q", got, want)
	}
	// The record is written before the model is asked anything, so the
	// turn never got as far as a round trip and nothing was said.
	if got, want := len(provider.threads), 0; got != want {
		t.Errorf("the model was asked %d times, want %d", got, want)
	}
	if got := out.String(); got != "" {
		t.Errorf("session() printed %q, want nothing", got)
	}
}

// TestSessionEndsOnAReaderThatBroke pins the fourth way out, and the reason it
// is not the first. A line past the scanner's ceiling stops the scan, and a
// listen that only closed its channel would hand that to the session as stdin
// having run out — a turn somebody typed swallowed, and the run reported a
// success. It comes back as an error instead, and the turns before it are
// still answered and still printed.
func TestSessionEndsOnAReaderThatBroke(t *testing.T) {
	huge := strings.Repeat("x", maxTurn+1)
	command, out, _ := session("why is the sky blue?\n" + huge + "\n")
	provider := answers("rayleigh scattering", "never asked")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil)
	if err == nil {
		t.Fatal("session() error = nil, want an error")
	}
	if got, want := err.Error(), "could not read the turn"; !strings.Contains(got, want) {
		t.Errorf("session() error = %q, want it to name %q", got, want)
	}
	// The turn before the bad line was answered, and its answer printed.
	if got, want := len(provider.threads), 1; got != want {
		t.Errorf("the model was asked %d times, want %d", got, want)
	}
	if got, want := out.String(), "rayleigh scattering\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}
	// And the id still comes back, so the session can be picked up.
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
}

// TestSessionReadsATurnUpToTheCeiling pins the other half of the same change:
// the ceiling was raised past bufio's default, so a long turn somebody pasted
// is a turn and not the end of the session.
func TestSessionReadsATurnUpToTheCeiling(t *testing.T) {
	long := strings.Repeat("x", 128<<10) // over bufio's own 64KiB default
	command, _, _ := session(long + "\n")
	provider := answers("that is a lot of x")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	if _, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), nil); err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}
	if got, want := len(provider.threads), 1; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	if got := said(provider.threads[0]); len(got) != 1 || got[0] != "User: "+long {
		t.Errorf("the turn handed over was not the long line intact")
	}
}

// TestSessionInterruptedMidTurnAsksAgain is the point of the whole thing. An
// interrupt while the model is being waited on ends that turn and nothing more:
// the prompt comes back, the next line is answered in the same exchange, and
// the thread the model is handed the second time is the proof — it still
// carries the question the interrupt cut short.
func TestSessionInterruptedMidTurnAsksAgain(t *testing.T) {
	command, out, errs := session("why is the sky blue?\nand at sunset?\n")
	entered := make(chan struct{})
	provider := &waiting{entered: entered, model: answers("the light travels further")}
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	// The interrupt is sent once the provider says it has the turn, so the
	// case interrupts a request rather than racing one.
	interrupts := make(chan os.Signal, 1)
	go func() {
		<-entered
		interrupts <- os.Interrupt
	}()

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("be brief")), interrupts)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got, want := len(provider.model.threads), 2; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	second := []string{
		"User: why is the sky blue?",
		"User: and at sunset?",
	}
	if got := said(provider.model.threads[1]); !slices.Equal(got, second) {
		t.Errorf("the second turn was handed %v, want %v", got, second)
	}

	// Only the turn that finished said anything, and the id is the one the
	// interrupted turn was already filed under.
	if got, want := out.String(), "the light travels further\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}
	if got, want := errs.String(), "interrupted"; !strings.Contains(got, want) {
		t.Errorf("session() reported %q, want it to mention %q", got, want)
	}
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
}

// TestSessionEndsOnASecondInterruptAtThePrompt pins the deliberate way out for
// fingers that expect one. There is nothing in flight for an interrupt at the
// prompt to end, so the first is answered with the way out rather than acted
// on, and the second takes it — quietly, like the end of input, since a caller
// who asked to leave has not been failed.
func TestSessionEndsOnASecondInterruptAtThePrompt(t *testing.T) {
	out, errs := &bytes.Buffer{}, &bytes.Buffer{}
	// Nothing is ever written to it: a prompt nobody has typed at.
	untyped, _ := io.Pipe()
	command := &Command{in: untyped, out: out, errs: errs}
	provider := answers()
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	interrupts := make(chan os.Signal, 2)
	interrupts <- os.Interrupt
	interrupts <- os.Interrupt

	opening := exchanges.Open(threads.New("be brief"))
	finished, err := command.session(context.Background(), agent, opening, interrupts)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	if got := len(provider.threads); got != 0 {
		t.Errorf("the model was asked %d times, want none", got)
	}
	if got := out.String(); got != "" {
		t.Errorf("session() printed %q, want nothing", got)
	}
	// The first interrupt said how to leave, and the exchange comes back
	// untouched: nothing was said in it, so nothing filed it.
	if got, want := errs.String(), "^C again"; !strings.Contains(got, want) {
		t.Errorf("session() reported %q, want it to mention %q", got, want)
	}
	if got := finished.ID(); got != "" {
		t.Errorf("session().ID() = %q, want none", got)
	}
}

// TestSessionDisarmsOnAnythingTyped pins the other half of that gesture. Two
// interrupts end a session only if they are two in a row; a caller who went
// back to talking in between is not a caller halfway through quitting.
//
// The case is choreographed on the prompt itself. Every step waits for the
// session to say it is at the prompt again before the next thing is done to
// it, which is what makes an interrupt land where the case says it lands
// rather than wherever the scheduler put it.
func TestSessionDisarmsOnAnythingTyped(t *testing.T) {
	first, second := make(chan struct{}), make(chan struct{})
	out, errs := &bytes.Buffer{}, &announcing{written: &bytes.Buffer{}, prompts: make(chan struct{}, 8)}
	command := &Command{
		in: io.MultiReader(
			held{release: first, reader: strings.NewReader("why is the sky blue?\n")},
			held{release: second, reader: strings.NewReader("and at sunset?\n")},
		),
		out:  out,
		errs: errs,
	}
	provider := answers("rayleigh scattering", "the light travels further")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	interrupts := make(chan os.Signal)
	go func() {
		<-errs.prompts // the first prompt: interrupt it, which arms
		interrupts <- os.Interrupt
		<-errs.prompts // and it asked again rather than ending
		close(first)   // a turn, which is what disarms

		<-errs.prompts // the prompt after that turn
		interrupts <- os.Interrupt
		<-errs.prompts // still asking: the turn undid the first interrupt
		close(second)
	}()

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")), interrupts)
	if err != nil {
		t.Fatalf("session() error = %v, want nil", err)
	}

	// Both turns were answered, which two interrupts in a row would have
	// left the second of unasked.
	if got, want := len(provider.threads), 2; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	if got, want := out.String(), "rayleigh scattering\nthe light travels further\n"; got != want {
		t.Errorf("session() printed %q, want %q", got, want)
	}
	if got, want := finished.ID(), "7"; got != want {
		t.Errorf("session().ID() = %q, want %q", got, want)
	}
}

// turning is a provider that works until it does not: it hands each call on to
// the model behind it, and fails once that has run out of replies. It is how a
// case says "the second turn is the one that goes wrong" without counting
// calls from the outside.
type turning struct {
	model *model
	err   error
}

func (t *turning) Invoke(ctx Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	if len(t.model.replies) == 0 {
		t.model.threads = append(t.model.threads, thread)
		return nil, t.err
	}
	return t.model.Invoke(ctx, thread, registry)
}

// waiting is a provider whose first call hangs until the context it was given
// is cancelled, which is what a request somebody is still waiting on looks like
// from in here. It says when it has been entered, so that a case can interrupt
// a turn rather than race one, and every call after that is the model behind
// it. The thread is kept either way: what the interrupted turn was asked is
// half of what a case has to say about the turn after it.
type waiting struct {
	entered chan struct{}
	model   *model
}

func (w *waiting) Invoke(ctx Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	if w.entered == nil {
		return w.model.Invoke(ctx, thread, registry)
	}

	w.model.threads = append(w.model.threads, thread)
	close(w.entered)
	w.entered = nil

	<-ctx.Done()
	return nil, ctx.Err()
}

// held is a reader nothing can be read from until it is released. It is how a
// case says what happened first: the line is not there to be read until the
// interrupt ahead of it has been taken.
type held struct {
	release <-chan struct{}
	reader  io.Reader
}

func (h held) Read(p []byte) (int, error) {
	<-h.release
	return h.reader.Read(p)
}

// announcing is a stream that says when the session wrote a prompt to it, so
// that a case can act between one turn and the next rather than guess at when
// the session got there. What was written is kept as well, since a case that
// waits on the prompts usually has something to say about the rest of it too.
type announcing struct {
	written *bytes.Buffer
	prompts chan struct{}
}

func (a *announcing) Write(p []byte) (int, error) {
	if string(p) == prompt {
		a.prompts <- struct{}{}
	}
	return a.written.Write(p)
}

func (a *announcing) String() string {
	return a.written.String()
}

// stumbling is a provider that fails one turn and answers the rest: the nth
// call it is handed comes back as the error and every other goes to the model
// behind it. It is how a case says "the second turn goes wrong and the third
// is fine", which [turning] cannot, since it only ever fails from some point
// on.
type stumbling struct {
	model *model
	on    int
	err   error
	calls int
}

func (s *stumbling) Invoke(ctx Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	s.calls++
	if s.calls == s.on {
		s.model.threads = append(s.model.threads, thread)
		return nil, s.err
	}
	return s.model.Invoke(ctx, thread, registry)
}

// wedged is a store that cannot write, standing in for the disk going away
// mid-conversation. The failure it produces is the agent's, not this type's:
// what a case built on it is pinning is that a record which is not being kept
// ends the session.
type wedged struct{}

func (wedged) Save(Context, Exchange) (Exchange, error) {
	return Exchange{}, errors.New("there is nowhere to write it")
}

func (wedged) Load(Context, string) (Exchange, error) {
	return Exchange{}, errors.New("not asked for here")
}
