package web_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/tools/web"
)

// page is an ordinary one: a head with a title in it, some markup that is not
// text, and the words a reader came for.
const page = `<!DOCTYPE html>
<html lang="en">
  <head>
    <meta charset="utf-8">
    <title>Go Documentation</title>
    <link rel="stylesheet" href="/style.css">
    <style>body { margin: 0 }</style>
    <script>window.analytics = true</script>
  </head>
  <body>
    <nav><a href="/">Home</a> <a href="/doc">Docs</a></nav>
    <h1>Effective Go</h1>
    <p>Go is a <b>new</b> language.</p>
    <p>Read it &amp; weep.</p>
    <script>document.write("<p>injected</p>")</script>
    <footer>&copy; the authors</footer>
  </body>
</html>`

// server stands in for whatever is at the other end. It records what the tool
// asked for, so a test can pin what went out as well as what came back.
type server struct {
	requests []*http.Request
}

// serve puts the handler behind TLS on loopback and hands back the options
// that point a tool at it.
//
// TLS rather than plain http because the tool refuses anything else outright,
// and this suite is about what happens after that refusal has been passed. It
// costs the address guard, which lives in the default client's dialer and not
// in this one — which is why the guard is proved separately, against the
// client New builds.
func serve(t *testing.T, handler http.HandlerFunc) (*server, string, []web.Option) {
	t.Helper()

	s := new(server)
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.requests = append(s.requests, r)
		handler(w, r)
	}))
	t.Cleanup(ts.Close)

	return s, ts.URL, []web.Option{web.WithHTTPClient(ts.Client())}
}

// html serves one body as a page.
func html(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, body)
	}
}

func args(t *testing.T, s string) use.Arguments {
	t.Helper()
	return use.Arguments(s)
}

// ask runs the tool against the url given, which is the shape of nearly every
// test here.
func ask(t *testing.T, url string, options []web.Option) (string, error) {
	t.Helper()

	raw, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		t.Fatalf("could not build the arguments: %v", err)
	}
	return web.New(options...).Execute(t.Context(), args(t, string(raw)))
}

// TestDefinition pins the half of a tool the model reads. The name is the key
// a use is resolved through, so it is a contract and not a label.
func TestDefinition(t *testing.T) {
	tool := web.New()

	if got, want := tool.Name(), "http_get"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if tool.Description() == "" {
		t.Error("Description() is empty, want something the model can read")
	}

	schema := tool.Schema()
	if got, want := schema["type"], "object"; got != want {
		t.Errorf("Schema()[\"type\"] = %v, want %v", got, want)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("Schema()[\"properties\"] is %T, want map[string]any", schema["properties"])
	}
	if _, ok := properties["url"]; !ok {
		t.Error("Schema() has no property \"url\"")
	}

	required, ok := schema["required"].([]string)
	if !ok || len(required) != 1 || required[0] != "url" {
		t.Errorf("Schema()[\"required\"] = %v, want [url]", schema["required"])
	}

	// The schema is on its way to a wire format, so what matters is that it
	// survives the trip as JSON rather than that it is a map at all.
	if _, err := json.Marshal(schema); err != nil {
		t.Errorf("json.Marshal(Schema()) error = %v, want nil", err)
	}
}

// TestSatisfiesThePort is the assertion the rest of the package rests on: a
// tool the core cannot hold is not a tool.
func TestSatisfiesThePort(t *testing.T) {
	var _ definitions.Type = web.New()
}

