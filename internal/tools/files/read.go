package files

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

const (
	// maxLines is the ceiling on one read. A model handed ten thousand
	// lines is not reading any of them, and a window it can move is worth
	// more than a wall it cannot.
	maxLines = 2000

	// maxBytes is the ceiling on what one file is allowed to make this
	// process read. How large a file the process was pointed at is not its
	// call — the same instinct as the listing's ceiling on entries, and the
	// weather tool's on a response body.
	maxBytes = 256 << 10

	// scanning is the starting size of the buffer lines are read into. It
	// grows up to the byte ceiling, so a file that is one very long line is
	// read short rather than failing to be read at all.
	scanning = 64 << 10
)

// Read shows the model what is in one file in the workspace.
type Read struct {
	workspace
}

// NewRead builds the reading tool over root, under the same terms as
// [NewList]: absolute, already resolved through symlinks, no default.
func NewRead(root string) Read {
	return Read{workspace{root: root}}
}

func (Read) Name() string { return "read_file" }

func (Read) Description() string {
	return "Read a text file in the workspace. The path is relative to the " +
		"workspace root, and the answer is the file's lines, numbered. Pass " +
		"offset to start somewhere other than the first line and limit to ask " +
		"for fewer lines; a read that stopped short says so and names the line " +
		"to ask again from. The workspace is all this tool can see: a path " +
		"leading outside it is refused, as is a directory, a file that is not " +
		"text, and anything inside .git."
}

func (Read) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type": "string",
				"description": "File to read, relative to the workspace root, for " +
					"example 'internal/core/tools/tools.go'.",
			},
			"offset": map[string]any{
				"type": "integer",
				"description": "Line to start from, counting from 1. Defaults to 1, " +
					"the start of the file.",
				"minimum": 1,
			},
			"limit": map[string]any{
				"type": "integer",
				"description": fmt.Sprintf("Lines to return, at most %d. Defaults to %d.",
					maxLines, maxLines),
				"minimum": 1,
				"maximum": maxLines,
			},
		},
		"required": []string{"path"},
	}
}

type readArguments struct {
	Path   string `json:"path"`
	Offset int    `json:"offset"`
	Limit  int    `json:"limit"`
}

// Execute resolves the file asked for, reads the window asked for, and writes
// it out as prose. Every error is written to be read by the model: it is the
// one that has to decide whether to ask for somewhere else, or for the rest.
func (tool Read) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args readArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}
	if strings.TrimSpace(args.Path) == "" {
		return "", fmt.Errorf("path is required")
	}
	if args.Offset < 0 {
		return "", fmt.Errorf("offset must be 1 or more, got %d", args.Offset)
	}
	if args.Limit < 0 {
		return "", fmt.Errorf("limit must be 1 or more, got %d", args.Limit)
	}
	if args.Offset == 0 {
		args.Offset = 1
	}
	if args.Limit == 0 || args.Limit > maxLines {
		args.Limit = maxLines
	}

	file, info, err := tool.resolve(args.Path)
	if err != nil {
		return "", err
	}
	if info.IsDir() {
		return "", fmt.Errorf("not a file: '%s'", args.Path)
	}
	// The judgement is made on the resolved path, so a link into .git is
	// refused the same as naming it outright.
	if tool.git(file) {
		return "", fmt.Errorf("the contents of .git are not readable: '%s'", args.Path)
	}

	read, err := tool.read(ctx, file, info, args)
	if err != nil {
		return "", err
	}

	return content(tool.relative(file), read, args), nil
}

// reading is one file as the model will be shown it: the window that was
// asked for, how many lines were behind it, and the two ways the answer can
// be short of the whole file.
type reading struct {
	lines   []string
	total   int
	clipped bool // the byte ceiling stopped the read before the file ended
}

