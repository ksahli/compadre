// Package anthropic adapts the inference port to the Anthropic Messages
// API. It is one of the two places that know an SDK exists, the other being
// whatever wiring picks it.
//
// The mapping runs both ways. Outbound, a thread becomes request
// parameters: core roles become SDK messages, and the thread's instructions
// become the system prompt, which is where this API wants them. Inbound,
// each of the response's content blocks becomes an assistant message: text
// as itself, a tool call as the argument JSON the model sent, which holds
// the call until the port carries tool use as something other than text, and
// what the round trip was metered at as the count on that message. Everything
// the port has no shape for is dropped on the way through.
//
// The model's own reasoning is among what is dropped, and deliberately. No
// thinking parameter is sent, which on the models this adapter reaches is what
// leaves the model reasoning, so the blocks it reasoned in do arrive. They are
// this API's own vocabulary and not the port's, and carrying them would mean
// naming them in the core and in the record, which is a vendor's idea in the
// one place that is meant to have none. So they are read and let go, and the
// turn goes back without them. The API takes such a turn: it strips what no
// longer makes a whole one and carries on unreasoned.
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
