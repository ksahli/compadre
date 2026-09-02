// Package exchanges contains a thread together with the id it is filed under.
//
// A thread is what was said, and nothing else: instructions and turns, held
// immutably, with no identity of its own. That is deliberate. Identity is not
// something an exchange has by being an exchange — it is something a store
// gives it by writing it down — and putting an id on
// [github.com/ksahli/compadre/internal/core/threads.Type] would mean every
// thread in the program carried a field that only a database cares about, and
// that most of them would have to leave empty.
//
// So the pairing lives here instead, one level out. A thread stays what it
// was; an exchange is that thread and the answer to where it was filed. The
// code that has no interest in storage goes on handling threads, and only the
// code that does reaches for this.
//
// [Open] is an exchange that has not been written down yet: its id is empty,
// and a store handed one files it and says what it called it. Which is why
// [github.com/ksahli/compadre/internal/core/memory.Store.Save] gives an
// exchange back rather than nothing — the id is the store's to mint, and the
// caller has no way to learn it otherwise.
//
// An exchange is immutable for the same reason the thread it holds is.
// [Type.With] returns the exchange that follows and leaves the receiver as it
// was, so the loop that grows an exchange turn by turn is doing the same thing
// to the pair that it was already doing to the thread.
package exchanges
