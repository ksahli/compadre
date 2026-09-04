// Package anthropic adapts the inference port to the Anthropic Messages
// API. It is one of the two places that know an SDK exists, the other being
// whatever wiring picks it.
//
// The mapping runs both ways. Outbound, a thread becomes request
// parameters: core roles become SDK messages, and the thread's instructions
// become the system prompt, which is where this API wants them. Inbound,
// each of the response's content blocks becomes an assistant message: text
// as itself, a tool call as the argument JSON the model sent, which holds
// the call until the port carries tool use as something other than text.
// Everything the port has no shape for — usage among it — is dropped on the
// way through.
//
// The model's own reasoning is not among that. No thinking parameter is sent,
// which on the models this adapter reaches is what leaves the model reasoning,
// and the API expects the blocks it reasoned in handed back with the answer to
// the tool they asked for. So they are carried: read into
// [github.com/ksahli/compadre/internal/core/messages.Thinking], written down
// with the turn, and sent again unread and unedited, because a thought this
// package rewrote is one the API refuses and a thought it dropped is one the
// model has to think again.
//
// What is not dropped is a turn that never became an answer. A status code
// that says the API would not take the request, and a stop reason that says
// the model stopped short of finishing one, are both this vendor accounting
// for itself in words the core does not have and should not learn. They are
// read here and reported as one of this package's own errors, so that what
// reaches the person at the terminal is what became of their run rather than
// a dump of the exchange that failed.
//
// The model and the token ceiling are the adapter's, not the thread's: they
// arrive at [New] from the wiring, which is what lets the command line choose
// them without the core acquiring a word for either. An adapter built without
// an opinion on them falls back to [Model] and [Tokens].
package anthropic
