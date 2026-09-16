package workspace

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// TestOpenDocuments_NormalizationTwinsLeaveEveryAnalysisLoading opens two files
// whose names differ only by Unicode normalization, and a third file, on a
// filesystem that keeps both names. The twins share one identity, so the editor
// must hand the loader one of them: every analysis still loads, and the twin
// opened first is the one analysed.
func TestOpenDocuments_NormalizationTwinsLeaveEveryAnalysisLoading(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	nfc := filepath.Join(dir, "café.yammm")
	nfd := filepath.Join(dir, "café.yammm")
	mainPath := filepath.Join(dir, "main.yammm")
	files := []struct{ path, text string }{
		{nfc, "schema \"nfc\"\n\ntype A {\n\tid String primary\n}\n"},
		{nfd, "schema \"nfd\"\n\ntype B {\n\tid String primary\n}\n"},
		{mainPath, "schema \"main\"\n\ntype M {\n\tid String primary\n}\n"},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, []byte(f.text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	nfcInfo, err := os.Stat(nfc)
	if err != nil {
		t.Fatal(err)
	}
	nfdInfo, err := os.Stat(nfd)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(nfcInfo, nfdInfo) {
		t.Skip("the filesystem folds Unicode normalization, so the two names are one file")
	}

	ws := newTestWorkspace(t, quietLogger(), Config{})
	analyzed := make(chan string, 16)
	ws.setAnalysisCompletedHook(func(u string) {
		select {
		case analyzed <- u:
		default:
		}
	})
	for _, f := range files {
		uri := lsputil.PathToURI(f.path)
		ws.OpenDocument(uri, 1, f.text)
		waitFor(t, analyzed, uri)
	}

	for _, f := range files[1:] {
		uri := lsputil.PathToURI(f.path)
		snap := ws.LatestSnapshot(uri)
		if snap == nil || snap.Schema == nil {
			var err error
			if snap != nil {
				err = snap.Result.Err()
			}
			t.Errorf("analysis of %s did not load with both twins open: %v", filepath.Base(f.path), err)
		}
	}
	if snap := ws.LatestSnapshot(lsputil.PathToURI(nfd)); snap != nil && snap.Schema != nil && snap.Schema.Name() != "nfc" {
		t.Errorf("the NFD twin analysed schema %q; want \"nfc\", the text of the twin opened first", snap.Schema.Name())
	}
}
