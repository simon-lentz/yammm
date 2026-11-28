package hostpath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
)

// TestErrNotDirectory_MatchesENOTDIRAndNoMissingPath holds the refusal's two
// readings on every host: errors.Is finds syscall.ENOTDIR in it, never
// fs.ErrNotExist, and its text is Unix's ENOTDIR text, wrapped or not.
func TestErrNotDirectory_MatchesENOTDIRAndNoMissingPath(t *testing.T) {
	t.Parallel()
	wrapped := &fs.PathError{Op: "stat", Path: "f/", Err: ErrNotDirectory}
	for _, err := range []error{ErrNotDirectory, wrapped} {
		if !errors.Is(err, syscall.ENOTDIR) {
			t.Errorf("errors.Is(%v, syscall.ENOTDIR) = false", err)
		}
		if errors.Is(err, fs.ErrNotExist) {
			t.Errorf("errors.Is(%v, fs.ErrNotExist) = true; a file that exists is not missing", err)
		}
		if !strings.HasSuffix(err.Error(), "not a directory") {
			t.Errorf("%v does not read as not a directory", err)
		}
	}
}

// TestRefused_JudgesTheElementADirectoryFormNames holds Stat, OpenFile and
// RefusedWrite to one rule on every host: a path that ends in a separator or a
// "." element is refused as not a directory when its last element is a file or
// sits under one, and a write also where it would create a file in an existing
// directory. A write under a missing directory is refused as missing; a read
// is refused as not a directory nowhere else.
func TestRefused_JudgesTheElementADirectoryFormNames(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	file := filepath.Join(base, "f.ys")
	writeEmpty(t, file)
	dir := filepath.Join(base, "d")
	mkdirAll(t, dir)
	absent := filepath.Join(base, "n.ys")
	underFile := filepath.Join(file, "x")
	underMissing := filepath.Join(base, "nodir", "n.ys")
	sep := string(filepath.Separator)

	const (
		allowed = iota
		notDirectory
		missing
		failed
	)
	type row struct {
		path        string
		read, write int
	}
	tests := []row{
		{file + sep, notDirectory, notDirectory},
		{file + "/", notDirectory, notDirectory},
		{file + sep + ".", notDirectory, notDirectory},
		{file + sep + sep, notDirectory, notDirectory},
		{underFile + sep, notDirectory, notDirectory},
		{filepath.Join(underFile, "y") + sep + ".", notDirectory, notDirectory},
		{absent + sep, allowed, notDirectory},
		{absent + sep + ".", allowed, notDirectory},
		{underMissing + sep, allowed, missing},
		{filepath.Join(underMissing, "deeper") + sep, allowed, missing},
		{dir + sep, allowed, allowed},
		{dir + sep + ".", allowed, allowed},
		{file, allowed, allowed},
		{underFile, notDirectory, allowed},
		{absent, allowed, allowed},
		{dir, allowed, allowed},
	}
	if runtime.GOOS != "windows" {
		toFile, toDir, dangling := filepath.Join(base, "to-file"), filepath.Join(base, "to-dir"), filepath.Join(base, "dangling")
		symlink(t, file, toFile)
		symlink(t, dir, toDir)
		symlink(t, absent, dangling)
		tests = append(tests,
			row{os.DevNull + sep, notDirectory, notDirectory},
			row{toFile + sep, notDirectory, notDirectory},
			row{toDir + sep, allowed, allowed},
			row{dangling + sep, allowed, notDirectory})

		// A link target's ".." is taken on disk: s/.. is other, not base.
		other := filepath.Join(base, "other")
		mkdirAll(t, filepath.Join(other, "deep"))
		mkdirAll(t, filepath.Join(other, "isdir"))
		writeEmpty(t, filepath.Join(base, "isdir"))
		mkdirAll(t, filepath.Join(base, "absent-there"))
		mkdirAll(t, filepath.Join(base, "missing-there"))
		writeEmpty(t, filepath.Join(other, "isfile"))
		mkdirAll(t, filepath.Join(base, "isfile"))
		symlink(t, filepath.Join("other", "deep"), filepath.Join(base, "s"))
		viaS := func(name string) string { return filepath.Join(base, "s") + sep + ".." + sep + name + sep }
		dirForm := filepath.Join(base, "to-file-dir-form")
		symlink(t, file+sep, dirForm)
		tests = append(tests,
			row{viaS("isdir"), allowed, allowed},
			row{viaS("isfile"), notDirectory, notDirectory},
			row{viaS("absent-there"), allowed, notDirectory},
			row{viaS(filepath.Join("missing-there", "x")), allowed, missing},
			row{filepath.Join(dirForm, "x") + sep, notDirectory, notDirectory})

		// A dangling link is judged where the kernel lands through it.
		toMissing, chain, underFileLink, loop := filepath.Join(base, "to-missing"), filepath.Join(base, "chain"), filepath.Join(base, "to-under-file"), filepath.Join(base, "loop")
		symlink(t, filepath.Join("nodir", "x"), toMissing)
		symlink(t, "to-missing", chain)
		symlink(t, filepath.Join("f.ys", "x"), underFileLink)
		symlink(t, "loop", loop)
		tests = append(tests,
			row{toMissing + sep, allowed, missing},
			row{chain + sep, allowed, missing},
			row{underFileLink + sep, notDirectory, notDirectory},
			row{loop + sep, allowed, failed})

		// Each hop's own directory-form ending is judged away, so a chain
		// lands where the kernel lands; an empty target is no link.
		toMissingDirForm, toAbsentDot, empty := filepath.Join(base, "to-missing-dir-form"), filepath.Join(base, "to-absent-dot"), filepath.Join(base, "empty")
		symlink(t, "to-missing"+sep, toMissingDirForm)
		symlink(t, "n.ys"+sep+".", toAbsentDot)
		tests = append(tests,
			row{toMissingDirForm + sep, allowed, missing},
			row{toAbsentDot + sep, allowed, notDirectory})
		if err := os.Symlink("", empty); err == nil {
			tests = append(tests, row{empty + sep, allowed, notDirectory})
		}
	}
	stat := func(p string) error { _, err := Stat(p); return err }
	open := func(p string) error {
		f, err := OpenFile(p, os.O_RDONLY)
		if err == nil {
			f.Close() //nolint:gosec // opened only to learn whether the open is refused
		}
		return err
	}
	for _, tc := range tests {
		for _, door := range []struct {
			name    string
			refused func(string) error
			want    int
			read    bool
		}{
			{"Stat", stat, tc.read, true},
			{"OpenFile", open, tc.read, true},
			{"RefusedWrite", RefusedWrite, tc.write, false},
		} {
			err := door.refused(tc.path)
			switch {
			case door.want == notDirectory:
				if !errors.Is(err, ErrNotDirectory) {
					t.Errorf("%s(%q) = %v; want ErrNotDirectory", door.name, tc.path, err)
				}
			case door.read:
				// A read's other answers are the host's: only the refusal is ruled.
				if errors.Is(err, ErrNotDirectory) {
					t.Errorf("%s(%q) = %v; want no ErrNotDirectory", door.name, tc.path, err)
				}
			case door.want == allowed:
				if err != nil {
					t.Errorf("%s(%q) = %v; want nil", door.name, tc.path, err)
				}
			case door.want == missing:
				if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotDirectory) {
					t.Errorf("%s(%q) = %v; want the host's missing-path error", door.name, tc.path, err)
				}
			case door.want == failed:
				if err == nil || errors.Is(err, ErrNotDirectory) || errors.Is(err, fs.ErrNotExist) {
					t.Errorf("%s(%q) = %v; want the stat's own failure", door.name, tc.path, err)
				}
			}
		}
	}
}