// TestExecuteReadsAPage is the ordinary case end to end: a page in, the words
// of it out, and none of the markup that carried them.
func TestExecuteReadsAPage(t *testing.T) {
	s, url, options := serve(t, html(page))

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	for _, want := range []string{
		url,                   // where it came from
		"200 OK", "text/html", // what it was
		"Go Documentation", // the title, which a head is not all machinery for
		"Effective Go", "Go is a new language.",
		"Read it & weep.", "© the authors",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Execute() = %q, want it to contain %q", out, want)
		}
	}

	for _, unwanted := range []string{
		"<p>", "</html>", "stylesheet", // markup and the attributes carrying it
		"window.analytics", "margin: 0", // what the dropped elements held
		"injected", // the script that tried to write a paragraph
	} {
		if strings.Contains(out, unwanted) {
			t.Errorf("Execute() = %q, want it not to contain %q", out, unwanted)
		}
	}

	if len(s.requests) != 1 {
		t.Fatalf("server saw %d requests, want 1", len(s.requests))
	}
	// A server is entitled to know who is asking, and a blank or borrowed
	// name would be a small lie.
	if got, want := s.requests[0].Header.Get("User-Agent"), "compadre"; got != want {
		t.Errorf("User-Agent = %q, want %q", got, want)
	}
	if got := s.requests[0].Header.Get("Accept"); !strings.Contains(got, "text/html") {
		t.Errorf("Accept = %q, want it to ask for text", got)
	}
}

// TestExecuteLeavesTextAlone pins which types are reduced and which are handed
// over as they were. Reducing JSON would be taking the structure out of the
// one kind of answer that is nothing but structure.
func TestExecuteLeavesTextAlone(t *testing.T) {
	cases := []struct {
		name string
		kind string
		body string
		want string
	}{
		{"plain text", "text/plain; charset=utf-8", "a < b\nand so on", "a < b\nand so on"},
		{"json", "application/json", `{"a":["<b>",1]}`, `{"a":["<b>",1]}`},
		{"an api's own json type", "application/vnd.api+json", `{"a":1}`, `{"a":1}`},
		{"xml", "application/xml", "<note><to>you</to></note>", "<note><to>you</to></note>"},
		{"an atom feed", "application/atom+xml", "<feed><title>t</title></feed>", "<feed><title>t</title></feed>"},
		{"markdown", "text/markdown", "# Title\n\n- one\n- two", "# Title\n\n- one\n- two"},
		{"csv", "text/csv", "a,b\n1,2", "a,b\n1,2"},
		// xhtml is html however it is spelled, and is reduced.
		{"xhtml", "application/xhtml+xml", "<p>hello</p>", "hello"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", c.kind)
				_, _ = fmt.Fprint(w, c.body)
			})

			out, err := ask(t, url, options)
			if err != nil {
				t.Fatalf("Execute() error = %v, want nil", err)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("Execute() = %q, want it to contain %q", out, c.want)
			}
		})
	}
}

// TestExecuteCountsTheBytes pins the heading, down to the noun agreeing with
// the number. It reads as a sentence or it reads as a machine.
func TestExecuteCountsTheBytes(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{"x", "1 byte"},
		{"xy", "2 bytes"},
	}

	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				_, _ = fmt.Fprint(w, c.body)
			})

			out, err := ask(t, url, options)
			if err != nil {
				t.Fatalf("Execute() error = %v, want nil", err)
			}
			if !strings.Contains(out, c.want) {
				t.Errorf("Execute() = %q, want it to contain %q", out, c.want)
			}
		})
	}
}

// TestExecuteSaysWhenThereIsNoText pins that a page with nothing on it says so.
// Handing back a heading and then silence would leave the model unable to tell
// an empty page from a broken tool.
func TestExecuteSaysWhenThereIsNoText(t *testing.T) {
	_, url, options := serve(t, html(`<html><head><script>var a = 1</script></head><body><div></div></body></html>`))

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !strings.Contains(out, "there is no text on the page") {
		t.Errorf("Execute() = %q, want it to say the page has no text", out)
	}
}

// TestExecuteFollowsRedirects pins both halves of the policy: the hop is taken
// without spending a turn of the model's attention on it, and the page that
// actually answered is named.
func TestExecuteFollowsRedirects(t *testing.T) {
	s, url, options := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/moved" {
			http.Redirect(w, r, "/here", http.StatusMovedPermanently)
			return
		}
		html(`<p>the page that answered</p>`)(w, r)
	})

	out, err := ask(t, url+"/moved", options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	for _, want := range []string{
		url + "/here",                       // where it ended up
		"redirected from " + url + "/moved", // and where it was asked for
		"the page that answered",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Execute() = %q, want it to contain %q", out, want)
		}
	}
	if len(s.requests) != 2 {
		t.Errorf("server saw %d requests, want 2", len(s.requests))
	}
}

