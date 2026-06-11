package snapshot_test

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/snapshot"
)

// seedValidYS writes a well-formed .ys file at path and returns the path.
// The payload is produced by Marshal against testSchema(t)'s fixtures so
// it round-trips cleanly through HeaderOnlyRead.
func seedValidYS(t *testing.T, path string) {
	t.Helper()
	s := testSchema(t)
	company := mustValidInstance(t, s, "Company", []any{"c1"},
		map[string]any{"id": "c1", "title": "Acme"})
	snap := buildSnapshot(t, s, company)

	data, result := snapshot.Marshal(context.Background(), snap)
	require.NoError(t, result.Err(), "seed: Marshal should not error")
	require.NoError(t, snapshot.WriteFile(path, data), "seed: WriteFile should not error")
}

// seedCorruptYS writes junk bytes at path so HeaderOnlyRead fails with
// E_SNAPSHOT_MALFORMED on it.
func seedCorruptYS(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("not valid json at all"), 0o600))
}

// collectScanDir drains the ScanDir iterator into a slice plus an
// optional trailing iterator-level error. Tests that need mid-iteration
// control construct the loop explicitly.
func collectScanDir(ctx context.Context, dir string) (entries []snapshot.ScanEntry, trailingErr error) {
	for entry, err := range snapshot.ScanDir(ctx, dir) {
		if err != nil {
			trailingErr = err
			return
		}
		entries = append(entries, entry)
	}
	return
}

// ---------- ScanDir (iterator form) ----------

func TestScanDir_HappyPath(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))
	seedValidYS(t, filepath.Join(dir, "c.ys"))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err, "happy path should not yield iterator error")
	require.Len(t, entries, 3, "should yield exactly the three valid files")

	for _, entry := range entries {
		assert.False(t, entry.Result.HasErrors(), "%s: expected OK result", entry.Name)
		assert.NotNil(t, entry.Header, "%s: expected non-nil Header", entry.Name)
		assert.NotEmpty(t, entry.Name, "Name must be populated on every yield")
		assert.Equal(t, filepath.Join(dir, entry.Name), entry.Path,
			"Path must equal filepath.Join(dir, Name) on every yield")
	}
}

func TestScanDir_EmptyDirectory(t *testing.T) {
	dir := t.TempDir()

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err, "empty dir should yield no iterator error")
	assert.Empty(t, entries, "empty directory yields no entries")
}

func TestScanDir_NoYsFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "README"), []byte("x"), 0o600))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "directory with no .ys files yields no entries")
}

func TestScanDir_OnlyTmpFiles(t *testing.T) {
	// Seed *.ys.tmp files directly (not via WriteFile, whose success
	// would rename them away). Pins that ScanDir's TmpSuffix filter
	// skips staging files left behind by a crashed WriteFile — the
	// ScanDir half of PR-4's "shared-constant contract" test.
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "stale.ys.tmp"), []byte("junk"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "another.ys.tmp"), []byte("junk"), 0o600))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "*.ys.tmp files must be skipped")
}

func TestScanDir_SkipsSubdirectoriesNamedLikeYsFiles(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(dir, "bogus.ys"), 0o750))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "directories matching .ys should be skipped, not opened")
}

func TestScanDir_DirectoryDoesNotExist(t *testing.T) {
	var got []snapshot.ScanEntry
	var trailingEntry snapshot.ScanEntry
	var trailingErr error
	for entry, err := range snapshot.ScanDir(context.Background(), "/does/not/exist/yammm-scan-test") {
		if err != nil {
			trailingEntry = entry
			trailingErr = err
			continue
		}
		got = append(got, entry)
	}

	require.Error(t, trailingErr, "nonexistent dir should surface as iterator-level error")
	require.ErrorIs(t, trailingErr, fs.ErrNotExist,
		"iterator error should wrap fs.ErrNotExist")
	assert.Empty(t, got, "no per-file entries should yield")
	assert.Equal(t, snapshot.ScanEntry{}, trailingEntry,
		"zero-value ScanEntry must accompany dir-level error pair")
}

func TestScanDir_PathIsFileNotDirectory(t *testing.T) {
	dir := t.TempDir()
	asFile := filepath.Join(dir, "not-a-dir")
	require.NoError(t, os.WriteFile(asFile, []byte("x"), 0o600))

	var trailingErr error
	var trailingEntry snapshot.ScanEntry
	for entry, err := range snapshot.ScanDir(context.Background(), asFile) {
		if err != nil {
			trailingEntry = entry
			trailingErr = err
			continue
		}
		t.Fatalf("unexpected per-file yield for file-as-dir: %+v", entry)
	}

	require.Error(t, trailingErr, "file-as-dir should surface as iterator-level error")
	assert.Equal(t, snapshot.ScanEntry{}, trailingEntry,
		"zero-value ScanEntry must accompany dir-level error pair")
}

