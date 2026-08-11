package lsp_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// Diagnostic columns are the same whether a document is opened at its real
// path or through a symlinked one. The analyzer-level test pins the same
// property against a direct Analyze call; this one pins it through the server,
// which is the altitude a client sees and the one that test cannot reach —
// the server canonicalises before Analyze, so the two exercise different code.
func TestServer_SymlinkedOpenPositionsDiagnosticsIdentically(t *testing.T) {
	const src = "schema \"ranges\"\n" +
		"\n" +
		"type Doc {\n" +
		"\tid String primary\n" +
		"\ttitle Enum[\"🎉日\", \"🎉日\"]\n" +
		"}\n"

	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temp base: %v", err)
	}
	realDir := filepath.Join(base, "real")
	if err := os.MkdirAll(realDir, 0o750); err != nil {
		t.Fatal(err)
	}
	linkDir := filepath.Join(base, "link")
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Skipf("symlinks not supported: %v", err)
	}
	realPath := filepath.Join(realDir, "main.yammm")
	if err := os.WriteFile(realPath, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	linkPath := filepath.Join(linkDir, "main.yammm")

	columnsAt := func(root, openAt string) []uint32 {
		t.Helper()
		h := newTestHarness(t, root)
		if err := h.OpenDocument(openAt, src); err != nil {
			t.Fatalf("open %s: %v", openAt, err)
		}

		var diags []protocol.Diagnostic
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			h.Sync()
			if diags = h.Diagnostics(testutil.PathToURI(openAt)); len(diags) > 0 {
				break
			}
			time.Sleep(20 * time.Millisecond)
		}
		if len(diags) == 0 {
			t.Fatalf("no diagnostics published for %s — the fixture converts nothing", openAt)
		}

		cols := make([]uint32, 0, len(diags)*2)
		for _, d := range diags {
			cols = append(cols, d.Range.Start.Character, d.Range.End.Character)
		}
		return cols
	}

	// The correct UTF-16 columns for the duplicate-enum diagnostics. Rune
	// columns — what a registry miss falls back to — would be [7 23 18 22].
	want := []uint32{7, 25, 19, 24}

	if got := columnsAt(realDir, realPath); !slices.Equal(got, want) {
		t.Errorf("columns at the real path = %v, want %v", got, want)
	}
	if got := columnsAt(linkDir, linkPath); !slices.Equal(got, want) {
		t.Errorf("columns through the symlink = %v, want %v", got, want)
	}
}
