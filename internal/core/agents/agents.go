package agents

import (
	"context"
	"fmt"

	"github.com/ksahli/compadre/internal/core/exchanges"
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

// Turns is the ceiling on one exchange. A model that keeps asking for tools
// would otherwise spend without end, and stopping loudly is better than that.
//
// It is exported because it is a fact about the runtime a caller may need to
// state — a command explaining itself, a test pinning the bound — rather than
// a number this package keeps to itself.
const Turns = 10

// Type is an agent: a model to reach, the tools it may ask for, and where the
// record of what happened is kept. All three are what the agent is, which is
// why they sit on the value; the exchange is what it is asked to do, which is
// why that arrives at [Type.Converse]. The same agent can be asked to run two
// exchanges, and neither can reach the other.
type Type struct {
	provider Provider
	registry Registry
	store    Store
}

// New builds an agent from the model it reaches through, the tools it may
// offer, and the store it keeps the record in. All three are arguments and
// none has a default: which provider answers, which tools are on offer and
// which engine keeps the record are the wiring's call, and an agent built with
// the wrong one is not degraded but pointed somewhere else.
//
// An empty registry is legal and means the model is offered no tools, which
// makes the loop one round trip and an answer.
func New(provider Provider, registry Registry, store Store) Type {
	return Type{provider: provider, registry: registry, store: store}
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
func (a Type) Converse(ctx Context, exchange Exchange) (Exchange, error) {
	// The opening turn is written down before the model is asked anything,
	// so that a run refused on its first round trip still left behind what
	// it was asked to do. It is also what mints the id, which means the
	// exchange has one from here on and every later save writes under it.
	exchange, err := a.save(ctx, exchange)
	if err != nil {
		return exchange, err
	}

	for range Turns {
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

	return exchange, fmt.Errorf("gave up after %d turns: the model kept asking for tools", Turns)
}

// save writes the exchange down and returns it filed. A store that cannot
// write ends the run, unlike a tool that cannot run: a failed tool is a result
// the model reads and tries again from, and a record that is not being kept is
// not something the model can do anything about. The exchange comes back
// either way, because what could not be written is still what was said.
func (a Type) save(ctx Context, exchange Exchange) (Exchange, error) {
	saved, err := a.store.Save(ctx, exchange)
	if err != nil {
		return exchange, fmt.Errorf("could not keep the record of the exchange: %w", err)
	}
	return saved, nil
}
