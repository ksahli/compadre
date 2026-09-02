package exchanges_test

import (
	"slices"
	"testing"

	"github.com/ksahli/compadre/internal/core/exchanges"
	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
)

// said renders a thread as one line per message, so a case can state what an
// exchange is carrying without reaching inside it.
func said(thread threads.Type) []string {
	got := []string{}
	for _, message := range thread.Messages() {
		for _, content := range message.Content() {
			if text, ok := content.Text(); ok {
				got = append(got, message.Role()+":"+text)
			}
		}
	}
	return got
}

func opening() threads.Type {
	return threads.New("be brief", messages.New(roles.User, messages.Text("hello")))
}

func TestNew(t *testing.T) {
	cases := []struct {
		name     string
		exchange exchanges.Type
		id       string
		thread   []string
	}{
		{
			// An exchange that has been written down knows where.
			name:     "an exchange that has been filed",
			exchange: exchanges.New("7", opening()),
			id:       "7",
			thread:   []string{"User:hello"},
		},
		{
			// And one that has not has an empty id, which is how a
			// store tells the two apart.
			name:     "an exchange that has not been filed",
			exchange: exchanges.Open(opening()),
			id:       "",
			thread:   []string{"User:hello"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.exchange.ID(); got != c.id {
				t.Errorf("Type.ID() = %q, want %q", got, c.id)
			}
			if got := said(c.exchange.Thread()); !slices.Equal(got, c.thread) {
				t.Errorf("Type.Thread() = %v, want %v", got, c.thread)
			}
		})
	}
}

// TestWithKeepsTheID pins what [exchanges.Type.With] is for: an exchange grows
// because the thread it holds grew, and where it is filed does not change when
// it does.
func TestWithKeepsTheID(t *testing.T) {
	exchange := exchanges.New("7", opening())

	grown := exchange.With(exchange.Thread().Append(
		messages.New(roles.Assistant, messages.Text("hola"))))

	if got := grown.ID(); got != "7" {
		t.Errorf("Type.ID() = %q, want %q", got, "7")
	}
	if got, want := said(grown.Thread()), []string{"User:hello", "Assistant:hola"}; !slices.Equal(got, want) {
		t.Errorf("Type.Thread() = %v, want %v", got, want)
	}
}

// TestWithLeavesTheReceiverAlone pins the immutability the loop leans on: the
// exchange that follows is a new value, and the one it followed is untouched.
func TestWithLeavesTheReceiverAlone(t *testing.T) {
	exchange := exchanges.New("7", opening())

	exchange.With(exchange.Thread().Append(
		messages.New(roles.Assistant, messages.Text("hola"))))

	if got, want := said(exchange.Thread()), []string{"User:hello"}; !slices.Equal(got, want) {
		t.Errorf("Type.Thread() = %v, want %v", got, want)
	}
}
