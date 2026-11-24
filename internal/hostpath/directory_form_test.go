package hostpath

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
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

// TestRefused_JudgesTheElementADirectoryFormNames holds RefusedRead and
// RefusedWrite to one rule on every host: a path that ends in a separator or a
// "." element is refused as not a directory when its last element is a file or
// sits under one, and a write also where it would create a file in an existing
// directory. A write under a missing directory is refused as missing; a
// directory, and a path not in directory form, are left to the caller.
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
		{underFile, allowed, allowed},
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
	for _, tc := range tests {
		for _, door := range []struct {
			name    string
			refused func(string) error
			want    int
		}{{"RefusedRead", RefusedRead, tc.read}, {"RefusedWrite", RefusedWrite, tc.write}} {
			err := door.refused(tc.path)
			switch door.want {
			case allowed:
				if err != nil {
					t.Errorf("%s(%q) = %v; want nil", door.name, tc.path, err)
				}
			case notDirectory:
				if !errors.Is(err, ErrNotDirectory) {
					t.Errorf("%s(%q) = %v; want ErrNotDirectory", door.name, tc.path, err)
				}
			case missing:
				if !errors.Is(err, fs.ErrNotExist) || errors.Is(err, ErrNotDirectory) {
					t.Errorf("%s(%q) = %v; want the host's missing-path error", door.name, tc.path, err)
				}
			case failed:
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
