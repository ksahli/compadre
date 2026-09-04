package anthropic_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/providers/anthropic"

	"github.com/anthropics/anthropic-sdk-go/option"
)

// request is the subset of the Messages API payload the adapter is
// responsible for building. Asserting on it is how the outbound mapping is
// reached: the function that builds it is unexported, so the wire is the
// only place its work is visible.
type request struct {
	Model     string `json:"model"`
	MaxTokens int64  `json:"max_tokens"`
	System    []struct {
		Text string `json:"text"`
	} `json:"system"`
	Messages []struct {
		Role    string         `json:"role"`
		Content []contentBlock `json:"content"`
	} `json:"messages"`
	Tools []struct {
		Name        string          `json:"name"`
		Description string          `json:"description"`
		InputSchema json.RawMessage `json:"input_schema"`
	} `json:"tools"`
}

// contentBlock is a block of one turn's content, wide enough for every shape
// the adapter can send: the fields that do not belong to the shape that
// arrived stay at their zero values.
type contentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	IsError   bool            `json:"is_error"`
	Content   []struct {
		Text string `json:"text"`
	} `json:"content"`
}

// text is a text block — the only kind the adapter has a shape for.
func text(content string) string {
	encoded, err := json.Marshal(content)
	if err != nil {
		panic(err)
	}
	return `{"type":"text","text":` + string(encoded) + `}`
}

// toolUse is how a tool call arrives: the id the answer has to come back
// with, the tool's name, and the arguments the model chose.
var toolUse = `{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"go.mod"}}`

// thinking is a block this adapter has no shape for, and so the case that
// pins what happens to one: it is skipped rather than guessed at.
var thinking = `{"type":"thinking","thinking":"hmm","signature":"sig"}`

// reply is a whole response: the envelope every message carries, with the
// given content blocks inside it.
func reply(blocks ...string) string {
	return stopped("end_turn", blocks...)
}

// stopped is reply with the reason the model gave for stopping, which is what
// separates a whole turn from one the adapter has to refuse.
func stopped(reason string, blocks ...string) string {
	return `{"id":"msg_1","type":"message","role":"assistant",` +
		`"model":"claude-sonnet-5","content":[` + strings.Join(blocks, ",") + `],` +
		`"stop_reason":"` + reason + `","stop_sequence":null,` +
		`"usage":{"input_tokens":1,"output_tokens":2}}`
}

// options point the adapter at a test server instead of the real API. This is
// what Option exists for. Retries are off so that a failing case is served
// once and asserted once.
func options(url string) []anthropic.Option {
	return []anthropic.Option{
		option.WithBaseURL(url),
		option.WithAPIKey("test"),
		option.WithMaxRetries(0),
	}
}

// stub serves the given response body and captures the request the adapter
// sent. The counter it returns is how many times the server was asked.
func stub(t *testing.T, body string) (*request, *int, []anthropic.Option) {
	t.Helper()
	captured, calls := new(request), new(int)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		payload, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request body: %v", err)
			return
		}
		if err := json.Unmarshal(payload, captured); err != nil {
			t.Errorf("decoding request body: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return captured, calls, options(server.URL)
}

// failing stands in for an API that refuses the request.
func failing(t *testing.T) []anthropic.Option {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, `{"type":"error","error":{"type":"internal_server_error","message":"boom"}}`)
	}))
	t.Cleanup(server.Close)
	return options(server.URL)
}

// said renders one content block as a line a table can state, naming the
// shape it turned out to be so that a call and a sentence about a call are
// never mistaken for one another.
func said(content messages.Content) string {
	if text, ok := content.Text(); ok {
		return "text:" + text
	}
	if use, ok := content.Use(); ok {
		return "use:" + use.ID() + ":" + use.Name() + ":" + string(use.Arguments())
	}
	if result, ok := content.Result(); ok {
		failed := "ok"
		if result.Failed() {
			failed = "failed"
		}
		return "result:" + result.ID() + ":" + failed + ":" + result.Content()
	}
	return "unknown"
}

