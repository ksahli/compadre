package agents_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/agents"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

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
		thread  []string
	}{
		{
			// Nothing asked for is the end of it: one call, and
			// the exchange is the opening turn and the answer.
			name:    "a reply that asks for nothing ends the exchange",
			replies: [][]messages.Type{says("hola")},
			calls:   1,
			thread:  []string{"User(text:hello)", "Assistant(text:hola)"},
		},
		{
			// The call, then its answer as a user turn, then the
			// sentence the model wrote once it had read it.
			name:    "a call is run and answered, and the exchange goes on",
			replies: [][]messages.Type{asks("toolu_1", "works"), says("hola")},
			calls:   2,
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
			calls: 2,
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
			thread:  []string{"User(text:hello)"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &model{replies: c.replies}

			finished, err := agents.New(provider, registry()).Converse(context.Background(), opening())
			if err != nil {
				t.Fatalf("Converse() error = %v, want nil", err)
			}

			if provider.calls() != c.calls {
				t.Errorf("provider was asked %d times, want %d", provider.calls(), c.calls)
			}
			// The thread that comes back is the whole exchange,
			// which is the reason it comes back at all.
			if got := record(finished); !slices.Equal(got, c.thread) {
				t.Errorf("thread = %v, want %v", got, c.thread)
			}
		})
	}
}

// TestConverseLeavesTheOpeningThreadAlone pins the immutability the rest of
// the design leans on. The exchange is built by appending, and appending
// returns the thread that follows: a caller still holding the one it opened
// with has to find it as it was.
func TestConverseLeavesTheOpeningThreadAlone(t *testing.T) {
	provider, thread := &model{replies: [][]messages.Type{says("hola")}}, opening()

	if _, err := agents.New(provider, registry()).Converse(context.Background(), thread); err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}

	if got, want := record(thread), []string{"User(text:hello)"}; !slices.Equal(got, want) {
		t.Errorf("the opening thread = %v, want %v", got, want)
	}
}

// TestConverseStopsAtTheCeiling pins the bound. A model that never stops
// asking is stopped, and loudly: an exchange that runs forever spends forever.
func TestConverseStopsAtTheCeiling(t *testing.T) {
	provider := &model{replies: [][]messages.Type{asks("toolu_1", "works")}}

	_, err := agents.New(provider, registry()).Converse(context.Background(), opening())
	if err == nil {
		t.Fatal("Converse() error = nil, want an error")
	}
	if provider.calls() != agents.Turns {
		t.Errorf("provider was asked %d times, want %d", provider.calls(), agents.Turns)
	}
}

// TestConverseReportsFailure pins the first error ending the run, with the
// turn it failed on contributing nothing.
func TestConverseReportsFailure(t *testing.T) {
	refused := errors.New("refused")
	provider := &model{err: refused}

	_, err := agents.New(provider, registry()).Converse(context.Background(), opening())
	if !errors.Is(err, refused) {
		t.Fatalf("Converse() error = %v, want %v", err, refused)
	}
	if provider.calls() != 1 {
		t.Errorf("provider was asked %d times, want 1", provider.calls())
	}
}

// TestConverseReturnsWhatItHadWhenItStopped pins the promise the doc makes
// about the pair: the thread comes back on failure too, and is what had
// accumulated by then. It is what lets a caller print a run that failed rather
// than losing everything said before the thing that went wrong.
func TestConverseReturnsWhatItHadWhenItStopped(t *testing.T) {
	t.Run("a provider that refused", func(t *testing.T) {
		provider := &model{err: errors.New("refused")}

		finished, err := agents.New(provider, registry()).Converse(context.Background(), opening())
		if err == nil {
			t.Fatal("Converse() error = nil, want an error")
		}
		if finished == nil {
			t.Fatal("Converse() thread = nil, want the exchange as it stood")
		}
		// Nothing was said before the refusal, so the exchange is the
		// turn that opened it and no more.
		if got, want := record(finished), []string{"User(text:hello)"}; !slices.Equal(got, want) {
			t.Errorf("thread = %v, want %v", got, want)
		}
	})

	t.Run("a model that would not stop", func(t *testing.T) {
		// A turn that says something and asks for a tool, repeated
		// until the ceiling: what comes back has to hold the sentence.
		provider := &model{replies: [][]messages.Type{
			{messages.New(roles.Assistant,
				messages.Text("here we go"),
				messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`))),
			)},
		}}

		finished, err := agents.New(provider, registry()).Converse(context.Background(), opening())
		if err == nil {
			t.Fatal("Converse() error = nil, want an error")
		}
		if finished == nil {
			t.Fatal("Converse() thread = nil, want the exchange as it stood")
		}

		// The opening turn, then a call and its answer for every turn
		// the ceiling allowed.
		if got, want := len(finished.Messages()), 1+2*agents.Turns; got != want {
			t.Errorf("thread holds %d messages, want %d", got, want)
		}
		if got := record(finished); !strings.Contains(strings.Join(got, "\n"), "text:here we go") {
			t.Errorf("thread = %v, want it to hold what was said before the ceiling", got)
		}
	})
}

// TestConverseWithNoTools pins that an empty registry is legal: the model is
// offered nothing, which makes the loop one round trip and an answer.
func TestConverseWithNoTools(t *testing.T) {
	provider := &model{replies: [][]messages.Type{says("hola")}}

	finished, err := agents.New(provider, definitions.New()).Converse(context.Background(), opening())
	if err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}
	if provider.calls() != 1 {
		t.Errorf("provider was asked %d times, want 1", provider.calls())
	}
	if got, want := record(finished), []string{"User(text:hello)", "Assistant(text:hola)"}; !slices.Equal(got, want) {
		t.Errorf("thread = %v, want %v", got, want)
	}
}
