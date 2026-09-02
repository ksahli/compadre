package exchanges

import (
	"github.com/ksahli/compadre/internal/core/threads"
)

type (
	Thread = threads.Type
)

// Type is a thread together with the id it is filed under. The thread is what
// was said; the id is where it was written down, and the two are kept side by
// side rather than folded into one because only one of them is the exchange
// itself. A thread that has never been stored is still a whole exchange.
//
// Like the thread it carries, an exchange is immutable: [Type.With] returns
// the exchange that follows and leaves the receiver as it was.
type Type struct {
	id     string
	thread Thread
}

// ID is where the exchange is filed. It is empty until a store has written
// the exchange down and said what it called it.
func (exchange Type) ID() string {
	return exchange.id
}

func (exchange Type) Thread() Thread {
	return exchange.thread
}

// With returns the exchange that follows: the same id, and the thread as it
// now stands. It is what an exchange grows by, since the thread it holds
// grows by [github.com/ksahli/compadre/internal/core/threads.Type.Append]
// rather than in place.
func (exchange Type) With(thread Thread) Type {
	return New(exchange.id, thread)
}

// New builds an exchange from the id it is filed under and the thread filed
// there.
func New(id string, thread Thread) Type {
	return Type{id: id, thread: thread}
}

// Open is an exchange that has not been written down yet: a thread and no id.
// A store that is handed one files it and says what it called it.
func Open(thread Thread) Type {
	return New("", thread)
}
