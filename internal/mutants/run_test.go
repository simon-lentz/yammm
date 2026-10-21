package main

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/gittree"
)

// A binary this program stops at the limit reads TIMEOUT whatever it printed,
// and the limit holds the run before the change too.
func TestRun_StopsABinaryAtTheLimit(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("h/h.go", "package h\n\nfunc Wait() {}\n")
	// Each printed line is one go test's own text could hold for a failure.
	// TestDeadline fails when go test's own deadline is the runner's limit.
	f.write("h/h_test.go", "package h\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestQuick(t *testing.T) {}\n\n"+
		"func TestDeadline(t *testing.T) {\n\tif d, ok := t.Deadline(); !ok || time.Until(d) < 30*time.Second {\n\t\tt.Fatal(\"go test stops this binary at the runner's limit\")\n\t}\n}\n\n"+
		"func TestPrintsThenWaits(t *testing.T) {\n\tfmt.Println(\"    --- FAIL: TestInner (0.00s)\")\n\tfmt.Println(\"FAIL\\tfake\\t0.01s\")\n\tWait()\n}\n")
	f.write("slow/slow.go", "package slow\n\nconst S = 1\n")
	f.write("slow/slow_test.go", "package slow\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestSlow(t *testing.T) { time.Sleep(time.Hour) }\n")
	f.index()
	f.env = []string{limitVar + "=15s"}
	f.mutant("hang", "m/m.go", "return a + b", "for {\n\t}", "./m/")
	f.mutant("hang-after-printing-a-failure", "h/h.go", "func Wait() {}", "func Wait() { select {} }", "./h/")
	f.mutant("slow-before-the-change", "slow/slow.go", "S = 1", "S = 2", "./slow/")

	r := f.run(f.args("3")...)

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{"hang": verdictTimeout, "hang-after-printing-a-failure": verdictTimeout, "slow-before-the-change": verdictRed})
	for id, tests := range map[string]string{"hang": "TestAdd", "hang-after-printing-a-failure": "TestPrintsThenWaits", "slow-before-the-change": ""} {
		if got := rows[id+"/asis"]["failing tests"]; got != tests {
			t.Errorf("mutant %s names the tests %q, want %q", id, got, tests)
		}
	}
	for id, want := range map[string]string{"hang": "=== example.com/fx/m was stopped at the limit\n", "slow-before-the-change": "=== example.com/fx/slow was stopped at the limit\n"} {
		if log := r.log(t, id); !strings.Contains(log, want) {
			t.Errorf("mutant %s: the log does not hold %q:\n%s", id, want, log)
		}
	}
	if runtime.GOOS != "windows" {
		if log := r.log(t, "hang"); !strings.Contains(log, "SIGQUIT: quit") || !strings.Contains(log, "goroutine ") {
			t.Errorf("the stopped binary's stacks are not in the log:\n%s", log)
		}
	}
}

