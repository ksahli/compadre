package agents

import (
	"context"
	"errors"
	"fmt"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/failures"
	"github.com/ksahli/compadre/internal/core/inference"
	"github.com/ksahli/compadre/internal/core/memory"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/tools"
)

type (
	Context  = context.Context
	Message  = messages.Type
	Exchange = exchanges.Type
	Provider = inference.Provider
	Registry = tools.Registry
	Store    = memory.Store
)

// Turns is the ceiling on one exchange a caller names when it has no reason
// to name another. A model that keeps asking for tools would otherwise spend
// without end, and stopping loudly is better than that.
//
// It is exported because it is a fact about the runtime a caller may need to
// state — a command explaining itself, a test pinning the bound — rather than
// a number this package keeps to itself. The ceiling an agent actually runs to
// is the one it was built with; this is only the number to reach for.
const Turns = 10

// errUnanswered is what a call left hanging is answered with. It is written
// the way any tool that could not run is written, because that is what
// happened: the tool did not run, and the model is the one that has to decide
// what to do about it.
var errUnanswered = errors.New("the tool did not run: the exchange stopped before it could be answered")

// Type is an agent: a model to reach, the tools it may ask for, and where the
// record of what happened is kept. All three are what the agent is, which is
// why they sit on the value; the exchange is what it is asked to do, which is
// why that arrives at [Type.Converse]. The same agent can be asked to run two
// exchanges, and neither can reach the other.
type Type struct {
	provider Provider
	registry Registry
	store    Store
	turns    int
}

// New builds an agent from the model it reaches through, the tools it may
// offer, the store it keeps the record in, and the ceiling on the turns it
// will run. All four are arguments and none has a default: which provider
// answers, which tools are on offer, which engine keeps the record and how
// long a run may go on are the wiring's call, and an agent built with the
// wrong one is not degraded but pointed somewhere else.
//
// A ceiling of zero or less falls back to [Turns] rather than being refused.
// This returns a value and has nowhere to report an error to, and an agent
// that would end every exchange before its first round trip is a worse answer
// than the number a caller who named none would have meant.
//
// An empty registry is legal and means the model is offered no tools, which
// makes the loop one round trip and an answer.
func New(provider Provider, registry Registry, store Store, turns int) Type {
	if turns <= 0 {
		turns = Turns
	}

	return Type{provider: provider, registry: registry, store: store, turns: turns}
}

// Converse runs the exchange to its end and returns it as it ended. Each turn
// the thread goes out, the replies are folded back in and written down,
// anything the model asked for is run and answered in one message and that is
// written down too, and round again. The model asking for nothing is the end
// of it.
//
// The exchange that comes back is the whole of it, and it comes back whether
// the run ended well or badly — a caller that wants to print what was said
// before reporting a failure needs the turns that got that far, and a caller
// continuing an exchange needs the id it was filed under. So this is a (value,
// error) pair that means something other than the usual: the exchange is never
// zero and is always what had accumulated when the loop stopped. The error
// says whether it stopped because there was nothing left to ask.
//
// The first error ends the run, and the turn it failed on contributes nothing:
// a reply that failed is not a reply.
//
// An exchange handed over with a call nobody answered — the shape a run that
// was interrupted mid-turn leaves behind — is answered before anything else
// happens. See [repaired].
func (a Type) Converse(ctx Context, exchange Exchange) (Exchange, error) {
	// The opening turn is written down before the model is asked anything,
	// so that a run refused on its first round trip still left behind what
	// it was asked to do. It is also what mints the id, which means the
	// exchange has one from here on and every later save writes under it.
	//
	// An exchange arriving with a call left unanswered is answered first,
	// so that the repair goes down with it: what was picked up broken is
	// filed mended, and is not broken again the next time it is read back.
	exchange, err := a.save(ctx, repaired(exchange))
	if err != nil {
		return exchange, err
	}

	for range a.turns {
		thread := exchange.Thread()

		replies, err := a.provider.Invoke(ctx, thread, a.registry)
		if err != nil {
			return exchange, err
		}

		requests := []tools.Use{}
		for _, reply := range replies {
			for _, content := range reply.Content() {
				if use, ok := content.Use(); ok {
					requests = append(requests, use)
				}
			}
		}

		// The model's turn has to be in the record before the
		// answers to it are: a result with no call ahead of it is
		// not something the model can read. That holds for the
		// record on disk for the same reason it holds for the one
		// in memory, so the save happens here rather than once the
		// answers have been folded in too.
		exchange, err = a.save(ctx, exchange.With(thread.Append(replies...)))
		if err != nil {
			return exchange, err
		}

		if len(requests) == 0 {
			return exchange, nil
		}

		answers := []messages.Content{}
		for _, request := range requests {
			// Invoke does not fail: a tool that did is a result
			// the model reads and recovers from.
			result := tools.Invoke(ctx, request, a.registry)
			answers = append(answers, messages.Result(result))
		}

		answered := exchange.Thread().Append(messages.New(roles.User, answers...))
		exchange, err = a.save(ctx, exchange.With(answered))
		if err != nil {
			return exchange, err
		}
	}

	return exchange, fmt.Errorf("gave up after %d turns: the model kept asking for tools", a.turns)
}

