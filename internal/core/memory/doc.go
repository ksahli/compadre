// Package memory is the port an exchange is kept through.
//
// It is the second port in the core, and it is shaped like the first on
// purpose. [github.com/ksahli/compadre/internal/core/inference.Provider] names
// no vendor and lets an adapter in providers be the only code that knows an
// SDK exists; [Store] names no engine and lets an adapter in stores be the
// only code that knows about SQL. Neither port has a default, and neither is
// something the core picks for itself — which engine keeps the record is the
// wiring's call in the same way that which model answers is.
//
// [Store.Save] returns an exchange rather than nothing, and that is the one
// part of this interface that needs saying. An exchange arriving for the first
// time has no id, because an id is what a store gives it; a caller that wanted
// to mint one itself would be deciding what a row is called on behalf of a
// database it knows nothing about. So the store files it and hands back the
// exchange with the id it chose, and every later save writes under that.
//
// What a store is not asked to do is diff. An exchange only ever grows by
// appending — that is the invariant
// [github.com/ksahli/compadre/internal/core/threads] holds up — so a store is
// free to write only the turns it has not seen. That is a permission the port
// grants rather than a requirement it imposes: a store that rewrote the whole
// exchange each time would still be correct, only slower.
//
// A load of an id nothing was filed under is an error, not an empty exchange.
// The two are different answers, and a port that returned the same value for
// both would be inviting a caller to continue a conversation that never
// happened.
package memory
