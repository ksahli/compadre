// Package failures contains the core's word for a failure worth giving up on.
//
// Nothing here describes what went wrong — that is the vocabulary of whichever
// adapter it went wrong in, and the core has none. What the core owns is the
// one question a caller driving turns has to ask about any failure: is there
// any point in a next one. A rate limit and a reply cut off at the ceiling say
// yes; credentials the API will not take and a record that cannot be written
// say no.
//
// It is its own package rather than a name on one of the ports because both
// ports need it and neither owns it. A store that cannot write is settled for
// the same reason a key the API refuses is settled, and putting the word on
// [github.com/ksahli/compadre/internal/core/inference] would make the store's
// adapter borrow inference's vocabulary to say something that has nothing to
// do with reaching a model.
package failures
