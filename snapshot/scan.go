package snapshot

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/hostpath"
)

// ScanEntry is one directory entry's state during a [ScanDir] iteration.
//
// Name and Path are populated on every per-file yield — including
// corrupt-file and per-file I/O-failure paths — so consumers can log or
// pass entries across package boundaries without reassembling paths via
// [filepath.Join]. Header is nil whenever Err is not.
//
// The zero value (ScanEntry{}) is only yielded alongside an iterator-level
// error (dir-open failure or ctx cancellation); both Name and Path are empty
// there because no file was reached.
type ScanEntry struct {
	// Name is the basename of the file (e.g., "CA.ys").
	Name string
	// Path is the full filesystem path, equivalent to filepath.Join(dir, Name).
	Path string
	// Header holds the parsed header; nil whenever Err is not.
	Header *Header
	// Err is nil when the file's header parsed, and otherwise a
	// *[diag.ResultError] holding why it did not: a Fatal E_SNAPSHOT_IO for a
	// file that does not open, or a refusal [ReadHeader] makes.
	Err error
	// FileSize is the file's size on disk in bytes, or zero when unknown.
	//
	// It is populated whenever the file was opened, including when the header
	// then failed to parse — a corrupt file still occupies disk, and a caller
	// accounting for space needs its size before it branches on Header. Zero
	// means the file is empty, could not be opened or could not be stat'd; no
	// diagnostic is raised for a failed stat, because a scan that succeeds
	// today must not start failing over a size nobody asked for.
	FileSize int64
	// ModTime is the file's modification time, or the zero Time when unknown.
	// It comes from the same stat as FileSize and carries the same contract:
	// populated whenever the file was opened, zero otherwise, no diagnostic.
	ModTime time.Time
}

