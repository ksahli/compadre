// Package inference contains the port a model is reached through.
//
// This is the one interface the core uses to reach a model, and the reason
// no vendor is named anywhere inside the core. Dependencies point one way:
// providers know core, never the reverse. An adapter satisfying [Provider]
// is the only code that knows an SDK exists.
//
// The port is deliberately narrow. Give it a thread and the tools the model
// may ask for, get the replies back whole: one round trip, no loop, and the
// replies are not folded back into the thread — that is the caller's call.
// What a provider does with the thread's instructions is the provider's
// business; each API wants them somewhere different.
//
// The registry is an argument rather than something a provider is built with,
// because what a model may reach for belongs to the exchange, not to the
// adapter: the same provider can be asked one question with tools and the
// next without. Turning a definition's schema into whatever shape an API
// wants is the adapter's work, like every other mapping it does.
//
// Replies come back as a slice because one response carries several content
// blocks. A reply asking for a tool is one of them, so the caller can see it
// as [github.com/ksahli/compadre/internal/core/messages.Content] and needs no
// stop reason to know the model is not finished. What the port still does not
// carry: stop reason, token usage, and anything else that would make a reply
// more than what was said. Adding any of them means widening [Message], which
// is a change to the vocabulary, not to an adapter.
//
// Foreign types are re-exported here as aliases (Context, Message, Thread,
// Registry) so that code depending on the port need not import them itself.
// That idiom recurs throughout the tree.
package inference
