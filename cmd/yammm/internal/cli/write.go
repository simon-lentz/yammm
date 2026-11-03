package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"

	"github.com/simon-lentz/yammm/internal/hostpath"
	"github.com/simon-lentz/yammm/snapshot"
)

// NewFileMode is the mode [WriteFile] gives a file that does not exist yet.
//
// Owner-only, because a CLI write carries whatever the graph held: an export
// can contain every value in the schema, and 0o644 publishes it to every
// account on the host.
const NewFileMode fs.FileMode = 0o600

// stagingPattern is the [os.CreateTemp] pattern of every staging file. Its
// length does not depend on the target's name, so a basename near the name
// limit can still be replaced, and it ends in [snapshot.TmpSuffix], which
// [snapshot.ScanDir] skips, so a staging file a crash leaves is never scanned.
const stagingPattern = ".yammm-*" + snapshot.TmpSuffix

// maxLinkHops bounds how many symlinks a write follows — the bound Linux sets
// (MAXSYMLINKS) — so a loop is refused rather than followed.
const maxLinkHops = 40

// writeTarget is where a write to an operator-named path lands, and how.
type writeTarget struct {
	path    string      // the file a replacement lands on: the named path, its symlinks followed
	mode    fs.FileMode // the mode a replacement carries
	through bool        // written through the named path in place rather than replaced
}

// resolveWriteTarget follows the symlinks of path, which [HostPath] gave, and
// decides how a write to it lands: an absent or regular file is replaced; a
// descriptor path, or any file that is neither regular nor a directory, such as
// a FIFO or a device, is written through; a directory, a loop of links, a
// regular file it cannot open for writing and a path it cannot stat or read as
// a link are refused. A descriptor path is decided by its name, because
// /dev/stdout stats as whatever its descriptor holds; every other path by the
// file it reaches.
func resolveWriteTarget(path string) (writeTarget, error) {
	resolved := path
	for hops := 0; ; hops++ {
		if hostpath.Descriptor(resolved) {
			return writeTarget{path: path, through: true}, nil
		}
		info, err := os.Lstat(resolved)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return writeTarget{path: resolved, mode: NewFileMode}, nil
		case err != nil:
			return writeTarget{}, bare(err)
		case info.Mode()&fs.ModeSymlink != 0:
			if hops == maxLinkHops {
				return writeTarget{}, syscall.ELOOP
			}
			dest, err := os.Readlink(resolved)
			if err != nil {
				return writeTarget{}, bare(err)
			}
			resolved = hostpath.FollowLink(resolved, dest)
		case info.IsDir():
			return writeTarget{}, syscall.EISDIR
		case !info.Mode().IsRegular():
			// Opening a FIFO to test permission would block until a reader
			// attaches, so the write itself is the test.
			return writeTarget{path: path, through: true}, nil
		default:
			// A rename needs write permission on the directory and none on the
			// file, so without this a read-only file would become replaceable.
			f, err := os.OpenFile(resolved, os.O_WRONLY, 0)
			if err != nil {
				return writeTarget{}, bare(err)
			}
			f.Close() //nolint:gosec // opened only to test permission
			return writeTarget{path: resolved, mode: info.Mode().Perm()}, nil
		}
	}
}

// WriteFile writes data to path and never leaves a file it replaces half-written.
//
// It writes the file [HostPath] gives for path, the one `yammm check` reads.
// It follows that path's symlinks, so a link survives and the file it names is
// written. A regular file, or one that does not exist yet, is replaced: the
// payload is staged beside it, synced, given its mode and renamed over it. A
// FIFO, a device, /dev/stdin, /dev/stdout, /dev/stderr or /dev/fd/N is written through,
// since no rename can stand in for one. Anything else is refused, and every error names path.
func WriteFile(path string, data []byte) error {
	host, err := HostPath(path)
	var t writeTarget
	if err == nil {
		t, err = resolveWriteTarget(host)
	}
	if err == nil {
		if t.through {
			err = writeThrough(t.path, data)
		} else {
			err = replace(t, data)
		}
	}
	if err != nil {
		return &fs.PathError{Op: "write", Path: path, Err: err}
	}
	return nil
}

// replace stages data beside t.path and renames it over t.path. A staging
// failure is a refusal, never a cue to write in place: that would turn a full
// disk into a truncated file.
func replace(t writeTarget, data []byte) (retErr error) {
	f, err := os.CreateTemp(hostpath.Parent(t.path), stagingPattern)
	if err != nil {
		return fmt.Errorf("stage replacement: %w", bare(err))
	}
	tmp := f.Name()
	defer func() {
		if retErr != nil {
			os.Remove(tmp) //nolint:gosec // best-effort cleanup on a failed write
		}
	}()
	if err := writeSyncChmodClose(f, data, t.mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, t.path); err != nil {
		return fmt.Errorf("rename into place: %w", bare(err))
	}
	return nil
}

// writeThrough continues the stream path holds, for a target no rename can
// replace. It appends and never truncates: Linux reopens /dev/stdout's file, and
// truncating it would erase a `>>` log. A FIFO's open blocks until a reader
// attaches, as os.WriteFile's does.
func writeThrough(path string, data []byte) (retErr error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return bare(err)
	}
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = bare(err)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return bare(err)
	}
	return nil
}

// bare strips the path an [fs.PathError] or [os.LinkError] names, so a cause
// met on a staging file or a resolved link is reported against the operator's
// path instead.
func bare(err error) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return pe.Err
	}
	if le, ok := errors.AsType[*os.LinkError](err); ok {
		return le.Err
	}
	return err
}

// writeSyncChmodClose writes, flushes, chmods and closes f, keeping the close
// error a delayed-allocation filesystem reports a failed write through. The
// chmod precedes the rename: the other order leaves the content readable at
// the wider mode for a moment.
func writeSyncChmodClose(f *os.File, data []byte, mode fs.FileMode) (retErr error) {
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close: %w", bare(err))
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write: %w", bare(err))
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync: %w", bare(err))
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("chmod: %w", bare(err))
	}
	return nil
}
