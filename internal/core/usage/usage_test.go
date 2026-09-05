package usage_test

import (
	"testing"

	"github.com/ksahli/compadre/internal/core/usage"
)

// TestNew pins what a count carries and that building one marks it as taken.
func TestNew(t *testing.T) {
	cases := []struct {
		name   string
		count  usage.Type
		input  int64
		output int64
	}{
		{"a turn that cost something", usage.New(1204, 318), 1204, 318},
		// A turn measured at nothing is still a turn somebody
		// measured, which is the whole reason Counted exists.
		{"a turn counted at nothing", usage.New(0, 0), 0, 0},
		// No meter counts backwards. A negative is a provider
		// mismapping its own response, and is taken as none rather
		// than carried into the record as a debt.
		{"counts that came back negative", usage.New(-3, -7), 0, 0},
		{"one of them negative", usage.New(12, -7), 12, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.count.Input(); got != c.input {
				t.Errorf("Input() = %d, want %d", got, c.input)
			}
			if got := c.count.Output(); got != c.output {
				t.Errorf("Output() = %d, want %d", got, c.output)
			}
			if !c.count.Counted() {
				t.Error("Counted() = false, want true: New builds a count somebody took")
			}
		})
	}
}

// TestZero is the case the whole type turns on: a turn nobody counted, which
// every turn said to the model rather than by it is. It has to be tellable
// from a turn counted at zero, or a reader is shown a number nobody measured.
func TestZero(t *testing.T) {
	var count usage.Type

	if count.Counted() {
		t.Error("Counted() = true, want false: the zero count was never taken")
	}
	if got := count.Input(); got != 0 {
		t.Errorf("Input() = %d, want 0", got)
	}
	if got := count.Output(); got != 0 {
		t.Errorf("Output() = %d, want 0", got)
	}
	if count == usage.New(0, 0) {
		t.Error("the zero count matches a count of zero, and the two are different facts")
	}
}
