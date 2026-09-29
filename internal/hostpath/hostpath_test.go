package hostpath

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestParent_KeepsEveryComponentBeforeTheLast(t *testing.T) {
	sep := string(filepath.Separator)
	cases := []struct{ in, want string }{
		{"f", "."},
		{"d" + sep + "f", "d"},
		{"d" + sep + sep + "f", "d"},
		{"d" + sep + "f" + sep, "d"},
		{"d" + sep + "link" + sep + ".." + sep + "f", "d" + sep + "link" + sep + ".."},
		{".." + sep + "f", ".."},
		{sep + "f", sep},
		{sep, sep},
		{sep + "d" + sep + "." + sep + "f", sep + "d" + sep + "."},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases,
			struct{ in, want string }{`C:\f`, `C:\`},
			struct{ in, want string }{`C:f`, `C:.`},
			struct{ in, want string }{`C:\d\f`, `C:\d`},
		)
	}
	for _, c := range cases {
		if got := Parent(c.in); got != c.want {
			t.Errorf("Parent(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFollowLink_ReadsATargetFromTheLinksDirectoryUncleaned(t *testing.T) {
	sep := string(filepath.Separator)
	abs := filepath.Join(t.TempDir(), "x")
	cases := []struct{ link, dest, want string }{
		{"d" + sep + "l", "t", "d" + sep + "t"},
		{"d" + sep + "l", ".." + sep + "t", "d" + sep + ".." + sep + "t"},
		{"d" + sep + "m" + sep + ".." + sep + "l", "t", "d" + sep + "m" + sep + ".." + sep + "t"},
		{"l", "t", "." + sep + "t"},
		{sep + "l", "t", sep + "t"},
		{"d" + sep + "l", abs, abs},
	}
	if runtime.GOOS == "windows" {
		cases = append(cases, struct{ link, dest, want string }{`D:\d\l`, `\t`, `D:\t`})
	}
	for _, c := range cases {
		if got := FollowLink(c.link, c.dest); got != c.want {
			t.Errorf("FollowLink(%q, %q) = %q, want %q", c.link, c.dest, got, c.want)
		}
	}
}

// TestParent_NamesTheDirectoryTheKernelWritesIn holds Parent to the kernel on a
// tree where cleaning and the kernel disagree: a file created at p is found in
// Parent(p) under p's last element.
func TestParent_NamesTheDirectoryTheKernelWritesIn(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "deep", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(filepath.Join("real", "deep", "x"), link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	sep := string(filepath.Separator)
	for _, p := range []string{
		link + sep + ".." + sep + "f",
		link + sep + "." + sep + ".." + sep + "g",
		root + sep + "real" + sep + "deep" + sep + "x" + sep + ".." + sep + "h",
	} {
		if err := os.WriteFile(p, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.Stat(Parent(p) + sep + filepath.Base(p))
		if err != nil {
			t.Fatalf("Parent(%q) = %q: %v", p, Parent(p), err)
		}
		if !os.SameFile(want, got) {
			t.Errorf("Parent(%q) = %q names another directory", p, Parent(p))
		}
	}
}

// TestFollowLink_NamesTheFileTheKernelReaches creates a file through links
// whose targets, relative and absolute, climb out of a linked directory, and
// finds it at the path FollowLink gives.
func TestFollowLink_NamesTheFileTheKernelReaches(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "real", "deep", "x"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join("real", "deep", "x"), filepath.Join(root, "lr")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	sep := string(filepath.Separator)
	for name, dest := range map[string]string{
		"u.json": "lr" + sep + ".." + sep + "e.json",
		"a.json": root + sep + "lr" + sep + ".." + sep + "f.json",
	} {
		link := filepath.Join(root, name)
		if err := os.Symlink(dest, link); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(link, []byte("k"), 0o600); err != nil {
			t.Fatal(err)
		}
		want, err := os.Stat(link)
		if err != nil {
			t.Fatal(err)
		}
		got, err := os.Lstat(FollowLink(link, dest))
		if err != nil {
			t.Fatalf("FollowLink(%q, %q) = %q: %v", link, dest, FollowLink(link, dest), err)
		}
		if !os.SameFile(want, got) {
			t.Errorf("FollowLink(%q, %q) = %q names another file", link, dest, FollowLink(link, dest))
		}
	}
}

func TestText_EvaluatesADotDotOnTheText(t *testing.T) {
	sep := string(filepath.Separator)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct{ in, want string }{
		{"", ""},
		{"f", "f"},
		{"d" + sep + ".." + sep + "f", "f"},
		{"d" + sep + "." + sep + "f" + sep, "d" + sep + "f"},
		{"d" + sep + "link" + sep + ".." + sep + "f", "d" + sep + "f"},
		{"..", filepath.Dir(wd)},
		{".." + sep + "f", filepath.Join(filepath.Dir(wd), "f")},
		{"..f", "..f"},
		{sep + "a" + sep + ".." + sep + "b", sep + "b"},
	}
	for _, c := range cases {
		got, err := Text(c.in)
		if err != nil || got != c.want {
			t.Errorf("Text(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}

// A path that climbs out of a working directory entered through a symbolic link
// climbs from the directory's logical spelling, as filepath.Abs spells it, where
// the kernel would climb from the directory the link reached.
func TestText_ClimbsFromTheWorkingDirectoryAsSpelled(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "phys", "inner"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "logical"), 0o750); err != nil {
		t.Fatal(err)
	}
	cwd := filepath.Join(root, "logical", "cwd")
	if err := os.Symlink(filepath.Join(root, "phys", "inner"), cwd); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Chdir(cwd)
	got, err := Text(".." + string(filepath.Separator) + "x.ys")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(root, "logical", "x.ys"); got != want {
		t.Errorf("Text(../x.ys) from %s = %q, want %q", cwd, got, want)
	}
}
