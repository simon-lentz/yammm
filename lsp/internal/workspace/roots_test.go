package workspace

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// A workspace root and the document paths matched against it must canonicalise
// the same way, or FindModuleRoot's prefix match misses. AddRoot canonicalised
// by resolving before cleaning while every other site cleans first, so a root
// URI containing ".." across a symlink registered a path no document matched.
func TestAddRoot_CanonicalisesLikeEveryOtherSite(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "a", "real")
	if err := os.MkdirAll(real, 0o750); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(base, "target")
	if err := os.MkdirAll(target, 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	// Concatenated by hand: filepath.Join cleans, resolving the ".." before
	// the code under test — whose subject is when to clean — ever sees it.
	rootArg := base + string(filepath.Separator) + "link" +
		string(filepath.Separator) + ".." + string(filepath.Separator) + "target"

	// The expectation is derived here rather than taken from the code under
	// test: absolute, then cleaned, then resolved.
	abs, err := filepath.Abs(rootArg)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(filepath.Clean(abs))
	if err != nil {
		t.Fatal(err)
	}

	w := NewWorkspace(slog.New(slog.DiscardHandler), Config{})
	w.AddRoot(lsputil.PathToURI(rootArg))

	// One level down: with no root, FindModuleRoot falls back to the file's own
	// directory, which a doc sitting directly in the root makes indistinguishable.
	sub := filepath.Join(want, "sub")
	if err := os.MkdirAll(sub, 0o750); err != nil {
		t.Fatal(err)
	}
	doc := filepath.Join(sub, "schema.yammm")

	got, err := w.FindModuleRoot(doc)
	if err != nil {
		t.Fatalf("FindModuleRoot(%q): %v", doc, err)
	}
	if got != want {
		t.Errorf("FindModuleRoot(%q) = %q, want %q — the root was registered under a path no document matches",
			doc, got, want)
	}

	// RemoveRoot has to canonicalise identically or it removes nothing, and
	// the fallback is what proves the root is gone rather than merely unmatched.
	w.RemoveRoot(lsputil.PathToURI(rootArg))
	got, err = w.FindModuleRoot(doc)
	if err != nil {
		t.Fatalf("FindModuleRoot(%q) after RemoveRoot: %v", doc, err)
	}
	if got != sub {
		t.Errorf("FindModuleRoot(%q) = %q after RemoveRoot, want the directory fallback %q", doc, got, sub)
	}
}
