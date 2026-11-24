package scripttest

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPackagesScript_ListsThePackagesHoldingATrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("doc.go", "package yammm\n")
	f.write("cafe/café.go", "package cafe\n")
	f.write(".gitignore", "untracked/\nuntracked2/\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.write("untracked2/u.go", "package untracked2\n")

	r := f.run("packages.sh")
	r.wantCode(t, 0)
	want := fixtureModule + "\n" +
		fixtureModule + "/cafe\n" +
		fixtureModule + "/internal/mutants\n" +
		fixtureModule + "/internal/raceskip\n" +
		fixtureModule + "/internal/testjson\n" +
		fixtureModule + "/internal/testsummary\n" +
		fixtureModule + "/ok\n"
	if r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
	if wantErr := "packages: leaving out 2 package(s) that git ignores:\n  " + fixtureModule + "/untracked\n  " + fixtureModule + "/untracked2\n"; r.stderr != wantErr {
		t.Errorf("stderr = %q, want %q", r.stderr, wantErr)
	}
}

func TestPackagesScript_RefusesAModuleWithNoTrackedGoFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index("go.mod", "scripts")

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: no package holds a tracked Go file\n")
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing", r.stdout)
	}
}

// An untracked Go file in a tracked package stops the run, named, whether git
// ignores it or not: a go command building the package compiles it unless a
// build constraint leaves it out, and no commit holds it. A name go never
// reads, one in testdata, and one in a package git ignores that holds no
// tracked Go file are not refused.
func TestPackagesScript_RefusesAnUntrackedGoFileInATrackedPackage(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("ok/testdata/fixture.go", "package fixture\n")
	f.write(".gitignore", "ok/ignored*.go\nuntracked/\n")
	f.index()
	f.write("ok/helper.go", "package ok\n")
	f.write("ok/helper_test.go", "package ok\n")
	f.write("ok/ignored.go", "package ok\n")
	f.write("ok/_scratch.go", "package ok\n")
	f.write("ok/testdata/untracked.go", "package fixture\n")
	f.write("untracked/u.go", "package untracked\n")

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 3 untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n  ok/helper.go\n  ok/helper_test.go\n  ok/ignored.go\n")
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing", r.stdout)
	}

	for _, name := range []string{"ok/helper.go", "ok/helper_test.go", "ok/ignored.go"} {
		if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	f.run("packages.sh").wantCode(t, 0)
}

// A go.work in effect makes the go command read a workspace, however many
// modules it uses, so the run refuses it.
func TestPackagesScript_RefusesAWorkspace(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("go.work", "go "+goDirective()+"\n\nuse .\n")
	f.index()
	f.env = []string{"GOWORK=" + filepath.Join(f.dir, "go.work")}

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: the go command reads the workspace "+filepath.Join(f.dir, "go.work"))
}

// The list is the build's the environment and the arguments name, and a
// tracked directory that build reads no package from is named on stderr and
// written to the --unbuilt file.
func TestPackagesScript_ListsOneBuildAndNamesWhatItLeavesOut(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("winonly/w_windows.go", "package winonly\n")
	f.write("tagged/t.go", "//go:build special\n\npackage tagged\n")
	f.index()
	unbuilt := filepath.Join(t.TempDir(), "unbuilt")

	f.env = []string{"GOOS=linux", "GOARCH=amd64"}
	r := f.run("packages.sh", "--unbuilt", unbuilt)
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: this build (linux/amd64) reads no package from 2 tracked director(ies):\n  tagged\n  winonly\n")
	if got, err := os.ReadFile(unbuilt); err != nil || string(got) != "tagged\nwinonly\n" {
		t.Errorf("the --unbuilt file holds %q, %v; want tagged and winonly", got, err)
	}

	f.env = []string{"GOOS=windows", "GOARCH=amd64"}
	r = f.run("packages.sh", "-tags=special")
	r.wantCode(t, 0)
	r.wantStdout(t, fixtureModule+"/tagged\n")
	r.wantStdout(t, fixtureModule+"/winonly\n")
}

