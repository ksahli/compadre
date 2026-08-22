// This test is in-package rather than in invoke_test: what New produces is a
// Command whose fields are unexported, and there is no exported surface that
// shows what it parsed short of running the command against a real API.
package invoke

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		instructions string
		input        string
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

// model stands in for a provider. It answers each call with the next reply it
// was given, keeping the threads it was asked to continue so that a test can
// say what the loop sent back; once the replies run out it repeats the last
// one, which is what lets a case ask for an exchange that never ends.
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
		return nil, nil
	}
	reply := m.replies[0]
	if len(m.replies) > 1 {
		m.replies = m.replies[1:]
	}
	return reply, nil
}

func (m *model) calls() int { return len(m.threads) }

// last is the final thread the model was asked to continue: what the loop had
// folded together by the time it stopped.
func (m *model) last() threads.Type { return m.threads[len(m.threads)-1] }

// tool is a tool with its answer handed to it, so that a case can have one
// that works and one that does not without a service behind either.
type tool struct {
	name   string
	answer string
	err    error
}

func (t tool) Name() string        { return t.name }
func (t tool) Description() string { return "a tool" }
func (t tool) Schema() map[string]any {
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

func (t tool) Execute(context.Context, use.Arguments) (string, error) {
	return t.answer, t.err
}

// said renders one content block as a line a case can state, naming the shape
// it turned out to be.
func said(content messages.Content) string {
	if text, ok := content.Text(); ok {
		return "text:" + text
	}
	if request, ok := content.Use(); ok {
		return "use:" + request.ID() + ":" + request.Name()
	}
	if result, ok := content.Result(); ok {
		failed := "ok"
		if result.Failed() {
			failed = "failed"
		}
		return "result:" + result.ID() + ":" + failed + ":" + result.Content()
	}
	return "unknown"
}

// record is every message of a thread, one entry each: the role it took and
// the blocks it carried.
func record(thread threads.Type) []string {
	got := []string{}
	for _, message := range thread.Messages() {
		blocks := []string{}
		for _, content := range message.Content() {
			blocks = append(blocks, said(content))
		}
		got = append(got, message.Role()+"("+strings.Join(blocks, "|")+")")
	}
	return got
}

// says is one assistant turn saying something and nothing else.
func says(what string) []messages.Type {
	return []messages.Type{messages.New(roles.Assistant, messages.Text(what))}
}

// asks is one assistant turn asking for a tool.
func asks(id, name string) []messages.Type {
	return []messages.Type{messages.New(roles.Assistant,
		messages.Use(use.New(id, name, use.Arguments(`{}`))))}
}

func opening() threads.Type {
	return threads.New("", messages.New(roles.User, messages.Text("hello")))
}

func registry() tools.Registry {
	return definitions.New(
		tool{name: "works", answer: "the answer"},
		tool{name: "breaks", err: errors.New("it broke")},
	)
}

func TestConverse(t *testing.T) {
	cases := []struct {
		name    string
		replies [][]messages.Type
		calls   int
		printed string
		thread  []string
	}{
		{
			// Nothing asked for is the end of it: one call, and
			// what was said is what is printed.
			name:    "a reply that asks for nothing ends the exchange",
			replies: [][]messages.Type{says("hola")},
			calls:   1,
			printed: "hola\n",
			thread:  []string{"User(text:hello)", "Assistant(text:hola)"},
		},
		{
			// The call, then its answer as a user turn, then the
			// sentence the model wrote once it had read it.
			name:    "a call is run and answered, and the exchange goes on",
			replies: [][]messages.Type{asks("toolu_1", "works"), says("hola")},
			calls:   2,
			printed: "hola\n",
			thread: []string{
				"User(text:hello)",
				"Assistant(use:toolu_1:works)",
				"User(result:toolu_1:ok:the answer)",
				"Assistant(text:hola)",
			},
		},
		{
			// A tool that fails is a result the model reads, not
			// an error that ends the run.
			name:    "a tool that fails answers with its failure",
			replies: [][]messages.Type{asks("toolu_1", "breaks"), says("hola")},
			calls:   2,
			printed: "hola\n",
			thread: []string{
				"User(text:hello)",
				"Assistant(use:toolu_1:breaks)",
				"User(result:toolu_1:failed:it broke)",
				"Assistant(text:hola)",
			},
		},
		{
			// And so is a name nothing in the registry answers to.
			name:    "a tool nothing answers to is answered anyway",
			replies: [][]messages.Type{asks("toolu_1", "nowhere"), says("hola")},
			calls:   2,
			printed: "hola\n",
			thread: []string{
				"User(text:hello)",
				"Assistant(use:toolu_1:nowhere)",
				"User(result:toolu_1:failed:unknown tool: 'nowhere')",
				"Assistant(text:hola)",
			},
		},
		{
			// Every call of one turn is answered, in the order they
			// were asked for and in one message: the API pairs them
			// by id, and the model is owed all of them at once.
			name: "every call of a turn is answered in one turn",
			replies: [][]messages.Type{
				{messages.New(roles.Assistant,
					messages.Text("let me look"),
					messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`))),
					messages.Use(use.New("toolu_2", "breaks", use.Arguments(`{}`))),
				)},
				says("hola"),
			},
			calls:   2,
			printed: "let me look\nhola\n",
			thread: []string{
				"User(text:hello)",
				"Assistant(text:let me look|use:toolu_1:works|use:toolu_2:breaks)",
				"User(result:toolu_1:ok:the answer|result:toolu_2:failed:it broke)",
				"Assistant(text:hola)",
			},
		},
		{
			// A reply with nothing in it asks for nothing, which
			// is an end like any other rather than a turn to keep
			// going from.
			name:    "a reply that says nothing ends the exchange",
			replies: [][]messages.Type{{}},
			calls:   1,
			printed: "",
			thread:  []string{"User(text:hello)"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider, out := &model{replies: c.replies}, &strings.Builder{}

			if err := converse(context.Background(), provider, registry(), opening(), out); err != nil {
				t.Fatalf("converse() error = %v, want nil", err)
			}

			if provider.calls() != c.calls {
				t.Errorf("provider was asked %d times, want %d", provider.calls(), c.calls)
			}
			if out.String() != c.printed {
				t.Errorf("printed %q, want %q", out.String(), c.printed)
			}

			// The last thread the provider saw plus the reply it
			// answered with is the whole of the exchange.
			whole := provider.last().Append(c.replies[len(c.replies)-1]...)
			if got := record(whole); !slices.Equal(got, c.thread) {
				t.Errorf("thread = %v, want %v", got, c.thread)
			}
		})
	}
}

// TestConverseStopsAtTheCeiling pins the bound. A model that never stops
// asking is stopped, and loudly: an exchange that runs forever spends forever.
func TestConverseStopsAtTheCeiling(t *testing.T) {
	provider, out := &model{replies: [][]messages.Type{asks("toolu_1", "works")}}, &strings.Builder{}

	err := converse(context.Background(), provider, registry(), opening(), out)
	if err == nil {
		t.Fatal("converse() error = nil, want an error")
	}
	if provider.calls() != turns {
		t.Errorf("provider was asked %d times, want %d", provider.calls(), turns)
	}
}

// TestConverseReportsFailure pins the first error ending the run, with
// nothing of that turn printed ahead of it.
func TestConverseReportsFailure(t *testing.T) {
	refused := errors.New("refused")
	provider, out := &model{err: refused}, &strings.Builder{}

	err := converse(context.Background(), provider, registry(), opening(), out)
	if !errors.Is(err, refused) {
		t.Fatalf("converse() error = %v, want %v", err, refused)
	}
	if out.String() != "" {
		t.Errorf("printed %q, want nothing", out.String())
	}
	if provider.calls() != 1 {
		t.Errorf("provider was asked %d times, want 1", provider.calls())
	}
}
