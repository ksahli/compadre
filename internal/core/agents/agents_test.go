package agents_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/agents"
	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/failures"
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

// recorder stands in for a store. It keeps a rendering of every exchange it
// was asked to write, so that a case can say what the record looked like at
// each point rather than only at the end, and it files the first exchange it
// sees under an id of its own so that carrying that id forward can be pinned.
// It can be told to fail on the nth write.
type recorder struct {
	id     string
	fail   error
	failOn int
	saves  []string
	filed  []string
	live   []bool // whether the context each write arrived on was still live
}

func (r *recorder) Save(ctx context.Context, exchange exchanges.Type) (exchanges.Type, error) {
	r.filed = append(r.filed, exchange.ID())
	r.saves = append(r.saves, strings.Join(record(exchange.Thread()), " "))
	r.live = append(r.live, ctx.Err() == nil)

	if r.fail != nil && len(r.saves) >= r.failOn {
		return exchange, r.fail
	}

	if exchange.ID() == "" {
		exchange = exchanges.New(r.id, exchange.Thread())
	}
	return exchange, nil
}

func (r *recorder) Load(context.Context, string) (exchanges.Type, error) {
	return exchanges.Type{}, errors.New("not asked for here")
}

func (r *recorder) writes() int { return len(r.saves) }

// store is a recorder that files what it is given under "7".
func store() *recorder { return &recorder{id: "7"} }

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
		got = append(got, message.Role().String()+"("+strings.Join(blocks, "|")+")")
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

// unfiled is the exchange a caller opens with: a thread and no id yet.
func unfiled() exchanges.Type {
	return exchanges.Open(opening())
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
			provider, kept := &model{replies: c.replies}, store()

			finished, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled())
			if err != nil {
				t.Fatalf("Converse() error = %v, want nil", err)
			}

			if provider.calls() != c.calls {
				t.Errorf("provider was asked %d times, want %d", provider.calls(), c.calls)
			}
			// The thread that comes back is the whole exchange,
			// which is the reason it comes back at all.
			if got := record(finished.Thread()); !slices.Equal(got, c.thread) {
				t.Errorf("thread = %v, want %v", got, c.thread)
			}
			// And what was written down last is the same thing:
			// the record the run leaves behind is the run.
			if got, want := kept.saves[len(kept.saves)-1], strings.Join(c.thread, " "); got != want {
				t.Errorf("last write = %q, want %q", got, want)
			}
		})
	}
}

// TestConverseLeavesTheOpeningThreadAlone pins the immutability the rest of
// the design leans on. The exchange is built by appending, and appending
// returns the thread that follows: a caller still holding the one it opened
// with has to find it as it was.
func TestConverseLeavesTheOpeningThreadAlone(t *testing.T) {
	provider, opened := &model{replies: [][]messages.Type{says("hola")}}, unfiled()

	if _, err := agents.New(provider, registry(), store(), agents.Turns).Converse(context.Background(), opened); err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}

	if got, want := record(opened.Thread()), []string{"User(text:hello)"}; !slices.Equal(got, want) {
		t.Errorf("the opening thread = %v, want %v", got, want)
	}
	// The id the store gave the exchange is on what came back, not on
	// what the caller still holds.
	if got := opened.ID(); got != "" {
		t.Errorf("the opening exchange was filed under %q, want it left unfiled", got)
	}
}

// TestConverseStopsAtTheCeiling pins the bound. A model that never stops
// asking is stopped, and loudly: an exchange that runs forever spends forever.
//
// The bound is the one the agent was built with, so the cases are the shapes a
// caller can build one with: a ceiling of its own, the default named, and a
// number that is no ceiling at all — which falls back rather than ending the
// run before its first round trip, since [agents.New] has nowhere to report an
// error to and a silent no-op would be the worse answer.
func TestConverseStopsAtTheCeiling(t *testing.T) {
	cases := []struct {
		name  string
		turns int
		asked int
	}{
		{name: "the ceiling the caller named", turns: 2, asked: 2},
		{name: "the ceiling the package names", turns: agents.Turns, asked: agents.Turns},
		{name: "a ceiling that is none", turns: 0, asked: agents.Turns},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &model{replies: [][]messages.Type{asks("toolu_1", "works")}}

			_, err := agents.New(provider, registry(), store(), c.turns).Converse(context.Background(), unfiled())
			if err == nil {
				t.Fatal("Converse() error = nil, want an error")
			}
			// The number is in the report because a caller who
			// moved the bound is owed the bound they moved it to.
			if want := fmt.Sprintf("%d turns", c.asked); !strings.Contains(err.Error(), want) {
				t.Errorf("Converse() error = %q, want it to mention %q", err, want)
			}
			if provider.calls() != c.asked {
				t.Errorf("provider was asked %d times, want %d", provider.calls(), c.asked)
			}
			// And it is not settled. A run that spent its turns on
			// tools says nothing about the next question, which a
			// shorter one may well finish inside the same bound.
			if errors.Is(err, failures.ErrSettled) {
				t.Errorf("Converse() error = %v, want it not to be settled", err)
			}
		})
	}
}