// A file behind a build tag is judged by the build that compiles it: the plain
// build holds neither the mutant nor its syntax error. A Go file under a
// directory the go command ignores, as a fixture under testdata is, is data a
// test reads, and the plain build judges it. The run before the change and the
// changed tree's build are the judging configuration's: bm is red under the
// race detector alone, and a race-only constant's rename breaks only the race
// build of a command. The caller's GOEXPERIMENT reaches no configuration. One
// run holds them all, so they share its build cache, where separate tests each
// built the race standard library in a cache of their own.
func TestRun_JudgesAFileUnderTheConfigurationThatCompilesIt(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.env = []string{"GOEXPERIMENT=jsonv2"}
	f.write("bm/racy.go", "//go:build race\n\npackage bm\n\nconst Racy = true\n")
	f.write("bm/notracy.go", "//go:build !race\n\npackage bm\n\nconst Racy = false\n")
	f.write("bm/racy_test.go", "package bm\n\nimport \"testing\"\n\nfunc TestNotRace(t *testing.T) {\n\tif Racy {\n\t\tt.Fatal(\"red under the race detector\")\n\t}\n}\n")
	f.write("ro/ro.go", "package ro\n")
	f.write("ro/race.go", "//go:build race\n\npackage ro\n\nconst RaceOnly = 1\n")
	f.write("ro/ro_test.go", "package ro\n\nimport \"testing\"\n\nfunc TestRo(t *testing.T) {}\n")
	f.write("rt/main.go", "package main\n\nfunc main() {}\n")
	f.write("rt/race.go", "//go:build race\n\npackage main\n\nimport \"example.com/fx/ro\"\n\nvar _ = ro.RaceOnly\n")
	f.write("m/race.go", "//go:build race\n\npackage m\n\nconst Race = true\n")
	f.write("m/norace.go", "//go:build !race\n\npackage m\n\nconst Race = false\n")
	f.write("m/race_test.go", "package m\n\nimport (\n\t\"os\"\n\t\"runtime/debug\"\n\t\"testing\"\n)\n\nfunc TestRace(t *testing.T) {\n\tinfo, _ := debug.ReadBuildInfo()\n"+
		"\trace := false\n\tfor _, s := range info.Settings {\n\t\tif s.Key == \"-race\" {\n\t\t\trace = s.Value == \"true\"\n\t\t}\n\t}\n"+
		"\tif Race != race {\n\t\tt.Fatal(\"Race is not the build's\")\n\t}\n}\n\n"+
		"func TestFixture(t *testing.T) {\n\tif b, err := os.ReadFile(\"testdata/fixture.go\"); err != nil || string(b) != \"package fixture\\n\" {\n\t\tt.Fatalf(\"the fixture is %q, %v\", b, err)\n\t}\n}\n")
	f.write("m/other.go", "//go:build elsewhere\n\npackage m\n\nconst Other = 1\n")
	f.write("m/broken.go", "//go:build elsewhere\n\npackage wrong\n")
	f.write("m/panicky.go", "//go:build elsewhere\n\npackage m\n\nfunc init() { panic(\"pulled into the build\") }\n")
	f.write("m/testdata/fixture.go", "package fixture\n")
	f.write("m/Em.go", "//go:build mermaid\n\npackage m\n\nimport _ \"embed\"\n\n//go:embed assets/em.txt\nvar Em string\n")
	f.write("m/Em_test.go", "//go:build mermaid\n\npackage m\n\nimport \"testing\"\n\nfunc TestEm(t *testing.T) {\n\tif Em != \"em\\n\" {\n\t\tt.Fatal(Em)\n\t}\n}\n")
	f.write("m/assets/em.txt", "em\n")
	// A file and its test that one gate configuration alone compiles.
	for name, tag := range map[string]string{"J": "goexperiment.jsonv2", "Mm": "mermaid", "N4": "neo4j_integration"} {
		f.write("m/"+name+".go", "//go:build "+tag+"\n\npackage m\n\nconst "+name+" = 1\n")
		f.write("m/"+name+"_test.go", "//go:build "+tag+"\n\npackage m\n\nimport \"testing\"\n\nfunc Test"+name+"(t *testing.T) {\n\tif "+name+" != 1 {\n\t\tt.Fatal(\""+name+"\")\n\t}\n}\n")
	}
	f.index()
	f.mutant("value", "m/race.go", "Race = true", "Race = false", "")
	f.mutant("syntax", "m/race.go", "Race = true", "Race = = true", "")
	f.mutant("plain", "m/norace.go", "Race = false", "Race = true", "")
	f.mutant("no-configuration", "m/other.go", "Other = 1", "Other = 2", "")
	f.mutant("into-the-build", "m/other.go", "//go:build elsewhere", "//go:build !elsewhere", "")
	f.mutant("fixture-read-as-data", "m/testdata/fixture.go", "package fixture", "package other", "./m/")
	f.mutant("broken-into-the-build", "m/broken.go", "//go:build elsewhere", "//go:build !elsewhere", "")
	f.mutant("panic-into-the-build", "m/panicky.go", "//go:build elsewhere", "//go:build !elsewhere", "")
	f.mutant("jsonv2", "m/J.go", "J = 1", "J = 2", "")
	f.mutant("mermaid", "m/Mm.go", "Mm = 1", "Mm = 2", "")
	f.mutant("neo4j-integration", "m/N4.go", "N4 = 1", "N4 = 2", "")
	f.mutant("embedded-from-below", "m/assets/em.txt", "em", "xx", "./m/")
	f.mutant("race-red", "bm/racy.go", "Racy = true", "Racy = false", "./bm/")
	f.mutant("race-build", "ro/race.go", "RaceOnly = 1", "RaceOnlyX = 1", "./ro/")

	r := f.run(f.args("3")...)

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{
		"value": verdictKilled, "syntax": verdictNoBuild, "plain": verdictKilled, "no-configuration": verdictNotCompiled, "into-the-build": verdictSurvived,
		"fixture-read-as-data": verdictKilled, "broken-into-the-build": verdictNoBuild, "panic-into-the-build": verdictKilled,
		"jsonv2": verdictKilled, "mermaid": verdictKilled, "neo4j-integration": verdictKilled, "embedded-from-below": verdictKilled,
		"race-red": verdictRed, "race-build": verdictNoBuild,
	})
	for id, config := range map[string]string{
		"value": "race", "syntax": "race", "plain": "default", "no-configuration": "", "into-the-build": "default", "fixture-read-as-data": "default",
		"broken-into-the-build": "default", "panic-into-the-build": "default", "jsonv2": "jsonv2", "mermaid": "mermaid", "neo4j-integration": "neo4j_integration",
		"embedded-from-below": "mermaid", "race-red": "race", "race-build": "race",
	} {
		if got := rows[id+"/asis"]["configuration"]; got != config {
			t.Errorf("mutant %s is judged under %q, want %q", id, got, config)
		}
	}
}

// countingTest appends a line to the file FX_RUN_LOG names each time it runs.
const countingTest = "package m\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n" +
	"\tf, err := os.OpenFile(os.Getenv(\"FX_RUN_LOG\"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)\n\tif err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\tif _, err := f.WriteString(os.Getenv(\"TMPDIR\") + \"\\n\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n\tif err := f.Close(); err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\tif Add(1, 2) != 3 {\n\t\tt.Fatal(\"Add(1, 2) is wrong\")\n\t}\n}\n"

