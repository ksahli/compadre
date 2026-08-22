// Package weather answers questions about the weather over Open-Meteo.
//
// It is the first tool in the tree that implements
// [github.com/ksahli/compadre/internal/core/tools/definitions.Type], and it
// sits here rather than in core for the same reason a provider does: a tool
// that calls an outside service is an adapter. The core says what a tool is;
// this package is one. The dependency points the one way the rest of the tree
// points — weather knows core, and core does not learn that Open-Meteo exists.
//
// Open-Meteo asks for no credentials, which is why it is the service behind
// this tool. The project has one secret, the model provider's key, and it is
// read by the SDK at the edge. A tool that needed a second one would have to
// invent config handling that does not exist yet, and a weather report is not
// worth that.
//
// The endpoints and the HTTP client are fields on [Tool] rather than
// constants, set through [WithBaseURLs] and [WithHTTPClient]. That is what
// lets the tests drive the whole tool without reaching the network, the same
// insistence as the provider tests pointing an SDK option at an httptest
// server. Left alone, [New] returns something that calls the real service.
//
// Two requests, not one: Open-Meteo forecasts by coordinate, so a name has to
// be geocoded first. The report names the place that came back, because "Paris"
// resolves to somewhere and the model should be able to see it was the wrong
// somewhere.
//
// What Execute returns is prose, not JSON. The reader is a model, and a
// sentence costs it less than a shape it has to decode before it can read.
// The units in that prose are read off the response rather than assumed: the
// service was told which system to answer in and it says what it answered in.
//
// Everything that can go wrong comes back as an error, and none of them stop
// the program: a name that matches nothing, a day count out of range, a
// service that answers 500.
// [github.com/ksahli/compadre/internal/core/tools.Invoke] turns each into a
// failed result, and the model is the one that decides whether to spell the
// place differently and ask again. So the errors here are written to be read
// by it — lowercase, naming the value that was wrong.
//
// The way a model reaches this is the way it reaches any tool: the wiring
// gathers it into a registry, hands that to the provider alongside the thread,
// and runs what comes back through
// [github.com/ksahli/compadre/internal/core/tools.Invoke]. The command in
// [github.com/ksahli/compadre/commands/invoke] is what does that today. This
// package knows none of it — a tool is asked and answers, and what carried the
// question is not its business.
package weather
