package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

// maxContent is the ceiling on one write. It stands beside maxBytes rather
// than reusing it: how much of a file this process is willing to read and how
// much it is willing to lay down are two policies that happen to agree today,
// and one of them moving should not drag the other.
const maxContent = 256 << 10

// Write creates a file in the workspace.
type Write struct {
	workspace
}

// NewWrite builds the writing tool over root, under the same terms as
// [NewList] and [NewRead]: absolute, already resolved through symlinks, no
// default. The terms matter most here — a read pointed at the wrong root
// leaks, a write pointed at it damages.
func NewWrite(root string) Write {
	return Write{workspace{root: root}}
}

func (Write) Name() string { return "write_file" }

func (Write) Description() string {
	return "Create a text file in the workspace. The path is relative to the " +
		"workspace root, and directories on the way that do not exist are " +
		"created. This tool only creates: a path that already exists is " +
		"refused rather than replaced, so write to a new path instead of " +
		"rewriting one. The workspace is all this tool can reach: a path " +
		"leading outside it is refused, as is a directory and anything " +
		"inside .git."
}

func (Write) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type": "string",
				"description": "File to create, relative to the workspace root, for " +
					"example 'internal/core/tools/tools.go'. It must not already exist.",
			},
			"content": map[string]any{
				"type": "string",
				"description": fmt.Sprintf("The whole of what the file should contain, "+
					"at most %d bytes. It is written as given.", maxContent),
			},
		},
		"required": []string{"path", "content"},
	}
}

type writeArguments struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// Execute checks everything it can before it touches the filesystem, then
// creates the file. The order is the point: a refusal should leave the tree
// exactly as it found it, and a directory made on the way to a write that was
// never going to happen is a change nobody asked for.
//
// Every error is written to be read by the model: it is the one that has to
// decide whether to write somewhere else.
func (tool Write) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args writeArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(args.Content) > maxContent {
		return "", fmt.Errorf("content is %d bytes, which is more than the %d this tool will write",
			len(args.Content), maxContent)
	}
	// The counterpart of a read refusing to show a file that is not text.
	// A tool that will not hand those bytes back has no business laying
	// them down.
	if strings.IndexByte(args.Content, 0) >= 0 || !utf8.ValidString(args.Content) {
		return "", fmt.Errorf("content is not text, and this tool writes text files")
	}

	file, exists, err := tool.create(args.Path)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("already exists, and this tool does not replace: '%s'", args.Path)
	}
	// Judged on the resolved path, so a link into .git is refused the same
	// as naming it outright.
	if tool.git(file) {
		return "", fmt.Errorf("the contents of .git are not writable: '%s'", args.Path)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return "", fmt.Errorf("could not make the directory for '%s': %w", args.Path, err)
	}

	if err := create(file, args); err != nil {
		return "", err
	}

	return written(tool.relative(file), args.Content), nil
}

// create lays the file down. O_EXCL is what actually makes this tool create
// rather than replace: the check in Execute races anything else touching the
// tree and exists to give the model a sentence it can act on, while this is
// the kernel refusing to open a path that is already there. It refuses an
// existing symlink too, which is the case worth having it for — a link the
// check called new is a link a plain create would write straight through.
//
// A write that fails partway takes the file with it. Half a file left under a
// name the model believes it wrote is worse than no file at all.
func create(path string, args writeArguments) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return fmt.Errorf("already exists, and this tool does not replace: '%s'", args.Path)
		}
		return fmt.Errorf("could not create '%s': %w", args.Path, err)
	}

	if _, err := io.WriteString(file, args.Content); err != nil {
		file.Close()
		os.Remove(path)
		return fmt.Errorf("could not write '%s': %w", args.Path, err)
	}

	if err := file.Close(); err != nil {
		os.Remove(path)
		return fmt.Errorf("could not write '%s': %w", args.Path, err)
	}

	return nil
}

// written says what was laid down, in the terms a later read would report it
// in: lines and bytes. Prose rather than JSON, for the reason every tool here
// gives.
func written(path, content string) string {
	if content == "" {
		return fmt.Sprintf("%s (created, empty)", path)
	}

	lines := strings.Count(content, "\n")
	// A last line with nothing after it is still a line.
	if !strings.HasSuffix(content, "\n") {
		lines++
	}

	return fmt.Sprintf("%s (created, %s, %s)", path, plural(lines, "line"), plural(len(content), "byte"))
}

// plural says a count in words that read as a sentence. Every noun this tool
// counts with takes a plain 's'.
func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