// With one worker the packages run once before the change for each package set
// and spelling, and once for each mutant and spelling.
func TestRun_RunsThePackagesOnceBeforeTheChangeForASetAndSpelling(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("m/m_test.go", countingTest)
	f.index()
	runLog := filepath.Join(t.TempDir(), "runs.log")
	f.env = []string{"FX_RUN_LOG=" + runLog}
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
	f.mutant("m02", "m/m.go", "a + b", "a * b", "./m/")
	f.mutant("m03", "m/m.go", "a + b", "b - a", "./m")
	spell := filepath.Join(f.mutant("m04", "m/m.go", "a + b", "a - b - 0", "./m/"), "spell")
	if err := os.WriteFile(spell, []byte("asis folded\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	asis, folded, err := tmpSpellings(os.Getenv("TMPDIR"))
	if err != nil {
		t.Fatal(err)
	}

	r := f.run()

	r.wantCode(t, 0)
	rows := r.rows(t)
	want := map[string]string{"m01/asis": verdictKilled, "m02/asis": verdictKilled, "m03/asis": verdictKilled, "m04/asis": verdictKilled, "m04/folded": verdictKilled}
	runs := map[string]int{asis: 6, folded: 2} // ./m/ and ./m each once before the change, then four mutants; one and one
	if folded == "" {
		want["m04/folded"] = verdictNoFold
		runs = map[string]int{asis: 6}
	}
	for key, verdict := range want {
		if got := rows[key]["verdict"]; got != verdict {
			t.Errorf("%s reads %q, want %s", key, got, verdict)
		}
	}
	if len(rows) != len(want) {
		t.Errorf("results.tsv holds %d rows, want %d", len(rows), len(want))
	}
	logged, err := os.ReadFile(runLog)
	if err != nil {
		t.Fatal(err)
	}
	for tmp, n := range runs {
		if got := bytes.Count(logged, []byte(tmp+"\n")); got != n {
			t.Errorf("%d test runs under TMPDIR=%s, want %d\n%s", got, tmp, n, logged)
		}
	}
	if _, err := os.Stat(filepath.Join(r.out, "logs", "m04.folded.log")); (err == nil) != (folded != "") {
		t.Errorf("the folded spelling's log: stat says %v on a host whose folded spelling is %q", err, folded)
	}
}

// A test that writes into the tree changes what the next run would judge, so
// the worker puts back what it copied before each run.
func TestRun_RestoresTheTreeATestChanged(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("data.txt", "one\n")
	f.write("w/w.go", "package w\n\nconst W = 1\n")
	// The test fails when it finds what an earlier run of it left.
	f.write("w/w_test.go", "package w\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestWrites(t *testing.T) {\n"+
		"\tif b, err := os.ReadFile(\"../data.txt\"); err != nil || string(b) != \"one\\n\" {\n\t\tt.Fatalf(\"data.txt is %q, %v\", b, err)\n\t}\n"+
		"\tif _, err := os.Stat(\"../left/behind.txt\"); err == nil {\n\t\tt.Fatal(\"an earlier run's file is in the tree\")\n\t}\n"+
		"\tfor name, content := range map[string]string{\"../data.txt\": \"two\\n\", \"../left/behind.txt\": \"x\"} {\n"+
		"\t\t_ = os.MkdirAll(\"../left\", 0o750)\n\t\tif err := os.WriteFile(name, []byte(content), 0o600); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n\t}\n"+
		"\tif err := os.Remove(\"../m/m_test.go\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n}\n")
	f.index()
	f.mutant("w01", "w/w.go", "W = 1", "W = 2", "./w/")
	f.mutant("w02", "w/w.go", "W = 1", "W = 3", "./w/")

	r := f.run()

	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"w01": verdictSurvived, "w02": verdictSurvived})
	const restored = "and was restored: data.txt left m/m_test.go\n"
	if log := r.log(t, "w01"); strings.Count(log, restored) != 2 {
		t.Errorf("the log does not say twice what was restored, after the run before the change and after the mutant's:\n%s", log)
	}
	out, err := os.ReadFile(filepath.Join(r.out, "work", "worker.1.out"))
	if err != nil || strings.Count(string(out), restored) != 3 {
		t.Errorf("the worker's record does not say three times what was restored (%v):\n%s", err, out)
	}
}

// go test under -failfast reports no result for a package that starts after
// a failure, and a package with no result is no kill.
func TestRun_ReportsEveryPackageUnderACallersFailfast(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	for _, name := range []string{"p1", "p2", "p3", "p4"} {
		f.write(name+"/p.go", "package "+name+"\n")
		f.write(name+"/p_test.go", "package "+name+"\n\nimport (\n\t\"testing\"\n\n\t\"example.com/fx/m\"\n)\n\nfunc TestP(t *testing.T) {\n\tif m.Add(1, 2) != 3 {\n\t\tt.Fatal(\"sum\")\n\t}\n}\n")
	}
	f.index()
	f.env = []string{"GOFLAGS=-mod=mod -failfast"}
	f.mutant("m01", "m/m.go", "a + b", "a - b", "")

	r := f.run()

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
	if got := rows["m01/asis"]["packages"]; got != "./m ./p1 ./p2 ./p3 ./p4" {
		t.Errorf("the computed set is %q", got)
	}
	if log := r.log(t, "m01"); strings.Count(log, " failed\n") != 5 {
		t.Errorf("the log does not hold five failed packages:\n%s", log)
	}
}

// A test binary go test could not start left no record, and its package's
// failure is no kill.
func TestRun_ReadsABinaryThatDidNotStartAsNoBuild(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
	f.mutant("m02", "m/m.go", "a + b", "a + b + 0", "./m/")
	args := f.args("1")
	var stdout, stderr bytes.Buffer

	code := judgeSet(t.Context(), args, f.environment(), filepath.Join(t.TempDir(), "no-such-runner"), seed(t), &stdout, &stderr)

	r := result{code: code, stdout: stdout.String(), stderr: stderr.String(), out: args[2]}
	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"m01": verdictRed, "m02": verdictRed})
	if log := r.log(t, "m01"); !strings.Contains(log, "=== example.com/fx/m did not start\n") || !strings.Contains(log, "no-such-runner") {
		t.Errorf("the log does not say the binary did not start:\n%s", log)
	}
}

