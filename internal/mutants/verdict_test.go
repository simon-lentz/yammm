package main

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/testjson"
)

// verdictFixture adds to the fixture a package q that imports m and reads
// data.txt, packages nt and ntm with no test files (ntm imports m), a package
// red whose test fails, and a package pm whose TestMain can end the binary
// before any test; and, with no test files, a command and a package in a
// directory named tool.test that import m, and a package that embeds a file.
func verdictFixture(t *testing.T) *fixture {
	t.Helper()
	f := newFixture(t)
	f.write("m/m.go", "package m\n\nimport (\n\t\"fmt\"\n\t\"regexp\"\n)\n\nvar word = regexp.MustCompile(`vow+el`)\n\n"+
		"func Add(a, b int) int { return a + b }\n\nfunc Twice(a int) int { return a * 2 }\n\nfunc Double(a int) int { return a + a }\n\n"+
		"func Name(s string) string { return fmt.Sprintf(\"n=%s\", s) }\n\nfunc Matches(s string) bool { return word.MatchString(s) }\n")
	// The failing test prints two lines shaped as go test's own result lines and
	// one that is not valid UTF-8: none of them is a result.
	f.write("m/m_test.go", "package m\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n)\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n"+
		"\t\tfmt.Println(\"--- FAIL: TestGhost (0.00s)\")\n\t\tfmt.Println(\"ok  \\tnested/pkg\\t0.01s\")\n\t\tfmt.Println(\"raw \\xff byte\")\n"+
		"\t\tt.Fatal(\"Add(1, 2) is wrong\")\n\t}\n}\n\n"+
		"func TestExtra(t *testing.T) {\n\tif Name(\"x\") != \"n=x\" || !Matches(\"vowel\") {\n\t\tt.Fatal(\"extra\")\n\t}\n}\n")
	f.write("q/q.go", "package q\n\nimport \"example.com/fx/m\"\n\nfunc One() int { return m.Add(0, 1) }\n")
	f.write("q/q_test.go", "package q\n\nimport (\n\t\"os\"\n\t\"testing\"\n\n\t\"example.com/fx/m\"\n)\n\n"+
		"func TestOne(t *testing.T) {\n\tif One() != 1 || m.Double(1) != 2 {\n\t\tt.Fatal(\"one\")\n\t}\n}\n\n"+
		"func TestData(t *testing.T) {\n\tb, err := os.ReadFile(\"../data.txt\")\n\tif err != nil || string(b) != \"one\\n\" {\n\t\tt.Fatalf(\"data is %q, %v\", b, err)\n\t}\n}\n")
	f.write("data.txt", "one\n")
	f.write("m/x_plan9.s", "// plan9 only\n")
	f.write("cmd/tool/main.go", "package main\n\nimport \"example.com/fx/m\"\n\nfunc main() { _ = m.Twice(1) }\n")
	f.write("tool.test/t.go", "package tooltest\n\nimport \"example.com/fx/m\"\n\nvar T = m.Twice\n")
	f.write("emb/emb.go", "package emb\n\nimport _ \"embed\"\n\n//go:embed data.txt\nvar Data string\n")
	f.write("emb/data.txt", "x\n")
	f.write("nt/nt.go", "package nt\n\nconst N = 1\n")
	f.write("ntm/ntm.go", "package ntm\n\nimport \"example.com/fx/m\"\n\nfunc Two() int { return m.Twice(1) }\n")
	f.write("red/red.go", "package red\n\nconst R = 1\n")
	f.write("red/red_test.go", "package red\n\nimport \"testing\"\n\nfunc TestRed(t *testing.T) { t.Fatal(\"red before any change\") }\n\nfunc TestGreen(t *testing.T) {}\n")
	f.write("pm/pm.go", "package pm\n\nfunc Stop() bool { return false }\n")
	f.write("pm/pm_test.go", "package pm\n\nimport (\n\t\"fmt\"\n\t\"os\"\n\t\"testing\"\n)\n\n"+
		"func TestMain(m *testing.M) {\n\tif Stop() {\n\t\tfmt.Println(\"panic: test timed out after 1s\")\n\t\tos.Exit(1)\n\t}\n\tos.Exit(m.Run())\n}\n\nfunc TestOne(t *testing.T) {}\n")
	f.index()
	return f
}

