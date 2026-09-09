package cli

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/simon-lentz/yammm/snapshot"
)

// NewFileMode is the mode [WriteFile] gives a file that does not exist yet.
//
// Owner-only, because a CLI write carries whatever the graph held: an export
// can contain every value in the schema, and 0o644 publishes it to every
// account on the host.
const NewFileMode fs.FileMode = 0o600

// WriteFile writes data to path durably, keeping the mode the file already has.
//
// The payload is staged on a unique sibling name, synced, given its mode and
// only then renamed over path, so an interrupted write leaves the previous file
// intact rather than a truncated one. A file that does not exist yet is created
// at [NewFileMode]; an existing file keeps its own mode, whatever it is, because
// the CLI is replacing content and not taking over the operator's policy.
//
// The chmod runs on the staging file BEFORE the rename. The cheaper order —
// write, rename, then chmod the target — leaves a window in which the finished
// content is readable at the wider mode, which is the case this exists for: an
// export of restricted data onto a shared host. Do not simplify it back.
//
// It is deliberately not [snapshot.WriteFile], which is a library contract a
// consumer depends on and documents the opposite policy: 0o666 subject to
// umask, with callers told to chmod afterwards. That obligation is what this
// discharges for every CLI write, in the one order that closes the window.
func WriteFile(path string, data []byte) (retErr error) {
	mode, err := targetMode(path)
	if err != nil {
		return err
	}

	f, err := os.CreateTemp(filepath.Dir(path), stagingPattern(path))
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	defer func() {
		if retErr != nil {
			os.Remove(tmp) //nolint:gosec // best-effort cleanup on a failed write
		}
	}()

	if err := writeSyncChmodClose(f, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// targetMode returns the mode a write to path must produce, and refuses a
// target this process may not write.
//
// The permission check is not redundant: renaming a staging file over a target
// needs write permission on the DIRECTORY and none at all on the file, so
// without it the CLI would quietly gain the ability to replace a file the
// operator marked read-only — which every one of these paths refused while it
// used os.WriteFile. Durability is the change being made here; the authority to
// overwrite is not.
func targetMode(path string) (fs.FileMode, error) {
	info, err := os.Stat(path)
	if err != nil {
		return NewFileMode, nil //nolint:nilerr // a target that does not exist yet is created
	}
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return 0, err
	}
	f.Close() //nolint:gosec // opened only to test permission
	return info.Mode().Perm(), nil
}

// stagingPattern returns the [os.CreateTemp] pattern for path's staging file.
//
// It ends in [snapshot.TmpSuffix] because that is the suffix
// [snapshot.ScanDir] skips: a staging file an interrupted write leaves behind
// must be invisible to a directory scan, and a name ending in ".ys" is instead
// reported as a malformed snapshot.
func stagingPattern(path string) string {
	return filepath.Base(path) + ".*" + snapshot.TmpSuffix
}

// writeSyncChmodClose writes data to f, flushes it, sets its mode and closes it.
//
// The close error is reported rather than discarded: on a filesystem that
// delays allocation, a write that cannot be satisfied is reported at close and
// nowhere else, so dropping it turns a failed write into a silent success.
func writeSyncChmodClose(f *os.File, data []byte, mode fs.FileMode) (retErr error) {
	defer func() {
		if err := f.Close(); err != nil && retErr == nil {
			retErr = fmt.Errorf("close temp: %w", err)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
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
	target string
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
// writer for its contents. The file appears at its real name only at [StagedFiles.Commit].
func (s *StagedFiles) Create(name string) (io.Writer, error) {
	target := filepath.Join(s.dir, name)
	mode := NewFileMode
	if info, err := os.Stat(target); err == nil {
		mode = info.Mode().Perm()
	}
	f, err := os.CreateTemp(s.dir, stagingPattern(target))
	if err != nil {
		return nil, fmt.Errorf("create temp for %s: %w", name, err)
	}
	s.staged = append(s.staged, stagedFile{tmp: f.Name(), target: target, mode: mode, file: f})
	return f, nil
}

// Commit flushes every staged file and renames them all into place.
//
// A rename that fails after earlier ones succeeded cannot be undone, so
// everything that can fail happens first: the writes are flushed, chmod'd and
// closed for every file, and every target is checked to be renameable, before
// any rename runs. A directory standing where a file belongs is the reachable
// case and is refused here rather than half-way through the set.
func (s *StagedFiles) Commit() error {
	for i := range s.staged {
		if info, err := os.Stat(s.staged[i].target); err == nil && info.IsDir() {
			s.Rollback()
			return fmt.Errorf("%s exists and is a directory", filepath.Base(s.staged[i].target))
		}
	}
	for i := range s.staged {
		sf := &s.staged[i]
		err := syncChmodClose(sf.file, sf.mode)
		// Closed either way, so Rollback must not close it a second time.
		sf.file = nil
		if err != nil {
			s.Rollback()
			return fmt.Errorf("%s: %w", filepath.Base(sf.target), err)
		}
	}
	for i := range s.staged {
		sf := &s.staged[i]
		if err := os.Rename(sf.tmp, sf.target); err != nil {
			s.Rollback()
			return fmt.Errorf("rename %s: %w", filepath.Base(sf.target), err)
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
			retErr = fmt.Errorf("close temp: %w", err)
		}
	}()
	if err := f.Sync(); err != nil {
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("chmod temp: %w", err)
	}
	return nil
}
