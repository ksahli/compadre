package files_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/tools/files"
)

// writing writes what the model would have sent. Going through JSON rather
// than a struct is the point: raw bytes are what Execute is handed.
func writing(t *testing.T, path, content string) use.Arguments {
	t.Helper()

	raw, err := json.Marshal(map[string]any{"path": path, "content": content})
	if err != nil {
		t.Fatalf("could not encode arguments: %v", err)
	}
	return raw
}

// contents reads a file back the plain way, to check what the tool actually
// laid down rather than what it said it did.
func contents(t *testing.T, name string) string {
	t.Helper()

	raw, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("could not read back %s: %v", name, err)
	}
	return string(raw)
}

// TestWriteSatisfiesThePort is a compile-time claim written as a test: the
// tool is what the registry takes.
func TestWriteSatisfiesThePort(t *testing.T) {
	var _ definitions.Type = files.NewWrite(t.TempDir())
}

// TestWriteDefinition pins the half of a tool the model reads. The name is the
// key a use is resolved through, so it is a contract and not a label.
func TestWriteDefinition(t *testing.T) {
	tool := files.NewWrite(t.TempDir())

	if got, want := tool.Name(), "write_file"; got != want {
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
	for _, name := range []string{"path", "content"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("schema has no %q property", name)
		}
	}

	// Neither half of a write has a sensible default: there is no file to
	// guess at and no content to invent, so both are required and the
	// schema has to say so.
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema required = %T, want []string", schema["required"])
	}
	if len(required) != 2 || required[0] != "path" || required[1] != "content" {
		t.Errorf("schema required = %v, want [path content]", required)
	}

	// The schema is data on its way to a wire format. If it cannot be
	// encoded, the model never hears about the tool at all.
	if _, err := json.Marshal(schema); err != nil {
		t.Errorf("could not encode the schema: %v", err)
	}
}

// TestWriteExecute pins both halves of a write: what the model is told, and
// what is actually on disk afterwards. The second is the one that matters —
// a report is only worth what it describes.
func TestWriteExecute(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		content string
		at      string // where the bytes should land, relative to the root
		want    string
	}{
		{
			name:    "a file at the root",
			path:    "hello.go",
			content: "package main\n",
			at:      "hello.go",
			want:    "hello.go (created, 1 line, 13 bytes)",
		},
		{
			name:    "a file in a directory that already exists",
			path:    "internal/core/new.go",
			content: "one\ntwo\n",
			at:      "internal/core/new.go",
			want:    "internal/core/new.go (created, 2 lines, 8 bytes)",
		},
		{
			// The directories on the way are made, which is the
			// one thing this tool does beyond the file itself.
			name:    "a file under directories that do not exist yet",
			path:    "a/b/c/deep.txt",
			content: "here\n",
			at:      "a/b/c/deep.txt",
			want:    "a/b/c/deep.txt (created, 1 line, 5 bytes)",
		},
		{
			// A last line with nothing after it is still a line.
			name:    "content with no trailing newline",
			path:    "bare.txt",
			content: "one\ntwo",
			at:      "bare.txt",
			want:    "bare.txt (created, 2 lines, 7 bytes)",
		},
		{
			// An empty file is a real thing to want, and the
			// report says which kind of nothing it is.
			name: "an empty file",
			path: "blank.txt",
			at:   "blank.txt",
			want: "blank.txt (created, empty)",
		},
		{
			name:    "a path spelled the long way round",
			path:    "internal/../round.txt",
			content: "x",
			at:      "round.txt",
			want:    "round.txt (created, 1 line, 1 byte)",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, _ := workspace(t)
			tool := files.NewWrite(root)

			got, err := tool.Execute(t.Context(), writing(t, c.path, c.content))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got != c.want {
				t.Errorf("Execute() = %q, want %q", got, c.want)
			}
			if got := contents(t, filepath.Join(root, c.at)); got != c.content {
				t.Errorf("the file holds %q, want %q", got, c.content)
			}
		})
	}
}

