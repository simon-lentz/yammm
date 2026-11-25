package main

import (
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// listFiles reads the files a worker's copy holds: the checkout's tracked
// files, and its untracked files git does not ignore. It refuses a checkout
// whose working tree differs from its index, since a copy takes the working
// tree's bytes and indexes them.
func (r *run) listFiles(ctx context.Context) error {
	if _, err := gitOutput(ctx, r.checkout, r.env, "diff", "--quiet"); err != nil {
		if exit, ok := errors.AsType[*exec.ExitError](err); ok && exit.ExitCode() == 1 {
			return refusef("the checkout's unstaged tree is not clean")
		}
		return refusef("%v", err)
	}
	// The out directory is empty, so git lists nothing in it, and what the run
	// writes there later is in no worker's copy.
	list := func(args ...string) ([]string, error) {
		out, err := gitOutput(ctx, r.checkout, r.env, append([]string{"ls-files", "-z"}, args...)...)
		if err != nil {
			return nil, refusef("%v", err)
		}
		names := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00")
		return slices.DeleteFunc(names, func(name string) bool { return name == "" }), nil
	}
	var err error
	if r.tracked, err = list("--cached"); err != nil {
		return err
	}
	if r.others, err = list("--others", "--exclude-standard"); err != nil {
		return err
	}
	if len(r.tracked) == 0 {
		return refusef("the checkout %s tracks no file", r.checkout)
	}
	// git lists a submodule, and an untracked repository, as one entry that is
	// a directory, which no file copy can hold. Each file is opened as the copy
	// will read it, before anything is copied; git lists no FIFO.
	for _, name := range slices.Concat(r.tracked, r.others) {
		path := filepath.Join(r.checkout, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		switch {
		case err != nil:
			return refusef("the checkout holds %s, which cannot be read: %v", name, err)
		case info.IsDir():
			return refusef("the checkout holds %s as a submodule or a nested repository, which a worker's copy cannot hold", name)
		case info.Mode()&fs.ModeSymlink != 0:
		default:
			f, err := os.Open(path)
			if err != nil {
				return refusef("the checkout holds %s, which cannot be read: %v", name, err)
			}
			_ = f.Close()
		}
	}
	return nil
}

// An entry is one path of a worker's tree: a file or symbolic link, or a
// directory the copy did not make.
type entry struct {
	mode fs.FileMode
	sum  [sha256.Size]byte // of a file's bytes, or of a link's target
}

// A tree is one worker's copy of the checkout.
type tree struct {
	run   *run
	dir   string
	files map[string]entry       // by slash-separated path from dir
	dirs  map[string]fs.FileMode // each directory that holds a copied path, with its mode
	// index is the sum of what git reads from the index as copied, its flags
	// among them (readIndex), and objectSize the size of the copy's object names.
	index      [sha256.Size]byte
	objectSize int
	// repository holds every path under .git but the index.
	repository map[string]entry
}

// newTree copies the checkout's listed files to dir and gives the copy an
// index of the tracked ones. Nothing is committed: the copy has the tracked
// set and the bytes of the checkout, and none of its history.
func newTree(ctx context.Context, r *run, dir string) (*tree, error) {
	t := &tree{run: r, dir: dir}
	for _, name := range slices.Concat(r.tracked, r.others) {
		if err := t.copyIn(name); err != nil {
			return nil, err
		}
	}
	if _, err := t.git(ctx, nil, "init", "-q"); err != nil {
		return nil, err
	}
	format, err := t.git(ctx, nil, "rev-parse", "--show-object-format")
	if err != nil {
		return nil, err
	}
	var known bool
	if t.objectSize, known = objectSizes[strings.TrimSpace(string(format))]; !known {
		return nil, fmt.Errorf("the copy's repository has the object format %q", strings.TrimSpace(string(format)))
	}
	if err := t.stage(ctx); err != nil {
		return nil, err
	}
	if t.files, t.dirs, err = t.read(); err != nil {
		return nil, err
	}
	if t.index, err = t.staged(); err != nil {
		return nil, fmt.Errorf("reading the copy's index: %w", err)
	}
	if t.repository, err = t.readRepository(); err != nil {
		return nil, err
	}
	return t, nil
}

// copyIn copies one listed file, or symbolic link, from the checkout.
func (t *tree) copyIn(name string) error {
	from, to := filepath.Join(t.run.checkout, filepath.FromSlash(name)), filepath.Join(t.dir, filepath.FromSlash(name))
	info, err := os.Lstat(from)
	if err != nil {
		return fmt.Errorf("copying the checkout: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(to), 0o750); err != nil {
		return fmt.Errorf("copying the checkout: %w", err)
	}
	// A file the copy may not remove, as an access list can deny, is written in
	// place; restore's last read says whether that put it back. A link cannot be.
	removeErr := os.Remove(to)
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		if removeErr != nil && !errors.Is(removeErr, fs.ErrNotExist) {
			return fmt.Errorf("copying the checkout: %w", removeErr)
		}
		target, err := os.Readlink(from)
		if err == nil {
			err = os.Symlink(target, to)
		}
		if err != nil {
			return fmt.Errorf("copying the checkout: %w", err)
		}
	case info.Mode().IsRegular():
		content, err := os.ReadFile(from)
		if err == nil {
			err = writeFile(to, content, info.Mode().Perm())
		}
		if err != nil {
			return fmt.Errorf("copying the checkout: %w", err)
		}
	default:
		return fmt.Errorf("copying the checkout: %s is neither a file nor a symbolic link", from)
	}
	return nil
}

// writeFile writes content to path with exactly mode, whatever the umask.
func writeFile(path string, content []byte, mode fs.FileMode) error {
	if err := os.WriteFile(path, content, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Chmod(path, mode); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// git runs git on the copy's own repository and work tree, whatever its
// config says. Restore makes the repository again before it runs git, but a
// process the run did not stop can remove it after that: git would then find
// a repository above the copy, and stage would write that repository's index.
func (t *tree) git(ctx context.Context, input []byte, args ...string) ([]byte, error) {
	env := setEnv(setEnv(t.run.env, "GIT_DIR", filepath.Join(t.dir, ".git")), "GIT_WORK_TREE", t.dir)
	return gitInput(ctx, t.dir, env, input, args...)
}

// stage makes the copy's index hold the checkout's tracked files and no other.
func (t *tree) stage(ctx context.Context) error {
	_ = os.Remove(filepath.Join(t.dir, ".git", "index"))
	paths := []byte(strings.Join(t.run.tracked, "\x00"))
	_, err := t.git(ctx, paths, "add", "-f", "--pathspec-from-file=-", "--pathspec-file-nul")
	return err
}

// staged returns the sum of what git reads from the copy's index.
func (t *tree) staged() ([sha256.Size]byte, error) {
	return readIndex(filepath.Join(t.dir, ".git", "index"), t.objectSize)
}

// read walks the copy and returns every file and symbolic link outside .git,
// and every directory it enters with its mode. Once the copy is made, a
// directory it did not make is one entry, and nothing under it is walked.
func (t *tree) read() (files map[string]entry, dirs map[string]fs.FileMode, err error) {
	files, dirs = map[string]entry{}, map[string]fs.FileMode{}
	err = filepath.WalkDir(t.dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(t.dir, path)
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".git" {
				return filepath.SkipDir
			}
			// A directory the copy did not make is reported whole, with what it holds.
			if _, made := t.dirs[rel]; t.dirs != nil && !made {
				files[rel] = entry{mode: fs.ModeDir}
				return filepath.SkipDir
			}
			info, err := d.Info()
			if err != nil {
				return err //nolint:wrapcheck // wrapped below
			}
			dirs[rel] = info.Mode()
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err //nolint:wrapcheck // wrapped below
		}
		e := entry{mode: info.Mode()}
		// A path the copy does not hold is removed whatever it holds, and a test
		// can leave it unreadable.
		if _, held := t.files[rel]; t.files != nil && !held {
			files[rel] = e
			return nil
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			target, err := os.Readlink(path)
			if err != nil {
				return err //nolint:wrapcheck // wrapped below
			}
			e.sum = sha256.Sum256([]byte(target))
		} else if info.Mode().IsRegular() {
			content, err := os.ReadFile(path)
			switch {
			// A held file a test left unreadable without changing its mode, as
			// through an access list, keeps the zero sum: restore copies it again.
			case errors.Is(err, fs.ErrPermission) && t.files != nil:
			case err != nil:
				return err //nolint:wrapcheck // wrapped below
			default:
				e.sum = sha256.Sum256(content)
			}
		}
		files[rel] = e
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("reading the copy: %w", err)
	}
	return files, dirs, nil
}

// readRepository returns every path under the copy's .git but the index: a
// directory by its mode, a file or link by its mode and sum. It fails when a
// path cannot be read, and returns nil when .git is not a directory.
func (t *tree) readRepository() (map[string]entry, error) {
	root := filepath.Join(t.dir, ".git")
	if info, err := os.Lstat(root); err != nil || !info.IsDir() {
		return nil, err //nolint:wrapcheck // the caller wraps it
	}
	held := map[string]entry{}
	err := filepath.WalkDir(root, func(at string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, at)
		if err != nil {
			return err //nolint:wrapcheck // the caller wraps it
		}
		rel = filepath.ToSlash(rel)
		if rel == "index" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err //nolint:wrapcheck // the caller wraps it
		}
		e := entry{mode: info.Mode()}
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, err := os.Readlink(at)
			if err != nil {
				return err //nolint:wrapcheck // the caller wraps it
			}
			e.sum = sha256.Sum256([]byte(target))
		case info.Mode().IsRegular():
			content, err := os.ReadFile(at)
			if err != nil {
				return err //nolint:wrapcheck // the caller wraps it
			}
			e.sum = sha256.Sum256(content)
		}
		held[rel] = e
		return nil
	})
	return held, err //nolint:wrapcheck // the caller wraps it
}