// A test that asks for its deadline gets the gate's, so go test's wait for a
// binary's output to close, a tenth of it, is the gate's too.
func TestRun_GivesATestTheGatesDeadline(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("d/d.go", "package d\n\nfunc On() bool { return true }\n")
	f.write("d/d_test.go", "package d\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestDeadline(t *testing.T) {\n\tif dl, ok := t.Deadline(); !ok || time.Until(dl) < 9*time.Minute {\n\t\tt.Fatalf(\"deadline %v, %t\", dl, ok)\n\t}\n\tif !On() {\n\t\tt.Fatal(\"off\")\n\t}\n}\n")
	f.index()
	f.mutant("off", "d/d.go", "return true", "return false", "./d/")

	r := f.run()

	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"off": verdictKilled})
}

// A test only the mermaid job builds gets that job's deadline, not the full
// gate's.
func TestRun_GivesATestTheDeadlineOfItsConfigurationsGateJob(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("md/md.go", "//go:build mermaid\n\npackage md\n\nfunc On() bool { return true }\n")
	f.write("md/md_test.go", "//go:build mermaid\n\npackage md\n\nimport (\n\t\"testing\"\n\t\"time\"\n)\n\nfunc TestDeadline(t *testing.T) {\n\tif dl, ok := t.Deadline(); !ok || time.Until(dl) > 6*time.Minute {\n\t\tt.Fatalf(\"deadline %v, %t\", dl, ok)\n\t}\n\tif !On() {\n\t\tt.Fatal(\"off\")\n\t}\n}\n")
	f.index()
	f.mutant("off", "md/md.go", "return true", "return false", "./md/")

	r := f.run()

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{"off": verdictKilled})
	if got := rows["off/asis"]["configuration"]; got != "mermaid" {
		t.Errorf("the mutant is judged under %q, want mermaid", got)
	}
}

// Each configuration's timeout is the -timeout its gate job gives a go test
// that runs tests: scripts/test.sh's for the plain, race and jsonv2 builds,
// less its benchmark run, which runs no test, and the test workflow's tagged
// jobs' for the others. The benchmark run's -timeout and the default of its
// TEST_BENCH_LIMIT are benchLimit.
func TestConfigurations_TakeEachGateJobsTimeout(t *testing.T) {
	t.Parallel()
	read := func(path ...string) []byte {
		b, err := os.ReadFile(filepath.Join(append([]string{"..", ".."}, path...)...))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	want := map[string][]string{}
	for _, m := range regexp.MustCompile(`\bgo test [^\n]*-timeout=(\S+)`).FindAllSubmatch(read("scripts", "test.sh"), -1) {
		if bytes.Contains(m[0], []byte(`-run '^$'`)) {
			if d, err := time.ParseDuration(string(m[1])); err != nil || d != benchLimit {
				t.Errorf("scripts/test.sh's benchmark run takes -timeout=%s, and benchLimit is %v", m[1], benchLimit)
			}
			continue
		}
		for _, name := range []string{"default", "race", "jsonv2"} {
			want[name] = append(want[name], string(m[1]))
		}
	}
	for _, m := range regexp.MustCompile(`\bgo test [^\n]*-tags (\S+) -timeout (\S+)`).FindAllSubmatch(read(".github", "workflows", "yammm_test.yml"), -1) {
		want[string(m[1])] = append(want[string(m[1])], string(m[2]))
	}
	unset := regexp.MustCompile(`TEST_BENCH_LIMIT:-([^}]+)\}`).FindSubmatch(read("scripts", "test.sh"))
	if unset == nil {
		t.Fatal("scripts/test.sh gives TEST_BENCH_LIMIT no default")
	}
	if d, err := time.ParseDuration(string(unset[1])); err != nil || d != benchLimit {
		t.Errorf("scripts/test.sh's TEST_BENCH_LIMIT default is %s, and benchLimit is %v", unset[1], benchLimit)
	}
	for _, c := range configurations() {
		if len(want[c.name]) == 0 {
			t.Errorf("no gate job gives configuration %s a -timeout", c.name)
		}
		for _, timeout := range want[c.name] {
			if d, err := time.ParseDuration(timeout); err != nil || d != c.timeout {
				t.Errorf("configuration %s takes %v, and its gate job gives -timeout %s", c.name, c.timeout, timeout)
			}
		}
	}
}

// A mutant that makes a test exit 0 inside the test is killed, as the gate's
// summary fails a package that reports no result, though go test passes it.
func TestRun_KillsAMutantThatMakesATestExitInsideIt(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("e/e.go", "package e\n\nfunc Done() bool { return true }\n")
	f.write("e/e_test.go", "package e\n\nimport (\n\t\"syscall\"\n\t\"testing\"\n)\n\nfunc TestDone(t *testing.T) {\n\tif !Done() {\n\t\tsyscall.Exit(0)\n\t}\n}\n")
	f.index()
	f.mutant("exits", "e/e.go", "return true", "return false", "./e/")

	r := f.run()

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{"exits": verdictKilled})
	if got := rows["exits/asis"]["failing tests"]; got != "TestDone" {
		t.Errorf("the failing tests are %q, want TestDone", got)
	}
}

