package commands_test

import (
	"strings"
	"testing"

	"github.com/ksahli/compadre/commands"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"invoke", []string{"invoke", "-message", "hello"}},
		{"invoke with more of its own flags", []string{"invoke", "-message", "hello", "-usage"}},
		{"interact", []string{"interact"}},
		{"interact with its own flags", []string{"interact", "-usage"}},
		{"help", []string{"help"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := commands.New(c.arguments)
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if command == nil {
				t.Error("New() = nil, want a command")
			}
		})
	}
}

// TestNewRejectsUnknownNames pins the half that matters more: an unrecognised
// name is an error rather than a silent fallback to some default command, and
// the error says which name was not recognised.
func TestNewRejectsUnknownNames(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"a name nothing owns", []string{"summon"}},
		{"an empty name", []string{""}},
		{"a flag where a name belongs", []string{"-message"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := commands.New(c.arguments)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if command != nil {
				t.Errorf("New() = %v, want nil on error", command)
			}
			if got := c.arguments[0]; !strings.Contains(err.Error(), got) {
				t.Errorf("New() error = %q, want it to name %q", err, got)
			}
		})
	}
}

// TestNewRejectsNoArguments pins the precondition this package holds for
// itself: the caller having checked the length first is not something the
// switch below is entitled to assume.
func TestNewRejectsNoArguments(t *testing.T) {
	command, err := commands.New(nil)
	if err == nil {
		t.Fatal("New() error = nil, want an error")
	}
	if command != nil {
		t.Errorf("New() = %v, want nil on error", command)
	}
	if got := err.Error(); !strings.Contains(got, "missing command") {
		t.Errorf("New() error = %q, want it to mention %q", got, "missing command")
	}
}
