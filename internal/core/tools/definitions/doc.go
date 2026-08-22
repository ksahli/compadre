// Package definitions contains what a tool is and the registry it is looked
// up in.
//
// A definition is both halves of a tool at once: what the model is told —
// a name, a description, the shape of the arguments — and what runs when the
// model asks for it. They sit on one interface deliberately. A description
// with nothing behind it is a promise nothing keeps, and a behaviour the
// model was never told about is one it will never reach for.
//
// [Type.Schema] is a plain map rather than a typed value. The core does not
// know any particular tool's parameters, so it declines to pretend to a
// shape: the schema is data on its way to a wire format this package is not
// entitled to know. The same instinct runs through
// [github.com/ksahli/compadre/internal/core/tools/use], where the arguments
// stay raw JSON.
//
// [Type.Execute] returns an error, and that error is not a program failing.
// [github.com/ksahli/compadre/internal/core/tools.Invoke] turns it into a
// failed result: something the model reads and recovers from on the next
// turn, not something that stops the exchange.
//
// A [Map] is keyed by name because that is all a
// [github.com/ksahli/compadre/internal/core/tools/use.Type] arrives carrying.
// Where two definitions share a name the later one wins, since a registry is
// what the caller assembled and the last word on a name is the one it meant.
// [Map.List] hands the definitions back in no particular order — a map has
// none to give — so a caller that needs a stable sequence sorts it itself.
package definitions
