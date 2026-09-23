package scripttest

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// runMutantsFixture returns an indexed fixture holding run_mutants.sh, the
// mutate.sh it drives, and a package m whose test passes only while Add is a
// sum.
func runMutantsFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.copyScript("mutate.sh")
	f.copyScript("run_mutants.sh")
	f.write("m/m.go", addSource)
	f.write("m/m_test.go", "package m\n\nimport \"testing\"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\")\n\t}\n}\n")
	f.env = []string{"GOFLAGS=-mod=mod -count=1"}
	f.index()
	return f
}

// writeMutant writes one mutant directory rewriting the fixture's sum.
func (f *fixture) writeMutant(id, replace string) string {
	return f.writeMutantSearching(id, "a + b", replace)
}

// writeMutantSearching writes one mutant directory with its own search string,
// so a caller can build the mutant that matches nothing.
func (f *fixture) writeMutantSearching(id, search, replace string) string {
	f.t.Helper()
	dir := filepath.Join(f.dir, "mutants", id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		f.t.Fatal(err)
	}
	files := map[string]string{
		"file":    "m/m.go",
		"search":  search,
		"replace": replace,
		"spell":   "asis",
		"pkgs":    "./m/",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			f.t.Fatal(err)
		}
	}
	return dir
}

// verdicts maps each mutant id in results.tsv to its verdict.
func verdicts(t *testing.T, out string) map[string]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "results.tsv"))
	if err != nil {
		t.Fatalf("read results.tsv: %v", err)
	}
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
		cols := strings.Split(line, "\t")
		if len(cols) < 3 || cols[0] == "id" {
			continue
		}
		got[cols[0]] = cols[2]
	}
	return got
}

// The three refusals below all run before the first copy is made, so they hold
// on a host with no rsync.

func TestRunMutantsScript_RefusesAMutantMissingAFile(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	dir := f.writeMutant("incomplete", "a - b")
	if err := os.Remove(filepath.Join(dir, "spell")); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out")

	if r.code != 2 {
		t.Errorf("exit code %d, want 2\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "has no spell") {
		t.Errorf("stderr does not name the missing file: %q", r.stderr)
	}
}

// A file name is used byte for byte, so one written with a trailing newline
// names no file; it is refused before any copy is made.
func TestRunMutantsScript_RefusesAMutantNamingNoFile(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	dir := f.writeMutant("echoed", "a - b")
	if err := os.WriteFile(filepath.Join(dir, "file"), []byte("m/m.go\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out")

	r.wantCode(t, 2)
	r.wantStderr(t, "mutant echoed names")
	if _, err := os.Stat(filepath.Join(f.dir, "out", "work", "w1")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the checkout was copied before the refusal: stat says %v", err)
	}
}

// recordingGo is a go that logs GOTOOLCHAIN and its arguments for each call.
const recordingGo = `#!/usr/bin/env bash
printf '%s %s\n' "${GOTOOLCHAIN:-<unset>}" "$*" >>"$GO_SHIM_LOG"
exec "$GO_SHIM_REAL" "$@"
`

// A caller that chose no toolchain gets the checkout's for the runner's own go
// commands, not only for the check.
func TestRunMutantsScript_RunsItsGoCommandsUnderThePin(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	if err := os.MkdirAll(filepath.Join(f.dir, "mutants"), 0o750); err != nil {
		t.Fatal(err)
	}
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "go"), []byte(recordingGo), 0o700); err != nil { //nolint:gosec // a test shim the fixture executes
		t.Fatal(err)
	}
	log := filepath.Join(shim, "calls")
	f.env = append(f.env,
		"GOTOOLCHAIN=",
		"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GO_SHIM_REAL="+real,
		"GO_SHIM_LOG="+log,
	)

	r := f.run("run_mutants.sh", ".", "mutants", "out")

	r.wantCode(t, 2)
	r.wantStderr(t, "no mutants in")
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	lists := 0
	for line := range strings.Lines(string(b)) {
		if !strings.Contains(line, " list ") {
			continue
		}
		lists++
		if !strings.HasPrefix(line, "go"+goDirective()+" ") {
			t.Errorf("a go list ran outside the pin: %q", line)
		}
	}
	if lists == 0 {
		t.Errorf("the runner ran no go list\n%s", b)
	}
}

