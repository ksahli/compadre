// Package tools contains what a tool is, in the core's own terms.
//
// A tool is two things at once: what the model is told about it — a name, a
// description, the shape of its arguments — and what actually happens when
// the model asks for it. Both halves belong here. A tool is a capability the
// core is lending out, not a feature of whichever API the request happens to
// travel over, and nothing in this package names a vendor or imports an SDK.
//
// The argument schema is a plain map rather than a typed value, and the
// arguments themselves arrive as raw JSON. Neither is laziness. The core does
// not know any particular tool's parameters, so it declines to pretend to a
// shape: the schema is data on its way to a wire format this package is not
// entitled to know, and each tool unmarshals its own arguments. What crosses
// this boundary is JSON either way.
//
// A tool that fails is not a program that fails. [Invoke] returns a [Result]
// and never an error: an unknown name and a tool that blew up are both things
// the model must be shown, and can recover from on the next turn. That is why
// [Definition.Execute] hands back a string and an error while [Invoke] hands
// back a failed result — the error is a message, not an exit. It is the same
// instinct as a provider skipping a role it has no mapping for rather than
// guessing at one.
//
// [Invoke] also owns pairing a result to the call it answers, since a tool
// has no way of knowing the id it was called under.
//
// What this package does not do: run the loop. The vocabulary is here —
// [github.com/ksahli/compadre/internal/core/messages.Type] carries a [Use]
// and a [Result] as content, so a call can be read off a reply and an answer
// folded back into a thread — but reading a reply, calling [Invoke] on what it
// finds and asking the model again belongs above this package rather than in
// it. [github.com/ksahli/compadre/internal/core/agents] is where that happens.
//
// The line between the two is worth stating, because it is not the line
// between the core and its wiring. How an exchange proceeds is the runtime's
// own behaviour and lives in the core beside this vocabulary; which tools an
// exchange may reach for is the wiring's call, and reaches the agent as an
// argument. So the vocabulary does not run the loop, and the loop does not
// choose the tools.
//
// The types the subpackages own are re-exported here as aliases (Use,
// Definition, Result, Registry) so that code depending on a tool need not
// import three packages to name one. That idiom recurs throughout the tree.
package tools
