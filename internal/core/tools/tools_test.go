package tools_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

// stub is a tool that does whatever the test needs it to. It records the
// arguments it was handed so a test can pin that the bytes arrive unchanged.
type stub struct {
	name    string
	content string
	err     error
	seen    *use.Arguments
}

func (s stub) Name() string           { return s.name }
func (s stub) Description() string    { return "a stub" }
func (s stub) Schema() map[string]any { return map[string]any{"type": "object"} }

func (s stub) Execute(_ context.Context, arguments use.Arguments) (string, error) {
	if s.seen != nil {
		*s.seen = arguments
	}
	if s.err != nil {
		return "", s.err
	}
	return s.content, nil
}

func TestInvoke(t *testing.T) {
	registry := definitions.New(
		stub{name: "read_file", content: "the file"},
		stub{name: "broken", err: errors.New("could not parse arguments")},
	)

	cases := []struct {
		name    string
		use     tools.Use
		content string
		failed  bool
	}{
		{
			name:    "a tool that runs",
			use:     use.New("call_1", "read_file", nil),
			content: "the file",
			failed:  false,
		},
		{
			// A tool that fails is an answer the model reads, not an
			// error the program returns.
			name:    "a tool that fails",
			use:     use.New("call_2", "broken", nil),
			content: "could not parse arguments",
			failed:  true,
		},
		{
			// A name the registry does not carry is reported rather
			// than guessed at, and the model is told which name it was.
			name:    "a tool the registry does not have",
			use:     use.New("call_3", "bash", nil),
			content: "unknown tool: 'bash'",
			failed:  true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			result := tools.Invoke(t.Context(), c.use, registry)

			// The result has to name the call it answers: the tool
			// that ran never saw the id, so Invoke is what pairs them.
			if got := result.ID(); got != c.use.ID() {
				t.Errorf("Result.ID() = %q, want %q", got, c.use.ID())
			}
			if got := result.Content(); got != c.content {
				t.Errorf("Result.Content() = %q, want %q", got, c.content)
			}
			if got := result.Failed(); got != c.failed {
				t.Errorf("Result.Failed() = %v, want %v", got, c.failed)
			}
		})
	}
}

// TestInvokeOnEmptyRegistry pins that a registry with nothing in it answers
// rather than panics. Nothing guarantees a caller assembled one.
func TestInvokeOnEmptyRegistry(t *testing.T) {
	result := tools.Invoke(t.Context(), use.New("call_1", "read_file", nil), nil)

	if !result.Failed() {
		t.Errorf("Result.Failed() = false, want true")
	}
	if got, want := result.Content(), "unknown tool: 'read_file'"; got != want {
		t.Errorf("Result.Content() = %q, want %q", got, want)
	}
}

// TestInvokePassesArgumentsThrough pins that the arguments reach the tool
// exactly as the model sent them. The core does not parse them, so it must
// not reshape them either: a tool unmarshals what it was given.
func TestInvokePassesArgumentsThrough(t *testing.T) {
	var seen use.Arguments
	arguments := use.Arguments(`{"path":"main.go","offset":10}`)

	registry := definitions.New(stub{name: "read_file", seen: &seen})
	tools.Invoke(t.Context(), use.New("call_1", "read_file", arguments), registry)

	if string(seen) != string(arguments) {
		t.Errorf("Execute() arguments = %q, want %q", seen, arguments)
	}
}

func TestSuccessAndFailure(t *testing.T) {
	cases := []struct {
		name    string
		result  tools.Result
		content string
		failed  bool
	}{
		{"success", tools.Success("call_1", "the file"), "the file", false},
		{
			// Failure carries the error's text and not the error:
			// the model is the one that has to read it.
			"failure",
			tools.Failure("call_1", errors.New("no such file")),
			"no such file",
			true,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.result.ID(); got != "call_1" {
				t.Errorf("Result.ID() = %q, want %q", got, "call_1")
			}
			if got := c.result.Content(); got != c.content {
				t.Errorf("Result.Content() = %q, want %q", got, c.content)
			}
			if got := c.result.Failed(); got != c.failed {
				t.Errorf("Result.Failed() = %v, want %v", got, c.failed)
			}
		})
	}
}

// TestAliases pins that the names re-exported here are the subpackages' own
// types and not copies of them. A caller builds a Use through use.New and
// hands it to Invoke as a tools.Use; if these ever drift apart that stops
// compiling, which is the whole point of the aliases.
func TestAliases(t *testing.T) {
	var (
		_ tools.Use        = use.New("call_1", "read_file", json.RawMessage(`{}`))
		_ tools.Definition = stub{name: "read_file"}
		_ tools.Registry   = definitions.New(stub{name: "read_file"})
		_ tools.Result     = tools.Success("call_1", "the file")
	)
}
