package scripttest

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// hostTarget is this host as scripts/lint.sh and scripts/vet.sh name it.
func hostTarget() string { return runtime.GOOS + "/" + runtime.GOARCH }

// lintTargets is the order scripts/lint.sh lints in: this host, then
// windows/amd64 unless this host is windows/amd64.
func lintTargets() []string {
	if hostTarget() == "windows/amd64" {
		return []string{hostTarget()}
	}
	return []string{hostTarget(), "windows/amd64"}
}

// linterPackage is the pinned linter's command, which go.mod's tool directive names.
const linterPackage = "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"

var (
	linterModulesOnce sync.Once
	errLinterModules  error
)

// needLinterModules puts every module a build of the pinned linter reads into
// the module cache, once per test binary, with the caller's network settings: a
// fixture's go commands run with GOPROXY=off.
func needLinterModules(t *testing.T) {
	t.Helper()
	linterModulesOnce.Do(func() {
		cmd := exec.CommandContext(t.Context(), "go", "list", "-deps", linterPackage)
		cmd.Dir = repoRoot
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			errLinterModules = fmt.Errorf("go list -deps %s in the repository's module: %w\n%s", linterPackage, err, stderr.String())
		}
	})
	if errLinterModules != nil {
		t.Fatal(errLinterModules)
	}
}

// buildLinter returns the pinned linter's path: the build YAMMM_GOLANGCI_LINT
// names, as CI's host test jobs hand one built before the tests, or else one it
// builds into dir from the repository's module with GOEXPERIMENT empty, as
// scripts/linter.sh builds it. Unlike that script, it does not refuse a go env
// file that sets GOEXPERIMENT, which then reaches the build.
func buildLinter(t *testing.T, dir string) string {
	t.Helper()
	needLinterModules(t)
	if handed := os.Getenv("YAMMM_GOLANGCI_LINT"); handed != "" {
		return handed
	}
	path := filepath.Join(dir, "golangci-lint")
	if runtime.GOOS == "windows" {
		path += ".exe"
	}
	cmd := exec.CommandContext(t.Context(), "go", "build", "-o", path, linterPackage)
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOEXPERIMENT=")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build %s: %v\n%s", linterPackage, err, out)
	}
	return path
}

// Not parallel: it sets YAMMM_GOLANGCI_LINT for the whole process. The path
// names no file, so only a build that was taken, not made, returns it.
func TestBuildLinter_TakesTheBuildItIsHanded(t *testing.T) {
	handed := filepath.Join(t.TempDir(), "handed-golangci-lint")
	t.Setenv("YAMMM_GOLANGCI_LINT", handed)
	dir := t.TempDir()

	if got := buildLinter(t, dir); got != handed {
		t.Errorf("buildLinter returned %s, want the handed %s", got, handed)
	}
	if entries, err := os.ReadDir(dir); err != nil || len(entries) != 0 {
		t.Errorf("buildLinter wrote %v into its directory (%v), want nothing", entries, err)
	}
}

// newLintFixture returns a module with the repository's requirements, lint
// configuration and lint scripts, one package, and linter handed to the scripts.
// A golangci-lint first on its PATH fails every run, so a script that took it
// would lint nothing.
func newLintFixture(t *testing.T, linter string) *fixture {
	t.Helper()
	needLinterModules(t)
	f := &fixture{t: t, dir: t.TempDir()}
	f.copyFile("go.mod")
	f.pinGoDirective()
	f.copyFile("go.sum")
	f.copyFile(".golangci.yml")
	for _, name := range []string{"lint.sh", "lintconfig.sh", "linter.sh", "packages.sh", "toolchain.sh"} {
		f.copyScript(name)
	}
	f.write("ok/ok.go", "package ok\n")
	onPath := t.TempDir()
	gittree.WriteFile(t, onPath, "golangci-lint", []byte("#!/usr/bin/env bash\necho 'the golangci-lint on PATH ran' >&2\nexit 1\n"), 0o700)
	f.env = []string{
		// needLinterModules put everything the scripts read in the module cache.
		"HTTP_PROXY=http://127.0.0.1:9", "HTTPS_PROXY=http://127.0.0.1:9", "NO_PROXY=",
		"PATH=" + onPath + string(os.PathListSeparator) + os.Getenv("PATH"),
		"YAMMM_GOLANGCI_LINT=" + linter,
	}
	return f
}

