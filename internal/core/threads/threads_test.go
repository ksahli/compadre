package threads_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
)

// content flattens a thread's messages to what they said, so a table can
// state an expectation as a plain []string. Every message here is one of
// text: what a message can hold beyond that is the messages package's to
// test, not this one's.
func content(conversation []threads.Message) []string {
	contents := make([]string, 0, len(conversation))
	for _, message := range conversation {
		said := strings.Builder{}
		for _, block := range message.Content() {
			if text, ok := block.Text(); ok {
				said.WriteString(text)
			}
		}
		contents = append(contents, said.String())
	}
	return contents
}

func TestNew(t *testing.T) {
	cases := []struct {
		name         string
		thread       threads.Type
		instructions string
		contents     []string
	}{
		{
			name:         "no messages",
			thread:       threads.New("be brief"),
			instructions: "be brief",
			contents:     nil,
		},
		{
			name:         "one message",
			thread:       threads.New("be brief", messages.New(roles.User, messages.Text("a"))),
			instructions: "be brief",
			contents:     []string{"a"},
		},
		{
			name: "several messages",
			thread: threads.New("be brief",
				messages.New(roles.User, messages.Text("a")),
				messages.New(roles.Assistant, messages.Text("b")),
				messages.New(roles.User, messages.Text("c")),
			),
			instructions: "be brief",
			contents:     []string{"a", "b", "c"},
		},
		{
			name:         "empty instructions",
			thread:       threads.New("", messages.New(roles.User, messages.Text("a"))),
			instructions: "",
			contents:     []string{"a"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.thread.Instructions(); got != c.instructions {
				t.Errorf("Instructions() = %q, want %q", got, c.instructions)
			}
			if got := c.thread.Messages(); len(got) != len(c.contents) {
				t.Fatalf("Messages() has %d messages, want %d", len(got), len(c.contents))
			}
			if got := content(c.thread.Messages()); !slices.Equal(got, c.contents) {
				t.Errorf("Messages() = %q, want %q", got, c.contents)
			}
		})
	}
}

func TestAppend(t *testing.T) {
	cases := []struct {
		name     string
		thread   threads.Type
		contents []string
	}{
		{
			name:     "onto an empty thread",
			thread:   threads.New("be brief").Append(messages.New(roles.User, messages.Text("a"))),
			contents: []string{"a"},
		},
		{
			name: "one message",
			thread: threads.New("be brief", messages.New(roles.User, messages.Text("a"))).
				Append(messages.New(roles.Assistant, messages.Text("b"))),
			contents: []string{"a", "b"},
		},
		{
			name: "several messages keep their order",
			thread: threads.New("be brief", messages.New(roles.User, messages.Text("a"))).
				Append(messages.New(roles.Assistant, messages.Text("b")), messages.New(roles.User, messages.Text("c"))),
			contents: []string{"a", "b", "c"},
		},
		{
			name:     "nothing",
			thread:   threads.New("be brief", messages.New(roles.User, messages.Text("a"))).Append(),
			contents: []string{"a"},
		},
		{
			name: "chained",
			thread: threads.New("be brief", messages.New(roles.User, messages.Text("a"))).
				Append(messages.New(roles.Assistant, messages.Text("b"))).
				Append(messages.New(roles.User, messages.Text("c"))),
			contents: []string{"a", "b", "c"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := content(c.thread.Messages()); !slices.Equal(got, c.contents) {
				t.Errorf("Messages() = %q, want %q", got, c.contents)
			}
			// The instructions belong to the thread, not to a message, so
			// they survive every append.
			if got := c.thread.Instructions(); got != "be brief" {
				t.Errorf("Instructions() = %q, want %q", got, "be brief")
			}
		})
	}
}

// TestAppendKeepsRoles guards the half a table of contents cannot see: an
// appended message arrives with the role it was built with.
func TestAppendKeepsRoles(t *testing.T) {
	thread := threads.New("be brief", messages.New(roles.User, messages.Text("a"))).
		Append(messages.New(roles.Assistant, messages.Text("b")))
	conversation := thread.Messages()
	if len(conversation) != 2 {
		t.Fatalf("Messages() has %d messages, want 2", len(conversation))
	}
	if got := conversation[0].Role(); got != roles.User {
		t.Errorf("Messages()[0].Role() = %q, want %q", got, roles.User)
	}
	if got := conversation[1].Role(); got != roles.Assistant {
		t.Errorf("Messages()[1].Role() = %q, want %q", got, roles.Assistant)
	}
}

// TestAppendLeavesReceiverUnchanged states the immutability claim on its own.
// The two appends from a single receiver are the case that catches a future
// append onto a shared backing array.
func TestAppendLeavesReceiverUnchanged(t *testing.T) {
	original := threads.New("be brief", messages.New(roles.User, messages.Text("a")))

	next := original.Append(messages.New(roles.Assistant, messages.Text("b")))
	if got := content(original.Messages()); !slices.Equal(got, []string{"a"}) {
		t.Errorf("original Messages() = %q, want %q", got, []string{"a"})
	}
	if got := content(next.Messages()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("next Messages() = %q, want %q", got, []string{"a", "b"})
	}

	one := original.Append(messages.New(roles.Assistant, messages.Text("one")))
	two := original.Append(messages.New(roles.Assistant, messages.Text("two")))
	if got := content(one.Messages()); !slices.Equal(got, []string{"a", "one"}) {
		t.Errorf("one Messages() = %q, want %q", got, []string{"a", "one"})
	}
	if got := content(two.Messages()); !slices.Equal(got, []string{"a", "two"}) {
		t.Errorf("two Messages() = %q, want %q", got, []string{"a", "two"})
	}
}

// TestNewCopiesConversation pins the entry half of immutability: a caller that
// keeps hold of the slice it passed in cannot reach into the thread with it.
func TestNewCopiesConversation(t *testing.T) {
	conversation := []threads.Message{
		messages.New(roles.User, messages.Text("a")),
		messages.New(roles.Assistant, messages.Text("b")),
	}
	thread := threads.New("be brief", conversation...)

	conversation[0] = messages.New(roles.User, messages.Text("tampered"))

	want := []string{"a", "b"}
	if got := content(thread.Messages()); !slices.Equal(got, want) {
		t.Errorf("Messages() = %q, want %q", got, want)
	}
}

// TestMessagesCopiesOut pins the exit half: what Messages hands back is the
// caller's to scribble on.
func TestMessagesCopiesOut(t *testing.T) {
	thread := threads.New("be brief",
		messages.New(roles.User, messages.Text("a")),
		messages.New(roles.Assistant, messages.Text("b")),
	)

	thread.Messages()[0] = messages.New(roles.User, messages.Text("tampered"))

	want := []string{"a", "b"}
	if got := content(thread.Messages()); !slices.Equal(got, want) {
		t.Errorf("Messages() = %q, want %q", got, want)
	}
}
