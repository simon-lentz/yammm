package scripttest

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fullVet names the full scripts/vet.sh run, which no Windows job of CI makes.
const fullVet = "the full scripts/vet.sh"

// vetTargets is the order scripts/vet.sh vets in: this host, then every other
// target of its cross list.
func vetTargets() []string {
	cross := []string{
		"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64", "windows/amd64", "windows/arm64",
		"aix/ppc64", "android/arm64", "dragonfly/amd64", "freebsd/amd64", "illumos/amd64", "netbsd/amd64",
		"openbsd/amd64", "solaris/amd64",
	}
	targets := []string{hostTarget()}
	for _, target := range cross {
		if target != hostTarget() {
			targets = append(targets, target)
		}
	}
	return targets
}

// newVetFixture returns a module holding the scripts scripts/vet.sh runs and
// three packages that import nothing. Vet compiles the packages a target's
// files import, so a row over every target builds no standard library unless
// the file it adds imports one. Nothing is indexed until the caller calls
// index.
func newVetFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{t: t, dir: t.TempDir()}
	f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n")
	for _, name := range []string{"vet.sh", "packages.sh", "toolchain.sh"} {
		f.copyScript(name)
	}
	for _, pkg := range []string{"a", "ok", "z"} {
		f.write(pkg+"/"+pkg+".go", "package "+pkg+"\n")
	}
	return f
}

func TestVetScript_VetsEveryTargetAndTheRaceBuild(t *testing.T) {
	t.Parallel()
	host := hostTarget()
	rows := []struct {
		name string
		args []string
		want string
	}{
		{"every target", nil, "vet: clean for " + strings.Join(vetTargets(), " ") + ", and " + host + " -race\n"},
		{"this host alone", []string{"--host"}, "vet: clean for " + host + ", and " + host + " -race\n"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.args == nil {
				skipOnWindows(t, fullVet)
			}
			f := newVetFixture(t)
			// Vet type-checks this file only for a solaris or illumos build
			// with cgo enabled, which no cross target is, whatever the
			// caller exports.
			f.env = []string{"CGO_ENABLED=1"}
			f.write("ok/cgo_solaris.go", "//go:build cgo && solaris\n\npackage ok\n\nimport \"fmt\"\n\nfunc C() { fmt.Printf(\"%d\\n\", \"x\") }\n")
			f.index()

			r := f.run("vet.sh", row.args...)
			r.wantCode(t, 0)
			if r.stdout != row.want {
				t.Errorf("stdout = %q, want %q", r.stdout, row.want)
			}
		})
	}
}

func TestVetScript_FailsATargetWhoseSyscallPackageLacksACall(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name     string
		args     []string
		wantCode int
		stderr   string
	}{
		{"every target", nil, 1, "vet: FAILED for aix/ppc64 illumos/amd64 solaris/amd64 (of "},
		{"this host alone", []string{"--host"}, 0, ""},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if row.args == nil {
				skipOnWindows(t, fullVet)
			}
			f := newVetFixture(t)
			f.write("ok/fifo.go", "//go:build unix\n\npackage ok\n\nimport \"syscall\"\n\nfunc F() error { return syscall.Mkfifo(\"p\", 0o600) }\n")
			f.index()

			r := f.run("vet.sh", row.args...)
			r.wantCode(t, row.wantCode)
			r.wantStderr(t, row.stderr)
		})
	}
}

func TestVetScript_VetsTheRaceBuild(t *testing.T) {
	t.Parallel()
	host := hostTarget()
	f := newVetFixture(t)
	f.write("ok/race_test.go", "//go:build race\n\npackage ok\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestRaceOnly(t *testing.T) { fmt.Printf(\"%d\\n\", \"x\") }\n")
	f.index()

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 1)
	r.wantStderr(t, "vet: FAILED for "+host+" -race (of "+host+", and "+host+" -race)\n")
}

