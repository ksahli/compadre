package results_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/tools/results"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name    string
		result  results.Type
		id      string
		content string
		failed  bool
	}{
		{
			name:    "an answer",
			result:  results.New("call_1", "the file", false),
			id:      "call_1",
			content: "the file",
			failed:  false,
		},
		{
			// A failure is content like any other. What marks it is
			// the flag, not the absence of something to read.
			name:    "an explanation of a failure",
			result:  results.New("call_2", "no such file", true),
			id:      "call_2",
			content: "no such file",
			failed:  true,
		},
		{
			// A tool that succeeded with nothing to say is still a
			// success: empty content is not a failure.
			name:    "an answer with nothing in it",
			result:  results.New("call_3", "", false),
			id:      "call_3",
			content: "",
			failed:  false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.result.ID(); got != c.id {
				t.Errorf("Type.ID() = %q, want %q", got, c.id)
			}
			if got := c.result.Content(); got != c.content {
				t.Errorf("Type.Content() = %q, want %q", got, c.content)
			}
			if got := c.result.Failed(); got != c.failed {
				t.Errorf("Type.Failed() = %v, want %v", got, c.failed)
			}
		})
	}
}
