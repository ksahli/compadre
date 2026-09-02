// This test is in-package rather than in invoke_test, for two reasons. What
// New produces is a Command whose fields are unexported, and there is no
// exported surface that shows what it parsed short of running the command
// against a real API; and transcribe is unexported too, being the one piece of
// behaviour this package still owns now that the loop lives in core.
package invoke

import (
	"context"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/messages"
	"github.com/ksahli/compadre/internal/core/roles"
	"github.com/ksahli/compadre/internal/core/threads"
	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

func TestNew(t *testing.T) {
	cases := []struct {
		name         string
		arguments    []string
		instructions string
		input        string
	}{
		{
			name:      "no arguments leaves both empty",
			arguments: nil,
		},
		{
			name:      "a message on its own",
			arguments: []string{"-message", "why is the sky blue?"},
			input:     "why is the sky blue?",
		},
		{
			name:         "instructions on their own",
			arguments:    []string{"-instructions", "answer in one sentence"},
			instructions: "answer in one sentence",
		},
		{
			name:         "both, in either order",
			arguments:    []string{"-message", "hello", "-instructions", "be brief"},
			instructions: "be brief",
			input:        "hello",
		},
		{
			name:      "an explicitly empty message",
			arguments: []string{"-message", ""},
			input:     "",
		},
		{
			// A message that looks like a flag is still a message: the
			// value belongs to -message, not to the flag set.
			name:      "a message that looks like a flag",
			arguments: []string{"-message", "-instructions"},
			input:     "-instructions",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := New(c.arguments)
			if err != nil {
				t.Fatalf("New() error = %v, want nil", err)
			}
			if command == nil {
				t.Fatal("New() = nil, want a command")
			}
			if command.instructions != c.instructions {
				t.Errorf("instructions = %q, want %q", command.instructions, c.instructions)
			}
			if command.input != c.input {
				t.Errorf("input = %q, want %q", command.input, c.input)
			}
		})
	}
}

// TestNewReportsHelpAsItsUsage pins the one Parse error that is not a mistake.
// The binary's own help points a caller here, so what comes back has to be the
// flags this command takes and not the flag package's word for the request.
func TestNewReportsHelpAsItsUsage(t *testing.T) {
	for _, argument := range []string{"-h", "-help"} {
		t.Run(argument, func(t *testing.T) {
			command, err := New([]string{argument})
			if err == nil {
				t.Fatal("New() error = nil, want the usage")
			}
			if command != nil {
				t.Errorf("New() = %v, want nil on error", command)
			}
			for _, want := range []string{"-instructions", "-message"} {
				if got := err.Error(); !strings.Contains(got, want) {
					t.Errorf("New() error = %q, want it to name %q", got, want)
				}
			}
		})
	}
}

// TestNewRejectsUnparseableArguments pins the half the flag set used to take
// out of the caller's hands: an argument it cannot make sense of comes back as
// an error, rather than ending the process from inside the parser.
func TestNewRejectsUnparseableArguments(t *testing.T) {
	cases := []struct {
		name      string
		arguments []string
	}{
		{"a flag nothing defines", []string{"-nope"}},
		{"a flag missing its value", []string{"-message"}},
		{"a positional where a flag belongs", []string{"-instructions", "be brief", "-nope", "x"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command, err := New(c.arguments)
			if err == nil {
				t.Fatal("New() error = nil, want an error")
			}
			if command != nil {
				t.Errorf("New() = %v, want nil on error", command)
			}
		})
	}
}

// TestExecuteRejectsAnEmptyMessage pins what happens before a provider is
// ever built: an exchange with nothing to open it is refused here, where the
// caller can be told what to pass, rather than by the API in its own words.
func TestExecuteRejectsAnEmptyMessage(t *testing.T) {
	cases := []struct {
		name  string
		input string
	}{
		{"no message at all", ""},
		{"a message that is only spaces", "   "},
		{"a message that is only a newline", "\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			command := &Command{input: c.input}

			err := command.Execute(context.Background())
			if err == nil {
				t.Fatal("Execute() error = nil, want an error")
			}
			if got := err.Error(); !strings.Contains(got, "-message") {
				t.Errorf("Execute() error = %q, want it to name %q", got, "-message")
			}
		})
	}
}

// TestTranscribe pins what a reader is shown, which is the half of the
// exchange that is any of their business. The loop hands back the whole
// thing — the turn that opened it, every call the model made and every answer
// it got — and only what the model said in words comes out of here.
func TestTranscribe(t *testing.T) {
	thread := threads.New("be brief",
		messages.New(roles.User, messages.Text("hello")),
		messages.New(roles.Assistant,
			messages.Text("let me look"),
			messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`))),
		),
		messages.New(roles.User, messages.Result(tools.Success("toolu_1", "the answer"))),
		messages.New(roles.Assistant, messages.Text("hola")),
	)

	out := &strings.Builder{}
	transcribe(out, thread)

	if got, want := out.String(), "let me look\nhola\n"; got != want {
		t.Errorf("transcribe() printed %q, want %q", got, want)
	}
}

// TestTranscribeAnExchangeWithNothingSaid pins the quiet cases. A thread the
// loop never got an answer into prints nothing at all, rather than a blank
// line standing in for one — which is what makes it safe to call on a run that
// failed before the model ever spoke.
func TestTranscribeAnExchangeWithNothingSaid(t *testing.T) {
	cases := []struct {
		name   string
		thread threads.Type
	}{
		{
			"a run that failed on its first turn",
			threads.New("", messages.New(roles.User, messages.Text("hello"))),
		},
		{
			"an exchange with no turns in it at all",
			threads.New(""),
		},
		{
			"an assistant turn that only asked for a tool",
			threads.New("",
				messages.New(roles.User, messages.Text("hello")),
				messages.New(roles.Assistant,
					messages.Use(use.New("toolu_1", "works", use.Arguments(`{}`)))),
			),
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out := &strings.Builder{}
			transcribe(out, c.thread)

			if got := out.String(); got != "" {
				t.Errorf("transcribe() printed %q, want nothing", got)
			}
		})
	}
}
