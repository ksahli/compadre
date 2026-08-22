package files

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/ksahli/compadre/internal/core/tools/use"
)

// maxEntries is the ceiling on one listing. A recursive walk of a tree nobody
// measured first is not this process's to swallow whole, and a model reading
// ten thousand paths is not reading anything.
const maxEntries = 1000

// List lists what is in the workspace.
type List struct {
	workspace
}

// NewList builds the listing tool over root, which must be absolute and
// already resolved through symlinks — every judgement about whether a path is
// inside the workspace is a comparison against it, and a root that is itself a
// link would make each of those comparisons wrong.
//
// There is no option and no default. Unlike an endpoint, a root has no
// sensible value to fall back to: a tool with the wrong one is not degraded,
// it is pointed at somebody else's files.
func NewList(root string) List {
	return List{workspace{root: root}}
}

func (List) Name() string { return "list_files" }

func (List) Description() string {
	return "List the files and directories in the workspace. Paths are given " +
		"relative to the workspace root, and directories are marked with a " +
		"trailing slash. Pass path to list a directory other than the root, " +
		"relative to it; pass recursive to walk the whole subtree beneath it " +
		"rather than one level. The workspace is all this tool can see: a " +
		"path leading outside it is refused, and the contents of .git are " +
		"never listed."
}

func (List) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{
				"type": "string",
				"description": "Directory to list, relative to the workspace root, " +
					"for example 'internal/core'. Defaults to the root itself.",
			},
			"recursive": map[string]any{
				"type": "boolean",
				"description": "Walk the whole subtree beneath the directory rather " +
					"than listing one level. Defaults to false.",
			},
		},
		"required": []string{},
	}
}

type listArguments struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// Execute resolves the directory asked for, lists it, and writes the answer
// out as prose. Every error is written to be read by the model: it is the one
// that has to decide whether to ask for somewhere else.
func (tool List) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args listArguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}

	directory, info, err := tool.resolve(args.Path)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: '%s'", args.Path)
	}

	entries, truncated, err := tool.list(ctx, directory, args.Recursive)
	if err != nil {
		return "", err
	}

	return listing(tool.relative(directory), entries, truncated), nil
}

// entry is one line of the answer: where it is, and whether it can be
// descended into.
type entry struct {
	path      string
	directory bool
}

// list reads the directory, one level or all of them. The bool that comes
// back says the ceiling was reached, so the report can admit to being short
// rather than passing a partial listing off as the whole of it.
func (tool List) list(ctx context.Context, directory string, recursive bool) ([]entry, bool, error) {
	if !recursive {
		return tool.shallow(ctx, directory)
	}
	return tool.walk(ctx, directory)
}

func (tool List) shallow(ctx context.Context, directory string) ([]entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}

	// ReadDir sorts by filename, so the order is the caller's to rely on.
	found, err := os.ReadDir(directory)
	if err != nil {
		return nil, false, fmt.Errorf("could not read '%s': %w", tool.relative(directory), err)
	}

	entries := []entry{}
	for _, found := range found {
		if len(entries) == maxEntries {
			return entries, true, nil
		}
		entries = append(entries, entry{
			path:      tool.relative(filepath.Join(directory, found.Name())),
			directory: found.IsDir(),
		})
	}

	return entries, false, nil
}

// walk descends the subtree. WalkDir does not follow symlinks, which is what
// is wanted here twice over: a link out of the workspace is reported as an
// entry rather than traversed, and a link back into the tree cannot make the
// walk loop. The directory the walk started from is not listed as one of its
// own entries — the report already names it.
func (tool List) walk(ctx context.Context, directory string) ([]entry, bool, error) {
	entries, truncated := []entry{}, false

	err := filepath.WalkDir(directory, func(path string, d fs.DirEntry, err error) error {
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		if err != nil {
			// A directory that cannot be read is a hole in the
			// answer, not the end of it: the rest of the tree is
			// still worth having.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if path == directory {
			return nil
		}
		if len(entries) == maxEntries {
			truncated = true
			return fs.SkipAll
		}
		entries = append(entries, entry{
			path:      tool.relative(path),
			directory: d.IsDir(),
		})
		// Listed, then not entered: the model should be able to see
		// that .git is there, which is different from being shown
		// every object in it.
		if d.IsDir() && d.Name() == skipped {
			return fs.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, false, err
	}

	return entries, truncated, nil
}

// listing writes the answer the model reads. Prose rather than JSON, for the
// reason every tool here gives: the reader is a model, and a sentence costs it
// less than a shape it has to decode first. An empty directory says so, since
// a heading with nothing under it reads like a tool that failed quietly.
func listing(directory string, entries []entry, truncated bool) string {
	var out strings.Builder

	fmt.Fprintf(&out, "%s (%d entries)\n", directory, len(entries))

	if len(entries) == 0 {
		fmt.Fprintln(&out, "the directory is empty")
		return strings.TrimRight(out.String(), "\n")
	}

	for _, entry := range entries {
		name := entry.path
		if entry.directory {
			name += "/"
		}
		fmt.Fprintln(&out, name)
	}

	if truncated {
		fmt.Fprintf(&out, "listing stopped at %d entries; ask about a directory further in to see the rest\n", maxEntries)
	}

	return strings.TrimRight(out.String(), "\n")
}
