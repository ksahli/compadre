// Package threads contains the exchange a provider is asked to continue.
//
// A thread is the standing instructions plus the messages so far, in order.
// It is the whole of what a provider is given: there is no session, no
// handle, no state held on the far side. Every round trip restates the
// exchange from the beginning.
//
// Threads are immutable, and that is the invariant the rest of the design
// leans on. [Type.Append] returns the thread that follows and leaves the
// receiver as it was; [New] copies what it is given and [Type.Messages]
// copies what it hands back. So a thread can be held onto, shared, and
// appended to twice down two different branches, and none of those uses can
// reach into another.
package threads
