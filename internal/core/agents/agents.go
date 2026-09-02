package agents

import (
	"context"
	"fmt"

	"github.com/ksahli/compadre/internal/core/inference"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
)

type (
	Context  = context.Context
	Message  = messages.Type
	Thread   = threads.Type
	Provider = inference.Provider
	Registry = tools.Registry
)

// Turns is the ceiling on one exchange. A model that keeps asking for tools
// would otherwise spend without end, and stopping loudly is better than that.
//
// It is exported because it is a fact about the runtime a caller may need to
// state — a command explaining itself, a test pinning the bound — rather than
// a number this package keeps to itself.
const Turns = 10

// Type is an agent: a model to reach, and the tools it may ask for. Both are
// what the agent is, which is why they sit on the value; the thread is what it
// is asked to do, which is why that arrives at [Type.Converse]. The same agent
// can be asked to run two exchanges, and neither can reach the other.
type Type struct {
	provider Provider
	registry Registry
}

// New builds an agent from the model it reaches through and the tools it may
// offer. Both are arguments and neither has a default: which provider answers
// and which tools are on offer is the wiring's call, and an agent built with
// the wrong one is not degraded but pointed somewhere else.
//
// An empty registry is legal and means the model is offered no tools, which
// makes the loop one round trip and an answer.
func New(provider Provider, registry Registry) Type {
	return Type{provider: provider, registry: registry}
}

// Converse runs the exchange to its end and returns it as it ended. Each turn
// the thread goes out, the replies are folded back in, anything the model
// asked for is run and answered in one message, and round again. The model
// asking for nothing is the end of it.
//
// The thread that comes back is the whole exchange, and it comes back whether
// the run ended well or badly — a caller that wants to print what was said
// before reporting a failure needs the turns that got that far, and a caller
// continuing an exchange needs the record of it. So this is a (value, error)
// pair that means something other than the usual: the thread is never nil and
// is always what had accumulated when the loop stopped. The error says whether
// it stopped because there was nothing left to ask.
//
// The first error ends the run, and the turn it failed on contributes nothing:
// a reply that failed is not a reply.
func (a Type) Converse(ctx Context, thread Thread) (Thread, error) {
	for range Turns {
		replies, err := a.provider.Invoke(ctx, thread, a.registry)
		if err != nil {
			return thread, err
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
		// not something the model can read.
		thread = thread.Append(replies...)

		if len(requests) == 0 {
			return thread, nil
		}

		answers := []messages.Content{}
		for _, request := range requests {
			// Invoke does not fail: a tool that did is a result
			// the model reads and recovers from.
			result := tools.Invoke(ctx, request, a.registry)
			answers = append(answers, messages.Result(result))
		}
		thread = thread.Append(messages.New(roles.User, answers...))
	}

	return thread, fmt.Errorf("gave up after %d turns: the model kept asking for tools", Turns)
}
