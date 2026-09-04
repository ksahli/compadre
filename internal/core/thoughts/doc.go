// Package thoughts carries the model's own reasoning, as the model handed it
// over.
//
// It is here for one reason: an API that reasons before it answers expects
// that reasoning back with the answer to the tool it asked for. The reasoning
// is not something the core reads — nothing in it looks at a thought — but it
// is something the core has to keep, because a turn that arrives without it is
// a turn the model has to start over from.
//
// So a thought is opaque on purpose. The text may be empty, the signature is
// the API's own proof that the block is the one it wrote, and a redacted
// thought is nothing but a blob. None of it is this package's to interpret;
// all of it is this package's to hold unchanged.
package thoughts