// TestConverseReportsFailure pins the first error ending the run, with the
// turn it failed on contributing nothing.
func TestConverseReportsFailure(t *testing.T) {
	refused := errors.New("refused")
	provider := &model{err: refused}

	_, err := agents.New(provider, registry(), store(), agents.Turns).Converse(context.Background(), unfiled())
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
		provider, kept := &model{err: errors.New("refused")}, store()

		finished, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled())
		if err == nil {
			t.Fatal("Converse() error = nil, want an error")
		}
		// Nothing was said before the refusal, so the exchange is the
		// turn that opened it and no more — and that turn was written
		// down before the model was asked, so the record holds it too.
		if got, want := record(finished.Thread()), []string{"User(text:hello)"}; !slices.Equal(got, want) {
			t.Errorf("thread = %v, want %v", got, want)
		}
		if got := finished.ID(); got != "7" {
			t.Errorf("Type.ID() = %q, want %q", got, "7")
		}
		if got, want := kept.saves, []string{"User(text:hello)"}; !slices.Equal(got, want) {
			t.Errorf("writes = %v, want %v", got, want)
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

		finished, err := agents.New(provider, registry(), store(), agents.Turns).Converse(context.Background(), unfiled())
		if err == nil {
			t.Fatal("Converse() error = nil, want an error")
		}

		// The opening turn, then a call and its answer for every turn
		// the ceiling allowed.
		if got, want := len(finished.Thread().Messages()), 1+2*agents.Turns; got != want {
			t.Errorf("thread holds %d messages, want %d", got, want)
		}
		if got := record(finished.Thread()); !strings.Contains(strings.Join(got, "\n"), "text:here we go") {
			t.Errorf("thread = %v, want it to hold what was said before the ceiling", got)
		}
	})
}

// TestConverseWithNoTools pins that an empty registry is legal: the model is
// offered nothing, which makes the loop one round trip and an answer.
func TestConverseWithNoTools(t *testing.T) {
	provider := &model{replies: [][]messages.Type{says("hola")}}

	finished, err := agents.New(provider, definitions.New(), store(), agents.Turns).Converse(context.Background(), unfiled())
	if err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}
	if provider.calls() != 1 {
		t.Errorf("provider was asked %d times, want 1", provider.calls())
	}
	if got, want := record(finished.Thread()), []string{"User(text:hello)", "Assistant(text:hola)"}; !slices.Equal(got, want) {
		t.Errorf("thread = %v, want %v", got, want)
	}
}

// TestConverseWritesTheExchangeDownAsItHappens pins where the writes fall. The
// point of writing each turn rather than the whole run at the end is that the
// record is true at every moment, so what is pinned here is the sequence: the
// opening turn before the model is asked anything, then the model's turn, then
// the answers to it — never a result on disk with no call ahead of it.
func TestConverseWritesTheExchangeDownAsItHappens(t *testing.T) {
	provider, kept := &model{replies: [][]messages.Type{
		asks("toolu_1", "works"),
		says("hola"),
	}}, store()

	if _, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled()); err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}

	want := []string{
		"User(text:hello)",
		"User(text:hello) Assistant(use:toolu_1:works)",
		"User(text:hello) Assistant(use:toolu_1:works) User(result:toolu_1:ok:the answer)",
		"User(text:hello) Assistant(use:toolu_1:works) User(result:toolu_1:ok:the answer) Assistant(text:hola)",
	}
	if got := kept.saves; !slices.Equal(got, want) {
		t.Errorf("writes = %v, want %v", got, want)
	}
}