func TestRun_GivesEachMutantItsVerdict(t *testing.T) {
	t.Parallel()
	f := verdictFixture(t)
	want := map[string]string{}
	for _, m := range []struct{ id, file, search, replace, pkgs, verdict string }{
		{"killed", "m/m.go", "a + b }", "a - b }", "./m/", verdictKilled},
		{"file-spelled-otherwise", "./m//m.go", "a + b }", "b - a - 1 }", "./m/", verdictKilled},
		{"judged-by-an-importer", "m/m.go", "a + a }", "a + a + 1 }", "./q/", verdictKilled},
		{"killed-beside-a-passing-package", "m/m.go", `"n=%s", s`, `"m=%s", s`, "./m/ ./q/", verdictKilled},
		{"missing-import", "m/m_test.go", "import (\n\t\"fmt\"\n\t\"testing\"\n)", "import (\n\t\"fmt\"\n\t\"testing\"\n\n\t_ \"example.org/missing\"\n)", "./m/", verdictNoBuild},
		{"main-importer", "m/m.go", "a * 2", "a * 5", "./cmd/tool/", verdictNotCompiled},
		{"named-like-a-test-binary", "m/m.go", "a * 2", "a * 6", "./tool.test/", verdictNotCompiled},
		{"embedded-in-no-test-binary", "emb/data.txt", "x", "y", "./emb/", verdictNotCompiled},
		{"ignored-other-file", "m/x_plan9.s", "plan9", "plan10", "./m/", verdictNotCompiled},
		{"survived", "m/m.go", "a * 2", "a * 3", "", verdictSurvived},
		{"nomatch", "m/m.go", "zzz", "y", "./m/", verdictNoMatch},
		{"unchanged", "m/m.go", "a + b", "a + b", "./m/", verdictNoMatch},
		{"production-build", "m/m.go", "a + b }", "a + }", "./m/", verdictNoBuild},
		{"test-build", "m/m_test.go", "Add(1, 2) != 3", "Add(1, 2) != three", "./m/", verdictNoBuild},
		{"go-test-vet", "m/m.go", `"n=%s", s`, `"n=%d", s`, "./m/", verdictNoBuild},
		{"another-packages-test-build", "m/m.go", "func Double(", "func Triple(", "./m/ ./q/", verdictNoBuild},
		{"panic-at-init", "m/m.go", "vow+el", "vow+el(", "./m/", verdictKilled},
		// go vet's assign check is not one go test runs.
		{"vet-go-test-does-not-run", "m/m.go", "return a * 2", "a = a; return a * 2", "./m/", verdictSurvived},
		{"file-in-no-named-binary", "m/m_test.go", "t.Fatal(\"extra\")", "t.Fatal(\"other\")", "./nt/ ./red/", verdictNotCompiled},
		{"package-with-no-tests", "nt/nt.go", "N = 1", "N = 2", "./nt/", verdictNotCompiled},
		// ntm compiles m and has no test binary, and red's test binary does not link m.
		{"compiled-into-no-test-binary", "m/m.go", "a * 2", "a * 4", "./ntm/ ./red/", verdictNotCompiled},
		{"data-no-test-ran", "data.txt", "one", "two", "./nt/", verdictNoTest},
		{"data-unread", "data.txt", "one", "two", "./m/", verdictSurvived},
		{"data-read", "data.txt", "one", "two", "./q/", verdictKilled},
		{"red", "red/red.go", "R = 1", "R = 2", "./red/", verdictRed},
		{"red-again", "red/red.go", "R = 1", "R = 3", "./red/", verdictRed},
		{"main-exits-printing-a-timeout", "pm/pm.go", "return false", "return true", "./pm/", verdictKilled},
	} {
		f.mutant(m.id, m.file, m.search, m.replace, m.pkgs)
		want[m.id] = m.verdict
	}

	r := f.run(f.args("2")...)

	r.wantCode(t, 0)
	rows := r.wantVerdicts(t, want)
	for id, tests := range map[string]string{
		"killed": "TestAdd", "panic-at-init": "", "data-read": "TestData", "red": "TestRed", "red-again": "TestRed",
		"main-exits-printing-a-timeout": "", "survived": "",
	} {
		if got := rows[id+"/asis"]["failing tests"]; got != tests {
			t.Errorf("mutant %s names the failing tests %q, want %q", id, got, tests)
		}
	}
	t.Run("each row names its worker and its whole seconds", func(t *testing.T) {
		t.Parallel()
		for key, row := range rows {
			if _, err := strconv.Atoi(row["seconds"]); err != nil || (row["worker"] != "w1" && row["worker"] != "w2") {
				t.Errorf("%s: seconds %q and worker %q", key, row["seconds"], row["worker"])
			}
		}
	})
	t.Run("a set with no pkgs file is the file's package and its importers", func(t *testing.T) {
		t.Parallel()
		if got := rows["survived/asis"]["packages"]; got != "./cmd/tool ./m ./ntm ./q ./tool.test" {
			t.Errorf("the computed set is %q, want ./cmd/tool ./m ./ntm ./q ./tool.test", got)
		}
		if got := rows["killed/asis"]["packages"]; got != "./m/" {
			t.Errorf("the named set is %q, want it as written, ./m/", got)
		}
	})
	t.Run("a kill's log holds what the failing package printed and no other package", func(t *testing.T) {
		t.Parallel()
		log := r.log(t, "data-read")
		for _, want := range []string{"verdict=KILLED", "pkgs=./q/\n", "=== example.com/fx/q failed\n", "--- FAIL: TestData", `data is "two\n"`} {
			if !strings.Contains(log, want) {
				t.Errorf("the log does not hold %q:\n%s", want, log)
			}
		}
		log = r.log(t, "killed")
		for _, want := range []string{"--- FAIL: TestAdd", "Add(1, 2) is wrong", "\n--- FAIL: TestGhost (0.00s)\n", "\nok  \tnested/pkg\t0.01s\n", "raw \uFFFD byte\n", "FAIL\texample.com/fx/m\t"} {
			if !strings.Contains(log, want) {
				t.Errorf("the log does not hold %q:\n%s", want, log)
			}
		}
		if strings.Contains(log, "TestExtra") || strings.Contains(log, "example.com/fx/q") {
			t.Errorf("the log holds a test or a package that passed:\n%s", log)
		}
		if log := r.log(t, "killed-beside-a-passing-package"); !strings.Contains(log, "=== example.com/fx/m failed\n") || strings.Contains(log, "example.com/fx/q") {
			t.Errorf("the log of a kill beside a passing package names the wrong packages:\n%s", log)
		}
	})
	t.Run("a red log holds what failed before the change", func(t *testing.T) {
		t.Parallel()
		log := r.log(t, "red-again")
		for _, want := range []string{"the packages do not pass before the change", "=== example.com/fx/red failed\n", "red before any change"} {
			if !strings.Contains(log, want) {
				t.Errorf("the log does not hold %q:\n%s", want, log)
			}
		}
	})
	t.Run("a log says why a mutant has no verdict", func(t *testing.T) {
		t.Parallel()
		for id, want := range map[string]string{
			"file-in-no-named-binary":     "no test binary of the named packages compiles m/m_test.go on this host",
			"production-build":            "the changed tree does not build",
			"test-build":                  "=== example.com/fx/m was not built\n",
			"go-test-vet":                 "=== example.com/fx/m was not built\n",
			"another-packages-test-build": "=== example.com/fx/q was not built\n",
			"data-no-test-ran":            "none ran a test",
			"nomatch":                     "the search matched 0 time(s) in m/m.go",
			"unchanged":                   "the search matched 1 time(s) in m/m.go",
		} {
			if log := r.log(t, id); !strings.Contains(log, want) {
				t.Errorf("mutant %s: the log does not hold %q:\n%s", id, want, log)
			}
		}
		// What the go command printed about a build, as an event or on stderr.
		for id, want := range map[string]string{
			"test-build":     "undefined: three",
			"missing-import": "finding module for package example.org/missing",
		} {
			if log := r.log(t, id); !strings.Contains(log, want) {
				t.Errorf("mutant %s: the log does not hold %q:\n%s", id, want, log)
			}
		}
	})
	t.Run("the run keeps its record and removes its trees and its cache", func(t *testing.T) {
		t.Parallel()
		entries, err := os.ReadDir(filepath.Join(r.out, "work"))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		if want := []string{"results.w1.tsv", "results.w2.tsv", "worker.1.out", "worker.2.out"}; !slices.Equal(names, want) {
			t.Errorf("work holds %q after the run, want %q", names, want)
		}
		out, err := os.ReadFile(filepath.Join(r.out, "work", "worker.1.out"))
		if err != nil || !strings.Contains(string(out), " asis ") {
			t.Errorf("worker 1's record is %q, %v; want a line for each mutant it judged", out, err)
		}
		for _, want := range []string{"mutants: 27, workers: 2\n", "\n  KILLED: 7\n", "\n  NOBUILD: 5\n", "\n  NOTCOMPILED: 7\n", "\n  RED: 2\n", "\n  run seconds, summed: "} {
			if !strings.Contains(r.stdout, want) {
				t.Errorf("stdout does not hold %q:\n%s", want, r.stdout)
			}
		}
	})
}