// restore holds the copy to what was copied. It returns the paths that
// differed, each restored from the checkout, removed, or given back its copied
// mode, with .git when it made the copy's repository again and .git/index
// when it staged the index again. It fails when the copy cannot be made the
// same again, since a later run would judge another tree, and every failure
// reads "<dir> cannot be restored: <cause>".
func (t *tree) restore(ctx context.Context) ([]string, error) {
	differed, err := t.putBack(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be restored: %w", t.dir, err)
	}
	return differed, nil
}

// putBack does restore's work and returns a failure without restore's prefix.
func (t *tree) putBack(ctx context.Context) (differed []string, err error) {
	// Modes come first, each directory before what it holds: a directory left
	// without read or search permission cannot be walked, one without write
	// permission refuses the removal or making of each entry it holds, and a
	// file left unreadable cannot be compared. Nothing below a directory that
	// is gone, or that a test made a file or a link, is touched here: a link
	// would lead the change out of the copy.
	gone := map[string]bool{}
	dirs := slices.SortedFunc(maps.Keys(t.dirs), func(a, b string) int {
		return cmp.Or(cmp.Compare(depth(a), depth(b)), strings.Compare(a, b))
	})
	for _, name := range dirs {
		if name != "." && gone[path.Dir(name)] {
			gone[name] = true
			continue
		}
		at := filepath.Join(t.dir, filepath.FromSlash(name))
		info, err := os.Lstat(at)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("giving back its modes: %w", err)
		}
		if err != nil || !info.IsDir() {
			gone[name] = true
			continue
		}
		if info.Mode() != t.dirs[name] {
			differed = append(differed, name)
			if err := os.Chmod(at, t.dirs[name]); err != nil {
				return nil, fmt.Errorf("giving back its modes: %w", err)
			}
		}
	}
	// Paths are taken in order, so a failure names the same path run after run.
	for _, name := range slices.Sorted(maps.Keys(t.files)) {
		e := t.files[name]
		if !e.mode.IsRegular() || gone[path.Dir(name)] {
			continue
		}
		at := filepath.Join(t.dir, filepath.FromSlash(name))
		if info, err := os.Lstat(at); err == nil && info.Mode().IsRegular() && info.Mode() != e.mode {
			differed = append(differed, name)
			if err := os.Chmod(at, e.mode.Perm()); err != nil {
				return nil, fmt.Errorf("giving back its modes: %w", err)
			}
		}
	}
	now, _, err := t.read()
	if err != nil {
		return nil, err
	}
	for _, name := range slices.Sorted(maps.Keys(now)) {
		if want, ok := t.files[name]; !ok {
			differed = append(differed, name)
			if err := removeAll(filepath.Join(t.dir, filepath.FromSlash(name))); err != nil {
				return nil, err
			}
		} else if now[name] != want {
			differed = append(differed, name)
		}
	}
	for name := range t.files {
		if _, ok := now[name]; !ok {
			differed = append(differed, name)
		}
	}
	// A repository a test changed, removed, or left unreadable is made again
	// and staged below, since its config can change what git in the copy sees.
	// The removal pass above has already removed a .git that is not a directory.
	if repository, err := t.readRepository(); err != nil || !maps.Equal(repository, t.repository) {
		if err := removeAll(filepath.Join(t.dir, ".git")); err != nil {
			return nil, err
		}
		if _, err := t.git(ctx, nil, "init", "-q"); err != nil {
			return nil, err
		}
		differed = append(differed, ".git")
	}
	slices.Sort(differed)
	differed = slices.Compact(differed)
	for _, name := range differed {
		want, ok := t.files[name]
		if !ok {
			continue
		}
		// A path a test made another kind of entry, a directory with files in it
		// among them, goes whole before the copy.
		if e, found := now[name]; found && e.mode.Type() != want.mode.Type() {
			if err := removeAll(filepath.Join(t.dir, filepath.FromSlash(name))); err != nil {
				return nil, err
			}
		}
		if err := t.copyIn(name); err != nil {
			return nil, err
		}
	}
	// An index git cannot read, or one a test changed, is staged again.
	if index, err := t.staged(); err != nil || index != t.index {
		differed = append(differed, ".git/index")
		if err := t.stage(ctx); err != nil {
			return nil, err
		}
	}
	if len(differed) == 0 {
		return nil, nil
	}
	now, nowDirs, err := t.read()
	if err != nil {
		return nil, err
	}
	// A held path unlike what was copied names the cause, which the index,
	// staged from the copy's files, would otherwise hide.
	if len(now) == len(t.files) {
		if err := t.unlike(now); err != nil {
			return nil, err
		}
	}
	index, err := t.staged()
	if err != nil {
		return nil, fmt.Errorf("reading the copy's index: %w", err)
	}
	repository, err := t.readRepository()
	if err != nil {
		return nil, fmt.Errorf("reading the copy's repository: %w", err)
	}
	if index != t.index || len(now) != len(t.files) || !maps.Equal(nowDirs, t.dirs) || !maps.Equal(repository, t.repository) {
		return nil, fmt.Errorf("the copy still differs from what was copied after putting back %s", strings.Join(differed, " "))
	}
	return differed, nil
}

