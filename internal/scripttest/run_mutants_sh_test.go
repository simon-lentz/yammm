package scripttest

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
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
	b, err := os.ReadFile(filepath.Join(f.dir, "out", "logs", "killed.asis.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "Add(1, 2) is wrong") {
		t.Errorf("the killed mutant's log drops what its test reported:\n%s", b)
	}
}

// A kill whose test prints a byte that is not UTF-8 keeps its verdict, its
// failing test and that test's output, and every later mutant still gets a
// verdict: the byte after the failing test's line, before it, and inside a
// line the failing-test list reads. The run is under a UTF-8 locale, where
// macOS sed and tr refuse such input and GNU grep drops the line that holds
// the byte.
func TestRunMutantsScript_ReadsTestOutputThatIsNotUTF8(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	f.env = append(f.env, "LC_ALL=C.UTF-8")
	const assertion = "if Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\")"
	for id, replace := range map[string]string{
		"a-after":  "if Add(1, 2) == 3 {\n\t\tt.Fatal(\"Add(1, 2) is \\xff\")",
		"a-before": "println(\"early \\xff\")\n\tif Add(1, 2) == 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\")",
		"a-inline": "if Add(1, 2) == 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\\n--- FAIL: \\xff\")",
	} {
		dir := f.writeMutantSearching(id, assertion, replace)
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte("m/m_test.go"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f.writeMutant("b-killed", "a - b")
	f.writeMutant("c-survived", "b + a")

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	if r.code != 0 {
		t.Fatalf("exit code %d, want 0\nstdout:\n%s\nstderr:\n%s", r.code, r.stdout, r.stderr)
	}
	out := filepath.Join(f.dir, "out")
	rows := resultRows(t, out)
	for id, want := range map[string]struct{ verdict, fails string }{
		"a-after":    {"KILLED", "TestAdd "},
		"a-before":   {"KILLED", "TestAdd "},
		"a-inline":   {"KILLED", "TestAdd \xff "},
		"b-killed":   {"KILLED", "TestAdd "},
		"c-survived": {"SURVIVED", ""},
	} {
		if row := rows[id]; len(row) < 6 || row[2] != want.verdict || row[5] != want.fails {
			t.Errorf("mutant %q's row is %q, want verdict %q and failing tests %q", id, row, want.verdict, want.fails)
		}
	}
	for id, line := range map[string]string{
		"a-after":  "Add(1, 2) is \xff",
		"a-before": "early \xff",
		"a-inline": "--- FAIL: \xff",
	} {
		log, err := os.ReadFile(filepath.Join(out, "logs", id+".asis.log"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Contains(log, []byte(line)) {
			t.Errorf("%s's log drops the line its test printed, %q:\n%q", id, line, log)
		}
	}
}

// resultRows maps each mutant id in results.tsv to its columns.
func resultRows(t *testing.T, out string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(out, "results.tsv"))
	if err != nil {
		t.Fatalf("read results.tsv: %v", err)
	}
	rows := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(string(b), "\n"), "\n") {
		cols := strings.Split(line, "\t")
		if cols[0] != "id" {
			rows[cols[0]] = cols
		}
	}
	return rows
}

// A worker that stops before its first verdict writes no results file. The
// run still writes its summary and says a worker stopped, and exits non-zero.
func TestRunMutantsScript_ReportsAWorkerThatStoppedBeforeItsFirstVerdict(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	f := runMutantsFixture(t)
	dir := f.writeMutant("unknown", "a - b")
	if err := os.WriteFile(filepath.Join(dir, "spell"), []byte("sideways"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	r.wantCode(t, 1)
	if !strings.Contains(r.stderr, "a worker stopped early") || !strings.Contains(r.stdout, "wall clock:") {
		t.Errorf("the run did not report the stopped worker\nstdout:\n%s\nstderr:\n%s", r.stdout, r.stderr)
	}
	if rows := resultRows(t, filepath.Join(f.dir, "out")); len(rows) != 0 {
		t.Errorf("results.tsv holds rows for a run that judged nothing: %v", rows)
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
// each kill a script's shell makes, as its exit status and its arguments, and
// the log's path.
func killLogger(t *testing.T) (env []string, log string) {
	t.Helper()
	dir := t.TempDir()
	log = filepath.Join(dir, "kills")
	rc := filepath.Join(dir, "bashenv")
	const logged = `kill() { builtin kill "$@"; local rc=$?; printf '%s %s\n' "$rc" "$*" >>"$KILL_LOG"; return "$rc"; }` + "\n"
	if err := os.WriteFile(rc, []byte(logged), 0o600); err != nil {
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

// An out directory inside the checkout is left out of every worker's copy.
// A later worker's copy would otherwise hold an earlier worker's tree, whose
// .git mutate.sh's baseline key cannot hash, and its mutant would read OTHER.
// A name rsync would read as a wildcard names that directory alone: a tracked
// sibling it would also match stays in every copy, whose unstaged tree a
// worker refuses once a tracked file is missing. A backslash in a name with no
// wildcard is not an escape to rsync, and a ] alone is no wildcard, so such a
// name is written as it stands.
//
// The end-to-end run needs rsync, which the Windows job does not carry.
func TestRunMutantsScript_LeavesAnOutDirectoryInsideTheCheckoutOutOfEveryCopy(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
	for _, out := range []string{"runs/out", "runs/o[1]", "runs/o]1", `runs/o\1`} {
		t.Run(out, func(t *testing.T) {
			t.Parallel()
			f := runMutantsFixture(t)
			f.write("runs/o1/keep.txt", "tracked\n")
			f.index()
			f.writeMutant("m01", "a - b")
			f.writeMutant("m02", "b - a")

			r := f.run("run_mutants.sh", ".", "mutants", out, "2")

			r.wantCode(t, 0)
			got := verdicts(t, filepath.Join(f.dir, out))
			for _, id := range []string{"m01", "m02"} {
				if got[id] != "KILLED" {
					t.Errorf("mutant %q read %q, want KILLED (results.tsv: %v)", id, got[id], got)
				}
			}
		})
	}
}

// An out directory that is the checkout itself is refused before anything is
// written: every worker's tree would lie inside the checkout it copies.
func TestRunMutantsScript_RefusesTheCheckoutAsTheOutDirectory(t *testing.T) {
	t.Parallel()
	f := runMutantsFixture(t)
	f.writeMutant("m01", "a - b")

	r := f.run("run_mutants.sh", ".", "mutants", ".")

	r.wantCode(t, 2)
	r.wantStderr(t, "is the checkout")
	if _, err := os.Stat(filepath.Join(f.dir, "work")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the run wrote work/ before the refusal: stat says %v", err)
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
