package files_test

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ksahli/compadre/internal/core/tools"
	"github.com/ksahli/compadre/internal/core/tools/definitions"
	"github.com/ksahli/compadre/internal/core/tools/use"
	"github.com/ksahli/compadre/internal/tools/files"
)

// reading writes what the model would have sent. Going through JSON rather
// than a struct is the point: raw bytes are what Execute is handed.
func reading(t *testing.T, path string, offset, limit int) use.Arguments {
	t.Helper()

	body := map[string]any{"path": path}
	if offset != 0 {
		body["offset"] = offset
	}
	if limit != 0 {
		body["limit"] = limit
	}

	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("could not encode arguments: %v", err)
	}
	return raw
}

// TestReadSatisfiesThePort is a compile-time claim written as a test: the tool
// is what the registry takes.
func TestReadSatisfiesThePort(t *testing.T) {
	var _ definitions.Type = files.NewRead(t.TempDir())
}

// TestReadDefinition pins the half of a tool the model reads. The name is the
// key a use is resolved through, so it is a contract and not a label.
func TestReadDefinition(t *testing.T) {
	tool := files.NewRead(t.TempDir())

	if got, want := tool.Name(), "read_file"; got != want {
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
	for _, name := range []string{"path", "offset", "limit"} {
		if _, ok := properties[name]; !ok {
			t.Errorf("schema has no %q property", name)
		}
	}

	// Unlike the listing, this tool cannot answer without being told
	// where, so the path is required and the schema has to say so.
	required, ok := schema["required"].([]string)
	if !ok {
		t.Fatalf("schema required = %T, want []string", schema["required"])
	}
	if len(required) != 1 || required[0] != "path" {
		t.Errorf("schema required = %v, want [path]", required)
	}

	// The schema is data on its way to a wire format. If it cannot be
	// encoded, no request carrying this tool can be made.
	if _, err := json.Marshal(schema); err != nil {
		t.Errorf("schema does not marshal: %v", err)
	}
}

func TestReadExecute(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		offset, limit int
		want          string
	}{
		{
			name: "a file at the root, whole, numbered",
			path: "lines.txt",
			want: strings.Join([]string{
				"lines.txt (lines 1-5 of 5)",
				"1  one",
				"2  two",
				"3  three",
				"4  four",
				"5  five",
			}, "\n"),
		},
		{
			name: "a file named by a relative path",
			path: "internal/core/tools.go",
			want: strings.Join([]string{
				"internal/core/tools.go (lines 1-1 of 1)",
				"1  x",
			}, "\n"),
		},
		{
			// The window stopping short is not the end of the
			// story, so the answer says which line to ask from.
			name: "a window in the middle says where to ask again",
			path: "lines.txt", offset: 2, limit: 2,
			want: strings.Join([]string{
				"lines.txt (lines 2-3 of 5)",
				"2  two",
				"3  three",
				"stopped at line 3; ask again with offset 4 to see more",
			}, "\n"),
		},
		{
			name: "a window running to the end says nothing more",
			path: "lines.txt", offset: 4,
			want: strings.Join([]string{
				"lines.txt (lines 4-5 of 5)",
				"4  four",
				"5  five",
			}, "\n"),
		},
		{
			name: "an offset past the end says how long the file is",
			path: "lines.txt", offset: 10,
			want: strings.Join([]string{
				"lines.txt",
				"offset 10 is past the end of the file, which has 5 lines",
			}, "\n"),
		},
		{
			// A link inside the workspace is a path like any
			// other, and the answer names what it actually read.
			name: "a symlink to a file inside the workspace",
			path: "here",
			want: strings.Join([]string{
				"README.md (lines 1-1 of 1)",
				"1  x",
			}, "\n"),
		},
		{
			name: "a file named the long way round",
			path: "internal/../lines.txt", limit: 1,
			want: strings.Join([]string{
				"lines.txt (lines 1-1 of 5)",
				"1  one",
				"stopped at line 1; ask again with offset 2 to see more",
			}, "\n"),
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, _ := workspace(t)
			tool := files.NewRead(root)

			got, err := tool.Execute(t.Context(), reading(t, c.path, c.offset, c.limit))
			if err != nil {
				t.Fatalf("Execute() error = %v", err)
			}
			if got != c.want {
				t.Errorf("Execute() =\n%s\n\nwant\n%s", got, c.want)
			}
		})
	}
}