// unlike names why now, the copy as restore last reads it with every path
// counted, is not what was copied, by the first path in order, or returns nil.
// A held file read with no sum cannot be read. Restore writes only the
// checkout's bytes, so another held path absent or other while the checkout
// still holds what was copied was changed by a process the run did not stop;
// otherwise the checkout changed.
func (t *tree) unlike(now map[string]entry) error {
	for _, name := range slices.Sorted(maps.Keys(t.files)) {
		want := t.files[name]
		got, ok := now[name]
		switch {
		case ok && got == want:
			continue
		case ok && got.mode.IsRegular() && got.sum == [sha256.Size]byte{}:
			return fmt.Errorf("%s cannot be read", name)
		case !t.checkoutHolds(name, want):
			return fmt.Errorf("%s is not what was copied, so the checkout changed during the run", name)
		case !ok:
			return fmt.Errorf("%s is gone after it was copied back, so a process the run did not stop removes it", name)
		default:
			return fmt.Errorf("%s is not what was copied back from the checkout, so a process the run did not stop changes it", name)
		}
	}
	return nil
}

// checkoutHolds reports whether the checkout's name is still the kind of entry
// want is, with want's bytes, or link target, as read reads them, and a file's
// permission bits, which copyIn copies. A link's mode is not compared: it is
// the umask's on darwin and the BSDs, and always 0777 on Linux.
func (t *tree) checkoutHolds(name string, want entry) bool {
	from := filepath.Join(t.run.checkout, filepath.FromSlash(name))
	info, err := os.Lstat(from)
	if err != nil || info.Mode().Type() != want.mode.Type() {
		return false
	}
	if info.Mode().IsRegular() && info.Mode().Perm() != want.mode.Perm() {
		return false
	}
	var content []byte
	if info.Mode()&fs.ModeSymlink != 0 {
		target, err := os.Readlink(from)
		if err != nil {
			return false
		}
		content = []byte(target)
	} else if content, err = os.ReadFile(from); err != nil {
		return false
	}
	return sha256.Sum256(content) == want.sum
}

// depth counts the directories above name, a path from a tree's root; the root
// itself, ".", is above every other.
func depth(name string) int {
	if name == "." {
		return -1
	}
	return strings.Count(name, "/")
}

// errNoMatch says a mutant changed nothing.
var errNoMatch = errors.New("the search is not in the file, or the replacement leaves the file as it was")

// mutate rewrites m's file in the copy and returns a function that puts its
// bytes back.
func (t *tree) mutate(m mutant) (undo func() error, matches int, err error) {
	path := filepath.Join(t.dir, m.file)
	info, err := os.Stat(path)
	if err != nil {
		return nil, 0, fmt.Errorf("the worker's copy: %w", err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, fmt.Errorf("the worker's copy: %w", err)
	}
	matches = bytes.Count(original, m.search)
	changed := bytes.ReplaceAll(original, m.search, m.replace)
	if matches == 0 || bytes.Equal(changed, original) {
		return nil, matches, errNoMatch
	}
	if err := writeFile(path, changed, info.Mode().Perm()); err != nil {
		return nil, matches, err
	}
	return func() error { return writeFile(path, original, info.Mode().Perm()) }, matches, nil
}