// On a host or a TMPDIR with no folded spelling a mutant asked for under it
// gets a row, no run and no log; its asis spelling is still judged.
func TestRun_RecordsNoFoldWhereTMPDIRHasNoFoldedSpelling(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.Mkdir(plain, 0o750); err != nil {
		t.Fatal(err)
	}
	f.env = []string{"TMPDIR=" + plain}
	gittree.WriteFile(t, f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/"), "spell", []byte("asis folded"), 0o600)

	r := f.run()

	r.wantCode(t, 0)
	rows := r.rows(t)
	if got := rows["m01/folded"]["verdict"]; got != verdictNoFold {
		t.Errorf("m01 reads %q under the folded spelling, want %s", got, verdictNoFold)
	}
	if got := rows["m01/asis"]["verdict"]; got != verdictKilled {
		t.Errorf("m01 reads %q under the asis spelling, want %s", got, verdictKilled)
	}
	if entries, err := os.ReadDir(filepath.Join(r.out, "logs")); err != nil || len(entries) != 1 {
		t.Errorf("the run wrote %d logs, %v; want the asis row's alone", len(entries), err)
	}
}

// With no workers argument a run takes four.
func TestRun_TakesFourWorkersByDefault(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	for _, id := range []string{"m1", "m2", "m3", "m4", "m5"} {
		f.mutant(id, "m/m.go", "not in the file "+id, "x", "./m/")
	}
	args := f.args("")[:3]

	r := f.run(args...)

	r.wantCode(t, 0)
	if !strings.Contains(r.stdout, "mutants: 5, workers: 4\n") {
		t.Errorf("stdout does not name four workers:\n%s", r.stdout)
	}
}

// A file's set holds every tracked package that imports it from its code, its
// tests or its external tests, and ./docs; an importer in a directory git
// ignores is no member.
func TestRun_ComputesTheSetFromEveryTrackedImporter(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	imports := "import \"example.com/fx/m\"\n\nvar _ = m.Add\n"
	f.write("c/c.go", "package c\n\n"+imports)
	f.write("it/it.go", "package it\n")
	f.write("it/it_test.go", "package it\n\n"+imports)
	f.write("xt/xt.go", "package xt\n")
	f.write("xt/xt_test.go", "package xt_test\n\n"+imports)
	f.write("docs/docs_test.go", "package docs\n")
	f.write("other/o.go", "package other\n")
	f.write(".gitignore", "u/\n")
	f.index()
	f.write("u/u.go", "package u\n\n"+imports)
	f.mutant("m01", "m/m.go", "not in the file", "x", "")

	r := f.run()

	r.wantCode(t, 0)
	if got := r.rows(t)["m01/asis"]["packages"]; got != "./c ./docs ./it ./m ./xt" {
		t.Errorf("the computed set is %q, want ./c ./docs ./it ./m ./xt", got)
	}
}

// A run that is red before the change still puts back what its tests wrote.
func TestRun_RestoresTheTreeAfterARedRun(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("w/w.go", "package w\n\nconst W = 1\n")
	f.write("w/w_test.go", "package w\n\nimport (\n\t\"os\"\n\t\"testing\"\n)\n\nfunc TestWrites(t *testing.T) {\n"+
		"\tif err := os.WriteFile(\"../left.txt\", nil, 0o600); err != nil {\n\t\tt.Fatal(err)\n\t}\n\tt.Fatal(\"red before any change\")\n}\n")
	f.index()
	f.mutant("w01", "w/w.go", "W = 1", "W = 2", "./w/")

	r := f.run()

	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"w01": verdictRed})
	if log := r.log(t, "w01"); !strings.Contains(log, "after the run before the change and was restored: left.txt\n") {
		t.Errorf("the red run's log does not say what was restored:\n%s", log)
	}
}

// A shell that changed into a link to the checkout exports PWD so, and the go
// command spells every directory it lists from PWD.
func TestRun_ComputesTheSetWhenTheCallerReachedTheCheckoutByALink(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	if runtime.GOOS == "windows" {
		t.Skip("needs a symbolic link")
	}
	f := newFixture(t)
	link := filepath.Join(t.TempDir(), "co")
	if err := os.Symlink(f.checkout, link); err != nil {
		t.Fatal(err)
	}
	f.env = []string{"PWD=" + link}
	f.mutant("m01", "m/m.go", "a + b", "a - b", "")
	args := f.args("1")
	args[0] = link

	r := f.run(args...)

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
	if got := rows["m01/asis"]["packages"]; got != "./m" {
		t.Errorf("the computed set is %q, want ./m", got)
	}
}

// A mutants directory kept in git holds .git, which is no mutant, nor is a
// file beside the mutants.
func TestRun_ReadsNoDotEntryOrFileAsAMutant(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
	gittree.WriteFile(t, filepath.Join(f.root, "mutants", ".git"), "HEAD", []byte("ref: refs/heads/main\n"), 0o600)
	gittree.WriteFile(t, filepath.Join(f.root, "mutants"), "README", []byte("a file is no mutant either\n"), 0o600)

	r := f.run()

	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
}