func TestScanDir_CorruptFileAmongValid(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedCorruptYS(t, filepath.Join(dir, "b.ys"))
	seedValidYS(t, filepath.Join(dir, "c.ys"))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err, "per-file errors must NOT bubble as iterator errors")
	require.Len(t, entries, 3)

	byName := map[string]snapshot.ScanEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	assert.False(t, byName["a.ys"].Result.HasErrors(), "a.ys should parse cleanly")
	assert.NotNil(t, byName["a.ys"].Header)

	corrupt := byName["b.ys"]
	require.True(t, corrupt.Result.HasErrors(), "b.ys should report an error")
	assert.Nil(t, corrupt.Header, "corrupt entry must have Header==nil")

	var sawMalformed bool
	for iss := range corrupt.Result.Errors() {
		if iss.Code() == diag.E_SNAPSHOT_MALFORMED {
			sawMalformed = true
			break
		}
	}
	assert.True(t, sawMalformed, "corrupt file should surface E_SNAPSHOT_MALFORMED")

	assert.False(t, byName["c.ys"].Result.HasErrors(), "c.ys should parse cleanly")
	assert.NotNil(t, byName["c.ys"].Header)
}

func TestScanDir_PerFileIOFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod(0) permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode 0 permission check; run as non-root")
	}

	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "ok.ys"))
	denied := filepath.Join(dir, "denied.ys")
	seedValidYS(t, denied)
	require.NoError(t, os.Chmod(denied, 0), "strip permissions on denied.ys")
	// Restore permissions so t.TempDir() cleanup succeeds.
	t.Cleanup(func() { _ = os.Chmod(denied, 0o600) })

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err, "per-file IO error must NOT bubble as iterator error")
	require.Len(t, entries, 2, "iteration must continue past the denied file")

	byName := map[string]snapshot.ScanEntry{}
	for _, e := range entries {
		byName[e.Name] = e
	}

	ok := byName["ok.ys"]
	assert.False(t, ok.Result.HasErrors(), "ok.ys should succeed")
	assert.NotNil(t, ok.Header)

	bad := byName["denied.ys"]
	require.True(t, bad.Result.HasErrors(), "denied.ys should report Fatal E_SNAPSHOT_IO")
	assert.Nil(t, bad.Header, "denied entry must have Header==nil")
	assert.True(t, bad.Result.HasFatal(), "open-denied must be Fatal, not Error")

	var sawIO bool
	for iss := range bad.Result.Errors() {
		if iss.Code() == diag.E_SNAPSHOT_IO {
			sawIO = true
			break
		}
	}
	assert.True(t, sawIO, "denied file should surface E_SNAPSHOT_IO")
}

func TestScanDir_ContextCancelledMidIteration(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.ys", "b.ys", "c.ys", "d.ys"} {
		seedValidYS(t, filepath.Join(dir, name))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var received []snapshot.ScanEntry
	var trailingErr error
	var consumed int
	for entry, err := range snapshot.ScanDir(ctx, dir) {
		if err != nil {
			trailingErr = err
			break
		}
		received = append(received, entry)
		consumed++
		if consumed == 2 {
			cancel()
		}
	}

	require.Error(t, trailingErr, "cancellation should yield a trailing iterator error")
	require.ErrorIs(t, trailingErr, context.Canceled,
		"trailing error should be context.Canceled")
	assert.Len(t, received, 2, "only the files consumed before cancel should be received")
}

func TestScanDir_ContextAlreadyCancelledAtEntry(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before first iteration

	var received []snapshot.ScanEntry
	var trailingErr error
	for entry, err := range snapshot.ScanDir(ctx, dir) {
		if err != nil {
			trailingErr = err
			break
		}
		received = append(received, entry)
	}

	require.Error(t, trailingErr)
	require.ErrorIs(t, trailingErr, context.Canceled)
	assert.Empty(t, received, "already-cancelled ctx should skip os.ReadDir and yield no files")
}

func TestScanDir_CtxCancelBeatsPerFileIOError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod(0) permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode 0 permission check; run as non-root")
	}

	dir := t.TempDir()
	denied := filepath.Join(dir, "denied.ys")
	seedValidYS(t, denied)
	require.NoError(t, os.Chmod(denied, 0))
	t.Cleanup(func() { _ = os.Chmod(denied, 0o600) })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel before iteration starts

	var entryReceived bool
	var trailingErr error
	for entry, err := range snapshot.ScanDir(ctx, dir) {
		if err != nil {
			trailingErr = err
			assert.Equal(t, snapshot.ScanEntry{}, entry,
				"cancellation yield must carry zero-value ScanEntry, not a Fatal-E_SNAPSHOT_IO entry")
			break
		}
		entryReceived = true
	}

	require.Error(t, trailingErr, "cancellation must win over per-file error")
	require.ErrorIs(t, trailingErr, context.Canceled)
	assert.False(t, entryReceived, "denied.ys must not be yielded as a ScanEntry when ctx is cancelled")
}