func TestRunMutantsScript_RefusesAnEmptyMutantDirectory(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	if err := os.MkdirAll(filepath.Join(f.dir, "mutants"), 0o750); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out")

	if r.code != 2 {
		t.Errorf("exit code %d, want 2\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "no mutants") {
		t.Errorf("stderr does not say the set is empty: %q", r.stderr)
	}
}

// A dirty checkout is refused because every worker copies it: an uncommitted
// edit would ride into each copy and be scored as part of the mutant.
func TestRunMutantsScript_RefusesADirtyCheckout(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	f.writeMutant("sum", "a - b")
	f.write("m/m.go", addSource+"\nfunc Sub(a, b int) int { return a - b }\n")

	r := f.run("run_mutants.sh", ".", "mutants", "out")

	if r.code != 2 {
		t.Errorf("exit code %d, want 2\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "not clean") {
		t.Errorf("stderr does not name the dirty tree: %q", r.stderr)
	}
}

// One row per mutant, each carrying the outcome its run produced. A mutant that
// does not build, one whose tests do not build and one that matches nothing all
// leave the suite untouched, so an arm that scored any of them as a kill would
// report an unmeasured mutant as measured.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_RecordsOneVerdictPerMutant(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.writeMutant("killed", "a - b")
	f.writeMutant("survived", "b + a")
	f.writeMutant("nobuild", "a +")
	f.writeMutantSearching("nomatch", "a * b", "a - b")
	testBuild := f.writeMutantSearching("testbuild", `import "testing"`, "import (\n\t\"strings\"\n\t\"testing\"\n)")
	if err := os.WriteFile(filepath.Join(testBuild, "file"), []byte("m/m_test.go"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	if r.code != 0 {
		t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	got := verdicts(t, filepath.Join(f.dir, "out"))
	want := map[string]string{
		"killed":    "KILLED",
		"survived":  "SURVIVED",
		"nobuild":   "NOBUILD",
		"nomatch":   "NOMATCH",
		"testbuild": "NOBUILD",
	}
	for id, verdict := range want {
		if got[id] != verdict {
			t.Errorf("mutant %q read %q, want %q (results.tsv: %v)", id, got[id], verdict, got)
		}
	}
}

// Without a pkgs file the runner computes the package set from the import
// graph. The fixture module has no docs package, so the computed set is the
// import graph alone.
func TestRunMutantsScript_ComputesThePackageSetWithoutPkgs(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	dir := f.writeMutant("computed", "a - b")
	if err := os.Remove(filepath.Join(dir, "pkgs")); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	if r.code != 0 {
		t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	if got := verdicts(t, filepath.Join(f.dir, "out")); got["computed"] != "KILLED" {
		t.Errorf("mutant read %q, want KILLED (results.tsv: %v)", got["computed"], got)
	}
}

// A mutant no test was judged by reads NOBUILD whichever check caught it. The
// pre-build already refuses a mutant whose tests do not build, so the verdict
// run is shimmed to produce what only a tree that moved under it could: a
// package reporting a build failure after the pre-build passed.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_ReadsAVerdictRunThatRanNoTestAsNoBuild(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.writeMutant("norun", "a - b")
	// The worker's baseline is the first go test run and its verdict is the
	// second, so the shim replaces the verdict of this single mutant.
	f.shimVerdictRun("FAIL\t" + fixtureModule + "/m [build failed]\nFAIL\n")

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	if r.code != 0 {
		t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	if got := verdicts(t, filepath.Join(f.dir, "out")); got["norun"] != "NOBUILD" {
		t.Errorf("mutant read %q, want NOBUILD (results.tsv: %v)", got["norun"], got)
	}
}

// cacheEntriesNaming returns the files under cache whose bytes hold path. A
// compiled package archive holds the directory it was built from, which is how
// an entry written in a temporary tree is told from one written in a checkout.
func cacheEntriesNaming(t *testing.T, cache, path string) []string {
	t.Helper()
	var found []string
	err := filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if bytes.Contains(b, []byte(path)) {
			found = append(found, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", cache, err)
	}
	return found
}

// A run owns its build cache and its worker trees, removes both at exit and
// keeps its evidence. Every entry a worker's build writes is keyed by a copy
// the run then deletes, so an entry left in the caller's cache is one nothing
// reads again.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_RemovesItsCacheAndWorkerTrees(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.writeMutant("killed", "a - b")
	ambient := t.TempDir()
	f.env = append(f.env, "GOCACHE="+ambient)

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	r.wantCode(t, 0)
	// The script resolves its arguments with pwd -P, and a temporary directory
	// on darwin is reached through a symlink, so the trees it built in carry
	// the resolved name.
	dir, err := filepath.EvalSymlinks(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out")
	work := filepath.Join(out, "work")
	for _, gone := range []string{"gocache", "w1", "baselines.w1", "queue.1"} {
		if _, err := os.Stat(filepath.Join(work, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("work/%s survives the run: stat says %v", gone, err)
		}
	}
	for _, kept := range []string{
		filepath.Join(out, "results.tsv"),
		filepath.Join(out, "logs", "killed.asis.log"),
		filepath.Join(work, "worker.1.out"),
		filepath.Join(work, "results.w1.tsv"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("the removal took the run's evidence: %v", err)
		}
	}
	if naming := cacheEntriesNaming(t, ambient, filepath.Join(work, "w1")); len(naming) > 0 {
		t.Errorf("%d entries of the caller's cache name the worker tree, the first %s", len(naming), naming[0])
	}
}

// killLogger returns the environment that installs a BASH_ENV file logging
// each kill a script's shell makes before making it, and the log's path.
func killLogger(t *testing.T) (env []string, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "kills")
	rc := filepath.Join(dir, "bashenv")
	if err := os.WriteFile(rc, []byte("kill() { printf '%s\\n' \"$*\" >>\"$KILL_LOG\"; builtin kill \"$@\"; }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return []string{"BASH_ENV=" + rc, "KILL_LOG=" + log}, log
}

// A normal exit signals no process. Every worker has exited and been reaped by
// then, and the system may already have given a reaped worker's PID to another
// process.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_SignalsNothingAtANormalExit(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.writeMutant("m01", "a - b")
	f.writeMutant("m02", "b + a")
	env, log := killLogger(t)
	f.env = append(f.env, env...)

	r := f.run("run_mutants.sh", ".", "mutants", "out", "2")

	r.wantCode(t, 0)
	if b, err := os.ReadFile(log); err == nil && len(b) > 0 {
		t.Errorf("a normal exit signalled %q", b)
	} else if err != nil && !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}

// A search is used byte for byte, trailing newline included: without it,
// "return a + b" also names the first half of Twice, and mutate.sh rewrites
// every match.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_KeepsASearchsTrailingNewline(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.write("m/m.go", addSource+"\nfunc Twice(a, b int) int {\n\treturn a + b + a + b\n}\n\nfunc Once(a, b int) int {\n\treturn a + b\n}\n")
	f.index()
	f.writeMutantSearching("newline", "\treturn a + b\n", "\treturn a - b\n")

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	r.wantCode(t, 0)
	b, err := os.ReadFile(filepath.Join(f.dir, "out", "logs", "newline.asis.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "(1 occurrence(s) matched)") {
		t.Errorf("the search matched other than once:\n%s", b)
	}
}

// sleepingGo is a go whose every go test run that is not a pre-build records
// its PID and sleeps, so a worker stays busy until the test signals the run.
const sleepingGo = `#!/usr/bin/env bash
if [ "${1:-}" = "test" ]; then
	case " $* " in
	*" -exec=true "*) ;;
	*) printf '%s\n' "$$" >"$GO_SHIM_SLEEPER"; exec sleep 60 ;;
	esac
fi
exec "$GO_SHIM_REAL" "$@"
`

// A signalled run stops its running workers, then removes its cache and worker
// trees, and exits with the signal's status. The second worker has finished,
// unwaited behind the first, when the signal comes: it is not signalled,
// because its PID may already belong to another process.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_StopsItsWorkersWhenSignalled(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.writeMutant("a_slow", "a - b")
	f.writeMutantSearching("b_nomatch", "a * b", "a - b")
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "go"), []byte(sleepingGo), 0o700); err != nil { //nolint:gosec // a test shim the fixture executes
		t.Fatal(err)
	}
	sleeper := filepath.Join(shim, "sleeper")
	env, log := killLogger(t)
	f.env = append(f.env, env...)
	f.env = append(f.env,
		"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GO_SHIM_REAL="+real,
		"GO_SHIM_SLEEPER="+sleeper,
	)
	t.Cleanup(func() {
		if b, err := os.ReadFile(sleeper); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(b))); err == nil {
				if p, err := os.FindProcess(pid); err == nil {
					_ = p.Kill()
				}
			}
		}
	})

	//nolint:gosec // runs the repository's script, copied into the fixture's own module
	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(f.dir, "scripts", "run_mutants.sh"), ".", "mutants", "out", "2")
	cmd.Dir = f.dir
	cmd.Env = append(withoutRepositoryVars(t, fixtureEnv()), f.env...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		second, _ := os.ReadFile(filepath.Join(f.dir, "out", "work", "worker.2.out"))
		if _, err := os.Stat(sleeper); err == nil && bytes.Contains(second, []byte("NOMATCH")) {
			break
		}
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("no worker reached its baseline run\n%s", output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	var exit *exec.ExitError
	if err := cmd.Wait(); !errors.As(err, &exit) || exit.ExitCode() != 143 {
		t.Fatalf("run ended with %v, want exit 143\n%s", err, output.String())
	}

	dir, err := filepath.EvalSymlinks(f.dir)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(dir, "out", "work")
	for _, gone := range []string{"gocache", "w1", "w2"} {
		if _, err := os.Stat(filepath.Join(work, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("work/%s survives the signal: stat says %v", gone, err)
		}
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the run signalled no worker: %v", err)
	}
	var pids []int
	for field := range strings.FieldsSeq(string(b)) {
		if pid, err := strconv.Atoi(field); err == nil {
			pids = append(pids, pid)
		}
	}
	if len(pids) != 1 {
		t.Errorf("the run signalled %d processes, want the one running worker: %q", len(pids), b)
	}
	for _, pid := range pids {
		if p, err := os.FindProcess(pid); err == nil && p.Signal(syscall.Signal(0)) == nil {
			t.Errorf("worker %d still runs after the run exited", pid)
		}
	}
}
