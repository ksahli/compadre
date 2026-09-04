package thoughts_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/thoughts"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name      string
		thought   thoughts.Type
		text      string
		signature string
		data      string
		redacted  bool
	}{
		{
			name:      "reasoning as words",
			thought:   thoughts.New("hmm", "sig"),
			text:      "hmm",
			signature: "sig",
		},
		{
			// The shape an API keeping its reasoning to itself
			// hands back: the signature is the whole of it, and
			// that is a thought rather than the absence of one.
			name:      "reasoning with nothing written in it",
			thought:   thoughts.New("", "sig"),
			signature: "sig",
		},
		{
			// A redacted thought has no words and no signature of
			// its own: the blob is what there is to carry.
			name:     "reasoning that was withheld",
			thought:  thoughts.Redacted("blob"),
			data:     "blob",
			redacted: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.thought.Text(); got != c.text {
				t.Errorf("Text() = %q, want %q", got, c.text)
			}
			if got := c.thought.Signature(); got != c.signature {
				t.Errorf("Signature() = %q, want %q", got, c.signature)
			}
			data, redacted := c.thought.Data()
			if redacted != c.redacted {
				t.Errorf("Data() redacted = %v, want %v", redacted, c.redacted)
			}
			if data != c.data {
				t.Errorf("Data() = %q, want %q", data, c.data)
			}
		})
	}
}
