package hostpath

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
)

// Staging is a file created beside a target, for a write that renames it over
// the target. On darwin and Linux the create and the rename go through one
// descriptor of the directory that holds the target ([stageDir]), so each names
// one element and both name one directory; elsewhere each spells the path.
type Staging struct {
	File *os.File // the staging file, open for reading and writing
	dir  stageDir
	open bool   // whether dir is still held
	name string // the staging file's name in dir
	base string // the target's name in dir
}

// Stage opens the directory that holds target, as the kernel reaches it
// ([Parent]), and creates in it a staging file of mode perm under the umask,
// named name(token) for a random token. The file holds every byte before any
// later chmod, so perm is the widest mode its content may have. A name another
// file already holds is retried, as os.CreateTemp retries. A target in
// directory form ([NamesDirectory]) is refused with EISDIR, as rename(2)
// refuses a file over a directory.
func Stage(target string, perm fs.FileMode, name func(token string) string) (*Staging, error) {
	// filepath.Base drops the ending, so the rename would replace the element
	// itself, a link to a directory among them.
	if NamesDirectory(target) {
		return nil, &fs.PathError{Op: "stage", Path: target, Err: syscall.EISDIR}
	}
	dir, err := openStageDir(Parent(target))
	if err != nil {
		return nil, err
	}
	for range 10000 {
		token := strconv.FormatUint(uint64(rand.Uint32()), 10) //nolint:gosec // O_EXCL makes the name unique; the token only spreads the attempts
		n := name(token)
		f, err := dir.create(n, perm)
		switch {
		case errors.Is(err, fs.ErrExist):
			continue
		case err != nil:
			dir.close()
			return nil, err
		}
		return &Staging{File: f, dir: dir, open: true, name: n, base: filepath.Base(target)}, nil
	}
	dir.close()
	return nil, fmt.Errorf("no unused staging name beside %s: %w", target, fs.ErrExist)
}

// path names the element name of d, written after d's name with no cleaning: a
// cleaned join cancels a ".." against the link before it, and names the
// directory the text names, not the one the kernel reached.
func (d stageDir) path(name string) string {
	if os.IsPathSeparator(d.name[len(d.name)-1]) {
		return d.name + name
	}
	return d.name + string(filepath.Separator) + name
}

// Commit renames the staging file over the target, whose [Staging.File] the
// caller has closed, and releases the directory. A refused rename leaves the
// staging file for [Staging.Discard].
func (s *Staging) Commit() error {
	if err := s.dir.rename(s.name, s.base); err != nil {
		return err
	}
	s.dir.close()
	s.open = false
	return nil
}

// Discard closes the staging file, removes it, and releases the directory. It
// does nothing after a [Staging.Commit] that landed.
func (s *Staging) Discard() {
	if !s.open {
		return
	}
	s.File.Close()       //nolint:gosec // best-effort close before cleanup
	s.dir.remove(s.name) //nolint:errcheck,gosec // best-effort cleanup of a failed write
	s.dir.close()
	s.open = false
}
