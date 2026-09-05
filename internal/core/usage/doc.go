// Package usage contains what a turn cost, counted in tokens.
//
// A token count is the core's own vocabulary rather than a particular API's.
// Every model this runtime could reach is metered the same way — so much read,
// so much written — so the count is a fact about the exchange, not a field one
// vendor happens to put in a response. That is what separates it from the
// model's own reasoning, which is one API's idea and is dropped at the adapter
// that read it.
//
// The count belongs to the message it was taken for, and lives there: see
// [github.com/ksahli/compadre/internal/core/messages.Type.Usage]. A turn nobody
// counted — everything said to the model rather than by it — carries the zero
// [Type], which reports [Type.Counted] false. That is a different thing from a
// turn that cost nothing, and the two are kept apart so a reader is never shown
// a zero nobody measured.
package usage
