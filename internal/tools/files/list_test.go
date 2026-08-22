package files_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/tools/files"
)

// listing writes what the model would have sent. Going through JSON rather
// than a struct is the point: raw bytes are what Execute is handed.
func listing(t *testing.T, path string, recursive bool) use.Arguments {
	t.Helper()

	body := map[string]any{}
	if path != "" {
		body["path"] = path
	}
	if recursive {
		body["recursive"] = true
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("could not encode arguments: %v", err)
	}
	return raw
}

// TestListSatisfiesThePort is a compile-time claim written as a test: the tool
// is what the registry takes.
func TestListSatisfiesThePort(t *testing.T) {
	var _ definitions.Type = files.NewList(t.TempDir())
}

// TestListDefinition pins the half of a tool the model reads. The name is the
// key a use is resolved through, so it is a contract and not a label.
func TestListDefinition(t *testing.T) {
	tool := files.NewList(t.TempDir())

	if got, want := tool.Name(), "list_files"; got != want {
		t.Errorf("Name() = %q, want %q", got, want)
	}
	if tool.Description() == "" {
		t.Error("Description() is empty")
	}

	schema := tool.Schema()
	if got, want := schema["type"], "object"; got != want {
		t.Errorf("schema type = %v, want %v", got, want)
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		t.Fatalf("schema properties = %T, want map[string]any", schema["properties"])
	}
	for _, name := range []string{"path", "recursive"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("schema has no %q property", name)
		}
	}

	// The schema is data on its way to a wire format. If it cannot be
	// encoded, no request carrying this tool can be made.
	if _, err := json.Marshal(schema); err != nil {
		t.Errorf("schema does not marshal: %v", err)
	}
}

