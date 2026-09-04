// Package agents contains the loop that runs an exchange to its end, and
// keeps the record of it as it goes.
//
// The ports below this one are deliberately narrow.
// [github.com/ksahli/compadre/internal/core/inference.Provider] takes a thread
// and gives back replies, folds nothing back in, and knows nothing of what
// came before. [github.com/ksahli/compadre/internal/core/memory.Store] takes
// an exchange and writes it down, and decides nothing about when. This is the
// package that turns round trips into an exchange — send the thread, read the
// replies, run whatever the model asked for, send the answers back, and round
// again until it asks for nothing — and it is the package that decides when
// what happened is worth writing down.
//
// It sits in the core rather than in the command that used to hold it, because
// what it encodes is the runtime's own behaviour and not one caller's taste.
// That a call is answered before the model is asked again; that every call of
// one turn is answered together, in one message, because the API pairs them by
// id and the model is owed all of them at once; that the model's own turn is in
// the record before the answers to it, since a result with no call ahead of it
// is not something a model can read; that a tool which failed is a result
// rather than the end of the program — none of that is a command's opinion. A
// second caller would have to arrive at the same answers, and a command is a
// poor place to keep something that can only be tested through it.
//
// What is a caller's call arrives as an argument. Which model answers, which
// tools are on offer, which engine keeps the record and how many turns a run
// may take are handed to [New], because a runtime that chose its own provider
// would be naming a vendor in the core, and one that chose its own store would
// be naming an engine there. The exchange is handed to [Type.Converse] rather
// than to [New], so the agent is a thing that can be asked twice.
//
// That the run has a ceiling at all is not the caller's call, only where it
// sits. An exchange that never ends spends without end, and a model that keeps
// asking for tools is stopped loudly rather than left to run; a caller with no
// reason to move the bound names [Turns], which is exported so that saying so
// is not guessing at a number.
//
// # When the record is written
//
// The exchange is written down as it happens rather than once at the end, and
// the placing of each write is the same argument as the placing of each
// append. The opening turn goes down before the model is asked anything, so a
// run refused on its first round trip still left behind what it was asked to
// do — and that first write is what mints the id, so there is one to write
// under from then on. The model's turn goes down before the answers to it, for
// the reason the record in memory is built that way: a result with no call
// ahead of it is not something a model can read, and a record that could be
// read back in that state would be a record of something that could not have
// happened. The answers go down as one turn, once they are all in.
//
// What falls out of that is a record that is true at every moment rather than
// only at the end. An exchange that hit the ceiling, or whose provider refused
// on the fourth turn, or whose process was killed on the sixth, left behind
// the turns that got that far. There is no write on the way out of the loop
// for that reason: by the time the loop is leaving, whatever it had was
// already written.
//
// The ordering has a window in it, and the window is worth naming because
// something has to answer for it. Between the write that records a call and
// the write that records the answer, the record is in the one state nothing
// can continue from: a call with nothing replying to it, which a provider is
// entitled to refuse a turn over. Two things close it. A write is made on a
// context stripped of its cancellation, so an interrupt landing mid-turn still
// lands the answer — cancelling is how a caller says it no longer wants the
// answer, not how it says the exchange never happened. And an exchange that
// arrives in that state anyway, because a process was killed outright or was
// written before any of this was, has its hanging calls answered as failures
// before the model is asked anything. What that buys is the thing the record
// is for: every exchange in it can be picked back up.
//
// A store that cannot write ends the run, and that is the one failure here
// treated differently from a tool's. A tool that failed is a result the model
// reads and recovers from, because trying something else is a thing a model
// can do. A record that is not being kept is not, and carrying on would mean
// spending on turns nobody will be able to read back.
//
// [Type.Converse] hands back the exchange as it ended, and hands it back on
// failure too. That is worth stating because a (value, error) pair usually
// means the value is not to be trusted, and here it is: the exchange is never
// zero, and it is always what had accumulated when the loop stopped. A caller
// printing a run that failed needs the turns that got that far, and a caller
// that will one day continue an exchange needs the id it was filed under. What
// this package does not do is print any of it — where the words go is the
// wiring's business, and the core has no opinion about writing bytes.
package agents