// TestConverseCarriesTheIDTheStoreGaveIt pins the point of Save handing an
// exchange back. The opening write is what mints the id, and every write after
// it is filed under that one rather than opening a second exchange.
func TestConverseCarriesTheIDTheStoreGaveIt(t *testing.T) {
	provider, kept := &model{replies: [][]messages.Type{
		asks("toolu_1", "works"),
		says("hola"),
	}}, store()

	finished, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled())
	if err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}

	// The first write arrived unfiled, and every one after it under the id
	// the store answered with.
	if got, want := kept.filed, []string{"", "7", "7", "7"}; !slices.Equal(got, want) {
		t.Errorf("ids written under = %v, want %v", got, want)
	}
	if got := finished.ID(); got != "7" {
		t.Errorf("Type.ID() = %q, want %q", got, "7")
	}
}

// TestConverseStopsWhenTheRecordCannotBeKept pins the one failure treated
// unlike a tool's. A tool that failed is a result the model recovers from; a
// record that is not being kept is not something it can do anything about, so
// the run ends rather than spending on turns nobody will read back. What was
// said still comes back, because it was still said.
func TestConverseStopsWhenTheRecordCannotBeKept(t *testing.T) {
	full := errors.New("the disk is full")

	t.Run("on the opening write", func(t *testing.T) {
		provider := &model{replies: [][]messages.Type{says("hola")}}
		kept := &recorder{id: "7", fail: full, failOn: 1}

		finished, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled())
		if !errors.Is(err, full) {
			t.Fatalf("Converse() error = %v, want %v", err, full)
		}
		// And it is settled: a caller driving turns has nothing to
		// gain by asking for another one over a store that cannot
		// write, since the turns it took would go nowhere.
		if !errors.Is(err, failures.ErrSettled) {
			t.Errorf("Converse() error = %v, want it to be settled", err)
		}
		// The model was never asked: there was nowhere to put the
		// answer before there was a question to spend on.
		if provider.calls() != 0 {
			t.Errorf("provider was asked %d times, want 0", provider.calls())
		}
		if got, want := record(finished.Thread()), []string{"User(text:hello)"}; !slices.Equal(got, want) {
			t.Errorf("thread = %v, want %v", got, want)
		}
	})

	t.Run("part way through", func(t *testing.T) {
		provider := &model{replies: [][]messages.Type{
			asks("toolu_1", "works"),
			says("hola"),
		}}
		kept := &recorder{id: "7", fail: full, failOn: 2}

		finished, err := agents.New(provider, registry(), kept, agents.Turns).Converse(context.Background(), unfiled())
		if !errors.Is(err, full) {
			t.Fatalf("Converse() error = %v, want %v", err, full)
		}
		if !errors.Is(err, failures.ErrSettled) {
			t.Errorf("Converse() error = %v, want it to be settled", err)
		}
		if kept.writes() != 2 {
			t.Errorf("the store was asked to write %d times, want 2", kept.writes())
		}
		// The turn that could not be written is still what was said,
		// and comes back with the rest of it.
		want := []string{"User(text:hello)", "Assistant(use:toolu_1:works)"}
		if got := record(finished.Thread()); !slices.Equal(got, want) {
			t.Errorf("thread = %v, want %v", got, want)
		}
	})
}

