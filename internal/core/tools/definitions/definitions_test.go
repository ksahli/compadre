package definitions_test

import (
	"context"
	"slices"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
)

// stub is a definition that does nothing. These tests are about the registry,
// not about what a tool does when it runs.
type stub struct {
	name    string
	content string
}

func (s stub) Name() string           { return s.name }
func (s stub) Description() string    { return "a stub" }
func (s stub) Schema() map[string]any { return map[string]any{"type": "object"} }

func (s stub) Execute(context.Context, use.Arguments) (string, error) {
	return s.content, nil
}

// names flattens definitions to their names so a table can state a plain
// expectation about them.
func names(list []definitions.Type) []string {
	found := make([]string, 0, len(list))
	for _, definition := range list {
		found = append(found, definition.Name())
	}
	return found
}

func TestNew(t *testing.T) {
	cases := []struct {
		name     string
		registry definitions.Map
		want     []string
	}{
		{
			name:     "nothing",
			registry: definitions.New(),
			want:     []string{},
		},
		{
			name:     "one tool",
			registry: definitions.New(stub{name: "read_file"}),
			want:     []string{"read_file"},
		},
		{
			name: "several tools",
			registry: definitions.New(
				stub{name: "read_file"},
				stub{name: "bash"},
				stub{name: "write_file"},
			),
			want: []string{"bash", "read_file", "write_file"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := len(c.registry); got != len(c.want) {
				t.Errorf("len(Map) = %d, want %d", got, len(c.want))
			}
			for _, name := range c.want {
				definition, ok := c.registry[name]
				if !ok {
					t.Errorf("Map[%q] missing", name)
					continue
				}
				// A tool is keyed by the name it answers to, and
				// not by anything the caller passed alongside it.
				if got := definition.Name(); got != name {
					t.Errorf("Map[%q].Name() = %q, want %q", name, got, name)
				}
			}
		})
	}
}

// TestNewLastNameWins pins that a repeated name resolves to the definition
// given last. A registry is what the caller assembled, and the last word on a
// name is the one it meant.
func TestNewLastNameWins(t *testing.T) {
	registry := definitions.New(
		stub{name: "read_file", content: "first"},
		stub{name: "read_file", content: "second"},
	)

	if got := len(registry); got != 1 {
		t.Errorf("len(Map) = %d, want 1", got)
	}

	content, err := registry["read_file"].Execute(t.Context(), nil)
	if err != nil {
		t.Fatalf("Execute() error = %v, want nil", err)
	}
	if content != "second" {
		t.Errorf("Execute() = %q, want %q", content, "second")
	}
}

func TestList(t *testing.T) {
	cases := []struct {
		name     string
		registry definitions.Map
		want     []string
	}{
		{"empty registry", definitions.New(), []string{}},
		{"one tool", definitions.New(stub{name: "read_file"}), []string{"read_file"}},
		{
			"several tools",
			definitions.New(
				stub{name: "write_file"},
				stub{name: "read_file"},
				stub{name: "bash"},
			),
			[]string{"bash", "read_file", "write_file"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The order List returns is not part of the contract —
			// a map has none to give — so the test sorts before
			// comparing rather than pinning an order that would
			// differ from one run to the next.
			got := names(c.registry.List())
			slices.Sort(got)

			if !slices.Equal(got, c.want) {
				t.Errorf("List() = %v, want %v", got, c.want)
			}
		})
	}
}

// TestListCopiesOut pins that a caller holding the returned slice cannot
// reach back into the registry with it.
func TestListCopiesOut(t *testing.T) {
	registry := definitions.New(stub{name: "read_file"}, stub{name: "bash"})

	list := registry.List()
	list[0] = stub{name: "scribbled"}

	want := []string{"bash", "read_file"}
	got := names(registry.List())
	slices.Sort(got)

	if !slices.Equal(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}
