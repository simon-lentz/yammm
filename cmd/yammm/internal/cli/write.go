package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

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

// maxLinkHops bounds how many symlinks a write follows — the kernel's own
// bound — so a loop is refused rather than followed.
const maxLinkHops = 40

// errNotReplaceable refuses a target a set of files cannot rename into place.
var errNotReplaceable = errors.New("not a regular file, so a set of files cannot replace it")

// writeTarget is where a write to an operator-named path lands, and how.
type writeTarget struct {
	path    string      // the file a replacement lands on: the named path, its symlinks followed
	mode    fs.FileMode // the mode a replacement carries
	through bool        // written through the named path in place rather than replaced
}

// resolveWriteTarget follows path's symlinks and decides how a write to it
// lands: an absent or regular file is replaced; a FIFO, a device or a path
// under /dev/ is written through; anything else is refused. /dev/ is decided by
// the path, because /dev/stdout stats as whatever its descriptor holds.
func resolveWriteTarget(path string) (writeTarget, error) {
	resolved := path
	for hops := 0; ; hops++ {
		if underDev(resolved) {
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
			if !filepath.IsAbs(dest) {
				dest = filepath.Join(filepath.Dir(resolved), dest)
			}
			resolved = dest
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

// underDev reports whether path names something under /dev/.
func underDev(path string) bool {
	abs, err := filepath.Abs(path)
	return err == nil && strings.HasPrefix(abs, "/dev/")
}

// WriteFile writes data to path and never leaves a regular file half-written.
//
// It follows path's symlinks, so a link survives and the file it names is
// written. A regular file, or one that does not exist yet, is replaced: the
// payload is staged beside it, synced, given its mode and renamed over it. A
// FIFO, a device or a path under /dev/ is written through, since no rename can
// stand in for one. Anything else is refused, and every error names path.
func WriteFile(path string, data []byte) error {
	t, err := resolveWriteTarget(path)
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
	f, err := os.CreateTemp(filepath.Dir(t.path), stagingPattern)
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

// StagedFiles collects a set of files and puts them in place together.
//
// A partial set is worse than none: a directory that already looks like a
// complete export, with one type's file missing and nothing saying so, is read
// as the whole graph. Every file is staged beside its target and renamed only
// after the last one is written; a failure removes the staging files and leaves
// the directory as it was found.
//
// Renaming a staging DIRECTORY over the target would be simpler and is wrong:
// the output directory may hold files the operator put there, and a directory
// rename deletes them.
type StagedFiles struct {
	dir    string
	staged []stagedFile
}

type stagedFile struct {
	tmp    string
	name   string // the name the caller gave, which every error reports
	target string // where the rename lands: the name's symlinks followed
	mode   fs.FileMode
	file   *os.File
}

// NewStagedFiles prepares a staged write into dir, creating it if needed.
func NewStagedFiles(dir string) (*StagedFiles, error) {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	return &StagedFiles{dir: dir}, nil
}

// Create stages one file named name inside the set's directory and returns the
// writer for its contents. The file appears at its real name only at
// [StagedFiles.Commit]. It refuses, before anything is written, a target
// [WriteFile] would refuse and a target only a write in place could reach.
func (s *StagedFiles) Create(name string) (io.Writer, error) {
	// Two names that differ only in case are one file on a case-insensitive
	// filesystem: the second rename replaces the first's contents while the
	// directory keeps the first's spelling, so the set ends one file short and
	// the survivor carries one member's name over another's data. Refused on
	// every filesystem, because the directory is a portable artefact and the
	// caller cannot know where it will be read.
	for _, st := range s.staged {
		if strings.EqualFold(st.name, name) && st.name != name {
			return nil, fmt.Errorf("%s and %s differ only in case and cannot share one directory", st.name, name)
		}
	}
	path := filepath.Join(s.dir, name)
	t, err := resolveWriteTarget(path)
	if err == nil && t.through {
		err = errNotReplaceable
	}
	if err != nil {
		return nil, &fs.PathError{Op: "write", Path: path, Err: err}
	}
	f, err := os.CreateTemp(filepath.Dir(t.path), stagingPattern)
	if err != nil {
		return nil, &fs.PathError{Op: "write", Path: path, Err: fmt.Errorf("stage replacement: %w", bare(err))}
	}
	s.staged = append(s.staged, stagedFile{tmp: f.Name(), name: name, target: t.path, mode: t.mode, file: f})
	return f, nil
}

// Commit flushes every staged file and renames them all into place.
//
// A rename that fails after earlier ones succeeded cannot be undone, so
// everything that can fail happens first: the writes are flushed, chmod'd and
// closed for every file, and every target is checked to be renameable, before
// any rename runs. A directory that appeared where a file belongs since
// [StagedFiles.Create] is refused here rather than half-way through the set.
func (s *StagedFiles) Commit() error {
	for i := range s.staged {
		if info, err := os.Stat(s.staged[i].target); err == nil && info.IsDir() {
			s.Rollback()
			return fmt.Errorf("%s exists and is a directory", s.staged[i].name)
		}
	}
	for i := range s.staged {
		sf := &s.staged[i]
		err := syncChmodClose(sf.file, sf.mode)
		// Closed either way, so Rollback must not close it a second time.
		sf.file = nil
		if err != nil {
			s.Rollback()
			return fmt.Errorf("%s: %w", sf.name, err)
		}
	}
	for i := range s.staged {
		sf := &s.staged[i]
		if err := os.Rename(sf.tmp, sf.target); err != nil {
			s.Rollback()
			return fmt.Errorf("rename %s: %w", sf.name, bare(err))
		}
		sf.tmp = ""
	}
	return nil
}

// Rollback removes every staging file that has not been renamed into place.
// It is safe to call after [StagedFiles.Commit] and is idempotent.
func (s *StagedFiles) Rollback() {
	for i := range s.staged {
		sf := &s.staged[i]
		if sf.file != nil {
			sf.file.Close() //nolint:gosec // best-effort close before cleanup
			sf.file = nil
		}
		if sf.tmp != "" {
			os.Remove(sf.tmp) //nolint:gosec // best-effort cleanup
			sf.tmp = ""
		}
	}
}

// syncChmodClose is [writeSyncChmodClose] for a file something else has already
// written to.
func syncChmodClose(f *os.File, mode fs.FileMode) (retErr error) {
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close: %w", bare(err))
		}
	}()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync: %w", bare(err))
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("chmod: %w", bare(err))
	}
	return nil
}
