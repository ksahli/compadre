// Package messages contains message primitives.
//
// A message is the smallest thing the core knows how to say: the part it
// takes in the exchange, and what it says. What it says is a list of content
// blocks rather than a single value, because one turn is often several things
// at once — a sentence and the tool call it leads to, or every result a round
// of calls produced. [Text], [Thinking], [Use] and [Result] are the shapes a
// block comes in, and the set is closed: only this package can widen it.
//
// [Thinking] is the odd one: nothing in the core reads a thought, and nothing
// in the core ever will. It is carried because the API that produced it wants
// it back — a turn handed on without its reasoning is a turn the model has to
// begin again — so the core keeps it whole and hands it on unread.
//
// A reply arrives whole rather than as it is produced. [New] is the only way
// to build a message, and it does not constrain the role — the constants in
// [github.com/ksahli/compadre/internal/core/roles] are what the core spells
// its own roles with, not a closed set.
//
// Content is read by asking, not by switching: each accessor answers whether
// the block is that shape, so a block of the wrong kind cannot be mistaken
// for an empty one.
package messages
