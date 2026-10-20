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
	index [sha256.Size]byte      // of what git lists as staged
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
	if _, err := gitOutput(ctx, dir, r.env, "init", "-q"); err != nil {
		return nil, err
	}
	if err := t.stage(ctx); err != nil {
		return nil, err
	}
	var err error
	if t.files, t.dirs, err = t.read(); err != nil {
		return nil, err
	}
	if t.index, err = t.staged(ctx); err != nil {
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

// stage makes the copy's index hold the checkout's tracked files and no other.
func (t *tree) stage(ctx context.Context) error {
	_ = os.Remove(filepath.Join(t.dir, ".git", "index"))
	paths := []byte(strings.Join(t.run.tracked, "\x00"))
	_, err := gitInput(ctx, t.dir, t.run.env, paths, "add", "-f", "--pathspec-from-file=-", "--pathspec-file-nul")
	return err
}

func (t *tree) staged(ctx context.Context) ([sha256.Size]byte, error) {
	out, err := gitOutput(ctx, t.dir, t.run.env, "ls-files", "-z", "--stage")
	return sha256.Sum256(out), err
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
		return nil, nil, fmt.Errorf("reading %s: %w", t.dir, err)
	}
	return files, dirs, nil
}

// restore holds the copy to what was copied. It returns the paths that
// differed, each restored from the checkout, removed, or given back its copied
// mode, with .git/index when it staged the index again. It fails when the
// copy cannot be made the same again: a later run would judge another tree.
func (t *tree) restore(ctx context.Context) (differed []string, err error) {
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
			return nil, fmt.Errorf("restoring %s: %w", t.dir, err)
		}
		if err != nil || !info.IsDir() {
			gone[name] = true
			continue
		}
		if info.Mode() != t.dirs[name] {
			differed = append(differed, name)
			if err := os.Chmod(at, t.dirs[name]); err != nil {
				return nil, fmt.Errorf("restoring %s: %w", t.dir, err)
			}
		}
	}
	for name, e := range t.files {
		if !e.mode.IsRegular() || gone[path.Dir(name)] {
			continue
		}
		at := filepath.Join(t.dir, filepath.FromSlash(name))
		if info, err := os.Lstat(at); err == nil && info.Mode().IsRegular() && info.Mode() != e.mode {
			differed = append(differed, name)
			if err := os.Chmod(at, e.mode.Perm()); err != nil {
				return nil, fmt.Errorf("restoring %s: %w", t.dir, err)
			}
		}
	}
	now, _, err := t.read()
	if err != nil {
		return nil, err
	}
	for name, e := range now {
		if want, ok := t.files[name]; !ok {
			differed = append(differed, name)
			if err := removeAll(filepath.Join(t.dir, filepath.FromSlash(name))); err != nil {
				return nil, err
			}
		} else if e != want {
			differed = append(differed, name)
		}
	}
	for name := range t.files {
		if _, ok := now[name]; !ok {
			differed = append(differed, name)
		}
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
	index, err := t.staged(ctx)
	if err != nil {
		return nil, err
	}
	if index != t.index {
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
	index, err = t.staged(ctx)
	if err != nil {
		return nil, err
	}
	if index != t.index || len(now) != len(t.files) || !maps.Equal(nowDirs, t.dirs) {
		return nil, fmt.Errorf("%s cannot be restored to what was copied (%s)", t.dir, strings.Join(differed, " "))
	}
	for name, want := range t.files {
		switch got, ok := now[name]; {
		case got == want:
		case ok && got.mode.IsRegular() && got.sum == [sha256.Size]byte{}:
			return nil, fmt.Errorf("%s cannot be restored: %s cannot be read", t.dir, name)
		default:
			return nil, fmt.Errorf("%s cannot be restored: %s is not what was copied, so the checkout changed during the run", t.dir, name)
		}
	}
	return differed, nil
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
