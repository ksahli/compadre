// This test is in-package rather than in help_test: what New produces writes
// to os.Stdout, and the writer it holds is unexported, which is the same seam
// invoke's test reaches through.
package help

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestExecutePrintsTheUsage(t *testing.T) {
	out := &bytes.Buffer{}
	command := &Command{out: out}

	if err := command.Execute(context.Background()); err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}

	// Every command the binary answers to has to appear, or the usage is
	// out of date with the switch that dispatches it.
	for _, want := range []string{"compadre", "invoke", "help"} {
		if got := out.String(); !strings.Contains(got, want) {
			t.Errorf("Execute() printed %q, want it to mention %q", got, want)
		}
	}
}

// TestNewIgnoresArguments pins the choice not to refuse them: help is where a
// caller who does not know what to type ends up.
func TestNewIgnoresArguments(t *testing.T) {
	command, err := New([]string{"-nope", "invoke"})
	if err != nil {
		t.Fatalf("New() error = %v, want nil", err)
	}
	if command == nil {
		t.Fatal("New() = nil, want a command")
	}
}
