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
// Everything the port has no shape for — usage, stop reason — is dropped on
// the way through.
//
// The model and the token ceiling are the adapter's, not the thread's: they
// arrive at [New] from the wiring, which is what lets the command line choose
// them without the core acquiring a word for either. An adapter built without
// an opinion on them falls back to [Model] and [Tokens].
package anthropic
