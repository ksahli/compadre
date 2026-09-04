package messages_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/tools/results"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

// block is what a content block is expected to answer, whichever shape it is.
// Exactly one of the flags is true for any block this package can build.
type block struct {
	text   string
	isText bool

	id        string
	name      string
	arguments string
	isUse     bool

	resultID      string
	resultContent string
	failed        bool
	isResult      bool
}

func text(said string) block {
	return block{text: said, isText: true}
}

func TestNew(t *testing.T) {
	cases := []struct {
		name    string
		message messages.Type
		role    string
		content []block
	}{
		{
			name:    "a user saying something",
			message: messages.New(roles.User, messages.Text("hi")),
			role:    roles.User,
			content: []block{text("hi")},
		},
		{
			name:    "an assistant saying something",
			message: messages.New(roles.Assistant, messages.Text("hi")),
			role:    roles.Assistant,
			content: []block{text("hi")},
		},
		{
			name:    "something empty is still something said",
			message: messages.New(roles.User, messages.Text("")),
			role:    roles.User,
			content: []block{text("")},
		},
		{
			// A turn can be nothing but a tool call: the model
			// asks without prefacing it.
			name: "a tool call on its own",
			message: messages.New(roles.Assistant,
				messages.Use(use.New("call_1", "read_file", use.Arguments(`{"path":"main.go"}`))),
			),
			role: roles.Assistant,
			content: []block{{
				id:        "call_1",
				name:      "read_file",
				arguments: `{"path":"main.go"}`,
				isUse:     true,
			}},
		},
		{
			name: "an answer to a call that ran",
			message: messages.New(roles.User,
				messages.Result(results.New("call_1", "package main", false)),
			),
			role: roles.User,
			content: []block{{
				resultID:      "call_1",
				resultContent: "package main",
				failed:        false,
				isResult:      true,
			}},
		},
		{
			name: "an answer to a call that did not",
			message: messages.New(roles.User,
				messages.Result(results.New("call_1", "no such file", true)),
			),
			role: roles.User,
			content: []block{{
				resultID:      "call_1",
				resultContent: "no such file",
				failed:        true,
				isResult:      true,
			}},
		},
		{
			// The shape the old two-string message could not hold:
			// a sentence and the call it leads to, in that order.
			name: "a sentence and the call it leads to",
			message: messages.New(roles.Assistant,
				messages.Text("let me look"),
				messages.Use(use.New("call_1", "read_file", use.Arguments(`{"path":"go.mod"}`))),
			),
			role: roles.Assistant,
			content: []block{
				text("let me look"),
				{
					id:        "call_1",
					name:      "read_file",
					arguments: `{"path":"go.mod"}`,
					isUse:     true,
				},
			},
		},
		{
			// One turn carrying every result a round of calls
			// produced, in the order they were asked for.
			name: "several results in one turn",
			message: messages.New(roles.User,
				messages.Result(results.New("call_1", "a", false)),
				messages.Result(results.New("call_2", "b", true)),
			),
			role: roles.User,
			content: []block{
				{resultID: "call_1", resultContent: "a", isResult: true},
				{resultID: "call_2", resultContent: "b", failed: true, isResult: true},
			},
		},
		{
			// A message can say nothing at all, and that is not an
			// error: it is a turn with no content the core has a
			// shape for.
			name:    "a message that says nothing",
			message: messages.New(roles.Assistant),
			role:    roles.Assistant,
			content: nil,
		},
		{
			// New does not constrain the role, and the anthropic adapter
			// leans on that: its mapping has a branch for a role it does
			// not cover, which nothing could reach otherwise.
			name:    "a role outside the constants",
			message: messages.New("Stranger", messages.Text("x")),
			role:    "Stranger",
			content: []block{text("x")},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.message.Role(); got != c.role {
				t.Errorf("Role() = %q, want %q", got, c.role)
			}
			content := c.message.Content()
			if len(content) != len(c.content) {
				t.Fatalf("len(Content()) = %d, want %d", len(content), len(c.content))
			}
			for i, want := range c.content {
				assert(t, i, content[i], want)
			}
		})
	}
}

// assert checks that a block answers true from the accessor for its own shape
// and false from the others.
func assert(t *testing.T, i int, got messages.Content, want block) {
	t.Helper()

	said, isText := got.Text()
	if isText != want.isText {
		t.Errorf("Content()[%d].Text() ok = %v, want %v", i, isText, want.isText)
	}
	if isText && said != want.text {
		t.Errorf("Content()[%d].Text() = %q, want %q", i, said, want.text)
	}
	if !isText && said != "" {
		t.Errorf("Content()[%d].Text() = %q, want %q when not text", i, said, "")
	}

	request, isUse := got.Use()
	if isUse != want.isUse {
		t.Errorf("Content()[%d].Use() ok = %v, want %v", i, isUse, want.isUse)
	}
	if isUse {
		if request.ID() != want.id {
			t.Errorf("Content()[%d].Use().ID() = %q, want %q", i, request.ID(), want.id)
		}
		if request.Name() != want.name {
			t.Errorf("Content()[%d].Use().Name() = %q, want %q", i, request.Name(), want.name)
		}
		if got := string(request.Arguments()); got != want.arguments {
			t.Errorf("Content()[%d].Use().Arguments() = %q, want %q", i, got, want.arguments)
		}
	}

	answer, isResult := got.Result()
	if isResult != want.isResult {
		t.Errorf("Content()[%d].Result() ok = %v, want %v", i, isResult, want.isResult)
	}
	if isResult {
		if answer.ID() != want.resultID {
			t.Errorf("Content()[%d].Result().ID() = %q, want %q", i, answer.ID(), want.resultID)
		}
		if answer.Content() != want.resultContent {
			t.Errorf("Content()[%d].Result().Content() = %q, want %q", i, answer.Content(), want.resultContent)
		}
		if answer.Failed() != want.failed {
			t.Errorf("Content()[%d].Result().Failed() = %v, want %v", i, answer.Failed(), want.failed)
		}
	}
}

// The content a message was built from is copied in, so a caller that keeps
// hold of the slice it passed cannot reach back into the message with it.
func TestNewCopiesItsContent(t *testing.T) {
	content := []messages.Content{messages.Text("hi")}
	message := messages.New(roles.User, content...)

	content[0] = messages.Text("tampered")

	said, ok := message.Content()[0].Text()
	if !ok || said != "hi" {
		t.Errorf("Content()[0].Text() = %q, %v, want %q, true", said, ok, "hi")
	}
}

// And it is copied out, so scribbling on what Content returns leaves the
// message as it was.
func TestContentIsACopy(t *testing.T) {
	message := messages.New(roles.User, messages.Text("hi"))

	content := message.Content()
	content[0] = messages.Text("tampered")

	said, ok := message.Content()[0].Text()
	if !ok || said != "hi" {
		t.Errorf("Content()[0].Text() = %q, %v, want %q, true", said, ok, "hi")
	}
}