// TestReadExecuteRefuses is the point of the tool. The first four are ways out
// of the workspace, and the rest are files this tool has no business handing
// back. Each is told no by name rather than quietly answered about something
// else.
func TestReadExecuteRefuses(t *testing.T) {
	cases := []struct {
		name          string
		path          string
		offset, limit int
		absolute      bool // path is the outside file, filled in per run
		want          string
	}{
		{
			name: "climbing out with ..",
			path: "../outside/secret/keys.txt",
			want: "path escapes the workspace: '../outside/secret/keys.txt'",
		},
		{
			name: "climbing out the long way",
			path: "internal/../../outside/secret/keys.txt",
			want: "path escapes the workspace: 'internal/../../outside/secret/keys.txt'",
		},
		{
			name:     "an absolute path",
			absolute: true,
		},
		{
			name: "through a symlink pointing out of the workspace",
			path: "away/keys.txt",
			want: "path escapes the workspace: 'away/keys.txt'",
		},
		{
			name: "a directory rather than a file",
			path: "internal",
			want: "not a file: 'internal'",
		},
		{
			name: "the .git directory itself",
			path: ".git",
			want: "not a file: '.git'",
		},
		{
			name: "a file inside .git",
			path: ".git/HEAD",
			want: "the contents of .git are not readable: '.git/HEAD'",
		},
		{
			name: "a file inside .git, named the long way round",
			path: "internal/../.git/HEAD",
			want: "the contents of .git are not readable: 'internal/../.git/HEAD'",
		},
		{
			name: "something that is not there",
			path: "nowhere.txt",
			want: "no such path in the workspace: 'nowhere.txt'",
		},
		{
			name: "a file that is not text",
			path: "binary.dat",
			want: "not a text file: 'binary.dat'",
		},
		{
			name: "no path at all",
			path: "  ",
			want: "path is required",
		},
		{
			name: "an offset before the first line",
			path: "lines.txt", offset: -1,
			want: "offset must be 1 or more, got -1",
		},
		{
			name: "a negative limit",
			path: "lines.txt", limit: -3,
			want: "limit must be 1 or more, got -3",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			root, outside := workspace(t)
			tool := files.NewRead(root)

			path, want := c.path, c.want
			if c.absolute {
				path = filepath.Join(outside, "secret", "keys.txt")
				want = fmt.Sprintf("path must be relative to the workspace, got '%s'", path)
			}

			got, err := tool.Execute(t.Context(), reading(t, path, c.offset, c.limit))
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

func TestReadExecuteRefusesArgumentsItCannotRead(t *testing.T) {
	root, _ := workspace(t)

	_, err := files.NewRead(root).Execute(t.Context(), use.Arguments(`{"path":`))
	if err == nil {
		t.Fatal("Execute() error = nil, want an error")
	}
	if want := "could not parse arguments: "; !strings.HasPrefix(err.Error(), want) {
		t.Errorf("Execute() error = %q, want it to start with %q", err, want)
	}
}

// TestReadExecuteEmptyFile pins that a file with nothing in it says so, rather
// than coming back as a heading with silence under it.
func TestReadExecuteEmptyFile(t *testing.T) {
	root, _ := workspace(t)
	write(t, filepath.Join(root, "blank.txt"), nil)

	got, err := files.NewRead(root).Execute(t.Context(), reading(t, "blank.txt", 0, 0))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	want := "blank.txt\nthe file is empty"
	if got != want {
		t.Errorf("Execute() =\n%s\n\nwant\n%s", got, want)
	}
}

// TestReadExecuteStopsAtTheLineCeiling pins that a read which hit the line
// ceiling admits to it and names the line to ask from. A partial answer passed
// off as a whole one is worse than a short one.
func TestReadExecuteStopsAtTheLineCeiling(t *testing.T) {
	root, _ := workspace(t)

	const many = 2500
	var content strings.Builder
	for i := 1; i <= many; i++ {
		fmt.Fprintf(&content, "line %d\n", i)
	}
	write(t, filepath.Join(root, "long.txt"), []byte(content.String()))

	got, err := files.NewRead(root).Execute(t.Context(), reading(t, "long.txt", 0, 0))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	lines := strings.Split(got, "\n")
	// heading + 2000 lines + the line saying so
	if want := 1 + 2000 + 1; len(lines) != want {
		t.Fatalf("Execute() wrote %d lines, want %d", len(lines), want)
	}
	if want := fmt.Sprintf("long.txt (lines 1-2000 of %d)", many); lines[0] != want {
		t.Errorf("heading = %q, want %q", lines[0], want)
	}
	if want := "stopped at line 2000; ask again with offset 2001 to see more"; lines[len(lines)-1] != want {
		t.Errorf("last line = %q, want %q", lines[len(lines)-1], want)
	}
}

// TestReadExecuteStopsAtTheByteCeiling pins the other ceiling, and that a read
// which hit it does not claim to know how long the file is: a total that is
// really a floor would be a wrong answer rather than a missing one.
func TestReadExecuteStopsAtTheByteCeiling(t *testing.T) {
	root, _ := workspace(t)

	// One line, longer than the ceiling: nothing about it can be answered
	// whole, and there is no later line to point the model at.
	write(t, filepath.Join(root, "huge.txt"), []byte(strings.Repeat("x", 300<<10)))

	got, err := files.NewRead(root).Execute(t.Context(), reading(t, "huge.txt", 0, 0))
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	lines := strings.Split(got, "\n")
	if want := "huge.txt (lines 1-1)"; lines[0] != want {
		t.Errorf("heading = %q, want %q", lines[0], want)
	}
	if want := "the file was cut off at 262144 bytes"; !strings.HasPrefix(lines[len(lines)-1], want) {
		t.Errorf("last line = %q, want it to start with %q", lines[len(lines)-1], want)
	}
	// The line came back through the ceiling, not whole: '1  ' and then
	// exactly what the LimitReader allowed.
	if got, want := len(lines[1]), 3+(256<<10); got != want {
		t.Errorf("the line is %d bytes long, want %d: the read was not bounded", got, want)
	}
}

// TestReadExecuteCancelled pins that a read gives up when the exchange does.
func TestReadExecuteCancelled(t *testing.T) {
	root, _ := workspace(t)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	if _, err := files.NewRead(root).Execute(ctx, reading(t, "lines.txt", 0, 0)); err == nil {
		t.Error("Execute() error = nil, want one")
	}
}

// TestReadThroughInvoke drives the tool the way the loop does, to pin that the
// answer carries the id of the call it answers.
func TestReadThroughInvoke(t *testing.T) {
	root, _ := workspace(t)
	registry := definitions.New(files.NewRead(root))

	result := tools.Invoke(t.Context(), use.New("call_1", "read_file", reading(t, "lines.txt", 0, 0)), registry)

	if got, want := result.ID(), "call_1"; got != want {
		t.Errorf("ID() = %q, want %q", got, want)
	}
	if result.Failed() {
		t.Errorf("Failed() = true, content %q", result.Content())
	}
	if want := "3  three"; !strings.Contains(result.Content(), want) {
		t.Errorf("Content() = %q, want it to contain %q", result.Content(), want)
	}
}
