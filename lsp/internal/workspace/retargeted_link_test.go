package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// TestAnalyzeAndPublish_FollowsALinkRetargetedSinceTheDocumentOpened opens a
// document through a link, then points the link at a file in another directory.
// The next analysis must load the file the link names now: its relative import
// resolves beside that file, and its one diagnostic is published on the URI the
// editor opened.
func TestAnalyzeAndPublish_FollowsALinkRetargetedSinceTheDocumentOpened(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	for _, d := range []string{"a", "b"} {
		if err := os.MkdirAll(filepath.Join(base, d), 0o750); err != nil {
			t.Fatal(err)
		}
	}
	first := filepath.Join(base, "a", "a.yammm")
	second := filepath.Join(base, "b", "b.yammm")
	const plain = "schema \"a\"\n\ntype A {\n\tid String primary\n}\n"
	// The import resolves only beside b/b.yammm, and the unknown type is the one
	// diagnostic the load should report.
	const importing = "schema \"b\"\n\nimport \"./dep\" as dep\n\ntype B {\n\tid String primary\n\tname Strin\n\t--> USES (one) dep.D\n}\n"
	files := map[string]string{
		first:                                 plain,
		second:                                importing,
		filepath.Join(base, "b", "dep.yammm"): "schema \"dep\"\n\ntype D {\n\tid String primary\n}\n",
	}
	for p, c := range files {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(base, "link.yammm")
	if err := os.Symlink(filepath.Join("a", "a.yammm"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	ws := newTestWorkspace(t, quietLogger(), Config{})
	collector := &testutil.NotificationCollector{}
	ws.SetNotifier(collector.Notify)
	uri := lsputil.PathToURI(link)
	ws.OpenDocument(uri, 1, plain)

	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("b", "b.yammm"), link); err != nil {
		t.Fatal(err)
	}
	// Analysed in this goroutine, as OpenDocument analyses an open, so the snapshot
	// and the publication are read after the analysis has stored them.
	ws.documentChanged(uri, 2, importing)
	ws.analyzeAndPublish(t.Context(), uri)

	want, err := location.SourceIDFromPath(second)
	if err != nil {
		t.Fatal(err)
	}
	snap := ws.LatestSnapshot(uri)
	if snap == nil {
		t.Fatal("no snapshot after the link was retargeted")
	}
	if snap.EntrySourceID != want {
		t.Errorf("analysed %q, want the file the link names now, %q", snap.EntrySourceID, want)
	}
	diags := collector.DiagnosticsFor(uri)
	if len(diags) != 1 {
		t.Fatalf("published %d diagnostics on %s, want the unknown type alone: %+v", len(diags), uri, diags)
	}
	if code, _ := diags[0].Code.Value.(string); diags[0].Code == nil || code != diag.E_UNKNOWN_TYPE.String() {
		t.Errorf("diagnostic code = %v, want %v: %s", diags[0].Code, diag.E_UNKNOWN_TYPE, diags[0].Message)
	}
}