// classified returns a run over packages a, b and c after events and the
// runners' records, which are keyed by package name.
func classified(events []testjson.Event, records map[string]record, exit int) *testRun {
	run := &testRun{state: map[string]string{}, output: map[string][]printed{}, exit: exit}
	for _, name := range []string{"a", "b", "c"} {
		run.packages = append(run.packages, listed{ImportPath: name, Dir: name})
	}
	for _, e := range events {
		run.tally.Add(e)
	}
	run.classify(records)
	return run
}

func TestTestRun_ClassifiesAPackageByItsResultAndItsRecord(t *testing.T) {
	t.Parallel()
	pass := func(pkg string) []testjson.Event {
		return []testjson.Event{{Action: "run", Package: pkg, Test: "TestP"}, {Action: "pass", Package: pkg, Test: "TestP"}, {Action: "pass", Package: pkg}}
	}
	others := slices.Concat(pass("b"), pass("c"))
	ok := record{}
	for _, row := range []struct {
		name    string
		events  []testjson.Event // package a's
		record  *record          // package a's binary
		goFails bool             // go test exits 1 though a's binary exited 0
		state   string
		verdict string
	}{
		{name: "ran a test and passed", events: pass("a"), record: &ok, state: packagePassed, verdict: verdictSurvived},
		{name: "passed and ran no test", events: []testjson.Event{{Action: "pass", Package: "a"}}, record: &ok, state: packageNoTests, verdict: verdictSurvived},
		{name: "has no test files", events: []testjson.Event{{Action: "skip", Package: "a"}}, state: packageNoFiles, verdict: verdictSurvived},
		{name: "a test failed", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestF"}, {Action: "fail", Package: "a", Test: "TestF"}, {Action: "fail", Package: "a"}}, record: &record{Exit: 1}, state: packageFailed, verdict: verdictKilled},
		{name: "the binary ended in failure and no test reported", events: []testjson.Event{{Action: "fail", Package: "a"}}, record: &record{Exit: 2}, state: packageFailed, verdict: verdictKilled},
		{name: "stopped at the limit", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestH"}, {Action: "fail", Package: "a"}}, record: &record{Exit: 2, TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		{name: "stopped at the limit after a test reported a failure", events: []testjson.Event{{Action: "fail", Package: "a", Test: "TestF"}, {Action: "fail", Package: "a"}}, record: &record{Exit: 2, TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		{name: "stopped at the limit after a result event for the hung test", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestH"}, {Action: "pass", Package: "a", Test: "TestH"}, {Action: "fail", Package: "a"}}, record: &record{Exit: 2, TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		// A test that catches SIGQUIT can return, and its binary exit 0.
		{name: "stopped at the limit and then exited 0", events: pass("a"), record: &record{TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		// A test that exits 0 from its SIGQUIT handler leaves no package result.
		{name: "stopped at the limit and then exited 0 in the test", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestH"}, {Action: "pass", Package: "a", Test: "TestH"}}, record: &record{TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		// A test that exits 0 leaves no package result: go test passes it, and the
		// gate's summary fails it.
		{name: "exited 0 inside a test", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestX"}, {Action: "pass", Package: "a", Test: "TestX"}}, record: &ok, state: packageExited, verdict: verdictKilled},
		// go test fails a binary that exits 0 with a process still holding its
		// output: "Test I/O incomplete".
		{name: "exited 0 and left its output open", events: slices.Concat(pass("a")[:2], []testjson.Event{{Action: "fail", Package: "a"}}), record: &ok, goFails: true, state: packageExited, verdict: verdictKilled},
		{name: "stopped at the limit and then printed a skip", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestH"}, {Action: "skip", Package: "a"}}, record: &record{TimedOut: true}, state: packageTimedOut, verdict: verdictTimeout},
		{name: "not built", events: []testjson.Event{{Action: "fail", Package: "a", FailedBuild: "a [a.test]"}}, state: packageNotBuilt, verdict: verdictNoBuild},
		{name: "the build failure is not named", events: []testjson.Event{{Action: "fail", Package: "a"}}, state: packageNotStarted, verdict: verdictNoBuild},
		{name: "the binary did not start", events: []testjson.Event{{Action: "fail", Package: "a"}}, record: &record{StartError: "fork/exec a.test: no such file or directory"}, state: packageNotStarted, verdict: verdictNoBuild},
		{name: "reported nothing", state: packageNoResult, verdict: verdictNoBuild},
		{name: "a pass with no binary run", events: pass("a"), state: "", verdict: verdictError},
		{name: "a pass from a binary that failed", events: pass("a"), record: &record{Exit: 1}, state: "", verdict: verdictError},
		{name: "a printed failure left by a binary that exited 0 inside a test", events: []testjson.Event{{Action: "fail", Package: "a"}}, record: &ok, state: packageExited, verdict: verdictKilled},
		{name: "a printed skip from a binary that ran and exited 0", events: []testjson.Event{{Action: "skip", Package: "a"}}, record: &ok, state: packageNoTests, verdict: verdictSurvived},
		{name: "a printed skip from a binary that ran a test and exited 0", events: []testjson.Event{{Action: "run", Package: "a", Test: "TestP"}, {Action: "skip", Package: "a"}}, record: &ok, state: packagePassed, verdict: verdictSurvived},
	} {
		records := map[string]record{"b": ok, "c": ok}
		if row.record != nil {
			records["a"] = *row.record
		}
		// go test exits 1 when a package failed or reported nothing, and passes a
		// binary that ran and exited 0.
		exit := 0
		if row.events == nil || slices.ContainsFunc(row.events, func(e testjson.Event) bool { return e.Test == "" && e.Action == "fail" }) {
			exit = 1
		}
		if r := row.record; r != nil && r.StartError == "" && r.Exit == 0 && !row.goFails {
			exit = 0
		}
		run := classified(slices.Concat(row.events, others), records, exit)
		if got := run.state["a"]; got != row.state {
			t.Errorf("%s: the package %q, want %q", row.name, got, row.state)
		}
		if got := run.verdict(); got != row.verdict {
			t.Errorf("%s: verdict %s, want %s (fault %q)", row.name, got, row.verdict, run.fault)
		}
		if green := run.green(); green != (row.verdict == verdictSurvived) {
			t.Errorf("%s: green is %t", row.name, green)
		}
	}
}

func TestTestRun_GivesOneVerdictToARunOfSeveralPackages(t *testing.T) {
	t.Parallel()
	failed := func(pkg string) []testjson.Event {
		return []testjson.Event{{Action: "run", Package: pkg, Test: "TestF" + pkg}, {Action: "fail", Package: pkg, Test: "TestF" + pkg}, {Action: "fail", Package: pkg}}
	}
	hung := func(pkg string, tests ...string) []testjson.Event {
		events := []testjson.Event{}
		for _, test := range tests {
			events = append(events, testjson.Event{Action: "run", Package: pkg, Test: test})
		}
		return append(events, testjson.Event{Action: "fail", Package: pkg})
	}
	notBuilt := func(pkg string) []testjson.Event {
		return []testjson.Event{{Action: "fail", Package: pkg, FailedBuild: pkg}}
	}
	noTests := func(pkg string) []testjson.Event { return []testjson.Event{{Action: "pass", Package: pkg}} }
	stopped, bad, ok := record{Exit: 2, TimedOut: true}, record{Exit: 1}, record{}
	for _, row := range []struct {
		name    string
		events  []testjson.Event
		records map[string]record
		exit    int
		verdict string
		tests   []string // the failing-tests cell
	}{
		{name: "a failure beside a package stopped at the limit", events: slices.Concat(failed("a"), hung("b", "TestH"), noTests("c")), records: map[string]record{"a": bad, "b": stopped, "c": ok}, exit: 1, verdict: verdictKilled, tests: []string{"TestFa"}},
		{name: "a failure beside a package that was not built", events: slices.Concat(failed("a"), notBuilt("b"), noTests("c")), records: map[string]record{"a": bad, "c": ok}, exit: 1, verdict: verdictNoBuild},
		{name: "a stop beside a package that was not built", events: slices.Concat(hung("a", "TestH"), notBuilt("b"), noTests("c")), records: map[string]record{"a": stopped, "c": ok}, exit: 1, verdict: verdictNoBuild},
		{name: "two packages stopped, each test named once", events: slices.Concat(hung("a", "TestB", "TestA"), hung("b", "TestA"), noTests("c")), records: map[string]record{"a": stopped, "b": stopped, "c": ok}, exit: 1, verdict: verdictTimeout, tests: []string{"TestA", "TestB"}},
		// go test gives the pass of a binary that exits 0 inside a test to that test.
		{name: "a stopped binary that exited 0 inside its test", events: slices.Concat([]testjson.Event{{Action: "run", Package: "a", Test: "TestT"}, {Action: "pass", Package: "a", Test: "TestT"}}, noTests("b"), noTests("c")), records: map[string]record{"a": {TimedOut: true}, "b": ok, "c": ok}, verdict: verdictTimeout, tests: []string{"TestT"}},
		{name: "a binary that exited 0 inside its test", events: slices.Concat([]testjson.Event{{Action: "run", Package: "a", Test: "TestT"}, {Action: "pass", Package: "a", Test: "TestT"}}, noTests("b"), noTests("c")), records: map[string]record{"a": ok, "b": ok, "c": ok}, verdict: verdictKilled, tests: []string{"TestT"}},
		{name: "no package ran a test", events: slices.Concat(noTests("a"), noTests("b"), []testjson.Event{{Action: "skip", Package: "c"}}), records: map[string]record{"a": ok, "b": ok}, verdict: verdictNoTest},
		{name: "every package passed and go test failed", events: slices.Concat(noTests("a"), noTests("b"), noTests("c")), records: map[string]record{"a": ok, "b": ok, "c": ok}, exit: 1, verdict: verdictError},
		{name: "a package failed and go test passed", events: slices.Concat(failed("a"), noTests("b"), noTests("c")), records: map[string]record{"a": bad, "b": ok, "c": ok}, verdict: verdictError},
	} {
		run := classified(row.events, row.records, row.exit)
		if got := run.verdict(); got != row.verdict {
			t.Errorf("%s: verdict %s, want %s (fault %q)", row.name, got, row.verdict, run.fault)
		}
		if run.green() != (row.verdict == verdictNoTest) {
			t.Errorf("%s: green is %t", row.name, run.green())
		}
		var tests []string
		switch row.verdict {
		case verdictKilled:
			tests = run.failing()
		case verdictTimeout:
			tests = run.unfinished(packageTimedOut)
		}
		if !slices.Equal(tests, row.tests) {
			t.Errorf("%s: the tests named are %q, want %q", row.name, tests, row.tests)
		}
	}
	if run := (&testRun{}); run.verdict() != verdictNoTest {
		t.Error("a run that is not classified has a fault")
	}
	run := &testRun{}
	if run.classify(nil); run.verdict() != verdictError {
		t.Error("a run that named no package has a verdict")
	}
}

// A test a stopped binary exited inside keeps all its output in the log: its
// pass is the binary's, and no result line of its own ends what it printed.
func TestTestRun_ReportKeepsTheOutputOfATestTheBinaryExitedInside(t *testing.T) {
	t.Parallel()
	events := []testjson.Event{{Action: "run", Package: "a", Test: "TestT"}, {Action: "pass", Package: "a", Test: "TestT"}}
	run := classified(events, map[string]record{"a": {TimedOut: true}, "b": {}, "c": {}}, 0)
	run.output["a"] = []printed{{"TestT", "=== RUN   TestT\n"}, {"TestT", "waiting\n"}, {"TestT", "ok  \ta\t3.0s\n"}}
	var b strings.Builder
	run.report(&b)
	for _, want := range []string{"=== a was stopped at the limit\n", "waiting\n", "ok  \ta\t3.0s\n"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the report does not hold %q:\n%s", want, b.String())
		}
	}
}

// A failed package's log keeps a passed or skipped test's output only past its
// own result line, where a stopped binary's stacks arrive, keeps a failed
// test's output whole, and puts a fault first.
func TestTestRun_ReportKeepsWhatFollowsAFinishedTestsResultLine(t *testing.T) {
	t.Parallel()
	pass := func(pkg string) []testjson.Event {
		return []testjson.Event{{Action: "run", Package: pkg, Test: "TestP"}, {Action: "pass", Package: pkg, Test: "TestP"}, {Action: "pass", Package: pkg}}
	}
	events := slices.Concat([]testjson.Event{
		{Action: "run", Package: "a", Test: "TestP"},
		{Action: "pass", Package: "a", Test: "TestP"},
		{Action: "run", Package: "a", Test: "TestS"},
		{Action: "skip", Package: "a", Test: "TestS"},
		{Action: "run", Package: "a", Test: "TestF"},
		{Action: "fail", Package: "a", Test: "TestF"},
		{Action: "fail", Package: "a"},
	}, pass("b"), pass("c"))
	run := classified(events, map[string]record{"a": {Exit: 1}, "b": {}, "c": {}}, 1)
	run.output["a"] = []printed{
		{"TestP", "=== RUN   TestP\n"},
		{"TestP", "passed-own\n"},
		{"TestP", "--- PASS: TestP (0.00s)\n"},
		{"TestP", "stacks-after\n"},
		{"TestS", "skipped-own\n"},
		{"TestS", "--- SKIP: TestS (0.00s)\n"},
		{"TestF", "failed-own\n"},
		{"TestF", "--- FAIL: TestF (0.00s)\n"},
	}
	var b strings.Builder
	run.report(&b)
	for _, want := range []string{"=== a failed\n", "stacks-after\n", "failed-own\n", "--- FAIL: TestF"} {
		if !strings.Contains(b.String(), want) {
			t.Errorf("the report does not hold %q:\n%s", want, b.String())
		}
	}
	for _, unwanted := range []string{"passed-own", "skipped-own", "--- PASS", "--- SKIP", "=== b", "=== c"} {
		if strings.Contains(b.String(), unwanted) {
			t.Errorf("the report holds %q:\n%s", unwanted, b.String())
		}
	}
	run.fault = "the records disagree"
	b.Reset()
	run.report(&b)
	if !strings.HasPrefix(b.String(), "the run cannot be read: the records disagree\n") {
		t.Errorf("the report does not open on the fault:\n%s", b.String())
	}
}

// On a host with no folded spelling a mutant asked for under it gets a row and
// no run.
func TestWorker_RecordsNoFoldWhereTheHostHasNoFoldedSpelling(t *testing.T) {
	t.Parallel()
	w := &worker{tree: &tree{run: &run{asis: "/tmp/", folded: ""}}}
	if j := w.judge(t.Context(), mutant{id: "m01"}, "folded"); j.verdict != verdictNoFold || j.log.Len() != 0 {
		t.Errorf("the judgement is %q with the log %q, want NOFOLD and no log", j.verdict, j.log.String())
	}
}

// A test binary go list reports it cannot load, as one whose test file names
// no package, still compiles the file: the mutant is judged, and reads NOBUILD.
// The entries are go list's at go1.26.0 for such a test file.
func TestWorker_CompiledHoldsAFileATestBinaryThatFailsToLoadCompiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	w := &worker{tree: &tree{dir: dir}}
	pkg := filepath.Join(dir, "a")
	broken := &struct{ Err string }{Err: "expected 'IDENT', found 'EOF'"}
	entries := []listed{
		{ImportPath: "ex/a", Dir: pkg, Name: "a", GoFiles: []string{"a.go"}, TestGoFiles: []string{"a_test.go", "x_test.go"}, Error: broken},
		{ImportPath: "ex/a [ex/a.test]", Dir: pkg, Name: "a", GoFiles: []string{"a.go", "a_test.go", "x_test.go"}, Error: broken},
		{ImportPath: "ex/a.test", Name: "main", Deps: []string{"ex/a [ex/a.test]"}, Error: broken},
	}
	if compiled, known := w.compiled(entries, filepath.Join("a", "x_test.go")); !compiled || !known {
		t.Errorf("x_test.go: compiled %t, known %t; want both", compiled, known)
	}
	if compiled, known := w.compiled(entries, filepath.Join("a", "other.go")); compiled || known {
		t.Errorf("other.go: compiled %t, known %t; want neither", compiled, known)
	}
	for _, e := range []listed{{MFiles: []string{"f"}}, {FFiles: []string{"f"}}, {SwigFiles: []string{"f"}}, {SwigCXXFiles: []string{"f"}}, {InvalidGoFiles: []string{"f"}}} {
		e.ImportPath, e.Dir = "ex/b", filepath.Join(dir, "b")
		binary := listed{ImportPath: "ex/b.test", Name: "main", Deps: []string{"ex/b"}}
		if compiled, _ := w.compiled([]listed{e, binary}, filepath.Join("b", "f")); !compiled {
			t.Errorf("a file go list names in %+v is not held compiled", e)
		}
	}
}