// One run shows each target vetting its own package list: a finding in any one
// package fails every target and the race build, since each pass vets the whole
// list, not one end of it; a file only one architecture builds is vetted by the
// target that ships it, and a linux/arm64 file by android/arm64 too; a package
// whose every file is for one GOOS is vetted there alone; and each cross target
// lists its packages with cgo off, as it vets them, whatever the caller exports,
// so a package whose every file needs cgo is left out there.
func TestVetScript_VetsEachTargetsOwnPackageList(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, fullVet)
	host := hostTarget()
	targets := strings.Join(vetTargets(), " ")
	f := newVetFixture(t)
	bad := func(name string) string {
		return "func " + name + "(x int) int {\n\tx = x\n\treturn x\n}\n"
	}
	for _, pkg := range []string{"a", "ok", "z"} {
		// A finding in a file that imports nothing, so no target compiles a package for it.
		f.write(pkg+"/bad.go", "package "+pkg+"\n\n"+bad("Bad"))
	}
	shipped := map[string]string{"darwin/amd64": "BadDarwin", "linux/arm64": "BadLinux", "windows/arm64": "BadWindows"}
	for target, name := range shipped {
		goos, goarch, _ := strings.Cut(target, "/")
		f.write("ok/bad_"+goos+"_"+goarch+".go", "package ok\n\n"+bad(name))
	}
	f.write("only/only_illumos.go", "package only\n\n"+bad("Bad"))
	f.write("cg/c.go", "//go:build cgo\n\npackage cg\n")
	f.index()
	f.env = []string{"CGO_ENABLED=1"}

	r := f.run("vet.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "vet: FAILED for "+targets+" "+host+" -race (of "+targets+", and "+host+" -race)\n")
	finding := func(file string) string { return filepath.FromSlash(file) + ":4:2: self-assignment of x\n" }
	for _, pkg := range []string{"a", "ok", "z"} {
		r.wantStderr(t, finding(pkg+"/bad.go"))
	}
	for target := range shipped {
		goos, goarch, _ := strings.Cut(target, "/")
		r.wantStderr(t, finding("ok/bad_"+goos+"_"+goarch+".go"))
	}
	if n := strings.Count(r.stderr, finding("ok/bad_linux_arm64.go")); n < 2 {
		t.Errorf("the linux/arm64 file is vetted %d time(s), want linux/arm64 and android/arm64 both", n)
	}
	if n := strings.Count(r.stderr, finding("only/only_illumos.go")); n != 1 {
		t.Errorf("the illumos file is vetted %d time(s), want by illumos/amd64 alone", n)
	}
	// Each cross target's note names the directories under its own header.
	for _, target := range []string{"linux/amd64", "solaris/amd64", "windows/arm64"} {
		if target != host {
			r.wantStderr(t, "packages: this build ("+target+") reads no package from 2 tracked director(ies):\n  cg\n  only\n")
		}
	}
}

func TestVetScript_LeavesOutAnUntrackedPackage(t *testing.T) {
	t.Parallel()
	f := newVetFixture(t)
	f.write(".gitignore", "untracked/\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n\nimport \"fmt\"\n\nfunc F() { fmt.Printf(\"%d\\n\", \"x\") }\n")

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 0)
	r.wantStderr(t, "packages: leaving out 1 package(s) that git ignores:\n  "+fixtureModule+"/untracked\n")
}

// A tracked directory no build of the test workflow reads a package from fails
// the full run, named, from any host and whatever GOFLAGS the caller exports:
// one only a race build of a host CI does not test on reads, only a jsonv2
// build of a GOOS the jsonv2 job does not build, only the jsonv2 build with a
// tag, only one tag without the other outside the package that tag's job
// tests, only linux/amd64 with cgo off, or none. One only a build CI makes reads
// does not: a host's race build, its build with cgo on, the check job's lint
// under the linter's tags, a tag job's package or the jsonv2 job's build.
func TestVetScript_FailsATrackedDirectoryNoBuildReads(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, fullVet)
	f := newVetFixture(t)
	for dir, constraint := range map[string]string{
		"tagged":      "special",
		"mermaid":     "mermaid",
		"neo":         "neo4j_integration",
		"mermaidonly": "mermaid && !neo4j_integration",
		"winlint":     "windows && mermaid",
		"wincgo":      "windows && cgo && !race",
		"jsonlint":    "goexperiment.jsonv2 && mermaid",
		"jsonv2":      "goexperiment.jsonv2",
		"race":        "race",
		"linuxrace":   "linux && race",
		"winrace":     "windows && race",
		"jsonnorace":  "goexperiment.jsonv2 && !race",
		"darwincgo":   "darwin && cgo && !race",
		"bsdrace":     "freebsd && race",
		"darwinjson":  "darwin && goexperiment.jsonv2",
		"linuxnocgo":  "linux && amd64 && !cgo",
	} {
		f.write(dir+"/x.go", "//go:build "+constraint+"\n\npackage "+dir+"\n")
	}
	f.write("adapter/markdown/x.go", "//go:build mermaid\n\npackage markdown\n")
	f.write("adapter/neo4j/integration/x.go", "//go:build neo4j_integration\n\npackage integration\n")
	f.write(".gitignore", "untracked/\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")
	f.env = []string{"GOFLAGS=-tags=special"}

	r := f.run("vet.sh")
	r.wantCode(t, 1)
	r.wantStderr(t, "vet: no build of the test workflow reads a package from these tracked directories:\n"+
		"  bsdrace\n  darwinjson\n  jsonlint\n  linuxnocgo\n  mermaidonly\n  tagged\n"+
		"vet: FAILED for every target (of ")
	// Every list repeats the note on an ignored package; it prints once.
	if n := strings.Count(r.stderr, "packages: leaving out 1 package(s) that git ignores:"); n != 1 {
		t.Errorf("the note prints %d times, want once:\n%s", n, r.stderr)
	}
}

