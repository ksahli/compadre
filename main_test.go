package main

import (
	"bytes"
	"strings"
	"testing"
)

// TestRunReportsFailures covers the two ways the binary can refuse before a
// command ever runs. The success path is not here: the only command is invoke,
// and running it would reach the real API.
func TestRunReportsFailures(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
		reported  string
	}{
		{
			name:      "no command at all",
			arguments: nil,
			reported:  "missing command",
		},
		{
			// The name is echoed back so that a typo is visible in the
			// report rather than leaving the caller to guess.
			name:      "a name nothing owns",
			arguments: []string{"summon"},
			reported:  "summon",
		},
		{
			name:      "an argument invoke cannot parse",
			arguments: []string{"invoke", "-nope"},
			reported:  "-nope",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			stderr := &bytes.Buffer{}

			if got := run(c.arguments, stderr); got != 1 {
				t.Errorf("run() = %d, want 1", got)
			}
			if got := stderr.String(); !strings.Contains(got, c.reported) {
				t.Errorf("run() reported %q, want it to mention %q", got, c.reported)
			}
		})
	}
}