// contents is every block of every reply, in order.
func contents(replies []messages.Type) []string {
	got := []string{}
	for _, reply := range replies {
		for _, content := range reply.Content() {
			got = append(got, said(content))
		}
	}
	return got
}

// turn is one expected message on the wire: the role it arrived with, and
// the blocks it carried, rendered one per entry and joined so that a table
// can state a whole turn as a single string.
type turn struct {
	role string
	text string
}

// block renders one content block of the payload, naming its type so that a
// case can say which shape it expects and not merely what it reads as.
func block(b contentBlock) string {
	switch b.Type {
	case "text":
		return "text:" + b.Text
	case "tool_use":
		return "use:" + b.ID + ":" + b.Name + ":" + string(b.Input)
	case "tool_result":
		content := strings.Builder{}
		for _, inner := range b.Content {
			content.WriteString(inner.Text)
		}
		failed := "ok"
		if b.IsError {
			failed = "failed"
		}
		return "result:" + b.ToolUseID + ":" + failed + ":" + content.String()
	}
	return "unknown:" + b.Type
}

func turns(captured *request) []turn {
	got := make([]turn, 0, len(captured.Messages))
	for _, message := range captured.Messages {
		blocks := make([]string, 0, len(message.Content))
		for _, b := range message.Content {
			blocks = append(blocks, block(b))
		}
		got = append(got, turn{role: message.Role, text: strings.Join(blocks, "|")})
	}
	return got
}

func TestParameters(t *testing.T) {
	cases := []struct {
		name     string
		thread   threads.Type
		registry tools.Registry
		system   string
		turns    []turn
	}{
		{
			name:   "a user turn",
			thread: threads.New("", messages.New(roles.User, messages.Text("hello"))),
			turns:  []turn{{"user", "text:hello"}},
		},
		{
			name:   "instructions become the system prompt",
			thread: threads.New("be brief", messages.New(roles.User, messages.Text("hello"))),
			system: "be brief",
			turns:  []turn{{"user", "text:hello"}},
		},
		{
			// The system prompt is omitted rather than sent empty: an
			// absent instruction is not an instruction to say nothing.
			name:   "no instructions omits the system prompt",
			thread: threads.New("", messages.New(roles.User, messages.Text("hello"))),
			turns:  []turn{{"user", "text:hello"}},
		},
		{
			name: "turns keep their roles and their order",
			thread: threads.New("be brief",
				messages.New(roles.User, messages.Text("a")),
				messages.New(roles.Assistant, messages.Text("b")),
				messages.New(roles.User, messages.Text("c")),
			),
			system: "be brief",
			turns:  []turn{{"user", "text:a"}, {"assistant", "text:b"}, {"user", "text:c"}},
		},
		{
			// The shape the old one-string message could not send: a
			// sentence and the call it leads to, in one turn and in
			// the order the model said them.
			name: "a call goes out beside the sentence that leads to it",
			thread: threads.New("",
				messages.New(roles.User, messages.Text("what is in go.mod?")),
				messages.New(roles.Assistant,
					messages.Text("let me look"),
					messages.Use(use.New("toolu_1", "read_file", use.Arguments(`{"path":"go.mod"}`))),
				),
			),
			turns: []turn{
				{"user", "text:what is in go.mod?"},
				{"assistant", `text:let me look|use:toolu_1:read_file:{"path":"go.mod"}`},
			},
		},
		{
			// Arguments go back out as the model wrote them, keys
			// and order and all: they are its own JSON, and the
			// adapter does not have an opinion about their shape.
			name: "a call keeps the arguments as the model wrote them",
			thread: threads.New("",
				messages.New(roles.Assistant,
					messages.Use(use.New("toolu_1", "read_file", use.Arguments(`{"lines":3,"path":"go.mod"}`))),
				),
			),
			turns: []turn{{"assistant", `use:toolu_1:read_file:{"lines":3,"path":"go.mod"}`}},
		},
		{
			// Results are what the model is shown next, so they go
			// out as a user turn — one turn carrying every answer a
			// round of calls produced, failures included.
			name: "results go back as one user turn",
			thread: threads.New("",
				messages.New(roles.User,
					messages.Result(results.New("toolu_1", "module compadre", false)),
					messages.Result(results.New("toolu_2", "no such file", true)),
				),
			),
			turns: []turn{{"user", "result:toolu_1:ok:module compadre|result:toolu_2:failed:no such file"}},
		},
		{
			// A role the mapping does not cover is dropped rather than
			// guessed at, and the turns around it are unaffected.
			name: "an unmappable role is skipped",
			thread: threads.New("",
				messages.New(roles.User, messages.Text("a")),
				// Not one of the roles constants: that is the point.
				messages.New("Stranger", messages.Text("unmappable")),
				messages.New(roles.User, messages.Text("c")),
			),
			turns: []turn{{"user", "text:a"}, {"user", "text:c"}},
		},
		{
			// A turn with nothing in it is not sent: the API has no
			// shape for a message that says nothing, and an empty
			// content array is not the same as an empty sentence.
			name: "a message that says nothing is skipped",
			thread: threads.New("",
				messages.New(roles.User, messages.Text("a")),
				messages.New(roles.Assistant),
				messages.New(roles.User, messages.Text("c")),
			),
			turns: []turn{{"user", "text:a"}, {"user", "text:c"}},
		},
		{
			name:   "a thread with no messages",
			thread: threads.New("be brief"),
			system: "be brief",
			turns:  []turn{},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			captured, _, options := stub(t, reply(text("hola")))

			if _, err := anthropic.New("", 0, options...).Invoke(context.Background(), c.thread, c.registry); err != nil {
				t.Fatalf("Invoke() error = %v, want nil", err)
			}

			if got := turns(captured); !slices.Equal(got, c.turns) {
				t.Errorf("messages = %v, want %v", got, c.turns)
			}
			if c.system == "" {
				if len(captured.System) != 0 {
					t.Errorf("system = %v, want it omitted", captured.System)
				}
			} else {
				if len(captured.System) != 1 {
					t.Fatalf("system has %d blocks, want 1", len(captured.System))
				}
				if got := captured.System[0].Text; got != c.system {
					t.Errorf("system = %q, want %q", got, c.system)
				}
			}
		})
	}
}