func TestRun_JudgesFromAnOutDirectoryInsideTheCheckout(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	for _, name := range []string{"runs/out", "runs/o[1]"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("runs/o1/kept.txt", "a tracked sibling of the out directory\n")
			f.index()
			f.write("untracked.txt", "copied with the tracked files\n")
			f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
			f.mutant("m02", "m/m.go", "a + b", "b - a", "./m/")
			f.mutant("m03-untracked", "untracked.txt", "copied", "moved", "./m/")
			args := f.args("2")
			args[2] = filepath.Join(f.checkout, filepath.FromSlash(name))

			r := f.run(args...)

			r.wantCode(t, 0)
			r.wantVerdicts(t, map[string]string{"m01": verdictKilled, "m02": verdictKilled, "m03-untracked": verdictSurvived})
		})
	}
}

// The pin is the checkout's: a caller's own directory names another toolchain.
// Under a choice that lets go.mod pick, a check run there would pick the
// caller's.
func TestRun_PinsTheCheckoutsToolchainFromAnotherDirectory(t *testing.T) {
	skipJudgingOnWindows(t)
	for _, choice := range []string{"", "go" + goDirective() + "+auto"} {
		t.Run("GOTOOLCHAIN="+choice, func(t *testing.T) {
			f := newFixture(t)
			caller := t.TempDir()
			if err := os.WriteFile(filepath.Join(caller, "go.mod"), []byte("module caller\n\ngo 1.99.0\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			f.env = []string{"GOTOOLCHAIN=" + choice}
			f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
			t.Chdir(caller)

			r := f.run()

			r.wantCode(t, 0)
			r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
		})
	}
}

func TestRun_RefusesACheckoutWithAFileGitCannotRead(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a file its owner cannot read")
	}
	f := newFixture(t)
	f.write("data.txt", "one\n")
	f.index()
	if err := os.Chmod(filepath.Join(f.checkout, "data.txt"), 0); err != nil {
		t.Fatal(err)
	}
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")

	r := f.run()

	r.wantCode(t, 2)
	r.wantStderr(t, "data.txt")
	if strings.Contains(r.stderr, "not clean") {
		t.Errorf("a file git cannot read is reported as a change:\n%s", r.stderr)
	}
	r.wantNothingStarted(t)
}

// A mutant the run cannot judge reads ERROR with the reason in its log, and
// the run then exits 1: here a test changes the checkout itself, so the
// worker's copy cannot be held to what was copied.
func TestRun_ExitsOneWhenAMutantCannotBeJudged(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("data.txt", "one\n")
	f.write("w/w.go", "package w\n\nconst W = 1\n")
	f.write("w/w_test.go", "package w\n\nimport (\n\t\"os\"\n\t\"path/filepath\"\n\t\"testing\"\n)\n\nfunc TestWrites(t *testing.T) {\n"+
		"\tfor _, name := range []string{\"../data.txt\", filepath.Join(os.Getenv(\"FX_CHECKOUT\"), \"data.txt\")} {\n"+
		"\t\tif err := os.WriteFile(name, []byte(\"two\\n\"), 0o600); err != nil {\n\t\t\tt.Fatal(err)\n\t\t}\n\t}\n}\n")
	f.index()
	f.env = []string{"FX_CHECKOUT=" + f.checkout}
	f.mutant("w01", "w/w.go", "W = 1", "W = 2", "./w/")

	r := f.run()

	r.wantCode(t, 1)
	r.wantVerdicts(t, map[string]string{"w01": verdictError})
	r.wantStderr(t, "1 row(s) are ERROR")
	if log := r.log(t, "w01"); !strings.Contains(log, "data.txt is not what was copied, so the checkout changed during the run") {
		t.Errorf("the log does not say why:\n%s", log)
	}
}

// removeWork names each directory of the work it cannot remove and reports
// that it left one, here one whose parent a test made unwritable.
func TestRun_RemoveWorkNamesWhatItLeavesBehind(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("a directory's write bit stops no removal on Windows or for root")
	}
	work := t.TempDir()
	gittree.WriteFile(t, work, "w1/x.txt", []byte("x\n"), 0o600)
	if err := os.Chmod(work, 0o500); err != nil { //nolint:gosec // a directory the run must not be able to write
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(work, 0o700) }) //nolint:gosec // restoring the test's own directory
	var stderr bytes.Buffer
	if (&run{work: filepath.Join(work, "absent")}).removeWork(&stderr) || !strings.Contains(stderr.String(), "absent") {
		t.Errorf("removeWork over a work directory it cannot read reports it removed all; stderr:\n%s", stderr.String())
	}
	stderr.Reset()
	r := &run{work: work}
	if r.removeWork(&stderr) {
		t.Errorf("removeWork reports every directory removed; stderr:\n%s", stderr.String())
	}
	if !strings.Contains(stderr.String(), "w1") || !strings.Contains(stderr.String(), "which the run leaves behind") {
		t.Errorf("stderr does not name w1 as left behind:\n%s", stderr.String())
	}

	if err := os.Chmod(work, 0o700); err != nil { //nolint:gosec // restoring the test's own directory
		t.Fatal(err)
	}
	stderr.Reset()
	if !r.removeWork(&stderr) {
		t.Errorf("removeWork reports a directory left behind; stderr:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(work, "w1")); !os.IsNotExist(err) {
		t.Errorf("w1 is still there: %v", err)
	}
}

