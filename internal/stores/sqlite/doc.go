// Package sqlite keeps exchanges in a SQLite database.
//
// It is an adapter, and the only code in the tree that knows SQL exists.
// [github.com/ksahli/compadre/internal/core/memory.Store] is the port it
// answers to, and the core reaches it through nothing else — the same
// arrangement as providers and
// [github.com/ksahli/compadre/internal/core/inference.Provider], for the same
// reason. Which engine keeps the record is the wiring's choice, and the loop
// that writes the record has no idea what it chose.
//
// The driver is a pure-Go one, so nothing here needs a C toolchain to build or
// a cgo-enabled cross compile to ship. That is a real constraint on the choice
// rather than a preference: a runtime whose test suite only runs where a C
// compiler happens to be is a runtime with a worse test suite.
//
// # The shape on disk
//
// Three tables, and they are the core's own vocabulary written down: a thread
// is instructions and turns, a turn is a role and blocks, and a block is one
// of the shapes the core has. Nothing is a blob. The alternative — one row
// per exchange with the conversation as JSON in a column — would have been a
// tenth of this
// code, and would have made the database a place to put bytes rather than a
// place to ask questions. Which tool the model reached for, how often a call
// came back failed, what was said before it did: those are questions about the
// record, and they should be answerable by asking the record.
//
// The content table is the one part where the mapping is not one to one, and
// it is the closed content set that makes it so.
// [github.com/ksahli/compadre/internal/core/messages.Content] is an interface
// with unexported implementations and an unexported marker method, which means
// no encoder can round-trip it and the mapping has to be written out by hand
// here in any case. So the shape is named in a kind column, held closed by a
// CHECK the way the marker method holds it closed in Go, and each shape's
// fields get their own columns, null for the shapes that do not use them.
// Tool arguments stay raw JSON, unparsed, because they are unparsed
// everywhere else too: the core does not know any tool's parameters and the
// record of a call has no more business guessing at them than the call did.
//
// The set the CHECK holds closed is the core's, which means changing it is the
// one change editing schema.sql cannot make on its own: CREATE TABLE IF NOT
// EXISTS leaves a table already on disk exactly as it was, CHECK and all, and
// SQLite has no ALTER for one. There is no migration machinery here to close
// that gap. A record filed under a definition this one no longer writes is a
// record to start again from, not one to rebuild.
//
// # Writing
//
// [Store.Save] writes only the turns it has not seen, and it can do that
// because an exchange grows by appending and never any other way — the
// invariant [github.com/ksahli/compadre/internal/core/threads] holds up is
// what makes counting the rows enough to know where the new ones start. It is
// what makes a save after every turn cheap rather than a rewrite of the whole
// conversation ten times over. An exchange arriving shorter than what is
// already filed under it is refused rather than reconciled: that is not a
// thread that grew, and guessing which of the two is right would be inventing
// a record.
//
// Each save is one transaction. A turn half written down is a record of
// something that did not happen — a call with no result, or worse a result
// with no call — and the loop above this one goes to some trouble to make sure
// the record is never in that state.
//
// Identity is SQLite's own rowid, rendered as a string on the way out. Nothing
// in the core has to mint an id or reach for randomness to do it, which is the
// reason [github.com/ksahli/compadre/internal/core/memory.Store.Save] hands an
// exchange back at all.
package sqlite