// definition is a tool as the core defines one, with every answer handed to
// it. Nothing here executes: what is under test is the mapping of a
// definition onto the request, and a real tool would only put a service
// behind it.
type definition struct {
	name        string
	description string
	schema      map[string]any
}

func (d definition) Name() string                                         { return d.name }
func (d definition) Description() string                                  { return d.description }
func (d definition) Schema() map[string]any                               { return d.schema }
func (definition) Execute(context.Context, use.Arguments) (string, error) { return "", nil }

// TestParametersOffersTheRegistry pins what the model is told it may ask for.
// The registry is a map and has no order of its own, so the wire order is the
// adapter's to decide and this is where it is decided: by name.
func TestParametersOffersTheRegistry(t *testing.T) {
	weather := definition{
		name:        "weather",
		description: "the weather somewhere",
		schema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"location": map[string]any{"type": "string"},
			},
			"required": []string{"location"},
		},
	}
	clock := definition{
		name:        "clock",
		description: "the time somewhere",
		schema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}

	captured, _, options := stub(t, reply(text("hola")))

	thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
	registry := definitions.New(weather, clock)
	if _, err := anthropic.New("", 0, options...).Invoke(context.Background(), thread, registry); err != nil {
		t.Fatalf("Invoke() error = %v, want nil", err)
	}

	if len(captured.Tools) != 2 {
		t.Fatalf("tools = %d, want 2", len(captured.Tools))
	}

	names := []string{captured.Tools[0].Name, captured.Tools[1].Name}
	if want := []string{"clock", "weather"}; !slices.Equal(names, want) {
		t.Errorf("tools = %v, want %v in that order", names, want)
	}
	if got, want := captured.Tools[1].Description, weather.description; got != want {
		t.Errorf("description = %q, want %q", got, want)
	}

	// The schema arrives as JSON Schema, whatever shape the map that
	// described it had: properties as they were written, required as a
	// list, and the object type the API supplies for itself.
	var schema struct {
		Type       string `json:"type"`
		Properties struct {
			Location struct {
				Type string `json:"type"`
			} `json:"location"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(captured.Tools[1].InputSchema, &schema); err != nil {
		t.Fatalf("decoding input_schema: %v", err)
	}
	if schema.Type != "object" {
		t.Errorf("input_schema type = %q, want %q", schema.Type, "object")
	}
	if schema.Properties.Location.Type != "string" {
		t.Errorf("input_schema properties = %s, want the location the tool described", captured.Tools[1].InputSchema)
	}
	if want := []string{"location"}; !slices.Equal(schema.Required, want) {
		t.Errorf("input_schema required = %v, want %v", schema.Required, want)
	}
}

// TestParametersOffersNothingWithoutTools pins the other end: no tools at all
// is no offer, rather than an offer of none.
func TestParametersOffersNothingWithoutTools(t *testing.T) {
	for _, c := range []struct {
		name     string
		registry tools.Registry
	}{
		{name: "no registry", registry: nil},
		{name: "an empty registry", registry: definitions.New()},
	} {
		t.Run(c.name, func(t *testing.T) {
			captured, _, options := stub(t, reply(text("hola")))

			thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
			if _, err := anthropic.New("", 0, options...).Invoke(context.Background(), thread, c.registry); err != nil {
				t.Fatalf("Invoke() error = %v, want nil", err)
			}

			if len(captured.Tools) != 0 {
				t.Errorf("tools = %v, want them omitted", captured.Tools)
			}
		})
	}
}

// TestParametersCarriesTheModelAndCeiling covers the two values that are the
// adapter's rather than the thread's, in both of the shapes a caller can build
// one with. Named, they go out as named; left at their zero values, the
// package's own defaults go out instead — which is the whole of what "no
// opinion" means here, and the reason a request can never ask for no tokens.
func TestParametersCarriesTheModelAndCeiling(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		tokens  int64
		sent    string
		ceiling int64
	}{
		{
			name:    "as the caller named them",
			model:   "claude-opus-5",
			tokens:  4096,
			sent:    "claude-opus-5",
			ceiling: 4096,
		},
		{
			name:    "as the package's own where the caller named none",
			sent:    anthropic.Model,
			ceiling: anthropic.Tokens,
		},
		{
			// A ceiling nobody could have meant falls back rather
			// than going out: MaxTokens: -1 is a request the API
			// refuses and a caller cannot read the reason for.
			name:    "as the package's own where the ceiling is no ceiling",
			tokens:  -1,
			sent:    anthropic.Model,
			ceiling: anthropic.Tokens,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			captured, _, options := stub(t, reply(text("hola")))

			thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
			if _, err := anthropic.New(c.model, c.tokens, options...).Invoke(context.Background(), thread, nil); err != nil {
				t.Fatalf("Invoke() error = %v, want nil", err)
			}

			if captured.Model != c.sent {
				t.Errorf("model = %q, want %q", captured.Model, c.sent)
			}
			if captured.MaxTokens != c.ceiling {
				t.Errorf("max_tokens = %d, want %d", captured.MaxTokens, c.ceiling)
			}
		})
	}
}

func TestInvoke(t *testing.T) {
	cases := []struct {
		name    string
		blocks  []string
		replies int
		want    []string
	}{
		{
			name:    "a single text block",
			blocks:  []string{text("hola")},
			replies: 1,
			want:    []string{"text:hola"},
		},
		{
			// One reply carrying every block, in the order they
			// arrived: a response is one turn, however many things
			// the model said in it.
			name:    "several text blocks arrive in order",
			blocks:  []string{text("a"), text("b"), text("c")},
			replies: 1,
			want:    []string{"text:a", "text:b", "text:c"},
		},
		{
			// A tool call arrives whole: the id the answer has to
			// carry back, the tool's name, and the arguments as the
			// model sent them.
			name:    "a tool call arrives whole",
			blocks:  []string{text("a"), toolUse, text("b")},
			replies: 1,
			want:    []string{"text:a", `use:toolu_1:read_file:{"path":"go.mod"}`, "text:b"},
		},
		{
			// A block this mapping has no shape for is skipped, and
			// the blocks around it are unaffected.
			name:    "a block with no shape is skipped",
			blocks:  []string{text("a"), thinking, text("b")},
			replies: 1,
			want:    []string{"text:a", "text:b"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, calls, options := stub(t, reply(c.blocks...))

			thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
			replies, err := anthropic.New("", 0, options...).Invoke(context.Background(), thread, nil)
			if err != nil {
				t.Fatalf("Invoke() error = %v, want nil", err)
			}
			if len(replies) != c.replies {
				t.Errorf("replies = %d, want %d", len(replies), c.replies)
			}
			if got := contents(replies); !slices.Equal(got, c.want) {
				t.Errorf("replies said %q, want %q", got, c.want)
			}
			if *calls != 1 {
				t.Errorf("server was asked %d times, want 1", *calls)
			}
		})
	}
}

// TestInvokeReportsFailure pins the other end: a request the API refuses comes
// back as an error, rather than passing for an empty reply.
// TestInvokeRefusesANonReply pins the responses the adapter will not pass on
// as if they were answers. Each of these ends an exchange, and an exchange
// that ends because there was nothing to read has to be told apart from one
// that ends because the model was done.
func TestInvokeRefusesANonReply(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		reason string
	}{
		{
			// Half a turn, and possibly half a tool call: the
			// arguments of one cut off here stop mid-JSON and fail
			// in the tool, several turns away from the cause.
			name:   "a reply cut off at the ceiling",
			body:   stopped("max_tokens", text("as I was say")),
			reason: "cut off",
		},
		{
			name:   "a reply the model declined to give",
			body:   stopped("refusal", text("")),
			reason: "declined",
		},
		{
			name:   "a thread that no longer fits",
			body:   stopped("model_context_window_exceeded"),
			reason: "context window",
		},
		{
			// Nothing to say is no turn at all, rather than a turn
			// that says nothing.
			name:   "no blocks at all",
			body:   reply(),
			reason: "nothing to read",
		},
		{
			// A response that is nothing but blocks this mapping
			// has no shape for is likewise no reply.
			name:   "nothing but blocks with no shape",
			body:   reply(thinking),
			reason: "nothing to read",
		},
		{
			// A paused turn is the exchange still going, not the
			// end of it, and stopping quietly here would read as
			// the model having finished.
			name:   "a paused turn carrying nothing",
			body:   stopped("pause_turn"),
			reason: "nothing to read",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, calls, options := stub(t, c.body)

			thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
			replies, err := anthropic.New("", 0, options...).Invoke(context.Background(), thread, nil)
			if err == nil {
				t.Fatalf("Invoke() error = nil, want an error")
			}
			if !strings.Contains(err.Error(), c.reason) {
				t.Errorf("Invoke() error = %q, want it to mention %q", err, c.reason)
			}
			if len(replies) != 0 {
				t.Errorf("replies = %q, want none", contents(replies))
			}
			if *calls != 1 {
				t.Errorf("server was asked %d times, want 1", *calls)
			}
		})
	}
}

func TestInvokeReportsFailure(t *testing.T) {
	options := failing(t)

	thread := threads.New("", messages.New(roles.User, messages.Text("hello")))
	replies, err := anthropic.New("", 0, options...).Invoke(context.Background(), thread, nil)
	if err == nil {
		t.Fatal("Invoke() error = nil, want an error")
	}
	if len(replies) != 0 {
		t.Errorf("replies = %q, want none", contents(replies))
	}
}