// read walks the file once, keeping the lines the window asked for and
// counting the rest. Once rather than seeking, because a line offset is not a
// byte offset and there is no way to find the one from the other without
// reading what is in between.
//
// The read goes through a LimitReader: this is a file the process was pointed
// at, and how many bytes of it there are is not its call.
func (tool Read) read(ctx context.Context, path string, info os.FileInfo, args readArguments) (reading, error) {
	if err := ctx.Err(); err != nil {
		return reading{}, err
	}

	file, err := os.Open(path)
	if err != nil {
		return reading{}, fmt.Errorf("could not read '%s': %w", args.Path, err)
	}
	defer file.Close()

	// Size is what says the read was cut short. A file that grew between
	// the stat and the open is read to the ceiling either way; the flag is
	// about what the report is allowed to claim, not about the bound.
	out := reading{lines: []string{}, clipped: info.Size() > maxBytes}

	scanner := bufio.NewScanner(io.LimitReader(file, maxBytes))
	// One byte of headroom over the ceiling, so a line that is exactly the
	// whole of what the LimitReader allowed is a token that still fits.
	scanner.Buffer(make([]byte, 0, scanning), maxBytes+1)

	last := args.Offset + args.Limit - 1
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return reading{}, err
		}

		line := scanner.Bytes()
		// Refused rather than shown: bytes that are not text cost the
		// model context and tell it nothing, and a file it cannot read
		// is something it should be told plainly.
		if bytes.IndexByte(line, 0) >= 0 || !utf8.Valid(line) {
			return reading{}, fmt.Errorf("not a text file: '%s'", args.Path)
		}

		out.total++
		if out.total >= args.Offset && out.total <= last {
			out.lines = append(out.lines, string(line))
		}
	}
	if err := scanner.Err(); err != nil {
		return reading{}, fmt.Errorf("could not read '%s': %w", args.Path, err)
	}

	return out, nil
}

// content writes the answer the model reads: a heading naming the file and
// the window, then the lines, numbered so the model can say which one it
// means. Prose rather than JSON, for the reason every tool here gives.
//
// A read that stopped short says so and names the line to ask again from —
// the counterpart of the listing telling the model to ask about a directory
// further in.
func content(path string, read reading, args readArguments) string {
	var out strings.Builder

	if read.total == 0 {
		fmt.Fprintf(&out, "%s\n", path)
		fmt.Fprintln(&out, "the file is empty")
		return strings.TrimRight(out.String(), "\n")
	}

	if len(read.lines) == 0 {
		fmt.Fprintf(&out, "%s\n", path)
		fmt.Fprintf(&out, "offset %d is past the end of the file, which has %s\n",
			args.Offset, count(read))
		return strings.TrimRight(out.String(), "\n")
	}

	first := args.Offset
	last := first + len(read.lines) - 1

	// A clipped read does not know how long the file is, so it does not
	// say: a total that is really a floor would be a wrong answer rather
	// than a missing one.
	if read.clipped {
		fmt.Fprintf(&out, "%s (lines %d-%d)\n", path, first, last)
	} else {
		fmt.Fprintf(&out, "%s (lines %d-%d of %d)\n", path, first, last, read.total)
	}

	width := len(fmt.Sprint(last))
	for i, line := range read.lines {
		fmt.Fprintf(&out, "%*d  %s\n", width, first+i, line)
	}

	// Two separate facts, and a read can be short for both reasons at once:
	// the window ended before the lines did, and the lines ended before the
	// file did. Saying only one of them would leave the other as a silent
	// hole in the answer.
	if last < read.total {
		fmt.Fprintf(&out, "stopped at line %d; ask again with offset %d to see more\n", last, last+1)
	}
	if read.clipped {
		fmt.Fprintf(&out, "the file was cut off at %d bytes; what is past that was not read\n", maxBytes)
	}

	return strings.TrimRight(out.String(), "\n")
}

// count says how long the file is in words that read as a sentence, and
// admits when the number is only what was read rather than the whole of it.
func count(read reading) string {
	if read.clipped {
		return fmt.Sprintf("at least %d lines", read.total)
	}
	if read.total == 1 {
		return "1 line"
	}
	return fmt.Sprintf("%d lines", read.total)
}
