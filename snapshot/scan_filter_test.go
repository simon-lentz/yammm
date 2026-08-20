package snapshot_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/snapshot"
)

// mixedScanDir seeds one directory holding every shape the walk classifies, so
// a filter test and an unfiltered control see the same input.
func mixedScanDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))
	seedCorruptYS(t, filepath.Join(dir, "corrupt.ys"))
	seedCorruptYS(t, filepath.Join(dir, "stage.ys.tmp"))
	seedCorruptYS(t, filepath.Join(dir, "notes.txt"))
	if err := os.Mkdir(filepath.Join(dir, "sub.ys"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	return dir
}

func drain(t *testing.T, seq func(func(snapshot.ScanEntry, error) bool)) []snapshot.ScanEntry {
	t.Helper()
	var out []snapshot.ScanEntry
	for entry, err := range seq {
		if err != nil {
			t.Fatalf("scan: %v", err)
		}
		out = append(out, entry)
	}
	return out
}

// Given no options the two entry points are one iteration, which is what lets
// ScanDir keep its documented behaviour while ScanDirWith grows options.
func TestScanDirWith_NoOptionsMatchesScanDir(t *testing.T) {
	dir := mixedScanDir(t)

	plain := drain(t, snapshot.ScanDir(t.Context(), dir))
	withNone := drain(t, snapshot.ScanDirWith(t.Context(), dir))

	if len(plain) == 0 {
		t.Fatal("the fixture yielded no entries, so this asserts nothing")
	}
	if len(plain) != len(withNone) {
		t.Fatalf("ScanDir yielded %d entries, ScanDirWith %d", len(plain), len(withNone))
	}
	for i, want := range plain {
		got := withNone[i]
		if got.Name != want.Name || got.Path != want.Path ||
			got.FileSize != want.FileSize || !got.ModTime.Equal(want.ModTime) ||
			(got.Header == nil) != (want.Header == nil) ||
			got.Result.HasErrors() != want.Result.HasErrors() {
			t.Errorf("entry %d differs:\n ScanDir     %+v\n ScanDirWith %+v", i, want, got)
		}
	}
}

// The filter is consulted for exactly the entries the unfiltered scan would
// yield, so a predicate never has to re-implement the walk's own rules.
func TestScanDirWith_FilterSeesOnlyAdmittedFiles(t *testing.T) {
	dir := mixedScanDir(t)

	var seenByFilter []string
	entries := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
			seenByFilter = append(seenByFilter, c.Name)
			return true
		})))

	var yielded []string
	for _, e := range entries {
		yielded = append(yielded, e.Name)
	}
	slices.Sort(seenByFilter)
	slices.Sort(yielded)
	if !slices.Equal(seenByFilter, yielded) {
		t.Errorf("filter saw %v, scan yielded %v", seenByFilter, yielded)
	}
	if slices.Contains(seenByFilter, "notes.txt") || slices.Contains(seenByFilter, "sub.ys") ||
		slices.Contains(seenByFilter, "stage.ys.tmp") {
		t.Errorf("filter saw a file the walk rejects: %v", seenByFilter)
	}
}

// A rejection is not an error: the file is absent, and nothing reports it.
func TestScanDirWith_RejectedFileYieldsNothingAndNoDiagnostic(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "keep.ys"))
	seedValidYS(t, filepath.Join(dir, "drop.ys"))

	entries := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
			return c.Name != "drop.ys"
		})))

	if len(entries) != 1 {
		t.Fatalf("yielded %d entries, want 1", len(entries))
	}
	if entries[0].Name != "keep.ys" {
		t.Errorf("yielded %q, want keep.ys", entries[0].Name)
	}
	if entries[0].Result.HasErrors() {
		t.Errorf("keep.ys reported errors: %v", entries[0].Result.Err())
	}
}

func TestScanDirWith_AllRejectedEndsNormally(t *testing.T) {
	dir := mixedScanDir(t)

	entries := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(snapshot.ScanCandidate) bool { return false })))

	if len(entries) != 0 {
		t.Errorf("yielded %d entries, want 0", len(entries))
	}
}

// isRegular admits a broken symlink so the open can report it, so the filter
// sits after that and is asked about it. Info surfaces the stat failure rather
// than hiding it behind a zero FileInfo.
func TestScanDirWith_FilterSeesABrokenSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.Symlink(filepath.Join(dir, "nothing-here"), filepath.Join(dir, "broken.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	var asked int
	entries := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
			asked++
			if _, err := c.Info(); err == nil {
				t.Error("Info on a broken symlink returned no error")
			}
			return true
		})))

	if asked != 1 {
		t.Errorf("filter asked %d times, want 1", asked)
	}
	if len(entries) != 1 || !entries[0].Result.HasErrors() {
		t.Fatalf("want one error-severity entry, got %+v", entries)
	}
	if entries[0].FileSize != 0 {
		t.Errorf("FileSize = %d on an unopenable file, want 0", entries[0].FileSize)
	}

	rejected := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(snapshot.ScanCandidate) bool { return false })))
	if len(rejected) != 0 {
		t.Errorf("rejecting a broken symlink still yielded %d entries", len(rejected))
	}
}

