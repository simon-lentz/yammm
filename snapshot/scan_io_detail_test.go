package snapshot_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// detailOf returns the named detail on the first issue carrying code.
func detailOf(res diag.Result, code diag.Code, key string) (string, bool) {
	for issue := range res.Issues() {
		if issue.Code() != code {
			continue
		}
		for _, d := range issue.Details() {
			if d.Key == key {
				return d.Value, true
			}
		}
	}
	return "", false
}

// A per-file open failure carries the underlying os error as a STRUCTURED
// detail. Three documents promise this — docs/API.md's Directory Iteration
// prose, ScanDir's godoc, and diag/code.go's E_SNAPSHOT_IO doc — and the error
// reached the free-text message only, so an operator could recover the cause
// by scraping the string or not at all.
//
// A broken symlink is the reachable trigger: the entry is included by design
// and open fails on it.
//
// Mutation: removing the DetailKeyDetail call in scanFile turns this red.
func TestScanIODetail_PerFileCarriesTheOSError(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "broken.ys")
	if err := os.Symlink(filepath.Join(dir, "does-not-exist.ys"), link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	entries, res := snapshot.ScanDirSlice(t.Context(), dir)
	if res.HasErrors() {
		t.Fatalf("the scan itself failed: %s", res)
	}
	if len(entries) != 1 {
		t.Fatalf("scanned %d entries, want 1", len(entries))
	}

	detail, ok := detailOf(entries[0].Result, diag.E_SNAPSHOT_IO, diag.DetailKeyDetail)
	if !ok {
		t.Fatalf("E_SNAPSHOT_IO carries no %q detail: %s", diag.DetailKeyDetail, entries[0].Result)
	}
	// The os error names the link, not its target: open() reports the path it
	// was given after the kernel fails to follow the dangling link.
	if !strings.Contains(detail, "broken.ys") || !strings.Contains(detail, "no such file") {
		t.Errorf("detail = %q, want the underlying os error", detail)
	}
}

// The dir-level failure carries it too. diag/code.go's promise covers "either a
// dir-level failure … or a per-file failure", so fixing only scanFile would
// have left half the claim false.
//
// Mutation: removing the DetailKeyDetail call in ScanDirSliceWith turns this
// red.
func TestScanIODetail_DirLevelCarriesTheOSError(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	_, res := snapshot.ScanDirSlice(t.Context(), missing)
	if !res.HasErrors() {
		t.Fatal("scanning a missing directory reported no error")
	}

	detail, ok := detailOf(res, diag.E_SNAPSHOT_IO, diag.DetailKeyDetail)
	if !ok {
		t.Fatalf("the dir-level E_SNAPSHOT_IO carries no %q detail: %s", diag.DetailKeyDetail, res)
	}
	if !strings.Contains(detail, "no-such-dir") {
		t.Errorf("detail = %q, want the underlying os error", detail)
	}
}