// TestResolve_ADirectoryFormRefusalIsNoMissingPath holds Resolve's refusal of a
// file named as a directory to ErrNotDirectory, which no caller that asks
// fs.ErrNotExist reads as a missing file on any host.
func TestResolve_ADirectoryFormRefusalIsNoMissingPath(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "f.json")
	writeEmpty(t, file)
	for _, typed := range []string{file + string(filepath.Separator), filepath.Join(file, "under.json")} {
		_, err := Resolve(typed)
		if !errors.Is(err, syscall.ENOTDIR) || errors.Is(err, fs.ErrNotExist) {
			t.Errorf("Resolve(%q) = %v; want ENOTDIR and no fs.ErrNotExist", typed, err)
		}
		if err != nil && !strings.HasSuffix(err.Error(), "not a directory") {
			t.Errorf("Resolve(%q) = %v; want it to read not a directory", typed, err)
		}
	}
}

// TestDirectoryElement_RemovesTheDirectoryFormEndingAlone holds the element a
// directory-form path is judged at: its trailing separators and "." elements
// go, and a ".." stays for the kernel to take.
func TestDirectoryElement_RemovesTheDirectoryFormEndingAlone(t *testing.T) {
	t.Parallel()
	sep := string(filepath.Separator)
	root := filepath.VolumeName(t.TempDir()) + sep
	for p, want := range map[string]string{
		"x" + sep:                          "x",
		"x" + sep + sep:                    "x",
		"x/":                               "x",
		"x" + sep + ".":                    "x",
		"x" + sep + "." + sep:              "x",
		"x" + sep + "." + sep + ".":        "x",
		"." + sep:                          ".",
		root + ".":                         root,
		"a" + sep + ".." + sep + "b" + sep: "a" + sep + ".." + sep + "b",
	} {
		if got := directoryElement(p); got != want {
			t.Errorf("directoryElement(%q) = %q, want %q", p, got, want)
		}
	}
}

