package lsputil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// Cleaning before resolving is what makes ".." across a symlink land where the
// loader lands it. Resolving first walks the link, applies ".." to the link's
// real parent, and arrives somewhere else entirely — a different file, not a
// different spelling of one.
func TestCanonicalPath_CleansBeforeResolving(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp base: %v", err)
	}
	// link -> a/real, with a target.yammm both in base and in a/.
	if err := os.MkdirAll(filepath.Join(base, "a", "real"), 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "a", "real"), link); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	outer := filepath.Join(base, "target.yammm")
	inner := filepath.Join(base, "a", "target.yammm")
	for _, p := range []string{outer, inner} {
		if err := os.WriteFile(p, []byte("schema \"t\"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// Built by concatenation, not filepath.Join — Join cleans, which would
	// collapse the ".." before CanonicalPath ever sees it.
	sep := string(filepath.Separator)
	got := lsputil.CanonicalPath(link + sep + ".." + sep + "target.yammm")
	if got != outer {
		t.Errorf("CanonicalPath = %q, want %q", got, outer)
		if got == inner {
			t.Error("resolved before cleaning: the link's real parent absorbed the '..'")
		}
	}
}

// A path naming no file keeps its cleaned form rather than erroring: a virtual
// markdown source and a deleted file both need a stable key.
func TestCanonicalPath_KeepsUnresolvablePaths(t *testing.T) {
	t.Parallel()

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp base: %v", err)
	}
	virtual := filepath.Join(base, "notes.md") + "#block-0"
	if got := lsputil.CanonicalPath(virtual); got != virtual {
		t.Errorf("CanonicalPath(%q) = %q, want it unchanged", virtual, got)
	}

	deleted := filepath.Join(base, "sub", "..", "gone.yammm")
	if got, want := lsputil.CanonicalPath(deleted), filepath.Join(base, "gone.yammm"); got != want {
		t.Errorf("CanonicalPath(%q) = %q, want %q", deleted, got, want)
	}
}