// A run whose rows are all judged exits 1 when a worker tree outlives it, as
// one does when a test denies deleting what its directory holds, and names it.
func TestRun_ExitsOneWhenAWorkerTreeCannotBeRemoved(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	if runtime.GOOS != "darwin" {
		t.Skip("an access list that denies delete_child is darwin's")
	}
	f := newFixture(t)
	f.write("m/lock_test.go", "package m\n\nimport (\n\t\"os/exec\"\n\t\"testing\"\n)\n\n"+
		"func TestLock(t *testing.T) {\n\tif out, err := exec.Command(\"chmod\", \"+a\", \"everyone deny delete_child\", \".\").CombinedOutput(); err != nil {\n\t\tt.Fatalf(\"%v: %s\", err, out)\n\t}\n}\n")
	f.index()
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")

	r := f.run()
	t.Cleanup(func() {
		// t.Context is done before a cleanup runs.
		//nolint:gosec // chmod, with the run's own out directory
		if out, err := exec.CommandContext(context.Background(), "chmod", "-R", "-N", r.out).CombinedOutput(); err != nil {
			t.Errorf("clear the access lists under %s: %v: %s", r.out, err, out)
		}
	})

	r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
	r.wantCode(t, 1)
	r.wantStderr(t, "which the run leaves behind")
}

// holdTest is the test file of a package whose On returns false, run in mode:
// "test" or "bench" starts, when On returns true, a process that keeps the test
// binary's output open for 75s or 30s from its start, past the binary's exit;
// "slow" makes the benchmark run a minute.
func holdTest(mode string) string {
	return `package PKG

import (
	"os"
	"os/exec"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if d, err := time.ParseDuration(os.Getenv("HOLD_FOR")); err == nil {
		time.Sleep(d)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func hold(tb testing.TB, d string) {
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "HOLD_FOR="+d)
	// On Windows the process outlives go test, and no directory a live
	// process works in can be removed, the worker tree's among them.
	cmd.Dir = os.TempDir()
	cmd.Stdout = os.Stdout
	if err := cmd.Start(); err != nil {
		tb.Fatal(err)
	}
}

func TestOn(t *testing.T) {
	if On() && "MODE" == "test" {
		hold(t, "75s")
	}
}

func BenchmarkOn(b *testing.B) {
	if On() && "MODE" == "bench" {
		hold(b, "30s")
	}
	if On() && "MODE" == "slow" {
		time.Sleep(time.Minute)
	}
	for b.Loop() {
	}
}
`
}

// The plain configuration judges a mutant as the gate runs its tests and then
// its benchmarks, in one run:
//   - a mutant only a benchmark sees is killed, as scripts/test.sh's run of every
//     benchmark fails the gate, where a test alone passes;
//   - a package whose only judge is a benchmark that ran and passed reads SURVIVED;
//   - the benchmarks run before the change only for a mutant they judge: with a
//     benchmark that fails already, a mutant the tests miss reads RED, and one
//     they kill reads KILLED;
//   - a test binary whose output stays open past go test's wait fails the run of
//     its tests, as it fails the gate's -race run;
//   - a benchmark binary's output is held to a wait of the runner's own, and a
//     benchmark binary that runs to TEST_BENCH_LIMIT is killed, as
//     scripts/test.sh fails it.
func TestRun_JudgesTestsAndBenchmarksAsTheGateRunsThem(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.write("b/b.go", "package b\n\nfunc Name() string { return \"ASSOC\" }\n")
	f.write("b/b_test.go", "package b\n\nimport \"testing\"\n\nfunc TestRuns(t *testing.T) {}\n\n"+
		"func BenchmarkName(b *testing.B) {\n\tif Name() != \"ASSOC\" {\n\t\tb.Fatal(\"the benchmark's setup is wrong\")\n\t}\n\tfor b.Loop() {\n\t}\n}\n")
	f.write("p/p.go", "package p\n\nfunc Name() string { return \"ASSOC\" }\n")
	f.write("p/p_test.go", "package p\n\nimport \"testing\"\n\nfunc BenchmarkName(b *testing.B) {\n\tfor b.Loop() {\n\t\t_ = Name()\n\t}\n}\n")
	f.write("r/r.go", "package r\n\nfunc Name() string { return \"ASSOC\" }\n\nfunc Other() int { return 1 }\n")
	f.write("r/r_test.go", "package r\n\nimport \"testing\"\n\nfunc TestName(t *testing.T) {\n\tif Name() != \"ASSOC\" {\n\t\tt.Fatal(\"wrong name\")\n\t}\n}\n\n"+
		"func BenchmarkBroken(b *testing.B) { b.Fatal(\"broken before any change\") }\n")
	for pkg, mode := range map[string]string{"htest": "test", "hbench": "bench", "hslow": "slow"} {
		f.write(pkg+"/"+pkg+".go", "package "+pkg+"\n\nfunc On() bool { return false }\n")
		f.write(pkg+"/"+pkg+"_test.go", strings.ReplaceAll(strings.ReplaceAll(holdTest(mode), "PKG", pkg), "MODE", mode))
		f.mutant(pkg, pkg+"/"+pkg+".go", "return false", "return true", "./"+pkg+"/")
	}
	f.index()
	f.mutant("bench-kills", "b/b.go", "\"ASSOC\"", "\"assoc\"", "./b/")
	f.mutant("bench-passes", "p/p.go", "\"ASSOC\"", "\"assoc\"", "./p/")
	f.mutant("tests-kill", "r/r.go", "\"ASSOC\"", "\"assoc\"", "./r/")
	f.mutant("tests-miss", "r/r.go", "return 1", "return 2", "./r/")
	f.env = append(f.env, benchLimitVar+"=10s")

	r := f.run(f.args("3")...)

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, map[string]string{
		"bench-kills": verdictKilled, "bench-passes": verdictSurvived, "tests-kill": verdictKilled, "tests-miss": verdictRed,
		"htest": verdictKilled, "hbench": verdictKilled, "hslow": verdictKilled,
	})
	for id, want := range map[string]string{
		"bench-kills": "the benchmark's setup is wrong",
		"tests-miss":  "broken before any change",
		"htest":       "Test I/O incomplete",
		"hbench":      "output was still open 5s after it exited",
		"hslow":       "a benchmark binary ran to the gate's limit of 10s",
	} {
		if log := r.log(t, id); !strings.Contains(log, want) {
			t.Errorf("%s's log does not hold %q:\n%s", id, want, log)
		}
		if got := rows[id+"/asis"]["configuration"]; got != "default" {
			t.Errorf("%s is judged under %q, want default", id, got)
		}
	}
}

