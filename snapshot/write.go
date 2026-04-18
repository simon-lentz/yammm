package snapshot

import (
	"fmt"
	"os"
)

// TmpSuffix is the extension [WriteFile] appends to path when staging a
// tmp+fsync+rename atomic write. Exported so callers and other yammm
// primitives (notably ScanDir's tmp-skip filter, landing in v0.3.0) key
// off the same constant rather than duplicating the string literal,
// preventing silent divergence on the convention across the snapshot
// package.
//
// Contract: a crashed WriteFile may leave a file at path+TmpSuffix as a
// partial write. Bespoke cleanup tools that enumerate or remove stale
// staging files reference TmpSuffix directly rather than hard-coding
// ".tmp" so a future change to the convention propagates through a
// single source of truth.
const TmpSuffix = ".tmp"

// WriteFile writes data to path atomically, using the tmp+fsync+rename
// pattern: the payload is staged at path+[TmpSuffix], fsync'd, closed,
// and renamed into place. If the process crashes between fsync and
// rename, the staging file is left behind as a partial write; consumers
// with bespoke sweeps reference [TmpSuffix] directly to enumerate or
// clean up these stale entries.
//
// On any error, WriteFile attempts to remove the path+[TmpSuffix]
// staging file as cleanup and returns the original error wrapped with a
// descriptive prefix.
//
// WriteFile does not validate that data is a valid .ys document. It is
// a general-purpose atomic-write primitive; callers are responsible for
// providing valid bytes (typically the output of [Marshal]).
//
// Durability semantics:
//   - File mode is 0o666 subject to umask (matching os.Create). Callers
//     who require stricter permissions should chmod the result after
//     WriteFile returns.
//   - WriteFile fsyncs the file before rename but does NOT fsync the
//     parent directory. On some filesystems (e.g., ext4 with
//     data=ordered and no commit barrier), the rename may not be
//     durable across a crash. Consumers with stronger durability
//     requirements should fork this helper and add parent-directory
//     fsync.
func WriteFile(path string, data []byte) error {
	tmp := path + TmpSuffix
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()      //nolint:gosec // best-effort close before cleanup
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on write failure
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()      //nolint:gosec // best-effort close before cleanup
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on sync failure
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on close failure
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on rename failure
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}