// TestExecuteDoesNotSayRedirectedWhenItWasNot pins the other side of it: the
// line is there to report a surprise, and a line that is always there reports
// nothing.
func TestExecuteDoesNotSayRedirectedWhenItWasNot(t *testing.T) {
	_, url, options := serve(t, html(`<p>a</p>`))

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if strings.Contains(out, "redirected from") {
		t.Errorf("Execute() = %q, want no redirect line", out)
	}
}

// TestExecuteRefusesToBeBouncedSomewhereElse pins that the fence does not stop
// at the first url. A public https address a model was talked into fetching
// cannot hand the fetch on to somewhere the model could not have asked for.
func TestExecuteRefusesToBeBouncedSomewhereElse(t *testing.T) {
	cases := []struct {
		name    string
		to      string
		message string
	}{
		{"to plain http", "http://example.com/", "only https can be fetched"},
		{"to a credentialed url", "https://user:pw@example.com/", "a url carrying credentials is refused"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, c.to, http.StatusFound)
			})

			out, err := ask(t, url, options)
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			if got := err.Error(); !strings.Contains(got, c.message) {
				t.Errorf("Execute() error = %q, want it to contain %q", got, c.message)
			}
		})
	}
}

// TestExecuteGivesUpOnAChain pins the ceiling. A chain built to waste the time
// of whoever follows it is answered by stopping, and saying so.
func TestExecuteGivesUpOnAChain(t *testing.T) {
	s, url, options := serve(t, func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/on", http.StatusFound)
	})

	out, err := ask(t, url, options)
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
	if got := err.Error(); !strings.Contains(got, "gave up after 5 redirects") {
		t.Errorf("Execute() error = %q, want it to report the ceiling", got)
	}
	// The first request plus the five hops the policy allowed before it
	// refused the sixth.
	if len(s.requests) != 6 {
		t.Errorf("server saw %d requests, want 6", len(s.requests))
	}
}

// TestExecuteReportsTheStatus pins that a server having a bad day is reported
// by its status rather than by its body: what a server puts in an error page
// is its own to shape, and repeating it would put noise in front of the model.
func TestExecuteReportsTheStatus(t *testing.T) {
	for _, status := range []int{
		http.StatusBadRequest,
		http.StatusUnauthorized,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusTooManyRequests,
		http.StatusInternalServerError,
		http.StatusServiceUnavailable,
	} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `<p>our engineers have been notified</p>`)
			})

			out, err := ask(t, url, options)
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			got := err.Error()
			if !strings.Contains(got, fmt.Sprint(status)) {
				t.Errorf("Execute() error = %q, want it to name the status", got)
			}
			if strings.Contains(got, "engineers") {
				t.Errorf("Execute() error = %q, want the body left out of it", got)
			}
		})
	}
}

// TestExecuteRefusesWhatIsNotText pins the judgements made from the headers,
// before a byte of the body is read. Each names what was wrong, because the
// model is the one that has to decide whether to ask for somewhere else.
func TestExecuteRefusesWhatIsNotText(t *testing.T) {
	cases := []struct {
		name    string
		kind    string
		message string
	}{
		{"an image", "image/png", "not text: 'image/png'"},
		{"a pdf", "application/pdf", "not text: 'application/pdf'"},
		{"an archive", "application/zip", "not text: 'application/zip'"},
		{"bytes with no name", "application/octet-stream", "not text: 'application/octet-stream'"},
		{"audio", "audio/mpeg", "not text: 'audio/mpeg'"},
		// A server that did not say is refused rather than guessed at:
		// sniffing would mean deciding, on a page's own say-so, that
		// what it sent is safe to read.
		{"nothing said", "", "the server did not say what it sent"},
		{"nonsense said", "text/html; charset=", "could not read the content type"},
		// Anything but utf-8 is refused rather than mangled, since text
		// run through the wrong table is worse than a sentence saying
		// it was not read.
		{"latin-1", "text/html; charset=iso-8859-1", "this tool only reads utf-8"},
		{"shift-jis", "text/plain; charset=shift_jis", "this tool only reads utf-8"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				if c.kind != "" {
					w.Header().Set("Content-Type", c.kind)
				} else {
					// Go labels an unlabelled body by
					// sniffing it, so saying nothing takes
					// saying so.
					w.Header()["Content-Type"] = nil
				}
				_, _ = w.Write([]byte("\x00\x01binary"))
			})

			out, err := ask(t, url, options)
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			if got := err.Error(); !strings.Contains(got, c.message) {
				t.Errorf("Execute() error = %q, want it to contain %q", got, c.message)
			}
		})
	}
}