// TestWriteExecuteRefuses is the point of the tool. The first four are ways
// out of the workspace, and the rest are paths this tool has no business
// laying bytes down on. Each is told no by name rather than quietly written
// somewhere else.
func TestWriteExecuteRefuses(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		content  string
		absolute bool // path is the outside directory, filled in per run
		want     string
	}{
		{
			name: "climbing out with ..",
			path: "../escape.txt",
			want: "path escapes the workspace: '../escape.txt'",
		},
		{
			name: "climbing out the long way",
			path: "internal/../../escape.txt",
			want: "path escapes the workspace: 'internal/../../escape.txt'",
		},
		{
			// The link exists and points out, so the walk resolves
			// it and the judgement is made on where it lands.
			name: "writing through a symlink pointing out",
			path: "away/planted.txt",
			want: "path escapes the workspace: 'away/planted.txt'",
		},
		{
			name:     "an absolute path",
			absolute: true,
		},
		{
			name: "a directory",
			path: "internal",
			want: "already exists, and this tool does not replace: 'internal'",
		},
		{
			name: "a file that is already there",
			path: "README.md",
			want: "already exists, and this tool does not replace: 'README.md'",
		},
		{
			// The link is what would be written through, so it
			// counts as existing even though it is not the file.
			name: "a symlink that is already there",
			path: "here",
			want: "already exists, and this tool does not replace: 'here'",
		},
		{
			name: "inside .git",
			path: ".git/hooks/pre-commit",
			want: "the contents of .git are not writable: '.git/hooks/pre-commit'",
		},
		{
			name: ".git itself",
			path: ".git",
			want: "already exists, and this tool does not replace: '.git'",
		},
		{
			name: "a path hung off a file",
			path: "README.md/nested.txt",
			want: "not a directory: 'README.md'",
		},
		{
			name: "no path at all",
			path: "   ",
			want: "path is required",
		},
		{
			name:    "content that is not text",
			path:    "binary.out",
			content: "PNG\x00\x1a",
			want:    "content is not text, and this tool writes text files",
		},
		{
			name:    "content past the ceiling",
			path:    "huge.txt",
			content: strings.Repeat("x", (256<<10)+1),
			want:    "content is 262145 bytes, which is more than the 262144 this tool will write",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, outside := workspace(t)
			tool := files.NewWrite(root)

			path, want := c.path, c.want
			if c.absolute {
				path = filepath.Join(outside, "planted.txt")
				want = "path must be relative to the workspace, got '" + path + "'"
			}

			_, err := tool.Execute(t.Context(), writing(t, path, c.content))
			if err == nil {
				t.Fatalf("Execute() error = nil, want %q", want)
			}
			if got := err.Error(); got != want {
				t.Errorf("Execute() error = %q, want %q", got, want)
			}
		})
	}
}

// TestWriteExecuteRefusalsLeaveNothingBehind is the other half of a refusal.
// Saying no is worth nothing if the tree changed on the way to saying it: a
// directory made for a write that never happened is a change nobody asked for.
func TestWriteExecuteRefusalsLeaveNothingBehind(t *testing.T) {
	cases := []struct {
		name    string
		path    string
		content string
		absent  string // must not exist under the root afterwards
	}{
		{
			name:   "a refused path makes no directories on the way",
			path:   ".git/hooks/pre-commit",
			absent: ".git/hooks",
		},
		{
			name:    "content past the ceiling makes nothing",
			path:    "deep/nested/huge.txt",
			content: strings.Repeat("x", (256<<10)+1),
			absent:  "deep",
		},
		{
			name:    "content that is not text makes nothing",
			path:    "deep/nested/binary.out",
			content: "PNG\x00",
			absent:  "deep",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, _ := workspace(t)

			if _, err := files.NewWrite(root).Execute(t.Context(), writing(t, c.path, c.content)); err == nil {
				t.Fatal("Execute() error = nil, want one")
			}

			if _, err := os.Lstat(filepath.Join(root, c.absent)); err == nil {
				t.Errorf("%s exists, and the write was refused", c.absent)
			}
		})
	}
}

// TestWriteExecuteDoesNotReplace pins the tool's one policy end to end: a
// second write to the same path is refused, and the first one's bytes are
// still there afterwards.
func TestWriteExecuteDoesNotReplace(t *testing.T) {
	root, _ := workspace(t)
	tool := files.NewWrite(root)

	if _, err := tool.Execute(t.Context(), writing(t, "once.txt", "first\n")); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	_, err := tool.Execute(t.Context(), writing(t, "once.txt", "second\n"))
	if err == nil {
		t.Fatal("Execute() error = nil, want one: the file was replaced")
	}
	if got, want := err.Error(), "already exists, and this tool does not replace: 'once.txt'"; got != want {
		t.Errorf("Execute() error = %q, want %q", got, want)
	}
	if got, want := contents(t, filepath.Join(root, "once.txt")), "first\n"; got != want {
		t.Errorf("the file holds %q, want %q", got, want)
	}
}

// TestWriteExecuteRefusesArgumentsItCannotRead pins that arguments which do
// not parse are the model's mistake to read, not a crash.
func TestWriteExecuteRefusesArgumentsItCannotRead(t *testing.T) {
	root, _ := workspace(t)

	_, err := files.NewWrite(root).Execute(t.Context(), use.Arguments(`{"path":`))
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}
	if want := "could not parse arguments: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Execute() error = %q, want it to start with %q", err, want)
	}
}

// TestWriteExecuteCancelled pins that a write gives up when the exchange does,
// and that nothing is left behind when it does.
func TestWriteExecuteCancelled(t *testing.T) {
	root, _ := workspace(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := files.NewWrite(root).Execute(ctx, writing(t, "late.txt", "x")); err == nil {
		t.Error("Execute() error = nil, want one")
	}
	if _, err := os.Lstat(filepath.Join(root, "late.txt")); err == nil {
		t.Error("late.txt exists, and the write was cancelled")
	}
}

// TestWriteThroughInvoke drives the tool the way the loop does, to pin that
// the answer carries the id of the call it answers.
func TestWriteThroughInvoke(t *testing.T) {
	root, _ := workspace(t)
	registry := definitions.New(files.NewWrite(root))

	result := tools.Invoke(t.Context(), use.New("call_1", "write_file", writing(t, "made.txt", "x\n")), registry)

	if got, want := result.ID(), "call_1"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
	if result.Failed() {
		t.Errorf("Failed() = true, content %q", result.Content())
	}
	if want := "made.txt (created, 1 line, 2 bytes)"; result.Content() != want {
		t.Errorf("Content() = %q, want %q", result.Content(), want)
	}
}