func TestListExecute(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		recursive bool
		want      string
	}{
		{
			name: "the root, one level, sorted, dotfiles shown",
			want: strings.Join([]string{
				". (9 entries)",
				".git/",
				".gitignore",
				"README.md",
				"away",
				"binary.dat",
				"empty/",
				"here",
				"internal/",
				"lines.txt",
			}, "\n"),
		},
		{
			name: "a subdirectory named by a relative path",
			path: "internal",
			want: strings.Join([]string{
				"internal (2 entries)",
				"internal/core/",
				"internal/notes.txt",
			}, "\n"),
		},
		{
			// The links are entries, not doors: 'away' points at a
			// directory outside the workspace and is listed
			// without being descended into, and neither is .git.
			name:      "recursive walks the tree but not .git or a link",
			recursive: true,
			want: strings.Join([]string{
				". (12 entries)",
				".git/",
				".gitignore",
				"README.md",
				"away",
				"binary.dat",
				"empty/",
				"here",
				"internal/",
				"internal/core/",
				"internal/core/tools.go",
				"internal/notes.txt",
				"lines.txt",
			}, "\n"),
		},
		{
			name: "an empty directory says so",
			path: "empty",
			want: strings.Join([]string{
				"empty (0 entries)",
				"the directory is empty",
			}, "\n"),
		},
		{
			name: "the root can be named the long way round",
			path: "internal/../empty",
			want: strings.Join([]string{
				"empty (0 entries)",
				"the directory is empty",
			}, "\n"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, _ := workspace(t)
			tool := files.NewList(root)

			got, err := tool.Execute(t.Context(), listing(t, c.path, c.recursive))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got != c.want {
				t.Errorf("Execute() =\n%s\n\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestListExecuteRefuses is the point of the tool. Each of these is a way out
// of the workspace, and each is told no by name rather than quietly answered
// about the root.
func TestListExecuteRefuses(t *testing.T) {
	cases := []struct {
		name      string
		path      string
		recursive bool
		absolute  bool // path is the outside dir, filled in per run
		want      string
	}{
		{
			name: "climbing out with ..",
			path: "..",
			want: "path escapes the workspace: '..'",
		},
		{
			name: "climbing out the long way",
			path: "internal/../../outside",
			want: "path escapes the workspace: 'internal/../../outside'",
		},
		{
			name:     "an absolute path",
			absolute: true,
		},
		{
			name: "a symlink pointing out of the workspace",
			path: "away",
			want: "path escapes the workspace: 'away'",
		},
		{
			name: "a symlink pointing out, walked",
			path: "away", recursive: true,
			want: "path escapes the workspace: 'away'",
		},
		{
			name: "somewhere that is not there",
			path: "nowhere",
			want: "no such path in the workspace: 'nowhere'",
		},
		{
			name: "a file rather than a directory",
			path: "README.md",
			want: "not a directory: 'README.md'",
		},
		{
			name: "a symlink to a file inside the workspace",
			path: "here",
			want: "not a directory: 'here'",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, outside := workspace(t)
			tool := files.NewList(root)

			path, want := c.path, c.want
			if c.absolute {
				path = outside
				want = fmt.Sprintf("path must be relative to the workspace, got '%s'", path)
			}

			got, err := tool.Execute(t.Context(), listing(t, path, c.recursive))
			if err == nil {
				t.Fatalf("Execute() = %q, want an error", got)
			}
			if err.Error() != want {
				t.Errorf("Execute() error = %q, want %q", err, want)
			}
			if got != "" {
				t.Errorf("Execute() = %q alongside an error, want %q", got, "")
			}
		})
	}
}

func TestListExecuteRefusesArgumentsItCannotRead(t *testing.T) {
	root, _ := workspace(t)

	_, err := files.NewList(root).Execute(t.Context(), use.Arguments(`{"path":`))
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}
	if want := "could not parse arguments: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Execute() error = %q, want it to start with %q", err, want)
	}
}

// TestListExecuteTruncates pins that a listing which hit the ceiling admits to
// it. A partial answer passed off as a whole one is worse than a short one.
func TestListExecuteTruncates(t *testing.T) {
	root := t.TempDir()
	root, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatalf("could not resolve the temp dir: %v", err)
	}

	const many = 1200
	for i := range many {
		name := filepath.Join(root, fmt.Sprintf("file-%04d", i))
		if err := os.WriteFile(name, []byte("x"), 0o644); err != nil {
			t.Fatalf("could not build the fixture: %v", err)
		}
	}

	for _, recursive := range []bool{false, true} {
		t.Run(fmt.Sprintf("recursive=%v", recursive), func(t *testing.T) {
			got, err := files.NewList(root).Execute(t.Context(), listing(t, "", recursive))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}

			lines := strings.Split(got, "\n")
			// heading + 1000 entries + the line saying so
			if want := 1 + 1000 + 1; len(lines) != want {
				t.Fatalf("Execute() wrote %d lines, want %d", len(lines), want)
			}
			if want := "listing stopped at 1000 entries"; !strings.HasPrefix(lines[len(lines)-1], want) {
				t.Errorf("last line = %q, want it to start with %q", lines[len(lines)-1], want)
			}
		})
	}
}

// TestListExecuteCancelled pins that a walk gives up when the exchange does.
func TestListExecuteCancelled(t *testing.T) {
	root, _ := workspace(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	for _, recursive := range []bool{false, true} {
		if _, err := files.NewList(root).Execute(ctx, listing(t, "", recursive)); err == nil {
			t.Errorf("recursive=%v: Execute() error = nil, want one", recursive)
		}
	}
}

// TestListThroughInvoke drives the tool the way the loop does, to pin that the
// answer carries the id of the call it answers.
func TestListThroughInvoke(t *testing.T) {
	root, _ := workspace(t)
	registry := definitions.New(files.NewList(root))

	result := tools.Invoke(t.Context(), use.New("call_1", "list_files", listing(t, "internal", false)), registry)

	if got, want := result.ID(), "call_1"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
	if result.Failed() {
		t.Errorf("Failed() = true, content %q", result.Content())
	}
	if want := "internal/notes.txt"; !strings.Contains(result.Content(), want) {
		t.Errorf("Content() = %q, want it to contain %q", result.Content(), want)
	}
}
