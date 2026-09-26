package cli

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
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

// errNotReplaceable refuses a target a set of files cannot rename into place.
var errNotReplaceable = errors.New("not a regular file, so a set of files cannot replace it")

// writeTarget is where a write to an operator-named path lands, and how.
type writeTarget struct {
	path    string      // the file a replacement lands on: the named path, its symlinks followed
	mode    fs.FileMode // the mode a replacement carries
	through bool        // written through the named path in place rather than replaced
}

// resolveWriteTarget follows the symlinks of path, which [HostPath] gave, and
// decides how a write to it lands: an absent or regular file is replaced; a
// FIFO, a device or a path under /dev/ is written through; anything else is
// refused. /dev/ is decided by the path, because /dev/stdout stats as whatever
// its descriptor holds.
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

// underDev reports whether path names something under /dev/: its text, each
// ".." in it taken as the kernel takes it, or the directory holding it once
// resolved on disk, which a link to /dev reaches without spelling it.
func underDev(path string) bool {
	if abs, err := filepath.Abs(kernelText(path)); err == nil && strings.HasPrefix(abs, "/dev/") {
		return true
	}
	dir, err := filepath.EvalSymlinks(hostpath.Parent(path))
	if err == nil {
		dir, err = filepath.Abs(dir)
	}
	return err == nil && (dir == "/dev" || strings.HasPrefix(dir, "/dev/"))
}

// WriteFile writes data to path and never leaves a regular file half-written.
//
// It writes the file [HostPath] gives for path, the one `yammm check` reads.
// It follows that path's symlinks, so a link survives and the file it names is
// written. A regular file, or one that does not exist yet, is replaced: the
// payload is staged beside it, synced, given its mode and renamed over it. A
// FIFO, a device or a path under /dev/ is written through, since no rename can
// stand in for one. Anything else is refused, and every error names path.
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

// A NamedFile is one file of the set [WriteFileSet] writes: its name inside the
// set's directory and its contents.
type NamedFile struct {
	Name string
	Data []byte
}

// stagedFile is one [NamedFile] written to its staging file and waiting for its
// rename.
type stagedFile struct {
	tmp    string
	name   string // the name the caller gave, which every error reports
	target string // where the rename lands: the name's symlinks followed
}

// WriteFileSet writes files into the directory [HostPath] gives for dir, all of
// them or, but for a rename the filesystem refuses after others succeeded, none:
// each is staged beside its target and renamed only once the last is staged, and
// a failure removes the staging files, since a partial set reads as a complete
// export with one type's file missing. The directory, with any
// missing parent, is made once the names are judged and is never removed, so a
// caller that must create nothing for a refused set judges its data first. A
// staging directory renamed over the target would be simpler and would delete
// the files the operator put there.
func WriteFileSet(dir string, files []NamedFile) error {
	set, err := stageFileSet(dir, files)
	if err != nil {
		return err
	}
	return set.commit()
}

// stagedSet is a set of files [stageFileSet] staged and [stagedSet.commit] has
// not yet renamed into place.
type stagedSet []stagedFile

// stageFileSet judges the names, creates dir and stages every file. A failure
// removes the staging files it made.
func stageFileSet(dir string, files []NamedFile) (stagedSet, error) {
	if err := judgeNames(files); err != nil {
		return nil, err
	}
	host, err := HostPath(dir)
	if err != nil {
		return nil, fmt.Errorf("create output directory: %w", &fs.PathError{Op: "mkdir", Path: dir, Err: err})
	}
	if err := os.MkdirAll(host, 0o750); err != nil {
		return nil, fmt.Errorf("create output directory: %w", err)
	}
	set := make(stagedSet, 0, len(files))
	for _, nf := range files {
		sf, err := stage(dir, nf)
		if err == nil {
			// Two names whose links reach one file would leave the set one file
			// short, as two names differing in case would.
			for _, earlier := range set {
				if earlier.sameTarget(sf) {
					os.Remove(sf.tmp) //nolint:gosec // best-effort cleanup
					err = fmt.Errorf("%s and %s name one file, %s", earlier.name, sf.name, sf.target)
					break
				}
			}
		}
		if err != nil {
			set.discard()
			return nil, err
		}
		set = append(set, sf)
	}
	return set, nil
}