// TestExecuteRefusesBytesCallingThemselvesText pins the check that catches
// what the content type could not: a body labelled as text that does not
// decode as any. Bytes that do not decode cost the model context and tell it
// nothing.
func TestExecuteRefusesBytesCallingThemselvesText(t *testing.T) {
	cases := []struct {
		name string
		body []byte
	}{
		{"a nul byte", []byte("hello\x00world")},
		{"invalid utf-8", []byte{0xff, 0xfe, 0x41, 0x42}},
		{"a png wearing a label", []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR")},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				_, _ = w.Write(c.body)
			})

			out, err := ask(t, url, options)
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			if got := err.Error(); !strings.Contains(got, "the answer is not text") {
				t.Errorf("Execute() error = %q, want it to refuse the bytes", got)
			}
		})
	}
}

// TestExecuteCutsTheText pins the smaller of the two ceilings on its own: a
// page well under what will be read off the wire, and still more than a model
// should be handed. An answer that hit it says so, because a partial answer
// passed off as a whole one is worse than a short one.
func TestExecuteCutsTheText(t *testing.T) {
	_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, strings.Repeat("a", 100<<10))
	})

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !strings.Contains(out, "the text was cut off at 65536 bytes") {
		t.Errorf("Execute() = %q, want it to say the text was cut", out[:min(len(out), 300)])
	}
	if strings.Contains(out, "the body was cut off") {
		t.Errorf("Execute() said the body was cut, want only the text: the page fit on the wire")
	}
}

// TestExecuteCutsTheBodyAndTheText pins that the two are separate facts and
// one answer can be short for both reasons at once. Saying only one would
// leave the other as a silent hole.
func TestExecuteCutsTheBodyAndTheText(t *testing.T) {
	_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, strings.Repeat("a", 4<<20))
	})

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	for _, want := range []string{
		"the body was cut off at 1048576 bytes",
		"the text was cut off at 65536 bytes",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("Execute() did not say %q, want both ceilings reported", want)
		}
	}
}

// TestExecuteCutsOnARuneBoundary pins that a page cut in the middle of a
// character is still text. The bytes a cut rune leaves behind are an artefact
// of where this process stopped reading, and letting them fail the text check
// would report a page as binary for the crime of being long.
func TestExecuteCutsOnARuneBoundary(t *testing.T) {
	// Three-byte runes divide into neither ceiling evenly, so both cuts
	// land mid-character.
	_, url, options := serve(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprint(w, strings.Repeat("あ", 1<<20))
	})

	out, err := ask(t, url, options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !strings.Contains(out, "あ") {
		t.Error("Execute() lost the text, want it cut and still readable")
	}
	if strings.Contains(out, "�") {
		t.Errorf("Execute() left a broken rune in the answer, want the cut on a boundary")
	}
}