// TestLinterScripts_WithThePinnedLinter takes buildLinter's linter, the build
// CI's host test job hands it or else the one build of this test process, and
// hands it to each of its fixtures, as CI's check job hands its build to
// scripts/lintconfig.sh.
func TestLinterScripts_WithThePinnedLinter(t *testing.T) {
	t.Parallel()
	linter := buildLinter(t, t.TempDir())

	// A Windows file, or one only windows/amd64 compiles, whose one call drops
	// an error, fails each target that compiles it, windows/amd64 among them,
	// and passes every other; under --host only this host is linted.
	for _, row := range []struct {
		name, file, src string
		arch            string // the one GOARCH the file compiles for, or "" for any
		fails           bool
	}{
		{
			"a Windows file that lints clean", "ok/move_windows.go",
			"//go:build windows\n\npackage ok\n\nimport \"os\"\n\nfunc Sep() string { return string(os.PathSeparator) }\n", "", false,
		},
		{
			"a Windows file that drops an error", "ok/move_windows.go",
			"//go:build windows\n\npackage ok\n\nimport \"os\"\n\nfunc Move() { os.Chdir(\"x\") }\n", "", true,
		},
		{
			"a windows/amd64 file that drops an error", "ok/move_windows_amd64.go",
			"package ok\n\nimport \"os\"\n\nfunc Move() { os.Chdir(\"x\") }\n", "amd64", true,
		},
	} {
		t.Run("lint.sh/"+row.name, func(t *testing.T) {
			t.Parallel()
			f := newLintFixture(t, linter)
			f.write(row.file, row.src)
			f.index()
			// want is the run's expected result over targets.
			want := func(t *testing.T, r result, targets []string) {
				t.Helper()
				var failed []string
				for _, target := range targets {
					goos, goarch, _ := strings.Cut(target, "/")
					if row.fails && goos == "windows" && (row.arch == "" || row.arch == goarch) {
						failed = append(failed, target)
					}
				}
				if len(failed) == 0 {
					r.wantCode(t, 0)
					r.wantStdout(t, "lint: clean for "+strings.Join(targets, " ")+"\n")
					return
				}
				r.wantCode(t, 1)
				r.wantStdout(t, "os.Chdir")
				r.wantStderr(t, "lint: FAILED for "+strings.Join(failed, " ")+" (of "+strings.Join(targets, " ")+")\n")
			}

			want(t, f.run("lint.sh"), lintTargets())
			want(t, f.run("lint.sh", "--host"), []string{hostTarget()})
			// The arguments after --host still reach the linter, which prints
			// its usage and lints nothing.
			f.run("lint.sh", "--host", "--help").wantStdout(t, "golangci-lint run [flags]")
		})
	}

	// The linter reads no untracked package, as scripts/vet.sh and
	// scripts/test.sh read none.
	t.Run("lint.sh/an untracked package", func(t *testing.T) {
		t.Parallel()
		f := newLintFixture(t, linter)
		f.write(".gitignore", "untracked/\n")
		f.index()
		f.write("untracked/u.go", "package untracked\n\nimport \"os\"\n\nfunc Move() { os.Chdir(\"x\") }\n")

		r := f.run("lint.sh")
		r.wantCode(t, 0)
		r.wantStderr(t, "packages: leaving out 1 package(s) that git ignores:\n  "+fixtureModule+"/untracked\n")
	})

	own, err := os.ReadFile(fromRoot(t, ".golangci.yml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		name    string
		config  string
		refused bool
	}{
		{"the repository's config", string(own), false},
		{"an unknown top-level key", "version: \"2\"\nbogus_top_level_key: true\n", true},
	} {
		t.Run("lintconfig.sh/"+row.name, func(t *testing.T) {
			t.Parallel()
			// The script reads the schema the pinned linter's module ships, so the
			// fixture carries the repository's module requirements.
			f := newLintFixture(t, linter)
			f.write(".golangci.yml", row.config)
			f.index()

			r := f.run("lintconfig.sh")
			if !row.refused {
				r.wantCode(t, 0)
				return
			}
			if r.code == 0 {
				t.Fatalf("exit 0 for a config the schema refuses\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
			if !strings.Contains(r.stdout+r.stderr, "bogus_top_level_key") {
				t.Errorf("the refusal does not name the key\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
			}
		})
	}
}

// fakeLinter writes a golangci-lint that appends, for each run, its target,
// CGO_ENABLED and arguments to record, and passes, and returns its path.
func fakeLinter(t *testing.T, record string) string {
	t.Helper()
	dir := t.TempDir()
	gittree.WriteFile(t, dir, "golangci-lint", []byte("#!/usr/bin/env bash\n"+
		"printf '%s/%s cgo=%s %s\\n' \"${GOOS:-}\" \"${GOARCH:-}\" \"${CGO_ENABLED:-}\" \"$*\" >>"+shellQuote(record)+"\n"), 0o700)
	return filepath.Join(dir, "golangci-lint")
}

// shellQuote quotes s as one bash word.
func shellQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// lint.sh runs the build it is handed, for each target with its tracked
// packages as directories, and windows/amd64 with cgo off.
func TestLintScript_RunsTheLinterItIsHanded(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the fake linter is a bash script, which Windows cannot execute as a program")
	}
	record := filepath.Join(t.TempDir(), "runs")
	f := newLintFixture(t, fakeLinter(t, record))
	// This host is the go command's whatever GOOS and GOARCH the caller exports.
	f.env = append(f.env, "CGO_ENABLED=1", "GOOS=windows", "GOARCH=386")
	f.write("doc.go", "package yammm\n")
	// A package whose every file needs cgo is linted on this host alone, and one
	// whose every file is for Windows on windows/amd64 alone.
	f.write("cg/c.go", "//go:build cgo\n\npackage cg\n")
	f.write("winonly/w_windows.go", "package winonly\n")
	f.index()

	r := f.run("lint.sh", "--fix")
	r.wantCode(t, 0)
	r.wantStdout(t, "lint: clean for "+strings.Join(lintTargets(), " ")+"\n")
	runs, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	want := hostTarget() + " cgo=1 run --fix . ./cg ./ok\n"
	if hostTarget() != "windows/amd64" {
		want += "windows/amd64 cgo=0 run --fix . ./ok ./winonly\n"
	}
	if string(runs) != want {
		t.Errorf("the linter ran as\n%s\nwant\n%s", runs, want)
	}
}

// A linter the scripts cannot execute stops them before any target, and lint.sh
// prints no summary, which would report a lint that did not run.
func TestLintScript_StopsWithoutALinterToRun(t *testing.T) {
	t.Parallel()
	newBareFixture := func(t *testing.T) *fixture {
		t.Helper()
		f := &fixture{t: t, dir: t.TempDir()}
		f.write("go.mod", "module "+fixtureModule+"\n\ngo "+goDirective()+"\n")
		for _, name := range []string{"lint.sh", "lintconfig.sh", "linter.sh", "packages.sh", "toolchain.sh"} {
			f.copyScript(name)
		}
		f.write("ok/ok.go", "package ok\n")
		f.index()
		return f
	}
	wantNoSummary := func(t *testing.T, r result) {
		t.Helper()
		for _, summary := range []string{"lint: clean", "lint: FAILED"} {
			if strings.Contains(r.stdout+r.stderr, summary) {
				t.Errorf("the run printed a summary, %q, and linted nothing\nstdout:\n%s\nstderr:\n%s", summary, r.stdout, r.stderr)
			}
		}
	}

	t.Run("the module names no such tool", func(t *testing.T) {
		t.Parallel()
		r := newBareFixture(t).run("lint.sh")
		r.wantCode(t, 2)
		r.wantStderr(t, "linter: go cannot build the pinned golangci-lint\n")
		wantNoSummary(t, r)
	})

	for _, script := range []string{"lint.sh", "lintconfig.sh"} {
		for _, handed := range []struct {
			name string
			make func(t *testing.T) string
		}{
			{"a path that holds no file", func(t *testing.T) string {
				t.Helper()
				return filepath.Join(t.TempDir(), "removed", "golangci-lint")
			}},
			{"a directory", func(t *testing.T) string {
				t.Helper()
				return t.TempDir()
			}},
			{"a file it cannot execute", func(t *testing.T) string {
				t.Helper()
				dir := t.TempDir()
				gittree.WriteFile(t, dir, "golangci-lint", []byte("#!/usr/bin/env bash\nexit 0\n"), 0o600)
				return filepath.Join(dir, "golangci-lint")
			}},
		} {
			t.Run(script+" handed "+handed.name, func(t *testing.T) {
				t.Parallel()
				if handed.name == "a file it cannot execute" && runtime.GOOS == "windows" {
					t.Skip("Windows keeps no execute bit")
				}
				path := handed.make(t)
				f := newLintFixture(t, path)
				f.index()

				r := f.run(script)
				r.wantCode(t, 2)
				r.wantStderr(t, "linter: "+path+" holds no linter this run can execute\n")
				wantNoSummary(t, r)
			})
		}
	}
}

// An experiment the go env file sets would reach the build through the empty
// GOEXPERIMENT the scripts set, so a run with one is refused before it builds.
func TestLinterScript_RefusesAnExperimentTheGoEnvFileSets(t *testing.T) {
	t.Parallel()
	for _, script := range []string{"lint.sh", "lintconfig.sh"} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			f := newLintFixture(t, "")
			f.index()
			env := filepath.Join(t.TempDir(), "go.env")
			gittree.WriteFile(t, filepath.Dir(env), "go.env", []byte("GOEXPERIMENT=jsonv2\n"), 0o600)
			f.env = append(f.env, "GOENV="+env)

			r := f.run(script)
			r.wantCode(t, 2)
			r.wantStderr(t, "linter: the go env file sets GOEXPERIMENT=jsonv2, which would reach the linter's build; run go env -u GOEXPERIMENT\n")
		})
	}
}