// commit renames every staged file into place, first refusing a directory that
// appeared where a file belongs since it was staged, because a rename that fails
// after earlier ones succeeded cannot be undone. A failure removes the staging
// files not yet renamed.
func (s stagedSet) commit() error {
	for _, sf := range s {
		if info, err := os.Stat(sf.target); err == nil && info.IsDir() {
			s.discard()
			return fmt.Errorf("%s exists and is a directory", sf.name)
		}
	}
	for i := range s {
		if err := os.Rename(s[i].tmp, s[i].target); err != nil {
			s.discard()
			return fmt.Errorf("rename %s: %w", s[i].name, bare(err))
		}
		s[i].tmp = ""
	}
	return nil
}

// discard removes every staging file of s not yet renamed into place.
func (s stagedSet) discard() {
	for i := range s {
		if s[i].tmp != "" {
			os.Remove(s[i].tmp) //nolint:gosec // best-effort cleanup
			s[i].tmp = ""
		}
	}
}

// sameTarget reports whether a's and s's renames land on one file: one file
// that exists, or one name in one directory, compared as [judgeNames] compares
// names, where the file does not exist yet.
func (s stagedFile) sameTarget(a stagedFile) bool {
	si, serr := os.Stat(s.target)
	ai, aerr := os.Stat(a.target)
	if serr == nil && aerr == nil {
		return os.SameFile(si, ai)
	}
	sd, serr := os.Stat(hostpath.Parent(s.target))
	ad, aerr := os.Stat(hostpath.Parent(a.target))
	return serr == nil && aerr == nil && os.SameFile(sd, ad) &&
		strings.EqualFold(filepath.Base(s.target), filepath.Base(a.target))
}

// judgeNames refuses a set whose names cannot share one directory, before
// anything is created.
func judgeNames(files []NamedFile) error {
	for i, nf := range files {
		if nf.Name == "." || nf.Name == ".." || filepath.Base(nf.Name) != nf.Name {
			return fmt.Errorf("%q is not a file name inside the output directory", nf.Name)
		}
		// Two names that differ only in case are one file on a
		// case-insensitive filesystem: the second rename replaces the first's
		// contents while the directory keeps the first's spelling, so the set
		// ends one file short and the survivor carries one member's name over
		// another's data. Refused on every filesystem, because the directory is
		// a portable artefact and the caller cannot know where it will be read.
		for _, earlier := range files[:i] {
			switch {
			case earlier.Name == nf.Name:
				return fmt.Errorf("%s is named twice in one set", nf.Name)
			case strings.EqualFold(earlier.Name, nf.Name):
				return fmt.Errorf("%s and %s differ only in case and cannot share one directory", earlier.Name, nf.Name)
			}
		}
	}
	return nil
}

// stage writes nf to a staging file beside the file its name reaches inside
// dir. It refuses, before anything is written, a target [WriteFile] would
// refuse and a target only a write in place could reach.
func stage(dir string, nf NamedFile) (stagedFile, error) {
	// The directory and the name are joined as text, the rule HostPath applies,
	// so the file lands in the directory WriteFileSet made.
	path := filepath.Join(dir, nf.Name)
	host, err := HostPath(path)
	var t writeTarget
	if err == nil {
		t, err = resolveWriteTarget(host)
	}
	if err == nil && t.through {
		err = errNotReplaceable
	}
	var f *os.File
	if err == nil {
		f, err = os.CreateTemp(hostpath.Parent(t.path), stagingPattern)
		if err != nil {
			err = fmt.Errorf("stage replacement: %w", bare(err))
		}
	}
	if err == nil {
		if err = writeSyncChmodClose(f, nf.Data, t.mode); err != nil {
			os.Remove(f.Name()) //nolint:gosec // best-effort cleanup on a failed write
		}
	}
	if err != nil {
		return stagedFile{}, &fs.PathError{Op: "write", Path: path, Err: err}
	}
	return stagedFile{tmp: f.Name(), name: nf.Name, target: t.path}, nil
}
