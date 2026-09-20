package scripttest

import (
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
// leave the suite untouched,
// so an arm that scored either as a kill would report an unmeasured mutant as
// measured.
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
