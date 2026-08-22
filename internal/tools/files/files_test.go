package files_test

import (
	"os"
	"path/filepath"
	"testing"
)

// workspace builds the tree every test here works over. It is deliberately
// awkward in the ways that matter: a dotfile that must be shown, a .git that
// must not be walked into or read out of, a link inside the workspace and a
// link pointing out of it, a file with lines to window and one that is not
// text at all.
//
//	.
//	├── .gitignore
//	├── .git/
//	│   └── HEAD
//	├── README.md
//	├── binary.dat
//	├── empty/
//	├── here -> README.md
//	├── away -> <outside>/secret
//	├── lines.txt
//	└── internal/
//	    ├── core/
//	    │   └── tools.go
//	    └── notes.txt
//
// The temp dir is resolved through symlinks first: macOS hands back /var,
// which is a link to /private/var, and a root that is itself a link would make
// every containment check compare two different spellings of the same place.
func workspace(t *testing.T) (root, outside string) {
	t.Helper()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("could not resolve the temp dir: %v", err)
	}

	root, outside = filepath.Join(base, "workspace"), filepath.Join(base, "outside")

	for _, dir := range []string{
		filepath.Join(root, ".git"),
		filepath.Join(root, "empty"),
		filepath.Join(root, "internal", "core"),
		filepath.Join(outside, "secret"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("could not build the fixture: %v", err)
		}
	}

	for _, file := range []string{
		filepath.Join(root, ".gitignore"),
		filepath.Join(root, "README.md"),
		filepath.Join(root, ".git", "HEAD"),
		filepath.Join(root, "internal", "notes.txt"),
		filepath.Join(root, "internal", "core", "tools.go"),
		filepath.Join(outside, "secret", "keys.txt"),
	} {
		write(t, file, []byte("x"))
	}

	write(t, filepath.Join(root, "lines.txt"), []byte("one\ntwo\nthree\nfour\nfive\n"))
	// A NUL in the first line: whatever this is, it is not something to
	// spend the model's context on.
	write(t, filepath.Join(root, "binary.dat"), []byte("\x89PNG\x00\x1a\n\xff\xfe"))

	link(t, filepath.Join(root, "README.md"), filepath.Join(root, "here"))
	link(t, filepath.Join(outside, "secret"), filepath.Join(root, "away"))

	return root, outside
}

func write(t *testing.T, name string, content []byte) {
	t.Helper()
	if err := os.WriteFile(name, content, 0o644); err != nil {
		t.Fatalf("could not build the fixture: %v", err)
	}
}

func link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, name); err != nil {
		t.Skipf("this filesystem will not make symlinks: %v", err)
	}
}
