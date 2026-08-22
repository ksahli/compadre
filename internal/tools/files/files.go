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

const (
	// maxEntries is the ceiling on one listing. A recursive walk of a tree
	// nobody measured first is not this process's to swallow whole, and a
	// model reading ten thousand paths is not reading anything.
	maxEntries = 1000

	// skipped is the one directory a walk does not enter — it is listed,
	// like anything else, but not descended into. Its contents are an
	// implementation detail of git that would spend the whole ceiling
	// before the walk reached anything the model asked about.
	skipped = ".git"
)

// Tool lists what is in the workspace. The root is a field rather than
// something read at call time: the tool answers about the one directory it
// was built with, and a test builds it with a directory of its own.
type Tool struct {
	root string
}

// New builds the tool over root, which must be absolute and already resolved
// through symlinks — every judgement about whether a path is inside the
// workspace is a comparison against it, and a root that is itself a link
// would make each of those comparisons wrong.
//
// There is no option and no default. Unlike an endpoint, a root has no
// sensible value to fall back to: a tool with the wrong one is not degraded,
// it is pointed at somebody else's files.
func New(root string) Tool {
	return Tool{root: root}
}

func (Tool) Name() string { return "list_files" }

func (Tool) Description() string {
	return "List the files and directories in the workspace. Paths are given " +
		"relative to the workspace root, and directories are marked with a " +
		"trailing slash. Pass path to list a directory other than the root, " +
		"relative to it; pass recursive to walk the whole subtree beneath it " +
		"rather than one level. The workspace is all this tool can see: a " +
		"path leading outside it is refused, and the contents of .git are " +
		"never listed."
}

func (Tool) Schema() map[string]any {
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

type arguments struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
}

// Execute resolves the directory asked for, lists it, and writes the answer
// out as prose. Every error is written to be read by the model: it is the one
// that has to decide whether to ask for somewhere else.
func (tool Tool) Execute(ctx context.Context, raw use.Arguments) (string, error) {
	var args arguments
	if err := json.Unmarshal(raw, &args); err != nil {
		return "", fmt.Errorf("could not parse arguments: %w", err)
	}

	directory, err := tool.resolve(args.Path)
	if err != nil {
		return "", err
	}

	entries, truncated, err := tool.list(ctx, directory, args.Recursive)
	if err != nil {
		return "", err
	}

	return report(tool.relative(directory), entries, truncated), nil
}

// resolve turns what the model asked for into an absolute path inside the
// workspace, or refuses. The refusal is the feature: '..', an absolute path
// and a symlink pointing away are each an answer about somebody else's files,
// and are told no by name rather than quietly clamped to the root — a model
// that reads what it did wrong can ask again, and one handed the root instead
// of what it asked for cannot tell that it was.
func (tool Tool) resolve(path string) (string, error) {
	if filepath.IsAbs(path) {
		return "", fmt.Errorf("path must be relative to the workspace, got '%s'", path)
	}

	// Join collapses '..' lexically, which handles the paths that never
	// touch a link. EvalSymlinks handles the rest, and is why the check
	// below is made against a path nothing can still redirect.
	target := filepath.Join(tool.root, path)

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", fmt.Errorf("no such path in the workspace: '%s'", path)
	}

	if !tool.contains(resolved) {
		return "", fmt.Errorf("path escapes the workspace: '%s'", path)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("no such path in the workspace: '%s'", path)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory: '%s'", path)
	}

	return resolved, nil
}

// contains reports whether an already-resolved absolute path is the workspace
// or sits beneath it. A Rel that fails at all — a different volume, on the
// platforms that have them — is not inside.
func (tool Tool) contains(resolved string) bool {
	rel, err := filepath.Rel(tool.root, resolved)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// relative names a path the way the model is shown it: relative to the root,
// with the root itself called '.'.
func (tool Tool) relative(resolved string) string {
	rel, err := filepath.Rel(tool.root, resolved)
	if err != nil {
		return resolved
	}
	return rel
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
func (tool Tool) list(ctx context.Context, directory string, recursive bool) ([]entry, bool, error) {
	if !recursive {
		return tool.shallow(ctx, directory)
	}
	return tool.walk(ctx, directory)
}

func (tool Tool) shallow(ctx context.Context, directory string) ([]entry, bool, error) {
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
func (tool Tool) walk(ctx context.Context, directory string) ([]entry, bool, error) {
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

// report writes the answer the model reads. Prose rather than JSON, for the
// reason every tool here gives: the reader is a model, and a sentence costs it
// less than a shape it has to decode first. An empty directory says so, since
// a heading with nothing under it reads like a tool that failed quietly.
func report(directory string, entries []entry, truncated bool) string {
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