// buildShimGo is a go that answers a build of the pinned linter by copying the
// linter GO_SHIM_LINTER names to the -o path, and records the GOEXPERIMENT the
// build ran under and that path, leaving every other command to the real
// toolchain.
const buildShimGo = `#!/usr/bin/env bash
if [ "$1" = build ] && [ "$2" = -o ] && [ "$4" = ` + linterPackage + ` ]; then
	printf 'GOEXPERIMENT=%s\nGOOS=%s\nGOARCH=%s\nout=%s\n' "${GOEXPERIMENT-unset}" "${GOOS-unset}" "${GOARCH-unset}" "$3" >"$GO_SHIM_RECORD"
	cp "$GO_SHIM_LINTER" "$3"
	exit 0
fi
exec "$GO_SHIM_REAL" "$@"
`

// Handed no linter, the scripts build one for this run, under no experiment
// whatever the caller exports, run it, and remove it as they exit.
func TestLinterScript_BuildsALinterOfItsOwn(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("the shims are bash scripts, which Windows cannot execute as programs")
	}
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, script := range []string{"lint.sh", "lintconfig.sh"} {
		t.Run(script, func(t *testing.T) {
			t.Parallel()
			runs := filepath.Join(t.TempDir(), "runs")
			f := newLintFixture(t, "")
			f.index()
			shim := t.TempDir()
			gittree.WriteFile(t, shim, "go", []byte(buildShimGo), 0o700)
			record := filepath.Join(t.TempDir(), "build")
			f.env = append(f.env,
				"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
				"GO_SHIM_REAL="+real, "GO_SHIM_LINTER="+fakeLinter(t, runs), "GO_SHIM_RECORD="+record,
				"GOEXPERIMENT=jsonv2", "GOOS=plan9", "GOARCH=386")

			r := f.run(script)
			r.wantCode(t, 0)
			built, err := os.ReadFile(record)
			if err != nil {
				t.Fatalf("the script built no linter: %v\nstdout:\n%s\nstderr:\n%s", err, r.stdout, r.stderr)
			}
			if want := "GOEXPERIMENT=\nGOOS=" + runtime.GOOS + "\nGOARCH=" + runtime.GOARCH + "\n"; !strings.HasPrefix(string(built), want) {
				t.Errorf("the linter was built under %q, want no experiment, for this host", built)
			}
			ran, err := os.ReadFile(runs)
			if err != nil || len(ran) == 0 {
				t.Errorf("the built linter did not run: %v", err)
			}
			_, out, _ := strings.Cut(strings.TrimSpace(string(built)), "out=")
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Errorf("the linter built at %s is still there: %v", out, err)
			}
		})
	}
}