// Info follows the link, so it describes the file the open will read — the same
// file FileSize and ModTime describe. A DirEntry-based implementation reports
// the link instead and fails here on Size alone.
func TestScanCandidate_InfoFollowsTheSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.dat")
	seedValidYS(t, target)
	if err := os.Symlink(target, filepath.Join(dir, "link.ys")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	targetInfo, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat target: %v", err)
	}
	linkInfo, err := os.Lstat(filepath.Join(dir, "link.ys"))
	if err != nil {
		t.Fatalf("lstat link: %v", err)
	}
	if targetInfo.Size() == linkInfo.Size() {
		t.Fatal("link and target report the same size, so this asserts nothing")
	}

	var asked int
	drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
			asked++
			got, infoErr := c.Info()
			if infoErr != nil {
				t.Fatalf("Info: %v", infoErr)
			}
			if got.Size() != targetInfo.Size() {
				t.Errorf("Info().Size() = %d, want the target's %d (got the link's %d)",
					got.Size(), targetInfo.Size(), linkInfo.Size())
			}
			return true
		})))
	if asked != 1 {
		t.Errorf("filter asked %d times, want 1", asked)
	}
}

// A caller-built candidate is the predicate-unit-test path, so it must resolve
// without the scan having filled the stat closure.
func TestScanCandidate_ZeroValueInfoStatsPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.ys")
	seedValidYS(t, path)

	info, err := snapshot.ScanCandidate{Name: "a.ys", Path: path}.Info()
	if err != nil {
		t.Fatalf("Info on a caller-built candidate: %v", err)
	}
	if info.Size() == 0 {
		t.Error("Info reported a zero size for a seeded fixture")
	}

	_, err = snapshot.ScanCandidate{Path: filepath.Join(dir, "missing.ys")}.Info()
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Info on a missing path = %v, want os.ErrNotExist", err)
	}
}

func TestScanDirWith_NilFilterAdmitsEverything(t *testing.T) {
	dir := mixedScanDir(t)

	plain := drain(t, snapshot.ScanDir(t.Context(), dir))
	nilFilter := drain(t, snapshot.ScanDirWith(t.Context(), dir, snapshot.WithScanFilter(nil)))

	if len(plain) != len(nilFilter) {
		t.Errorf("nil filter yielded %d entries, want ScanDir's %d", len(nilFilter), len(plain))
	}
}

func TestScanDirWith_LastFilterWins(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))

	entries := drain(t, snapshot.ScanDirWith(t.Context(), dir,
		snapshot.WithScanFilter(func(snapshot.ScanCandidate) bool { return false }),
		snapshot.WithScanFilter(func(snapshot.ScanCandidate) bool { return true })))

	if len(entries) != 1 {
		t.Errorf("yielded %d entries, want the last filter's 1", len(entries))
	}
}

// Cancellation is checked before any per-file work, so it beats the filter.
func TestScanDirWith_CancelledContextIsNotFiltered(t *testing.T) {
	dir := mixedScanDir(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	var asked int
	var sawErr error
	var entries int
	for entry, err := range snapshot.ScanDirWith(ctx, dir,
		snapshot.WithScanFilter(func(snapshot.ScanCandidate) bool {
			asked++
			return true
		})) {
		if err != nil {
			sawErr = err
			if entry.Name != "" || entry.Path != "" {
				t.Errorf("cancellation yielded a populated entry: %+v", entry)
			}
			continue
		}
		entries++
	}

	if asked != 0 {
		t.Errorf("filter asked %d times on a cancelled context, want 0", asked)
	}
	if entries != 0 {
		t.Errorf("yielded %d entries on a cancelled context, want 0", entries)
	}
	if !errors.Is(sawErr, context.Canceled) {
		t.Errorf("iterator error = %v, want context.Canceled", sawErr)
	}
}

func TestScanDirSliceWith_AppliesTheSameOptions(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "keep.ys"))
	seedValidYS(t, filepath.Join(dir, "drop.ys"))

	entries, result := snapshot.ScanDirSliceWith(t.Context(), dir,
		snapshot.WithScanFilter(func(c snapshot.ScanCandidate) bool {
			return c.Name != "drop.ys"
		}))

	if result.HasErrors() {
		t.Fatalf("ScanDirSliceWith: %v", result.Err())
	}
	if len(entries) != 1 || entries[0].Name != "keep.ys" {
		t.Errorf("materialized %+v, want only keep.ys", entries)
	}
}
