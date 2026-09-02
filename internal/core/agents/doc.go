// Package agents contains the loop that runs an exchange to its end.
//
// The port below this one is deliberately one round trip:
// [github.com/ksahli/compadre/internal/core/inference.Provider] takes a thread
// and gives back replies, folds nothing back in, and knows nothing of what
// came before. This is the package that turns round trips into an exchange —
// send the thread, read the replies, run whatever the model asked for, send
// the answers back, and round again until it asks for nothing.
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
// What is a caller's call arrives as an argument. Which model answers and
// which tools are on offer are handed to [New], because a runtime that chose
// its own provider would be naming a vendor in the core. The thread is handed
// to [Type.Converse] rather than to [New], so the agent is a thing that can be
// asked twice.
//
// The ceiling stays here, and it is the one bound that is not the caller's to
// pick yet. An exchange that never ends spends without end, and a model that
// keeps asking for tools is stopped loudly rather than left to run. [Turns] is
// exported so that a caller can say what the bound is without guessing at it.
//
// [Type.Converse] hands back the exchange as it ended, and hands it back on
// failure too. That is worth stating because a (value, error) pair usually
// means the value is not to be trusted, and here it is: the thread is never
// nil, and it is always what had accumulated when the loop stopped. A caller
// printing a run that failed needs the turns that got that far, and a caller
// that will one day continue an exchange needs the record of it. What this
// package does not do is print any of it — where the words go is the wiring's
// business, and the core has no opinion about writing bytes.
package agents
