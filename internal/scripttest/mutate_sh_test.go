package scripttest

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// countingTest appends a line to the file MUTATE_RUN_LOG names on every run, so
// a test counts the go test runs one mutate.sh call made.
const countingTest = `package m

import (
	"os"
	"testing"
)

func TestAdd(t *testing.T) {
	f, err := os.OpenFile(os.Getenv("MUTATE_RUN_LOG"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("run\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if Add(1, 2) != WANT {
		t.Fatal("Add(1, 2) is wrong")
	}
}
`

const addSource = "package m\n\nfunc Add(a, b int) int { return a + b }\n"

// mutateFixture returns an indexed fixture holding scripts/mutate.sh and a
// package m whose test counts its runs in runLog; the test passes when want is 3.
func mutateFixture(t *testing.T, want string) (f *fixture, runLog string) {
	t.Helper()
	f = newFixture(t)
	f.copyScript("mutate.sh")
	runLog = filepath.Join(t.TempDir(), "runs.log")
	f.write("m/m.go", addSource)
	f.write("m/m_test.go", strings.Replace(countingTest, "WANT", want, 1))
	f.env = []string{"GOFLAGS=-mod=mod -count=1", "MUTATE_RUN_LOG=" + runLog}
	f.index()
	return f, runLog
}

func (f *fixture) mutate() result {
	f.t.Helper()
	return f.run("mutate.sh", "m/m.go", "a + b", "a - b", "./m/")
}

func testRuns(t *testing.T, runLog string) int {
	t.Helper()
	b, err := os.ReadFile(runLog)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), "run\n")
}

func stamps(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0
	}
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// wantRestored fails unless the mutated file holds its source again.
func (f *fixture) wantRestored() {
	f.t.Helper()
	b, err := os.ReadFile(filepath.Join(f.dir, "m", "m.go"))
	if err != nil {
		f.t.Fatal(err)
	}
	if string(b) != addSource {
		f.t.Errorf("m/m.go holds %q after the run, want %q", b, addSource)
	}
}

func TestMutateScript_RunsTheBaselineBeforeEveryMutantWithoutACache(t *testing.T) {
	t.Parallel()
	f, runLog := mutateFixture(t, "3")

	for i := 1; i <= 2; i++ {
		r := f.mutate()
		r.wantCode(t, 0)
		r.wantStdout(t, "mutate: baseline green\n")
		r.wantStdout(t, "mutate: MUTANT KILLED")
		f.wantRestored()
		if got, want := testRuns(t, runLog), 2*i; got != want {
			t.Errorf("after call %d: %d test runs, want %d (a baseline and a mutant per call)", i, got, want)
		}
	}
}

func TestMutateScript_RecordsAGreenBaselineOncePerTreeAndEnvironment(t *testing.T) {
	t.Parallel()
	f, runLog := mutateFixture(t, "3")
	cache := filepath.Join(t.TempDir(), "baselines")
	f.env = append(f.env, "MUTATE_BASELINE_CACHE="+cache)

	steps := []struct {
		name       string
		change     func()
		wantCached bool
		wantRuns   int
		wantStamps int
	}{
		{name: "the first call runs the baseline and records it", change: func() {}, wantRuns: 2, wantStamps: 1},
		{name: "the same tree reuses the record", change: func() {}, wantCached: true, wantRuns: 3, wantStamps: 1},
		{
			name:     "an untracked file is a new tree",
			change:   func() { f.write("m/extra.go", "package m\n") },
			wantRuns: 5, wantStamps: 2,
		},
		{
			name:     "an edit to an indexed file is a new tree",
			change:   func() { f.write("ok/ok.go", "package ok\n\nconst Edited = 1\n") },
			wantRuns: 7, wantStamps: 3,
		},
		{
			name:     "another TMPDIR is a new environment",
			change:   func() { f.env = append(f.env, "TMPDIR="+t.TempDir()) },
			wantRuns: 9, wantStamps: 4,
		},
		{
			name:     "a YAMMM_ variable is a new environment",
			change:   func() { f.env = append(f.env, "YAMMM_PROBE=1") },
			wantRuns: 11, wantStamps: 5,
		},
		{name: "the changed tree and environment reuse their own record", change: func() {}, wantCached: true, wantRuns: 12, wantStamps: 5},
	}
	for _, step := range steps {
		step.change()
		r := f.mutate()
		r.wantCode(t, 0)
		r.wantStdout(t, "mutate: MUTANT KILLED")
		f.wantRestored()
		cached := strings.Contains(r.stdout, "mutate: baseline green (recorded for this tree in "+cache+")\n")
		fresh := strings.Contains(r.stdout, "mutate: baseline green\n")
		if cached != step.wantCached || fresh == step.wantCached {
			t.Errorf("%s: recorded baseline reused = %t, fresh baseline run = %t; want reused %t\nstdout:\n%s", step.name, cached, fresh, step.wantCached, r.stdout)
		}
		if got := testRuns(t, runLog); got != step.wantRuns {
			t.Errorf("%s: %d test runs in all, want %d", step.name, got, step.wantRuns)
		}
		if got := stamps(t, cache); got != step.wantStamps {
			t.Errorf("%s: %d recorded baselines, want %d", step.name, got, step.wantStamps)
		}
	}
}

// TestMutateScript_NamesTheTestThatKilledTheMutant runs the mutant after 25
// passing packages, whose ok lines come first in go test's output.
func TestMutateScript_NamesTheTestThatKilledTheMutant(t *testing.T) {
	t.Parallel()
	f, _ := mutateFixture(t, "3")
	pkgs := make([]string, 0, 26)
	for i := 1; i <= 25; i++ {
		name := fmt.Sprintf("p%02d", i)
		f.write(name+"/"+name+"_test.go", "package "+name+"\n\nimport \"testing\"\n\nfunc TestPasses(t *testing.T) {}\n")
		pkgs = append(pkgs, "./"+name+"/")
	}
	f.index()
	pkgs = append(pkgs, "./m/")

	r := f.run("mutate.sh", append([]string{"m/m.go", "a + b", "a - b"}, pkgs...)...)
	r.wantCode(t, 0)
	r.wantStdout(t, "mutate: MUTANT KILLED")
	r.wantStdout(t, "--- FAIL: TestAdd")
	r.wantStdout(t, "FAIL\t"+fixtureModule+"/m")
	if strings.Contains(r.stdout, "\nok ") {
		t.Errorf("stdout lists passing packages after the kill; want the failures alone\nstdout:\n%s", r.stdout)
	}
	f.wantRestored()
}

func TestMutateScript_RecordsNoRedBaseline(t *testing.T) {
	t.Parallel()
	f, runLog := mutateFixture(t, "4")
	cache := filepath.Join(t.TempDir(), "baselines")
	f.env = append(f.env, "MUTATE_BASELINE_CACHE="+cache)

	for i := 1; i <= 2; i++ {
		r := f.mutate()
		r.wantCode(t, 1)
		r.wantStderr(t, "mutate: the UNMUTATED tree is already red in ./m/, so no verdict is possible\n")
		f.wantRestored()
		if got := testRuns(t, runLog); got != i {
			t.Errorf("after call %d: %d test runs, want %d (the baseline alone, never skipped)", i, got, i)
		}
		if got := stamps(t, cache); got != 0 {
			t.Errorf("after call %d: %d recorded baselines, want none for a red tree", i, got)
		}
	}
}
