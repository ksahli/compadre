// Package messages contains message primitives.
//
// A message is the smallest thing the core knows how to say: the part it
// takes in the exchange, and what it says. What it says is a list of content
// blocks rather than a single value, because one turn is often several things
// at once — a sentence and the tool call it leads to, or every result a round
// of calls produced. [Text], [Use] and [Result] are the shapes a block comes
// in, and the set is closed: only this package can widen it.
//
// What is not among them is anything a particular API happens to put in a
// turn. A model's own reasoning is the case in point: it is the vocabulary of
// the API that produced it, not of the core, so it does not become a shape
// here. An adapter that reads one drops it, the way it drops everything else
// the port has no shape for.
//
// What a turn cost is the other side of that line and does cross it. Every
// model worth reaching is metered the same way — so much read, so much written
// — so a token count is a fact about the exchange rather than one vendor's
// field, and it rides on the message as [Type.Usage] rather than becoming a
// content block, because it is not something anybody said. A turn nobody
// counted carries the zero count and says so. See
// [github.com/ksahli/compadre/internal/core/usage].
//
// A reply arrives whole rather than as it is produced. [New] is the only way
// to build a message, and the role it takes is a closed set — the values in
// [github.com/ksahli/compadre/internal/core/roles] are the whole of what a
// turn can be taken by, and no caller can spell another.
//
// Content is read by asking, not by switching: each accessor answers whether
// the block is that shape, so a block of the wrong kind cannot be mistaken
// for an empty one.
package messages
