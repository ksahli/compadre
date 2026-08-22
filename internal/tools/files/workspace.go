package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// skipped is the one directory the tools here refuse to open up. A listing
// names it, like anything else, but does not descend into it, and a read is
// told no: its contents are an implementation detail of git that would spend
// a whole listing before the walk reached anything the model asked about, and
// that no question about the workspace is answered by.
const skipped = ".git"

// workspace is the bound both tools are built over: one rooted directory, and
// the judgement of whether a path the model asked for is inside it. It is
// unexported and shared rather than written twice, because a second copy of a
// containment check is a second chance to get it wrong.
//
// The root is a field rather than something read at call time: a tool answers
// about the one directory it was built with, and a test builds it with a
// directory of its own.
type workspace struct {
	root string
}

// resolve turns what the model asked for into an absolute path inside the
// workspace, or refuses. The refusal is the feature: '..', an absolute path
// and a symlink pointing away are each an answer about somebody else's files,
// and are told no by name rather than quietly clamped to the root — a model
// that reads what it did wrong can ask again, and one handed the root instead
// of what it asked for cannot tell that it was.
//
// What comes back is the resolved path and what it is. The kind is the
// caller's to judge: a directory is the answer to one tool here and the wrong
// thing to hand the other.
func (w workspace) resolve(path string) (string, os.FileInfo, error) {
	if filepath.IsAbs(path) {
		return "", nil, fmt.Errorf("path must be relative to the workspace, got '%s'", path)
	}

	// Join collapses '..' lexically, which handles the paths that never
	// touch a link. EvalSymlinks handles the rest, and is why the check
	// below is made against a path nothing can still redirect.
	target := filepath.Join(w.root, path)

	resolved, err := filepath.EvalSymlinks(target)
	if err != nil {
		return "", nil, fmt.Errorf("no such path in the workspace: '%s'", path)
	}

	if !w.contains(resolved) {
		return "", nil, fmt.Errorf("path escapes the workspace: '%s'", path)
	}

	info, err := os.Stat(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("no such path in the workspace: '%s'", path)
	}

	return resolved, info, nil
}

// contains reports whether an already-resolved absolute path is the workspace
// or sits beneath it. A Rel that fails at all — a different volume, on the
// platforms that have them — is not inside.
func (w workspace) contains(resolved string) bool {
	rel, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)))
}

// relative names a path the way the model is shown it: relative to the root,
// with the root itself called '.'.
func (w workspace) relative(resolved string) string {
	rel, err := filepath.Rel(w.root, resolved)
	if err != nil {
		return resolved
	}
	return rel
}

// git reports whether an already-resolved path is the .git directory or sits
// inside it. The judgement is made on the resolved path rather than what the
// model spelled, so a link into .git is caught the same as naming it outright.
func (w workspace) git(resolved string) bool {
	rel := w.relative(resolved)
	return rel == skipped || strings.HasPrefix(rel, skipped+string(filepath.Separator))
}
