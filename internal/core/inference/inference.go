package inference

import (
	"context"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
)

type (
	Context  = context.Context
	Message  = messages.Type
	Thread   = threads.Type
	Registry = tools.Registry
)

// Provider is the port a model is reached through: one method, one round
// trip, no vendor named. Implementations live in providers and are the only
// code that knows an SDK exists.
type Provider interface {
	// Invoke sends the thread to the model, telling it which tools it may
	// ask for, and returns its replies. An empty registry is legal and
	// means the model is offered none. Neither the replies nor anything
	// they ask for are folded back into the thread: that is the caller's
	// call.
	Invoke(Context, Thread, Registry) ([]Message, error)
}
