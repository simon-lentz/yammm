package snapshot

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon-lentz/yammm/diag"
)

// ScanEntry is one directory entry's state during a [ScanDir] iteration.
//
// Name and Path are populated on every per-file yield — including
// corrupt-file and per-file I/O-failure paths — so consumers can log or
// pass entries across package boundaries without reassembling paths via
// [filepath.Join]. Header is nil whenever Result has error-severity
// issues.
//
// The zero value (ScanEntry{}) is only yielded alongside an iterator-
// level error (dir-open failure or ctx cancellation); both Name and
// Path are empty there because no file was reached.
type ScanEntry struct {
	// Name is the basename of the file (e.g., "CA.ys").
	Name string
	// Path is the full filesystem path, equivalent to filepath.Join(dir, Name).
	Path string
	// Header holds the parsed header; nil whenever Result has
	// error-severity issues (malformed header, per-file I/O failure).
	Header *HeaderInfo
	// Result carries per-file diagnostics. OK() is true on the happy
	// path; HasErrors() is true when the file could not be parsed or
	// opened. See the package documentation for the diagnostic codes
	// emitted on each failure path.
	Result diag.Result
}

// ScanDir iterates over every .ys file in dir, yielding one
// (ScanEntry, nil) per file. The iterator is lazy: the directory is
// read once on first iteration, and each file's header is parsed on-
// demand via [HeaderOnlyRead], and inherits its limit: ScanDir validates each
// file's header and types table, never the document's outermost shape. Callers can break early without paying
// the parse cost for remaining files.
//
// Filtering:
//   - Only regular files (not directories) whose basename ends with
//     ".ys" are included.
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
//   - Dir-open error (ENOENT, EACCES, ENOTDIR, ...): yields exactly
//     one (ScanEntry{}, err) pair wrapping the os error and ends.
//   - Context cancellation observed between files: yields exactly
//     one (ScanEntry{}, ctx.Err()) pair (context.Canceled or
//     context.DeadlineExceeded) and ends.
//
// Per-file errors (corrupt header, per-file os.Open / Read failure)
// live on the yielded ScanEntry.Result; the iterator's error is nil
// for those and iteration continues. This cleanly separates "stop
// iterating" (err != nil) from "this entry failed; try the next"
// (entry.Result.HasErrors()). Context cancellation between files
// takes precedence over any concurrent per-file I/O failure.
//
// Typical caller:
//
//	for entry, err := range snapshot.ScanDir(ctx, dir) {
//	    if err != nil { return fmt.Errorf("scan: %w", err) }
//	    if entry.Result.HasErrors() { log.Warn(...); continue }
//	    use(entry.Header)
//	}
func ScanDir(ctx context.Context, dir string) iter.Seq2[ScanEntry, error] {
	return func(yield func(ScanEntry, error) bool) {
		// Check cancellation at entry so an already-cancelled ctx doesn't
		// pay the os.ReadDir cost.
		if err := ctx.Err(); err != nil {
			yield(ScanEntry{}, err)
			return
		}
		dirents, err := os.ReadDir(dir)
		if err != nil {
			yield(ScanEntry{}, fmt.Errorf("open dir %q: %w", dir, err))
			return
		}
		for _, dirent := range dirents {
			// ctx check between files — cancellation takes precedence
			// over a concurrent per-file I/O failure on the next file.
			if err := ctx.Err(); err != nil {
				yield(ScanEntry{}, err)
				return
			}
			if dirent.IsDir() {
				continue
			}
			name := dirent.Name()
			if !strings.HasSuffix(name, ".ys") {
				continue
			}
			// Belt-and-braces: a file named *.ys.tmp already fails the
			// .ys suffix check above; the explicit TmpSuffix check guards
			// against any future relaxation of the .ys-only filter.
			if strings.HasSuffix(name, TmpSuffix) {
				continue
			}
			entry := scanFile(ctx, name, filepath.Join(dir, name))
			if !yield(entry, nil) {
				return
			}
		}
	}
}

// scanFile opens path and parses its header via HeaderOnlyRead. On open
// failure it synthesizes a Fatal E_SNAPSHOT_IO issue on the returned
// entry's Result; otherwise it delegates to HeaderOnlyRead and returns
// whatever (header, result) pair that produces.
func scanFile(ctx context.Context, name, path string) ScanEntry {
	f, err := os.Open(path)
	if err != nil {
		c := diag.NewCollector(0)
		c.Collect(
			diag.NewIssue(diag.Fatal, diag.E_SNAPSHOT_IO,
				fmt.Sprintf("open %q: %v", path, err)).
				WithDetail("path", path).
				Build(),
		)
		return ScanEntry{Name: name, Path: path, Result: c.Result()}
	}
	defer f.Close()

	header, res := HeaderOnlyRead(ctx, f)
	return ScanEntry{Name: name, Path: path, Header: header, Result: res}
}

// ScanDirSlice is a convenience wrapper that materializes the full
// [ScanDir] iterator into a slice. Useful when the caller wants a
// complete snapshot of the directory state for further processing. The
// outer diag.Result surfaces operation-level errors (dir does not
// exist, permission denied, context cancellation); per-file errors
// live on each [ScanEntry]'s Result.
//
// Context cancellation returns partial results: the returned slice
// contains entries processed before cancellation, and the outer
// diag.Result carries a Fatal E_CONTEXT_CANCELLED issue. Callers that
// want fail-fast-on-cancel check result.HasFatal() before using the
// slice; callers that want partial results read the slice directly
// and treat the outer Result as advisory.
func ScanDirSlice(ctx context.Context, dir string) ([]ScanEntry, diag.Result) {
	var out []ScanEntry
	var collector *diag.Collector
	for entry, err := range ScanDir(ctx, dir) {
		if err != nil {
			collector = diag.NewCollector(0)
			code := diag.E_SNAPSHOT_IO
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				code = diag.E_CONTEXT_CANCELLED
			}
			collector.Collect(diag.NewIssue(diag.Fatal, code, err.Error()).Build())
			break
		}
		out = append(out, entry)
	}
	if collector != nil {
		return out, collector.Result()
	}
	return out, diag.OK()
}
