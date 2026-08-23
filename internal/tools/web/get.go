package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

const (
	// maxBody is the ceiling on what a server is allowed to make this
	// process read, the same one the weather tool keeps and for the same
	// reason: how many bytes there are at the other end is not this
	// process's call.
	maxBody = 1 << 20

	// maxText is the ceiling on what comes back to the model. It is a
	// smaller number than maxBody because it is a different question — the
	// first is what this process will read, the second is what a model can
	// be handed without the answer being worse than a shorter one.
	maxText = 64 << 10

	// agent is what this process calls itself. A server is entitled to
	// know who is asking, and a blank or borrowed name is a small lie.
	agent = "compadre"
)

// Tool fetches a web page and hands the model the text of it.
//
// The client is a field rather than something built per call, for the reason
// the weather tool gives about its endpoints: it is the seam a test reaches
// through, and nothing here touches the network unless the caller left the
// default in place. Left alone, [New] builds the guarded one.
type Tool struct {
	client *http.Client
}

// Option adjusts what [New] builds.
type Option func(*Tool)

// WithHTTPClient hands the tool the client to fetch with.
//
// What it takes is a copy, and the copy has the redirect policy put back on
// it. That policy is the fence on being sent elsewhere, and losing it to a
// test seam would mean the seam quietly turned off half of what this package
// is for — a client without it follows a hop anywhere, which is how a fetch
// aimed at a stub ends up on the open internet.
//
// The other half cannot be put back, and is worth being plain about: the
// address guard lives in a dialer, and a caller handing in a client is handing
// in the transport that dials. So a client passed here is a client that will
// dial whatever a name resolves to. That is why the guard is proved separately,
// against the client [New] builds.
func WithHTTPClient(client *http.Client) Option {
	return func(tool *Tool) {
		copied := *client
		copied.CheckRedirect = redirects
		tool.client = &copied
	}
}

// New builds the tool. The default is a client that refuses to dial anything
// but a public address, follows redirects only so far, and gives up rather
// than hanging.
func New(options ...Option) Tool {
	tool := Tool{client: guarded()}
	for _, option := range options {
		option(&tool)
	}
	return tool
}

func (Tool) Name() string { return "http_get" }

func (Tool) Description() string {
	return "Fetch a web page or an HTTP resource and read what is there. " +
		"Give the full https URL. HTML comes back as text with the markup " +
		"taken out; plain text, JSON and XML come back as they are. Only " +
		"https works, and only addresses on the open internet: a plain http " +
		"URL, a localhost or private address, and a url carrying credentials " +
		"are each refused. Redirects are followed and the page that answered " +
		"is named. A long page is cut off and says so. Anything that is not " +
		"text — an image, a PDF, an archive — is refused rather than handed " +
		"back as bytes."
}

func (Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{
				"type": "string",
				"description": "The full https URL to fetch, for example " +
					"'https://go.dev/doc/'. It must be https and must name a host " +
					"on the open internet.",
			},
		},
		"required": []string{"url"},
	}
}

type arguments struct {
	URL string `json:"url"`
}

// Execute reads the URL, fetches it and writes the answer out as prose. Every
// error here is written to be read by the model: it is the one that has to
// decide whether to ask for somewhere else.
func (tool Tool) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args arguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}

	asked := strings.TrimSpace(args.URL)
	if asked == "" {
		return "", fmt.Errorf("url is required")
	}

	address, err := url.Parse(asked)
	if err != nil {
		return "", fmt.Errorf("could not read the url '%s': %w", asked, err)
	}
	// The cheap half of the fence, before anything is dialled. The other
	// half is in the dialer, where the address is known.
	if err := reachable(address); err != nil {
		return "", err
	}

	got, err := tool.get(ctx, address)
	if err != nil {
		return "", err
	}

	return report(address, got), nil
}

// fetched is one answer as the model will be shown it: where it came from in
// the end, what it was, and the two ways it can be short of the whole page.
type fetched struct {
	final   string
	status  string
	kind    string
	size    int
	text    string
	clipped bool // the byte ceiling stopped the read before the body ended
	cut     bool // the text ceiling stopped the answer before the text ended
}