// repaired is the exchange with any call left unanswered answered.
//
// The loop writes the model's turn down before it runs what that turn asked
// for, and it has to: a result with no call ahead of it is not something the
// model can read. The cost of that ordering is a window, and a run that ends
// inside it — killed, or over a store that failed on the second write — leaves
// a record ending in a call nothing replied to.
//
// That is not a thread anything can be asked to continue. A provider is
// entitled to refuse a turn whose call was never answered, and the ones worth
// reaching do, so the exchange would be filed under an id that can be read
// back and never carried on. Answering the calls here is what makes every
// exchange in the record resumable, including the ones written before there
// was anything to repair them.
//
// Only the last turn is looked at, because it is the only place a call can be
// left hanging: the loop answers every call in the turn it was made in before
// it goes round again.
func repaired(exchange Exchange) Exchange {
	thread := exchange.Thread()

	conversation := thread.Messages()
	if len(conversation) == 0 {
		return exchange
	}

	last := conversation[len(conversation)-1]
	if last.Role() != roles.Assistant {
		return exchange
	}

	answers := []messages.Content{}
	for _, content := range last.Content() {
		if request, ok := content.Use(); ok {
			answers = append(answers, messages.Result(tools.Failure(request.ID(), errUnanswered)))
		}
	}
	if len(answers) == 0 {
		return exchange
	}

	return exchange.With(thread.Append(messages.New(roles.User, answers...)))
}

// save writes the exchange down and returns it filed. A store that cannot
// write ends the run, unlike a tool that cannot run: a failed tool is a result
// the model reads and tries again from, and a record that is not being kept is
// not something the model can do anything about. The exchange comes back
// either way, because what could not be written is still what was said.
//
// The write outlives a cancelled run, which is what the context is stripped of
// its cancellation for. An interrupt landing mid-turn would otherwise fail the
// write that was about to record the turn just paid for, and — worse — the one
// that answers a call already written down, leaving behind a record that ends
// in a question nothing replies to. Cancelling is how a caller says it no
// longer wants the answer; it is not how it says the exchange never happened.
//
// What that costs is a turn that cannot be interrupted while the store is
// wedged. The store is a local database with a busy timeout on it, so the wait
// is bounded, and a bounded wait is worth a record that is always whole.
//
// The failure is marked [failures.ErrSettled], which is how "ends the run"
// reaches a caller that is driving turns rather than running one. A session
// that came back to the prompt from this would go on answering while nothing
// was being written down, which is a session quietly losing the turns it is
// still taking. The ceiling at the end of [Type.Converse] is deliberately not
// marked: a shorter question may well finish inside it.
func (a Type) save(ctx Context, exchange Exchange) (Exchange, error) {
	saved, err := a.store.Save(context.WithoutCancel(ctx), exchange)
	if err != nil {
		return exchange, fmt.Errorf("could not keep the record of the exchange: %w (%w)", err, failures.ErrSettled)
	}
	return saved, nil
}
