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

// extraSource and extraTest give package m a function only a test calls, a
// format string vet reads and a package-level value built at init. Package q
// is a second package whose test build a mutant can break alone.
const (
	extraSource = "package m\n\nimport (\n\t\"fmt\"\n\t\"regexp\"\n)\n\nvar vowels = regexp.MustCompile(\"[aeiou]\")\n\n" +
		"func Double(a int) int { return 2 * a }\n\nfunc Name(s string) string { return fmt.Sprintf(\"n=%s\", s) }\n\n" +
		"func HasVowel(s string) bool { return vowels.MatchString(s) }\n"
	extraTest = "package m\n\nimport \"testing\"\n\nfunc TestExtra(t *testing.T) {\n" +
		"\tif Double(2) != 4 || Name(\"x\") != \"n=x\" || !HasVowel(\"a\") {\n\t\tt.Fatal(\"extra\")\n\t}\n}\n"
	qSource = "package q\n\nfunc One() int { return 1 }\n"
	qTest   = "package q\n\nimport \"testing\"\n\nfunc TestOne(t *testing.T) {\n\tif One() != 1 {\n\t\tt.Fatal(\"one\")\n\t}\n}\n"
)

// A verdict comes only from tests that ran. go build compiles no test file and
// runs no vet, so a mutant that failed go test before any test ran once read as
// killed; the check that refuses it must build and vet exactly what go test
// does, run none of it, and come before the verdict run.
func TestMutateScript_JudgesAMutantOnlyByTestsThatRan(t *testing.T) {
	t.Parallel()
	const refused = "mutate: the mutated tree DOES NOT BUILD its tests or fails their vet, so the suite cannot judge it"
	for _, c := range []struct {
		name, file, search, replace string
		pkgs                        []string
		code                        int
		want                        []string // substrings of stdout and stderr together
		runs                        int      // TestAdd runs, the baseline's included
	}{
		{
			name: "a test file that no longer builds", file: "m/extra_test.go",
			search: `import "testing"`, replace: "import (\n\t\"strings\"\n\t\"testing\"\n)",
			pkgs: []string{"./m/"}, code: 1, want: []string{refused, `"strings" imported and not used`}, runs: 1,
		},
		{
			name: "a production symbol only a test calls", file: "m/extra.go",
			search: "func Double(a int) int", replace: "func Double(a, b int) int",
			pkgs: []string{"./m/"}, code: 1, want: []string{refused, "not enough arguments in call to Double"}, runs: 1,
		},
		{
			name: "a production mutant go test's vet refuses", file: "m/extra.go",
			search: `"n=%s", s`, replace: `"n=%d", s`,
			pkgs: []string{"./m/"}, code: 1, want: []string{refused, "format %d has arg s of wrong type string"}, runs: 1,
		},
		{
			name: "another named package's test build, refused before the verdict run", file: "q/q_test.go",
			search: `import "testing"`, replace: "import (\n\t\"strings\"\n\t\"testing\"\n)",
			pkgs: []string{"./m/", "./q/"}, code: 1, want: []string{refused}, runs: 1,
		},
		{
			name: "a production mutant that does not build", file: "m/extra.go",
			search: "return 2 * a", replace: "return 2 *",
			pkgs: []string{"./m/"}, code: 1, want: []string{"mutate: the mutated tree DOES NOT BUILD, so the suite cannot judge it"}, runs: 1,
		},
		{
			name: "a panic at init is a kill", file: "m/extra.go",
			search: `"[aeiou]"`, replace: `"[aeiou"`,
			pkgs: []string{"./m/"}, code: 0, want: []string{"mutate: MUTANT KILLED"}, runs: 1,
		},
		{
			name: "a vet check go test does not run is no refusal", file: "m/extra.go",
			search: "{ return 2 * a }", replace: "{ a = a; return 2 * a }",
			pkgs: []string{"./m/"}, code: 1, want: []string{"mutate: MUTANT SURVIVED"}, runs: 2,
		},
		{
			name: "a test build outside the named packages is no refusal", file: "m/extra_test.go",
			search: `import "testing"`, replace: "import (\n\t\"strings\"\n\t\"testing\"\n)",
			pkgs: []string{"./ok/"}, code: 1, want: []string{"mutate: MUTANT SURVIVED"}, runs: 0,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			f, runLog := mutateFixture(t, "3")
			f.write("m/extra.go", extraSource)
			f.write("m/extra_test.go", extraTest)
			f.write("q/q.go", qSource)
			f.write("q/q_test.go", qTest)
			f.index()
			before, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(c.file)))
			if err != nil {
				t.Fatal(err)
			}

			r := f.run("mutate.sh", append([]string{c.file, c.search, c.replace}, c.pkgs...)...)
			r.wantCode(t, c.code)
			for _, w := range c.want {
				if !strings.Contains(r.stdout+r.stderr, w) {
					t.Errorf("output does not hold %q\nstdout:\n%s\nstderr:\n%s", w, r.stdout, r.stderr)
				}
			}
			if got := testRuns(t, runLog); got != c.runs {
				t.Errorf("%d runs of TestAdd, want %d", got, c.runs)
			}
			after, err := os.ReadFile(filepath.Join(f.dir, filepath.FromSlash(c.file)))
			if err != nil {
				t.Fatal(err)
			}
			if string(after) != string(before) {
				t.Errorf("%s holds %q after the run, want %q", c.file, after, before)
			}
		})
	}
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