// A checkout scripts/packages.sh --check refuses is refused with its message,
// whatever this host builds: an untracked Go file in a tracked package, ignored
// or not, one in a new package git does not ignore, and a tracked package below
// a go.mod git does not track. A package whose every Go file git ignores, and a
// name go never reads, are judged around.
func TestRun_RefusesWhatScriptsPackagesShRefuses(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	script, err := os.ReadFile(filepath.Join("..", "..", "scripts", "packages.sh"))
	if err != nil {
		t.Fatal(err)
	}
	other := "windows"
	if runtime.GOOS == "windows" {
		other = "linux"
	}
	for _, tt := range []struct {
		name    string
		tracked map[string]string
		files   map[string]string
		want    string // "" for a run that judges
	}{
		{"an untracked file in a tracked package", nil, map[string]string{"m/extra.go": "package m\n"}, "m/extra.go"},
		{"an ignored file in a tracked package", map[string]string{".gitignore": "m/gen.go\n"}, map[string]string{"m/gen.go": "package m\n"}, "m/gen.go"},
		{"an untracked test in a tracked package", nil, map[string]string{"m/extra_test.go": "package m\n"}, "m/extra_test.go"},
		{"an untracked file in a package this host does not build", map[string]string{"o/o_" + other + ".go": "package o\n"}, map[string]string{"o/extra_" + other + ".go": "package o\n"}, "o/extra_" + other + ".go"},
		{"a new package", nil, map[string]string{"n/n.go": "package n\n"}, "untracked package(s) that git does not ignore, which a tracked package can import and no commit holds; add or remove each:\n  n\n"},
		{"a tracked package below an untracked go.mod", map[string]string{".gitignore": "k/go.mod\n", "k/k.go": "package k\n"}, map[string]string{"k/go.mod": "module k\n"}, "below a go.mod git does not track, which go reads as another module and no commit holds; remove each go.mod:\n  k\n"},
		// One checkout judged around all three, since each run that judges costs a full one.
		{
			"a package git ignores, one whose every Go file git ignores, and a name go never reads",
			map[string]string{".gitignore": "vendored/\ngen/*.go\n"},
			map[string]string{"vendored/v.go": "package vendored\n", "gen/g.go": "package gen\n", "m/_scratch.go": "package m\n"},
			"",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			gittree.WriteFile(t, f.checkout, "scripts/packages.sh", script, 0o700)
			for name, content := range tt.tracked {
				f.write(name, content)
			}
			f.index()
			for name, content := range tt.files {
				f.write(name, content)
			}
			f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")

			r := f.run()

			if tt.want == "" {
				r.wantCode(t, 0)
				r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
				return
			}
			r.wantCode(t, 2)
			r.wantStderr(t, "scripts/packages.sh --check refuses the checkout, as each gate script that takes its package list would")
			r.wantStderr(t, tt.want)
			r.wantNothingStarted(t)
		})
	}
}

// With no seed, as the program runs, the run copies nothing into its cache and
// still judges: a mutant whose search is not in its file reads NOMATCH, which
// starts no go command.
func TestJudgeSet_JudgesWithNoSeed(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.mutant("m01", "m/m.go", "no such text", "a - b", "./m/")
	args := f.args("1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := judgeSet(t.Context(), args, f.environment(), self, nil, &stdout, &stderr)
	r := result{code: code, stdout: stdout.String(), stderr: stderr.String(), out: args[2]}
	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"m01": verdictNoMatch})
}

// failingFS is a build cache seed that cannot be read.
type failingFS struct{}

func (failingFS) Open(string) (fs.File, error) { return nil, errors.New("the seed cannot be read") }

// A seed the run cannot copy is named, nothing is judged, and the run exits 1;
// a run with no seed copies nothing.
func TestJudgeSet_ExitsOneWhenItCannotCopyItsSeed(t *testing.T) {
	t.Parallel()
	skipJudgingOnWindows(t)
	f := newFixture(t)
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
	args := f.args("1")
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := judgeSet(t.Context(), args, f.environment(), self, failingFS{}, &stdout, &stderr); code != 1 {
		t.Errorf("exit %d, want 1\nstderr:\n%s", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "the seed cannot be read") {
		t.Errorf("stderr does not name the seed's failure:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(args[2], "results.tsv")); !os.IsNotExist(err) {
		t.Errorf("a run that copied no seed wrote results.tsv: %v", err)
	}

	cache := t.TempDir()
	if err := copySeed(cache, nil); err != nil {
		t.Errorf("copySeed with no seed = %v, want nil", err)
	}
	if entries, _ := os.ReadDir(cache); len(entries) != 0 {
		t.Errorf("copySeed with no seed wrote %d entries", len(entries))
	}
}
