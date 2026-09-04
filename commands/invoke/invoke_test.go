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
		directory    string
		turns        int
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
			arguments: []string{"-max-tokens", "0", "-max-turns", "0"},
		},
		{
			name:         "all four bounds at once",
			arguments:    []string{"-model", "claude-opus-5", "-max-tokens", "4096", "-workspace", "/tmp", "-max-turns", "25", "-instructions", "be brief"},
			instructions: "be brief",
			model:        "claude-opus-5",
			tokens:       4096,
			directory:    "/tmp",
			turns:        25,
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
			if command.directory != c.directory {
				t.Errorf("directory = %q, want %q", command.directory, c.directory)
			}
			if command.turns != c.turns {
				t.Errorf("turns = %d, want %d", command.turns, c.turns)
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
			for _, want := range []string{"-instructions", "-message", "-exchange", "-store", "-model", "-max-tokens", "-workspace", "-max-turns"} {
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
		// These two parse as numbers and are refused here rather
		// than by the flag set: -3 is a bound nobody could have
		// meant, and running with a different one would be an
		// answer to a question nobody asked.
		{"a ceiling below zero", []string{"-max-tokens", "-3"}},
		{"turns below zero", []string{"-max-turns", "-3"}},
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
				turns = append(turns, message.Role()+": "+text)
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

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("be brief")))
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

	if _, err := command.session(context.Background(), agent, exchanges.Open(threads.New(""))); err != nil {
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

// TestSessionDoesNotSendABlankLine pins the one line that is not a turn. An
// exchange with nothing to carry it on is a request that would come back
// refused, and at a prompt the answer to that is to ask again rather than to
// spend it finding out.
func TestSessionDoesNotSendABlankLine(t *testing.T) {
	command, out, errs := session("\n   \n\t\n")
	provider := answers("rayleigh scattering")
	agent := agents.New(provider, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")))
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

	finished, err := command.session(context.Background(), agent, opening)
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

	finished, err := command.session(ctx, agent, exchanges.Open(threads.New("")))
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

// TestSessionEndsOnATurnThatFailed pins the third way out, which is the one
// that is not quiet. A turn that failed comes back as an error and the session
// stops there rather than asking again over a provider that has stopped
// working — but what the turns before it said is still printed, and the
// exchange still comes back carrying its id, because that is how the caller
// picks the session back up with -exchange.
func TestSessionEndsOnATurnThatFailed(t *testing.T) {
	command, out, _ := session("why is the sky blue?\nand at sunset?\nand at noon?\n")
	// One reply, so the provider answers the first turn and refuses the
	// second.
	failing := &turning{model: answers("rayleigh scattering"), err: errors.New("the model is out")}
	agent := agents.New(failing, definitions.New(), &recorder{id: "7"}, agents.Turns)

	finished, err := command.session(context.Background(), agent, exchanges.Open(threads.New("")))
	if err == nil {
		t.Fatal("session() error = nil, want an error")
	}
	if got, want := err.Error(), "the model is out"; !strings.Contains(got, want) {
		t.Errorf("session() error = %q, want it to name %q", got, want)
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