// An untracked Go file git does not ignore, in a directory go reads that holds
// no tracked Go file, stops the run with its directory named, whatever the
// build: a tracked package can import its package, and no commit holds it. A
// package whose every Go file git ignores is left out, named, though git does
// not ignore its directory, and so is the root's.
func TestPackagesScript_RefusesAnUntrackedPackageGitDoesNotIgnore(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write(".gitignore", "gen/*.go\n/root.go\n")
	f.index()
	f.write("newpkg/n.go", "package newpkg\n")
	f.write("newwin/n_windows.go", "package newwin\n")
	f.write("gen/g.go", "package gen\n")
	f.write("gen/g.txt", "not Go\n")
	f.write("root.go", "package yammm\n")
	f.write("ok/uses.go", "package ok\n\nimport _ \""+fixtureModule+"/newpkg\"\n")
	f.index("ok/uses.go")
	f.env = []string{"GOOS=linux", "GOARCH=amd64"}

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 2 untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n  newpkg\n  newwin\n")
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing", r.stdout)
	}

	for _, name := range []string{"newpkg/n.go", "newwin/n_windows.go", "ok/uses.go"} {
		if err := os.Remove(filepath.Join(f.dir, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	f.index()
	r = f.run("packages.sh")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: leaving out 2 package(s) that git ignores:\n  "+fixtureModule+"\n  "+fixtureModule+"/gen\n")
}

// A tracked Go file go reads, below a go.mod git does not track, ignored or
// not, stops the run: this checkout's go command reads it as another module's, and CI's
// as this one's.
func TestPackagesScript_RefusesATrackedPackageBelowAnUntrackedGoMod(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write(".gitignore", "k/go.mod\n")
	f.write("k/k.go", "package k\n")
	f.write("k/sub/s.go", "package sub\n")
	f.index()
	f.write("k/go.mod", "module k\n\ngo "+goDirective()+"\n")

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 2 tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  k\n  k/sub\n")
}

// An untracked Go file in a tracked directory below an untracked go.mod, ignored
// or not, is named in the run that names the go.mod: removing the go.mod makes
// the directory a package of this module, which compiles the file.
func TestPackagesScript_NamesAnUntrackedGoFileBelowAnUntrackedGoMod(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write(".gitignore", "k/go.mod\nk/ignored.go\n")
	f.write("k/k.go", "package k\n")
	f.write("k/sub/s.go", "package sub\n")
	f.index()
	f.write("k/go.mod", "module k\n\ngo "+goDirective()+"\n")
	f.write("k/extra.go", "package k\n")
	f.write("k/ignored.go", "package k\n")
	f.write("k/sub/extra.go", "package sub\n")

	r := f.run("packages.sh", "--check")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 2 tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  k\n  k/sub\n")
	r.wantStderr(t, "packages: 3 untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n  k/extra.go\n  k/ignored.go\n  k/sub/extra.go\n")
}

// An untracked Go file git does not ignore, below a go.mod the run asks to
// remove and in a directory holding no tracked Go file, is named in that run as
// the untracked package removing the go.mod makes. One below an untracked
// go.mod that shadows no tracked directory is not named.
func TestPackagesScript_NamesAnUntrackedPackageBelowTheGoModItAsksToRemove(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("x/y/a.go", "package y\n")
	f.write("a/b/x.go", "package b\n")
	f.write("m/go.mod", "module m\n\ngo "+goDirective()+"\n")
	f.write("m/sub/x.go", "package sub\n")
	f.index()
	f.write("x/go.mod", "module x\n\ngo "+goDirective()+"\n")
	f.write("x/c.go", "package x\n")
	f.write("x/newpkg/n.go", "package newpkg\n")
	f.write("scratch/go.mod", "module scratch\n\ngo "+goDirective()+"\n")
	f.write("scratch/s.go", "package scratch\n")
	// Two untracked go.mod files stacked above a tracked file: removing both
	// makes a/c this module's package.
	f.write("a/go.mod", "module a\n\ngo "+goDirective()+"\n")
	f.write("a/b/go.mod", "module b\n\ngo "+goDirective()+"\n")
	f.write("a/c/y.go", "package c\n")
	// Below a tracked nested module: removing m/sub/go.mod makes m/sub/c module m's.
	f.write("m/sub/go.mod", "module sub\n\ngo "+goDirective()+"\n")
	f.write("m/sub/c/y.go", "package c\n")

	r := f.run("packages.sh", "--check")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 3 tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  a/b\n  m/sub\n  x/y\n")
	r.wantStderr(t, "packages: 3 untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n  a/c\n  x\n  x/newpkg\n")
}

// A directory named "+" is a directory like any other: the untracked package
// below the go.mod the run asks to remove is named.
func TestPackagesScript_NamesAnUntrackedPackageBelowADirectoryNamedPlus(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("+/x.go", "package x\n")
	f.index()
	f.write("+/go.mod", "module plus\n\ngo "+goDirective()+"\n")
	f.write("+/b/y.go", "package b\n")

	r := f.run("packages.sh", "--check")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 1 tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  +\n")
	r.wantStderr(t, "packages: 1 untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n  +/b\n")
}

// go passes over a directory whose only Go file go never reads, one whose name
// begins with "." or "_", a nested module, one a go.mod ignore directive names,
// with or without a "/" at either end and whatever characters go mod edit -json
// escapes in the pattern, and one below a vendor directory, so no build leaving
// it out is named. The script reads one path to a line, so a directory whose own
// name holds a newline is outside this rule.
func TestPackagesScript_PassesOverWhatGoPassesOver(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	ignores := "\t./ig\n\tdeep\n\t./g3/\n\tg4/\n\t/h6\n\t./a&b\n\t\"./a\u2028b\"\n"
	f.write("a&b/x.go", "package ab\n")
	f.write("a\u2028b/x.go", "package ab\n")
	if runtime.GOOS != "windows" {
		ignores += "\t\"./c\\fd\"\n"
		f.write("c\fd/x.go", "package cd\n")
	}
	f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n\nignore (\n"+ignores+")\n")
	f.write("g3/g.go", "package g3\n")
	f.write("q/g4/g.go", "package g4\n")
	f.write("q/h6/h.go", "package h6\n")
	f.write("u/_x.go", "package u\n")
	f.write("_under/x.go", "package under\n")
	f.write("igx/x.go", "package igx\n")
	f.write(".dot/x.go", "package dot\n")
	f.write("nm/go.mod", "module nm\n\ngo "+goDirective()+"\n")
	f.write("nm/n.go", "package nm\n")
	f.write("ig/i.go", "package ig\n")
	f.write("x/deep/d.go", "package deep\n")
	f.write("v/vendor/s/s.go", "package s\n")
	f.index()

	r := f.run("packages.sh")
	r.wantCode(t, 0)
	if strings.Contains(r.stderr, "reads no package from") {
		t.Errorf("a directory go passes over is named as left out:\n%s", r.stderr)
	}
	// A pattern names whole path elements: ./ig leaves igx read.
	r.wantStdout(t, fixtureModule+"/igx\n")
}

// An untracked Go file beside a tracked one at the module's root is refused too.
func TestPackagesScript_RefusesAnUntrackedGoFileAtTheRoot(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("doc.go", "package yammm\n")
	f.index()
	f.write("extra.go", "package yammm\n")

	r := f.run("packages.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n  extra.go\n")
}

// A note names the build and its flags, and a relative --unbuilt file is the
// caller's, read from the directory the script runs in.
func TestPackagesScript_NamesTheBuildAndWritesWhereTheCallerAsks(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("tagged/t.go", "//go:build special\n\npackage tagged\n")
	f.index()
	f.env = []string{"GOOS=linux", "GOARCH=amd64"}

	r := f.runFrom(filepath.Join(f.dir, "ok"), "packages.sh", "--unbuilt", "rel.txt", "-tags=other")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: this build (linux/amd64 -tags=other) reads no package from 1 tracked director(ies):\n  tagged\n")
	if got, err := os.ReadFile(filepath.Join(f.dir, "ok", "rel.txt")); err != nil || string(got) != "tagged\n" {
		t.Errorf("ok/rel.txt holds %q, %v; want tagged", got, err)
	}

	f.run("packages.sh", "--unbuilt", "").wantCode(t, 2)
}

// A nested module whose directory name git quotes is read as go reads it, and
// not refused: the tracked go.mod list is read as git writes it with -z.
func TestPackagesScript_ReadsANestedModuleWhoseNameGitQuotes(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("é/go.mod", "module nested\n\ngo "+goDirective()+"\n")
	f.write("é/x.go", "package nested\n")
	f.index()

	r := f.run("packages.sh", "--check")
	r.wantCode(t, 0)
	if r.stderr != "" {
		t.Errorf("stderr = %q, want nothing", r.stderr)
	}
}

// A go.mod ignore pattern that names the root ends go's walk of ./..., so no
// package is read and nothing untracked is refused.
func TestPackagesScript_ReadsNoPackageWhenAPatternIgnoresTheRoot(t *testing.T) {
	t.Parallel()
	for _, pattern := range []string{"./", ".", "/"} {
		t.Run(pattern, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n\nignore "+pattern+"\n")
			f.index()
			f.write("ok/new.go", "package ok\n")

			r := f.run("packages.sh")
			r.wantCode(t, 1)
			if r.stderr != "packages: no package holds a tracked Go file\n" {
				t.Errorf("stderr = %q, want the empty-set refusal alone", r.stderr)
			}
		})
	}
}

// One run names every refusal it finds: a tracked package below an untracked
// go.mod, an untracked file in a tracked package and an untracked package.
func TestPackagesScript_NamesEveryRefusalInOneRun(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("k/k.go", "package k\n")
	f.index()
	f.write("k/go.mod", "module k\n\ngo "+goDirective()+"\n")
	f.write("ok/extra.go", "package ok\n")
	f.write("newpkg/n.go", "package newpkg\n")

	r := f.run("packages.sh", "--check")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 1 tracked director(ies) below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  k\n")
	r.wantStderr(t, "packages: 1 untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n  ok/extra.go\n")
	r.wantStderr(t, "packages: 1 untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n  newpkg\n")
}

// An ignore pattern that holds a newline is one pattern, as go reads it: the
// directories its lines would name are still read.
func TestPackagesScript_ReadsAnIgnorePatternThatHoldsANewlineAsOne(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n\nignore \"./a\\nb\"\n")
	f.write("a/a.go", "package a\n")
	f.write("b/b.go", "package b\n")
	f.index()

	r := f.run("packages.sh")
	r.wantCode(t, 0)
	r.wantStdout(t, fixtureModule+"/a\n"+fixtureModule+"/b\n")
}

