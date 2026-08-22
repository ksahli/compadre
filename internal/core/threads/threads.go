package threads

import (
	"slices"

	"github.com/ksahli/compadre/internal/core/messages"
)

type (
	Message = messages.Type
)

// Type is the exchange a provider is asked to continue: standing
// instructions, and the messages so far in order. It is the whole of what a
// provider is given, and it is immutable — every method that would change
// it returns the thread that follows instead.
type Type interface {
	// Instructions are the standing instructions for the exchange. They
	// are not a turn in it, so they sit here rather than in a message,
	// and each provider places them where its own API wants them.
	Instructions() string
	// Messages are the turns so far, oldest first. The slice is the
	// caller's to scribble on: what comes back is a copy.
	Messages() []Message
	Append(...Message) Type
}

type thread struct {
	instructions string
	messages     []Message
}

func (t thread) Instructions() string {
	return t.instructions
}

func (t thread) Messages() []Message {
	return slices.Clone(t.messages)
}

// Append folds the messages into the exchange and returns the thread that
// follows. The receiver is left as it was: threads are immutable.
func (t thread) Append(conversation ...Message) Type {
	return New(t.instructions, append(slices.Clone(t.messages), conversation...)...)
}

// New builds a thread from its instructions and the turns so far. The
// conversation is copied, so a caller that keeps hold of the slice it passed
// in cannot reach back into the thread with it.
func New(instructions string, conversation ...Message) Type {
	return thread{
		instructions: instructions,
		messages:     slices.Clone(conversation),
	}
}