// TestExecuteRejectsArguments pins the errors the model can fix and ask again
// with, and pins that none of them trouble a server first.
func TestExecuteRejectsArguments(t *testing.T) {
	cases := []struct {
		name      string
		arguments string
		message   string
		// prefix is set where the message wraps whatever the decoder
		// said. What is pinned is the sentence the model reads first;
		// the rest is the standard library's to word.
		prefix bool
	}{
		{"not json", `not json`, "could not parse arguments:", true},
		{"wrong type", `{"url":42}`, "could not parse arguments:", true},
		{"no url", `{}`, "url is required", false},
		{"empty url", `{"url":""}`, "url is required", false},
		{"blank url", `{"url":"   "}`, "url is required", false},
		{"unparseable", `{"url":"https://[::1"}`, "could not read the url", true},
		{"plain http", `{"url":"http://example.com"}`, "only https can be fetched, got 'http'", false},
		{"a file url", `{"url":"file:///etc/passwd"}`, "only https can be fetched, got 'file'", false},
		{"no scheme", `{"url":"example.com/doc"}`, "the url has no scheme", true},
		{"no host", `{"url":"https:///doc"}`, "the url has no host", true},
		{"credentials", `{"url":"https://user:pw@example.com"}`, "a url carrying credentials is refused", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, _, options := serve(t, html(`<p>a</p>`))

			out, err := web.New(options...).Execute(t.Context(), args(t, c.arguments))
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", out)
			}
			got := err.Error()
			if c.prefix && !strings.HasPrefix(got, c.message) {
				t.Errorf("Execute() error = %q, want it to start with %q", got, c.message)
			}
			if !c.prefix && got != c.message {
				t.Errorf("Execute() error = %q, want %q", got, c.message)
			}
			// A url that cannot be right is refused before anything
			// is dialled.
			if len(s.requests) != 0 {
				t.Errorf("server saw %d requests, want 0", len(s.requests))
			}
		})
	}
}

// TestExecuteTrimsTheURL pins that a url the model wrapped in whitespace is
// still the url it meant. It is the one piece of tidying done to the argument,
// and everything else about the spelling is judged as written.
func TestExecuteTrimsTheURL(t *testing.T) {
	_, url, options := serve(t, html(`<p>found it</p>`))

	out, err := ask(t, "  "+url+"\n", options)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if !strings.Contains(out, "found it") {
		t.Errorf("Execute() = %q, want the page", out)
	}
}

// TestExecuteCancelled pins that the context reaches the request. A tool that
// ignored it would keep a cancelled run waiting on a network it no longer has
// any reason to be on.
func TestExecuteCancelled(t *testing.T) {
	_, url, options := serve(t, html(page))

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	raw, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		t.Fatalf("could not build the arguments: %v", err)
	}

	out, err := web.New(options...).Execute(ctx, args(t, string(raw)))
	if err == nil {
		t.Fatalf("Execute() = %q, want an error", out)
	}
}

// TestThroughInvoke drives the tool the way the runtime does: assembled into a
// registry and reached by name. It is the one test that pins the tool answers
// to the id it was called under, and that a refusal comes back as a failed
// result rather than as the end of the program.
func TestThroughInvoke(t *testing.T) {
	_, url, options := serve(t, html(`<h1>Effective Go</h1>`))
	registry := definitions.New(web.New(options...))

	raw, err := json.Marshal(map[string]string{"url": url})
	if err != nil {
		t.Fatalf("could not build the arguments: %v", err)
	}

	result := tools.Invoke(t.Context(), use.New("call_1", "http_get", args(t, string(raw))), registry)
	if got, want := result.ID(), "call_1"; got != want {
		t.Errorf("Result.ID() = %q, want %q", got, want)
	}
	if result.Failed() {
		t.Errorf("Result.Failed() = true, want false: %s", result.Content())
	}
	if !strings.Contains(result.Content(), "Effective Go") {
		t.Errorf("Result.Content() = %q, want the page", result.Content())
	}

	// A refusal is an answer the model reads and decides about, not a
	// failure of the process.
	refused := tools.Invoke(t.Context(),
		use.New("call_2", "http_get", args(t, `{"url":"http://example.com"}`)), registry)
	if !refused.Failed() {
		t.Errorf("Result.Failed() = false, want true for a plain http url")
	}
	if !strings.Contains(refused.Content(), "only https can be fetched") {
		t.Errorf("Result.Content() = %q, want it to say why", refused.Content())
	}
}