// A --check run makes the refusals whatever YAMMM_PACKAGES_TRACKED names: a
// file an earlier run of the same tree wrote holds the tracked directories it
// passed, and an untracked Go file added since is still refused. A run that
// lists packages reads that file and makes no such refusal, as the script
// states.
func TestPackagesScript_CheckRefusesWhateverTheTrackedFileHolds(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index()
	f.env = []string{"YAMMM_PACKAGES_TRACKED=" + filepath.Join(t.TempDir(), "tracked")}
	f.run("packages.sh").wantCode(t, 0)

	f.write("ok/helper.go", "package ok\n")
	r := f.run("packages.sh", "--check")
	r.wantCode(t, 1)
	r.wantStderr(t, "packages: 1 untracked Go file(s) in a tracked package, which a go command building it compiles and no commit holds; add or remove each:\n  ok/helper.go\n")
	f.run("packages.sh").wantCode(t, 0)
}

// A --check run that passes writes no file under YAMMM_PACKAGES_TRACKED: a
// later listing run would otherwise read a tracked list no refusal made for it,
// and a path in a directory that does not exist would fail the check itself.
func TestPackagesScript_CheckWritesNoTrackedFile(t *testing.T) {
	t.Parallel()
	f := newFixture(t)
	f.index()
	dir := t.TempDir()
	for _, path := range []string{filepath.Join(dir, "absent"), filepath.Join(dir, "missing", "tracked")} {
		f.env = []string{"YAMMM_PACKAGES_TRACKED=" + path}
		f.run("packages.sh", "--check").wantCode(t, 0)
		if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("--check left %s behind: %v", path, err)
		}
	}
}
