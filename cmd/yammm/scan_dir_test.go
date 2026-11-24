package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
)

// detailFor returns the named detail on the first issue carrying code.
func detailFor(res diag.Result, code diag.Code, key string) (string, bool) {
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

// A directory scanDir cannot read is one fatal E_SNAPSHOT_IO issue whose detail
// carries the os error, and no entries.
func TestScanDir_AnUnreadableDirectoryIsOneFatalIOIssue(t *testing.T) {
	t.Parallel()
	missing := filepath.Join(t.TempDir(), "no-such-dir")

	entries, res := scanDir(t.Context(), missing)
	if len(entries) != 0 {
		t.Errorf("scanned %d entries from a missing directory", len(entries))
	}
	if !res.HasFatal() || !res.HasCode(diag.E_SNAPSHOT_IO) {
		t.Fatalf("result = %s, want a fatal E_SNAPSHOT_IO", res)
	}
	detail, ok := detailFor(res, diag.E_SNAPSHOT_IO, diag.DetailKeyDetail)
	if !ok {
		t.Fatalf("E_SNAPSHOT_IO carries no %q detail: %s", diag.DetailKeyDetail, res)
	}
	if !strings.Contains(detail, "no-such-dir") {
		t.Errorf("detail = %q, want the os error naming the directory", detail)
	}
}

// A cancelled context is one fatal E_CONTEXT_CANCELLED issue, never E_SNAPSHOT_IO.
func TestScanDir_ACancelledContextIsOneFatalCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createYSFixture(t, dir)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	entries, res := scanDir(ctx, dir)
	if len(entries) != 0 {
		t.Errorf("scanned %d entries under a cancelled context", len(entries))
	}
	if !res.HasFatal() || !res.HasCode(diag.E_CONTEXT_CANCELLED) || res.HasCode(diag.E_SNAPSHOT_IO) {
		t.Errorf("result = %s, want a fatal E_CONTEXT_CANCELLED alone", res)
	}
}

// An expired deadline is a cancellation, never an I/O failure.
func TestScanDir_AnExpiredDeadlineIsOneFatalCancellation(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createYSFixture(t, dir)
	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer cancel()

	entries, res := scanDir(ctx, dir)
	if len(entries) != 0 {
		t.Errorf("scanned %d entries past the deadline", len(entries))
	}
	if !res.HasFatal() || !res.HasCode(diag.E_CONTEXT_CANCELLED) || res.HasCode(diag.E_SNAPSHOT_IO) {
		t.Errorf("result = %s, want a fatal E_CONTEXT_CANCELLED alone", res)
	}
}

// A file the scan cannot parse fails on its own entry, and the scan's result
// stays clean.
func TestScanDir_AFileFailureStaysOnItsEntry(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	createYSFixture(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "bad.ys"), []byte("not a snapshot"), 0o600); err != nil {
		t.Fatal(err)
	}

	entries, res := scanDir(t.Context(), dir)
	if diagtest.Err(res) != nil {
		t.Fatalf("a file failure reached the scan's result: %s", res)
	}
	failed := map[string]bool{}
	for _, entry := range entries {
		failed[entry.Name] = entry.Err != nil
	}
	if len(failed) != 2 || !failed["bad.ys"] || failed["test.ys"] {
		t.Errorf("entries failed = %v, want bad.ys alone of [bad.ys test.ys]", failed)
	}
}

// cancelAfter reports no error for its first n Err calls and Canceled after.
type cancelAfter struct {
	context.Context //nolint:containedctx // a decorator holds the context it wraps
	n               int
	calls           int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.n {
		return context.Canceled
	}
	return nil
}

// A scan cancelled after it has collected entries returns none of them beside
// its fatal issue, at whichever Err call the cancellation lands.
func TestScanDir_ACancellationMidScanReturnsNoEntries(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.ys", "b.ys", "c.ys"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sawPartial := false
	for n := range 40 {
		ctx := &cancelAfter{Context: t.Context(), n: n}
		entries, res := scanDir(ctx, dir)
		if !res.HasFatal() {
			continue
		}
		sawPartial = sawPartial || ctx.calls > 2
		if len(entries) != 0 {
			t.Errorf("cancelled after %d Err calls: %d entries beside the fatal issue", n, len(entries))
		}
	}
	if !sawPartial {
		t.Fatal("no cancellation landed after the scan read a file")
	}
}