// --host asks one target, so it cannot say whether any build reads a tracked
// directory: one that only a tag no build sets reads passes it.
func TestVetScript_HostRunPassesADirectoryNoBuildReads(t *testing.T) {
	t.Parallel()
	f := newVetFixture(t)
	f.write("tagged/x.go", "//go:build special\n\npackage tagged\n")
	f.index()

	f.run("vet.sh", "--host").wantCode(t, 0)
}

// This host is the go command's, whatever GOOS and GOARCH the caller exports.
func TestVetScript_TakesTheHostFromTheToolchain(t *testing.T) {
	t.Parallel()
	f := newVetFixture(t)
	f.index()
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	f.env = []string{"GOOS=" + other}

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 0)
	if want := "vet: clean for " + hostTarget() + ", and " + hostTarget() + " -race\n"; r.stdout != want {
		t.Errorf("stdout = %q, want %q", r.stdout, want)
	}
}

// --host vets this host's own GOOS and GOARCH, whatever the caller exports: a
// finding in a file only this host builds fails it.
func TestVetScript_VetsThisHostWhateverTheCallerExports(t *testing.T) {
	t.Parallel()
	f := newVetFixture(t)
	f.write("ok/bad_"+runtime.GOOS+"_"+runtime.GOARCH+".go", "package ok\n\nfunc Bad(x int) int {\n\tx = x\n\treturn x\n}\n")
	f.index()
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	f.env = []string{"GOOS=" + other, "GOARCH=386"}

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 1)
	r.wantStderr(t, "vet: FAILED for "+hostTarget()+" "+hostTarget()+" -race (of ")
}

// A list that fails after the vets, as a tagged list whose file imports a
// package no module provides does, prints each note once.
// (TestVetScript_FailsATrackedDirectoryNoBuildReads holds a run that completes
// to the same rule, and a package only the race build reads.)
func TestVetScript_PrintsEachNoteOnceWhenAListFailsLate(t *testing.T) {
	t.Parallel()
	skipOnWindows(t, fullVet)
	f := newVetFixture(t)
	f.write(".gitignore", "untracked/\n")
	f.write("tagged/t.go", "//go:build mermaid\n\npackage tagged\n\nimport _ \"example.com/absent\"\n")
	f.index()
	f.write("untracked/u.go", "package untracked\n")

	late := f.run("vet.sh")
	late.wantCode(t, 1)
	if n := strings.Count(late.stderr, "packages: leaving out 1 package(s) that git ignores:"); n != 1 {
		t.Errorf("the note prints %d times, want once:\n%s", n, late.stderr)
	}
}

// A list packages.sh refuses stops vet.sh with its message. The refusal is the
// same for every target's list, so the --host run shows it.
func TestVetScript_StopsAtARefusedList(t *testing.T) {
	t.Parallel()
	f := newVetFixture(t)
	f.index()
	f.write("ok/new.go", "package ok\n")

	r := f.run("vet.sh", "--host")
	r.wantCode(t, 1)
	r.wantStderr(t, "untracked Go file(s) in a tracked package")
}