// get runs the request and makes text of what came back. It refuses in the
// order the file tools refuse in: everything that can be judged from the
// headers is judged before the body is read at all.
func (tool Tool) get(ctx context.Context, address *url.URL) (fetched, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return fetched{}, fmt.Errorf("could not ask for '%s': %w", address, err)
	}
	request.Header.Set("User-Agent", agent)
	request.Header.Set("Accept", "text/html, text/plain, application/json;q=0.9, */*;q=0.1")

	response, err := tool.client.Do(request)
	if err != nil {
		return fetched{}, fmt.Errorf("could not fetch '%s': %w", address, err)
	}
	defer response.Body.Close()

	// Reported by its status rather than by its body: what a server puts
	// in the body of an error is its own to shape, and repeating it would
	// put noise in front of the model.
	if response.StatusCode < 200 || response.StatusCode > 299 {
		return fetched{}, fmt.Errorf("the server answered %s for '%s'", response.Status, address)
	}

	kind, render, err := media(response.Header.Get("Content-Type"))
	if err != nil {
		return fetched{}, err
	}

	body, clipped, err := read(response.Body)
	if err != nil {
		return fetched{}, err
	}

	// Refused rather than shown, exactly as a read of a file that is not
	// text is: bytes that do not decode cost the model context and tell it
	// nothing.
	if bytes.IndexByte(body, 0) >= 0 || !utf8.Valid(body) {
		return fetched{}, fmt.Errorf("the answer is not text, though it was sent as '%s'", kind)
	}

	text := string(body)
	if render {
		text = flatten(text)
	}

	text, cut := clip(text)

	return fetched{
		// Where the fetch ended up, which a redirect can make somewhere
		// other than what was asked for.
		final:   response.Request.URL.String(),
		status:  response.Status,
		kind:    kind,
		size:    len(body),
		text:    text,
		clipped: clipped,
		cut:     cut,
	}, nil
}

// read takes the body through a ceiling. One byte over is asked for rather
// than exactly the ceiling, because that extra byte is the difference between
// a body that fit and one that was cut — and a cut answer has to be able to
// say so.
//
// A cut lands wherever the count ran out, which can be the middle of a rune.
// Those trailing bytes are dropped: they are an artefact of where this process
// stopped reading, and letting them fail the text check would report a page as
// binary for the crime of being long.
func read(body io.Reader) ([]byte, bool, error) {
	raw, err := io.ReadAll(io.LimitReader(body, maxBody+1))
	if err != nil {
		return nil, false, fmt.Errorf("could not read the answer: %w", err)
	}

	if len(raw) <= maxBody {
		return raw, false, nil
	}

	raw = raw[:maxBody]
	// At most the three bytes a truncated rune can leave behind.
	for range 3 {
		if r, size := utf8.DecodeLastRune(raw); r != utf8.RuneError || size > 1 {
			break
		}
		raw = raw[:len(raw)-1]
	}

	return raw, true, nil
}

// clip bounds what the model is handed, on a rune boundary so the answer is
// still text.
func clip(text string) (string, bool) {
	if len(text) <= maxText {
		return text, false
	}

	cut := maxText
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}

	return text[:cut], true
}

// media reads the content type and says both what it is and whether it needs
// reducing. A server that did not say is refused rather than guessed at:
// sniffing the bytes would mean deciding, on a page's own say-so, to treat
// what it sent as safe to read.
func media(header string) (kind string, render bool, err error) {
	if strings.TrimSpace(header) == "" {
		return "", false, fmt.Errorf("the server did not say what it sent, and this tool only reads text")
	}

	kind, parameters, err := mime.ParseMediaType(header)
	if err != nil {
		return "", false, fmt.Errorf("could not read the content type '%s': %w", header, err)
	}

	// Anything but utf-8 is refused rather than mangled. Decoding it would
	// mean a dependency, and text run through the wrong table is worse
	// than a sentence saying it was not read.
	switch charset := strings.ToLower(parameters["charset"]); charset {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
	default:
		return "", false, fmt.Errorf("the answer is in '%s', and this tool only reads utf-8", charset)
	}

	switch {
	case kind == "text/html", kind == "application/xhtml+xml":
		return kind, true, nil
	case strings.HasPrefix(kind, "text/"),
		kind == "application/json",
		kind == "application/xml",
		strings.HasSuffix(kind, "+json"),
		strings.HasSuffix(kind, "+xml"):
		return kind, false, nil
	}

	return "", false, fmt.Errorf("not text: '%s'", kind)
}

// report writes the answer the model reads: where it came from, what it was,
// then the text. Prose rather than JSON, for the reason every tool here gives.
//
// The page that answered is named whenever it is not the one asked for — the
// same instinct as the weather tool's heading naming the place the geocoder
// settled on, and for the same reason: a model should be able to see it was
// answered about somewhere else.
func report(asked *url.URL, got fetched) string {
	var out strings.Builder

	fmt.Fprintln(&out, got.final)
	if got.final != asked.String() {
		fmt.Fprintf(&out, "redirected from %s\n", asked)
	}
	fmt.Fprintf(&out, "%s · %s · %s\n", got.status, got.kind, plural(got.size, "byte"))
	fmt.Fprintln(&out)

	if strings.TrimSpace(got.text) == "" {
		fmt.Fprintln(&out, "there is no text on the page")
	} else {
		fmt.Fprintln(&out, got.text)
	}

	// Two separate facts, and one answer can be short for both reasons at
	// once: the read stopped before the body ended, and the text stopped
	// before the read did. Saying only one would leave the other as a
	// silent hole.
	if got.clipped {
		fmt.Fprintf(&out, "\nthe body was cut off at %d bytes; what is past that was not read\n", maxBody)
	}
	if got.cut {
		fmt.Fprintf(&out, "\nthe text was cut off at %d bytes; what is past that is not shown\n", maxText)
	}

	return strings.TrimRight(out.String(), "\n")
}

// plural says a count in words that read as a sentence.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