// chainLookups is a filesystem of one existing directory, dir, holding a chain
// of dangling links l0 to l(n-1), each to the next, the last to end, which is a
// regular file when file is set and otherwise absent. A stat of a link answers
// ENOENT, as a kernel's does within its own link bound.
type chainLookups struct {
	dir  string
	n    int
	file bool
}

func (c chainLookups) link(p string) (int, bool) {
	var i int
	if _, err := fmt.Sscanf(filepath.Base(p), "l%d", &i); err != nil || filepath.Dir(p) != c.dir || i >= c.n {
		return 0, false
	}
	return i, true
}

func (c chainLookups) Stat(p string) (fs.FileInfo, error) {
	switch p = filepath.Clean(p); {
	case p == c.dir:
		return os.Stat(os.TempDir())
	case c.file && p == filepath.Join(c.dir, "end"):
		return os.Stat(os.Args[0])
	}
	return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
}

func (c chainLookups) Lstat(p string) (fs.FileInfo, error) {
	if _, ok := c.link(filepath.Clean(p)); ok {
		return symlinkInfo{}, nil
	}
	return c.Stat(p)
}

func (c chainLookups) Readlink(p string) (string, error) {
	i, ok := c.link(filepath.Clean(p))
	switch {
	case !ok:
		return "", &fs.PathError{Op: "readlink", Path: p, Err: fs.ErrInvalid}
	case i == c.n-1:
		return "end", nil
	}
	return fmt.Sprintf("l%d", i+1), nil
}

type symlinkInfo struct{ fs.FileInfo }

func (symlinkInfo) Mode() fs.FileMode { return fs.ModeSymlink }

