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
// Owner-only, because a snapshot the CLI writes carries every value the graph
// held, and 0o644 publishes it to every account on the host.
const NewFileMode fs.FileMode = 0o600

// stagingName is the name of a staging file for a random token. Its length does
// not depend on the target's name, so a basename near the name limit can still
// be replaced, and it ends in [snapshot.TmpSuffix], which [snapshot.ScanDir]
// skips, so a staging file a crash leaves is never scanned.
func stagingName(token string) string { return ".yammm-" + token + snapshot.TmpSuffix }

// maxLinkHops bounds how many symlinks a write follows — the bound Linux sets
// (MAXSYMLINKS) — so a loop is refused rather than followed.
const maxLinkHops = 40

// writeTarget is where a write to an operator-named path lands, and how.
type writeTarget struct {
	path    string      // the file a replacement lands on: the named path, its symlinks followed
	mode    fs.FileMode // the mode a replacement carries
	through bool        // written through the named path in place rather than replaced
	stream  string      // for a write through, the link hop that names the descriptor, FIFO or device
	names   []string    // the named path, then each link destination the write follows
}

// resolveWriteTarget follows the symlinks of path, which [HostPath] gave, and
// decides how a write to it lands: an absent or regular file is replaced; a
// descriptor path, or any file that is neither regular nor a directory, such as
// a FIFO or a device, is written through; a directory, a loop of links, a
// regular file it cannot open for writing and a path it cannot stat or read as
// a link are refused, as is a link target in directory form that
// [hostpath.RefusedWrite] refuses. A descriptor path is decided by its name, because
// /dev/stdout stats as whatever its descriptor holds; every other path by the
// file it reaches.
func resolveWriteTarget(path string) (writeTarget, error) {
	resolved := path
	names := []string{path}
	for hops := 0; ; hops++ {
		if hostpath.Descriptor(resolved) {
			return writeTarget{path: path, through: true, stream: resolved, names: names}, nil
		}
		info, err := os.Lstat(resolved)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return writeTarget{path: resolved, mode: NewFileMode, names: names}, nil
		case err != nil:
			return writeTarget{}, bare(err)
		case info.Mode()&fs.ModeSymlink != 0:
			if hops == maxLinkHops {
				return writeTarget{}, syscall.ELOOP
			}
			next, err := hostpath.Hop(resolved)
			if err != nil {
				return writeTarget{}, bare(err)
			}
			resolved = next
			if err := hostpath.RefusedWrite(resolved); err != nil {
				return writeTarget{}, err
			}
			names = append(names, resolved)
		case info.IsDir():
			return writeTarget{}, syscall.EISDIR
		case !info.Mode().IsRegular():
			// Opening a FIFO to test permission would block until a reader
			// attaches, so the write itself is the test.
			return writeTarget{path: path, through: true, stream: resolved, names: names}, nil
		default:
			// A rename needs write permission on the directory and none on the
			// file, so without this a read-only file would become replaceable.
			f, err := os.OpenFile(resolved, os.O_WRONLY, 0)
			if err != nil {
				return writeTarget{}, bare(err)
			}
			f.Close() //nolint:gosec // opened only to test permission
			return writeTarget{path: resolved, mode: info.Mode().Perm(), names: names}, nil
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
// A path that ends in a separator or a "." element names a directory, so it is
// refused whatever its last element is, before anything is staged.
func WriteFile(path string, data []byte) error {
	host, err := HostPath(path)
	if err == nil {
		err = hostpath.RefusedWrite(host)
	}
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

// WrittenNames returns the names a [WriteFile] of path reaches its file
// through: the path [HostPath] gives, then each symlink destination it follows,
// up to the file, or to the descriptor path, FIFO or device it writes through.
func WrittenNames(path string) ([]string, error) {
	host, err := HostPath(path)
	if err != nil {
		return nil, err
	}
	t, err := resolveWriteTarget(host)
	if err != nil {
		return nil, err
	}
	return t.names, nil
}

// replace stages data beside t.path and renames it over t.path. A staging
// failure is a refusal, never a cue to write in place: that would turn a full
// disk into a truncated file.
func replace(t writeTarget, data []byte) (retErr error) {
	stage, err := t.stage()
	if err != nil {
		return fmt.Errorf("stage replacement: %w", bare(err))
	}
	defer func() {
		if retErr != nil {
			stage.Discard()
		}
	}()
	if err := writeSyncChmodClose(stage.File, data, t.mode); err != nil {
		return err
	}
	if err := stage.Commit(); err != nil {
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

// stage creates the staging file for t at the mode the write ends with, so the
// bytes are never readable more widely than the file they replace.
func (t writeTarget) stage() (*hostpath.Staging, error) {
	return hostpath.Stage(t.path, t.mode, stagingName)
}

// writeSyncChmodClose writes, flushes, chmods and closes f, keeping the close
// error a delayed-allocation filesystem reports a failed write through. f was
// created at mode under the umask, so the bytes are never readable more widely
// than mode; the chmod sets the bits the umask cleared, and it precedes the
// rename, so the replacement lands with its whole mode.
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