// hanging is the exchange a run interrupted mid-turn leaves behind: the
// model's turn is written down, because the loop has to write it before it can
// answer what it asks for, and then nothing answered it.
func hanging() exchanges.Type {
	return exchanges.New("7", opening().Append(
		messages.New(roles.Assistant, messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`)))),
	))
}

// TestConverseAnswersACallLeftHanging pins the repair. An exchange whose last
// turn is a call nobody answered is not one a provider will take, so it would
// be filed under an id that reads back and never carries on. The call is
// answered — as a failure, because that is what became of it — before the
// model is asked anything.
func TestConverseAnswersACallLeftHanging(t *testing.T) {
	provider := &model{replies: [][]messages.Type{says("picking up where we left off")}}
	recorder := store()

	agent := agents.New(provider, registry(), recorder, agents.Turns)
	finished, err := agent.Converse(context.Background(), hanging())
	if err != nil {
		t.Fatalf("Converse() error = %v, want nil", err)
	}

	// The model is handed the answer, not the question on its own.
	if got, want := provider.calls(), 1; got != want {
		t.Fatalf("the model was asked %d times, want %d", got, want)
	}
	want := []string{
		"User(text:hello)",
		"Assistant(use:toolu_1:works)",
		"User(result:toolu_1:failed:the tool did not run: the exchange stopped before it could be answered)",
	}
	if got := record(provider.threads[0]); !slices.Equal(got, want) {
		t.Errorf("the model was handed %v, want %v", got, want)
	}

	// And the repair is written down rather than only held: an exchange
	// picked up broken is filed mended.
	if len(recorder.saves) == 0 {
		t.Fatal("the store was not written to at all")
	}
	if got, want := recorder.saves[0], strings.Join(want, " "); got != want {
		t.Errorf("the first write was %q, want %q", got, want)
	}

	// The tool itself is not run. What it would have answered is not what
	// happened, and the model is the one that decides whether to ask again.
	if got := record(finished.Thread()); slices.Contains(got, "User(result:toolu_1:ok:the answer)") {
		t.Errorf("the hanging call was run rather than refused: %v", got)
	}
}

// TestConverseLeavesAFinishedExchangeAlone pins the other side of the repair:
// it is for the one shape that cannot be continued, and nothing else. An
// exchange that ended on an answer, and one whose calls were all answered, are
// both carried on as they are.
func TestConverseLeavesAFinishedExchangeAlone(t *testing.T) {
	cases := []struct {
		name   string
		thread threads.Type
	}{
		{
			name:   "one that ended on what the model said",
			thread: opening().Append(says("rayleigh scattering")...),
		},
		{
			name: "one whose call was answered",
			thread: opening().Append(
				messages.New(roles.Assistant, messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`)))),
				messages.New(roles.User, messages.Result(tools.Success("toolu_1", "the answer"))),
			),
		},
		{
			name:   "one with nothing said in it yet",
			thread: threads.New("be brief"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			provider := &model{replies: [][]messages.Type{says("carrying on")}}
			agent := agents.New(provider, registry(), store(), agents.Turns)

			if _, err := agent.Converse(context.Background(), exchanges.New("7", c.thread)); err != nil {
				t.Fatalf("Converse() error = %v, want nil", err)
			}
			if got, want := provider.calls(), 1; got != want {
				t.Fatalf("the model was asked %d times, want %d", got, want)
			}
			want := record(c.thread)
			if got := record(provider.threads[0]); !slices.Equal(got, want) {
				t.Errorf("the model was handed %v, want %v untouched", got, want)
			}
		})
	}
}

// cancelling is a provider that pulls the context out from under the run the
// way an interrupt does: it answers its first call and cancels as it returns,
// so the tools that follow and the write that records them happen on a context
// that is already done.
type cancelling struct {
	model  *model
	cancel context.CancelFunc
}

func (c *cancelling) Invoke(ctx context.Context, thread threads.Type, registry tools.Registry) ([]messages.Type, error) {
	if err := ctx.Err(); err != nil {
		c.model.threads = append(c.model.threads, thread)
		return nil, err
	}
	replies, err := c.model.Invoke(ctx, thread, registry)
	c.cancel()
	return replies, err
}

// TestConverseWritesTheRecordThoughTheRunWasCancelled is the bug this repair
// is the other half of. The loop writes the model's turn down before it runs
// what that turn asked for; an interrupt landing in between used to fail the
// write that answers it, leaving a record ending in a call nothing replied to
// — the one shape that cannot be picked back up. The writes outlive the
// cancellation, so what is left behind is whole.
func TestConverseWritesTheRecordThoughTheRunWasCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	provider := &cancelling{
		model:  &model{replies: [][]messages.Type{asks("toolu_1", "works")}},
		cancel: cancel,
	}
	recorder := store()

	agent := agents.New(provider, registry(), recorder, agents.Turns)
	finished, err := agent.Converse(ctx, unfiled())

	// The run ends, because the caller asked it to.
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Converse() error = %v, want it to be context.Canceled", err)
	}

	// But the record is whole: the call and the answer to it, not a call
	// on its own.
	want := []string{
		"User(text:hello)",
		"Assistant(use:toolu_1:works)",
		"User(result:toolu_1:ok:the answer)",
	}
	if got := record(finished.Thread()); !slices.Equal(got, want) {
		t.Errorf("Converse() came back with %v, want %v", got, want)
	}
	if got, want := recorder.saves[len(recorder.saves)-1], strings.Join(want, " "); got != want {
		t.Errorf("the last write was %q, want %q", got, want)
	}
	if slices.Contains(recorder.live, false) {
		t.Error("a write arrived on a context that was already done, and would have failed against a real store")
	}
}