// ScanDir iterates over the .ys files in dir that the filtering below admits,
// yielding one (ScanEntry, nil) per file. The iterator is lazy: the directory is
// read once on first iteration, and each file's header is parsed
// on-demand via [ReadHeader]. Callers can break early without
// paying the parse cost for remaining files.
//
// ScanDir inherits [ReadHeader]'s limit: it validates each file's
// header and types table, and judges neither the document's outermost shape
// nor its member names beyond refusing a top level that is no JSON object, a first
// section other than the header and a missing types section. It does
// not accept a misshapen document by omission — it does not see one. Use
// [Stat], [Verify] or [Unmarshal] when that matters.
//
// dir is cleaned before it is read, so a ".." cancels the element before it
// rather than following a symbolic link, and each yielded Path names an entry
// of the directory listed. An empty dir names no directory: it fails as a missing
// one does, and never lists the working directory. A cleaned dir whose element
// is not a directory, or sits under one that is not, fails with an error
// matching [syscall.ENOTDIR] on every host. An element a ".." cancels is never
// read, so "a.ys/.." lists the directory that holds a.ys.
//
// Filtering:
//   - Only regular files and symlinks are included, and a symlink only
//     when its target is a regular file or cannot be stat'd, a broken link
//     among them; a directory, FIFO, socket, or device is skipped whatever
//     its name.
//   - Of those, only files whose basename ends with ".ys" are included.
//   - Files whose basename ends with [TmpSuffix] are skipped (the
//     atomic-write staging files [WriteFile] leaves behind on crash).
//     The shared constant ensures ScanDir and WriteFile cannot drift
//     on the convention.
//   - Symlinks are followed; broken symlinks yield a per-file Fatal
//     E_SNAPSHOT_IO entry (the underlying os.Open error is surfaced
//     as a detail).
//   - Entries are yielded in the order returned by [os.ReadDir],
//     which sorts by filename. An empty directory, or one with no
//     .ys files, yields no entries — the iterator ends normally
//     without a dir-level error pair.
//
// The iterator's second yielded value is non-nil ONLY for
// operation-level failures that end iteration:
//   - Dir-open error (ENOENT, EACCES, ...): yields exactly one
//     (ScanEntry{}, err) pair wrapping the os error and ends; a dir whose
//     element is a file, or sits under one, wraps hostpath.ErrNotDirectory
//     in its place.
//   - Context cancellation observed at entry or between files: yields exactly
//     one (ScanEntry{}, ctx.Err()) pair (context.Canceled or
//     context.DeadlineExceeded) and ends.
//
// Per-file errors (corrupt header, per-file os.Open / Read failure)
// live on the yielded ScanEntry.Err; the iterator's error is nil
// for those and iteration continues. This cleanly separates "stop
// iterating" (err != nil) from "this entry failed; try the next"
// (entry.Err != nil). Context cancellation between files
// takes precedence over any concurrent per-file I/O failure.
//
// Typical caller:
//
//	for entry, err := range snapshot.ScanDir(ctx, dir) {
//	    if err != nil { return fmt.Errorf("scan: %w", err) }
//	    if entry.Err != nil { log.Warn(...); continue }
//	    use(entry.Header)
//	}
func ScanDir(ctx context.Context, dir string) iter.Seq2[ScanEntry, error] {
	if ctx == nil {
		panic("snapshot.ScanDir: nil context")
	}
	return func(yield func(ScanEntry, error) bool) {
		// Check cancellation at entry so an already-cancelled ctx doesn't
		// pay the os.ReadDir cost.
		if err := ctx.Err(); err != nil {
			yield(ScanEntry{}, err)
			return
		}
		// Cleaned as the yielded paths are, so a ".." cancels the element before
		// it rather than following a link, and each Path names an entry of the
		// directory read. Clean turns "" into ".", which os.ReadDir would list.
		listed := dir
		if dir != "" {
			listed = filepath.Clean(dir)
		}
		dirents, err := os.ReadDir(listed)
		if err != nil {
			yield(ScanEntry{}, fmt.Errorf("open dir %q: %w", dir, notDirectory(listed, err)))
			return
		}
		for _, dirent := range dirents {
			// ctx check between files — cancellation takes precedence
			// over a concurrent per-file I/O failure on the next file.
			if err := ctx.Err(); err != nil {
				yield(ScanEntry{}, err)
				return
			}
			name := dirent.Name()
			if !strings.HasSuffix(name, ".ys") {
				continue
			}
			path := filepath.Join(dir, name)
			if !isRegular(dirent, path) {
				continue
			}
			// Belt-and-braces: a file named *.ys.tmp already fails the
			// .ys suffix check above; the explicit TmpSuffix check guards
			// against any future relaxation of the .ys-only filter.
			if strings.HasSuffix(name, TmpSuffix) {
				continue
			}
			entry := scanFile(ctx, name, path)
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// isRegular reports whether the walk should descend to opening this entry.
//
// The DirEntry's own type comes from the directory read, which does not follow
// symlinks, so a symlink needs a stat to learn what it points at. Everything
// else is decided without a syscall. Opening a non-regular file is not merely
// wrong-answer: os.Open on a FIFO blocks until a writer appears, which would
// hang the scan rather than fail it.
//
// A stat failure — a broken symlink, most likely — falls through to the open,
// which reports it as the documented per-file E_SNAPSHOT_IO entry rather than
// vanishing from the walk.
func isRegular(dirent os.DirEntry, path string) bool {
	mode := dirent.Type()
	if mode.IsRegular() {
		return true
	}
	if mode&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return true
	}
	return info.Mode().IsRegular()
}

// scanFile opens path and parses its header via ReadHeader. On open
// failure it synthesizes a Fatal E_SNAPSHOT_IO issue in the returned
// entry's Err; otherwise it delegates to ReadHeader and returns
// whatever (header, err) pair that produces.
//
// The size is read from the open handle rather than from the DirEntry: fstat
// answers for the file that was actually opened, where the directory read's
// lstat answers for the symlink, and there is no window between the two reads
// for the file to change underneath.
func scanFile(ctx context.Context, name, path string) ScanEntry {
	f, err := os.Open(path)
	if err != nil {
		return ScanEntry{Name: name, Path: path, Err: failure(
			diag.NewIssue(diag.Fatal, diag.E_SNAPSHOT_IO,
				fmt.Sprintf("open %q: %v", path, err)).
				WithDetail(diag.DetailKeyFilePath, path).
				WithDetail(diag.DetailKeyDetail, err.Error()).
				Build(),
		)}
	}
	defer f.Close()

	var size int64
	var modTime time.Time
	if info, statErr := f.Stat(); statErr == nil {
		size = info.Size()
		modTime = info.ModTime()
	}

	header, err := ReadHeader(ctx, f)
	return ScanEntry{Name: name, Path: path, Header: header, Err: err, FileSize: size, ModTime: modTime}
}

// notDirectory returns err with [hostpath.ErrNotDirectory] in place of the
// host's error when listed, read as a directory, names a file or sits under
// one. Windows answers such a read ERROR_PATH_NOT_FOUND, which reads as a
// missing path.
func notDirectory(listed string, err error) error {
	if listed == "" || !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR) {
		return err
	}
	if hostpath.RefusedRead(listed+string(filepath.Separator)) == nil {
		return err
	}
	return &fs.PathError{Op: "open", Path: listed, Err: hostpath.ErrNotDirectory}
}
