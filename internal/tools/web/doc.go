// Package web lets the model fetch a web resource and read the text of it.
//
// It implements
// [github.com/ksahli/compadre/internal/core/tools/definitions.Type] and sits
// here rather than in core for the reason every tool in this tree does: the
// core says what a tool is, and a tool that reaches the network is an adapter
// onto the world. The dependency points the one way the rest of the tree
// points.
//
// What makes this one different from the weather tool, which also reaches a
// service, is that the address comes from the model. Weather talks to two
// endpoints the process chose and hardcoded; this talks to whatever it is
// told. That single fact is what the package is mostly about, because a tool
// that dials on request is a way to make this process probe the network it
// happens to be sitting on — the cloud metadata service on a link-local
// address, the admin panel bound to localhost, the database on the private
// subnet — and the one doing the asking is a model that can be talked into
// asking by a page it read a moment ago.
//
// So the fence is the substance here, the way containment is the substance of
// the files package, and it stands in two places because a URL and a socket
// are different things. The spelling is judged first and cheaply: https only,
// a host that is there, and no credentials carried in the URL. The address is
// judged second, at connect time, through [net.Dialer.Control] — which runs
// after resolution, with the concrete address about to be dialled, on every
// attempt and every redirect hop. A hostname is only a question asked of a
// resolver, and the answer can change between the moment it is checked and the
// moment it is used; judging the socket is the only version of the check that
// cannot be answered one way and dialled another. It is the same insistence as
// the file tools refusing to judge a path until it has been resolved through
// its symlinks.
//
// Redirects are followed rather than reported back, because a site moving a
// reader is ordinary and spending a turn of the model's attention on it would
// be a poor trade. They are followed only so far, and every hop is put through
// the same two judgements, so a public https URL cannot bounce the fetch
// somewhere the first URL would have been refused for naming outright.
//
// Only text comes back. HTML is reduced to the words in it by [flatten],
// which is not a parser and says so: the standard library has no HTML
// tokenizer, and the accuracy a dependency would buy is not accuracy this
// needs. Markup is most of a page's bytes and almost none of its meaning, and
// what the reduction is really for is not spending the model's context on
// somebody's div soup. A page malformed enough to defeat it leaks some markup
// through, which is a worse answer rather than a broken one. Anything that is
// not text at all — an image, a PDF, an archive — is refused by name rather
// than handed back as bytes, the same judgement the files package makes about
// reading a file that is not text, and for the same reason: bytes that do not
// decode cost the model context and tell it nothing.
//
// A charset that is not utf-8 is refused rather than mangled. Decoding it
// would mean a dependency, and text run through the wrong table is worse than
// a sentence saying it was not read.
//
// There are ceilings, and they are the same instinct as every other ceiling in
// this tree: how many bytes there are at the other end is not this process's
// call. maxBody bounds what will be read off the wire; maxText bounds what is
// handed to the model, and it is a smaller and separate number because it
// answers a different question. An answer that hit either says so, and says
// which — a partial answer passed off as a whole one is worse than a short one.
//
// The client is a field on [Tool], set through [WithHTTPClient], which is the
// seam the weather tool has in its endpoints: the tests drive the whole tool
// against an httptest server and nothing in the suite touches the network.
// The option takes a copy and puts the redirect policy back on it, because a
// seam that turned off the fence on being sent elsewhere would be a seam that
// let a fetch aimed at a stub wander off onto the open internet.
//
// The address guard cannot be put back the same way, and that is worth being
// plain about: it lives in a dialer, and a caller handing in a client is
// handing in the transport that dials. So a client passed through the option
// is a client that will dial whatever a name resolves to. That is why the
// guard is tested twice over: as a predicate on its own terms, and once
// through the default client pointed at a loopback server, which is the only
// test that proves the two halves are actually wired together.
//
// Everything that can go wrong comes back as an error, and none of them stop
// the program: a plain http URL, a private address, a 404, an image.
// [github.com/ksahli/compadre/internal/core/tools.Invoke] turns each into a
// failed result, and the model is the one that decides what to do about it. So
// the errors here are written to be read by it — lowercase, naming the value
// that was wrong.
package web