// TestLinkHops_FollowsTheBoundAndRefusesTheNext holds both judgements that
// follow dangling links by hand to one bound: a chain of maxLinkHops links is
// judged where it lands, and one link more is refused. No tree on disk can
// show it, since the kernel answers ELOOP to a stat of a chain past its own
// bound of 32 or 40.
func TestLinkHops_FollowsTheBoundAndRefusesTheNext(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "c")
	start := filepath.Join(dir, "l0") + string(filepath.Separator)

	if err := refusedWriteIn(chainLookups{dir: dir, n: maxLinkHops}, start); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("RefusedWrite through %d links to an absent file = %v; want ErrNotDirectory", maxLinkHops, err)
	}
	if err := refusedWriteIn(chainLookups{dir: dir, n: maxLinkHops + 1}, start); !errors.Is(err, errTooManyLinks) {
		t.Errorf("RefusedWrite through %d links = %v; want the bound's refusal", maxLinkHops+1, err)
	}

	missing := &fs.PathError{Op: "open", Path: start, Err: fs.ErrNotExist}
	under := filepath.Join(dir, "l0", "x")
	if err := ClassifyIn(chainLookups{dir: dir, n: maxLinkHops, file: true}, under, missing); !errors.Is(err, ErrNotDirectory) {
		t.Errorf("Classify under %d links to a file = %v; want ErrNotDirectory", maxLinkHops, err)
	}
	if err := ClassifyIn(chainLookups{dir: dir, n: maxLinkHops + 1, file: true}, under, missing); err != missing { //nolint:errorlint // the open's own error value, unwrapped, is the answer
		t.Errorf("Classify under %d links to a file = %v; want the open's own error", maxLinkHops+1, err)
	}
}

// TestRefusedWrite_NamesNoPath holds a refusal for a stat that fails to the
// host's bare cause, which the caller names the path with once.
func TestRefusedWrite_NamesNoPath(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	loop := filepath.Join(t.TempDir(), "loop")
	if err := os.Symlink("loop", loop); err != nil {
		t.Fatal(err)
	}
	err := RefusedWrite(loop + string(filepath.Separator))
	if _, isPath := errors.AsType[*fs.PathError](err); isPath || !errors.Is(err, syscall.ELOOP) {
		t.Errorf("RefusedWrite(a looping link in directory form) = %#v; want the bare ELOOP", err)
	}
}

// TestOpenFile_NamesThePathOnce holds an open's refusal to one path error that
// names the path once.
func TestOpenFile_NamesThePathOnce(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "missing.yammm")
	_, err := OpenFile(p, os.O_RDONLY)
	if n := strings.Count(fmt.Sprint(err), p); n != 1 || !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("OpenFile(missing) = %v; want ENOENT naming the path once, not %d times", err, n)
	}
}

// TestHop_AnEmptyTargetReachesNothing holds a link whose target is empty to
// ENOENT, as darwin's kernel answers it, never an index past its end.
func TestHop_AnEmptyTargetReachesNothing(t *testing.T) {
	t.Parallel()
	link := filepath.Join(t.TempDir(), "empty")
	if err := os.Symlink("", link); err != nil {
		t.Skipf("this host makes no link with an empty target: %v", err)
	}
	if got, err := Hop(link); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Hop(an empty link) = %q, %v; want ENOENT", got, err)
	}
}

// TestStat_UnderADanglingLinkWithALongTargetIsMissing holds a path under a
// dangling link to the kernel's answer, missing, when the climb's hand-built
// path through the link's target passes PATH_MAX and fails on its length.
func TestStat_UnderADanglingLinkWithALongTargetIsMissing(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs elevation on Windows")
	}
	dir := t.TempDir()
	parts := slices.Repeat([]string{strings.Repeat("m", 200)}, 5)
	parts = append(parts, strings.Repeat("m", 1014-len(filepath.Join(parts...))-1))
	target := filepath.Join(parts...)
	link := filepath.Join(dir, "l")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	_, err := Stat(filepath.Join(link, "x"))
	if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotDirectory) {
		t.Errorf("Stat(under a dangling link with a %d-byte target) = %v; want missing", len(target), err)
	}
}