func TestScanDir_EarlyBreakDoesNotParseRemainingFiles(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod(0) permission semantics differ on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root bypasses file-mode 0 permission check; run as non-root")
	}

	// Seed one valid readable file plus several zero-permission files.
	// If the iterator is truly lazy, breaking after the first yield
	// must not open the denied ones — so iteration visits aaa.ys and
	// stops.
	dir := t.TempDir()
	// aaa.ys sorts before others alphabetically so it yields first.
	seedValidYS(t, filepath.Join(dir, "aaa.ys"))
	for _, name := range []string{"b.ys", "c.ys", "d.ys"} {
		path := filepath.Join(dir, name)
		seedValidYS(t, path)
		require.NoError(t, os.Chmod(path, 0))
		p := path
		t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	}

	var count int
	for entry, err := range snapshot.ScanDir(context.Background(), dir) {
		require.NoError(t, err)
		assert.False(t, entry.Result.HasErrors(),
			"early-break path should only visit readable aaa.ys")
		count++
		break
	}
	assert.Equal(t, 1, count, "early break must short-circuit iteration before remaining files are opened")
}

// ---------- ScanDirSlice (slice form) ----------

func TestScanDirSlice_HappyPath(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedValidYS(t, filepath.Join(dir, "b.ys"))
	seedValidYS(t, filepath.Join(dir, "c.ys"))

	entries, result := snapshot.ScanDirSlice(context.Background(), dir)
	require.NoError(t, result.Err(), "happy path should produce OK outer Result")
	assert.Len(t, entries, 3)
}

func TestScanDirSlice_DirDoesNotExist(t *testing.T) {
	entries, result := snapshot.ScanDirSlice(context.Background(), "/does/not/exist/yammm-scan-slice-test")

	assert.Empty(t, entries, "no entries on dir-level failure")
	require.True(t, result.HasFatal(), "dir-level failure must surface as Fatal")

	var sawIO bool
	for iss := range result.Errors() {
		if iss.Code() == diag.E_SNAPSHOT_IO {
			sawIO = true
			break
		}
	}
	assert.True(t, sawIO, "dir-level failure should use E_SNAPSHOT_IO on the outer Result")
}

func TestScanDirSlice_CtxCancelMidIteration(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.ys", "b.ys", "c.ys", "d.ys"} {
		seedValidYS(t, filepath.Join(dir, name))
	}

	// Pre-cancelled ctx: the iterator yields its cancellation pair on
	// first check, so the slice comes back empty. The
	// cancellation-mid-stream shape is indistinguishable from "already
	// cancelled" at this granularity — both valid, both map onto Fatal
	// E_CONTEXT_CANCELLED on the outer Result.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	entries, result := snapshot.ScanDirSlice(ctx, dir)
	require.True(t, result.HasFatal())

	var sawCancel bool
	for iss := range result.Errors() {
		if iss.Code() == diag.E_CONTEXT_CANCELLED {
			sawCancel = true
			break
		}
	}
	assert.True(t, sawCancel, "cancellation must surface as E_CONTEXT_CANCELLED on outer Result")
	assert.Empty(t, entries, "entries processed before cancel land in the slice (here: zero)")
}

func TestScanDirSlice_AlreadyCancelledAtEntry(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	entries, result := snapshot.ScanDirSlice(ctx, dir)
	assert.Empty(t, entries)
	require.True(t, result.HasFatal())

	var sawCancel bool
	for iss := range result.Errors() {
		if iss.Code() == diag.E_CONTEXT_CANCELLED {
			sawCancel = true
			break
		}
	}
	assert.True(t, sawCancel)
}

func TestScanDirSlice_PerFileErrorIsNotDirLevel(t *testing.T) {
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "a.ys"))
	seedCorruptYS(t, filepath.Join(dir, "b.ys"))

	entries, result := snapshot.ScanDirSlice(context.Background(), dir)
	require.NoError(t, result.Err(),
		"per-file errors must NOT bubble to the outer Result in slice form")
	require.Len(t, entries, 2)

	var sawCorrupt, sawValid bool
	for _, e := range entries {
		switch e.Name {
		case "a.ys":
			sawValid = !e.Result.HasErrors()
		case "b.ys":
			sawCorrupt = e.Result.HasErrors()
		}
	}
	assert.True(t, sawValid, "valid file's per-entry Result must be OK")
	assert.True(t, sawCorrupt, "corrupt file's per-entry Result must carry errors")
}

// ---------- Cross-primitive contract ----------

func TestScanDir_SharedTmpSuffixConstantContract(t *testing.T) {
	// The ScanDir half of the cross-primitive contract pinned on the
	// WriteFile side in PR-4's TestWriteFile_TmpSuffixConstant: seed a
	// directory with foo.ys AND foo.ys.tmp side-by-side and assert the
	// iterator yields exactly one entry — the final file. Both
	// primitives key their filter off the same exported constant
	// (snapshot.TmpSuffix), so a future rename or value change of the
	// constant surfaces here if either primitive drifts.
	dir := t.TempDir()
	seedValidYS(t, filepath.Join(dir, "foo.ys"))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "foo.ys"+snapshot.TmpSuffix), []byte("stale"), 0o600,
	))

	entries, err := collectScanDir(context.Background(), dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "expected exactly foo.ys, not the sibling .tmp staging file")
	assert.Equal(t, "foo.ys", entries[0].Name)
}
