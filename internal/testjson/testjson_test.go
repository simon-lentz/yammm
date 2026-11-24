package testjson

import (
	"slices"
	"strings"
	"testing"
)

func TestRead_GivesEventsAndPlainLinesInOrder(t *testing.T) {
	t.Parallel()
	// Longer than the scanner's first buffer of 1 MiB.
	long := strings.Repeat("x", 2<<20)
	stream := `{"Action":"start","Package":"a"}` + "\n" +
		"go: a setup error\n" +
		`{"Action":"build-fail","ImportPath":"a [a.test]"}` + "\n" +
		`{"Package":"a"}` + "\n" +
		long + "\n" +
		`{"Action":"fail","Package":"a","Elapsed":0.25,"FailedBuild":"a [a.test]"}` + "\n"
	var got []string
	err := Read(strings.NewReader(stream), func(e Event) {
		line := e.Action + " " + e.Package + e.ImportPath
		if e.Elapsed != nil && *e.Elapsed == 0.25 {
			line += " timed " + e.FailedBuild
		}
		got = append(got, line)
	}, func(line string) {
		if line == long {
			line = "the long line"
		}
		got = append(got, "plain "+line)
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"start a", "plain go: a setup error", "build-fail a [a.test]", `plain {"Package":"a"}`, "plain the long line", "fail a timed a [a.test]"}
	if !slices.Equal(got, want) {
		t.Errorf("read %q, want %q", got, want)
	}
}

func TestRead_ReportsALineItCannotHold(t *testing.T) {
	t.Parallel()
	err := Read(strings.NewReader(strings.Repeat("x", maxLine+1)), func(Event) {}, func(string) {})
	if err == nil || !strings.Contains(err.Error(), "reading events") {
		t.Errorf("Read returned %v, want an error for a line past the limit", err)
	}
}

func TestTally_HoldsEachPackagesAndEachTestsLastReport(t *testing.T) {
	t.Parallel()
	var tally Tally
	if tally.Result("a") != "" || tally.Started("a") != 0 || tally.Passed("a", "TestA") || tally.Tests("a", "") != nil {
		t.Error("an empty tally reports something")
	}
	for _, e := range []Event{
		{Action: "start", Package: "a"},
		{Action: "run", Package: "a", Test: "TestA"},
		{Action: "run", Package: "a", Test: "TestA/sub"},
		{Action: "output", Package: "a", Test: "TestA/sub", Output: "x"},
		{Action: "pass", Package: "a", Test: "TestA/sub"},
		{Action: "fail", Package: "a", Test: "TestA"},
		{Action: "run", Package: "a", Test: "TestB"},
		{Action: "run", Package: "a", Test: "TestC"},
		{Action: "skip", Package: "a", Test: "TestC"},
		{Action: "run", Package: "a"},
		{Action: "fail", Package: "a"},
		{Action: "fail", Package: "b", FailedBuild: "b [b.test]"},
		{Action: "skip", Package: "c"},
		{Action: "run", Package: "d", Test: "TestD"},
		{Action: "pass", Package: "d", Test: "TestD"},
		{Action: "run", Package: "d", Test: "TestD"},
		{Action: "run", Package: "e", Test: "TestZ"},
		{Action: "run", Package: "e", Test: "TestY"},
		{Action: "run", Package: "e", Test: "TestX"},
		{Action: "run", Package: "e", Test: "TestW"},
		{Action: "run", Package: "e", Test: "TestV"},
		{Action: "run", Package: "e", Test: "TestU"},
	} {
		tally.Add(e)
	}
	for pkg, want := range map[string]string{"a": "fail", "b": "fail", "c": "skip", "d": ""} {
		if got := tally.Result(pkg); got != want {
			t.Errorf("Result(%s) = %q, want %q", pkg, got, want)
		}
	}
	if a, b := tally.FailedBuild("a"), tally.FailedBuild("b"); a != "" || b != "b [b.test]" {
		t.Errorf("FailedBuild = %q and %q, want none and b's", a, b)
	}
	if a, b := tally.Started("a"), tally.Started("b"); a != 3 || b != 0 {
		t.Errorf("Started = %d and %d, want 3 top-level tests and none", a, b)
	}
	if !tally.Passed("a", "TestA/sub") || tally.Passed("a", "TestA") || tally.Passed("a", "TestB") || tally.Passed("d", "TestD") {
		t.Error("Passed is wrong for a passed subtest, a failed test, an unfinished test or a test that started again")
	}
	if got, want := tally.Tests("e", ""), []string{"TestU", "TestV", "TestW", "TestX", "TestY", "TestZ"}; !slices.Equal(got, want) {
		t.Errorf("Tests(e, \"\") = %q, want them sorted, %q", got, want)
	}
	for result, want := range map[string][]string{"fail": {"TestA"}, "pass": {"TestA/sub"}, "skip": {"TestC"}, "": {"TestB"}} {
		if got := tally.Tests("a", result); !slices.Equal(got, want) {
			t.Errorf("Tests(a, %q) = %q, want %q", result, got, want)
		}
	}
}

func TestSplitAtResult(t *testing.T) {
	t.Parallel()
	long := "--- PASS: TestLong/" + strings.Repeat("x", 1100) + " (1.50s)\n"
	for _, row := range []struct {
		name                  string
		outputs               []string
		test                  string
		before, result, after int // how many events each part holds
	}{
		{name: "no output", test: "TestA"},
		{name: "no result line", outputs: []string{"=== RUN   TestA\n", "log\n"}, test: "TestA", before: 2},
		{name: "the line alone", outputs: []string{"--- PASS: TestA (0.00s)\n"}, test: "TestA", result: 1},
		{name: "a fail and what follows", outputs: []string{"log\n", "--- FAIL: TestA (0.00s)\n", "panic\n", "stack\n"}, test: "TestA", before: 1, result: 1, after: 2},
		{name: "a skip", outputs: []string{"    x_test.go:3: why\n", "--- SKIP: TestA (0.00s)\n"}, test: "TestA", before: 1, result: 1},
		{name: "after an open line", outputs: []string{"open", "--- PASS: TestA (0.30s)\n", "panic\n"}, test: "TestA", before: 1, result: 1, after: 1},
		{name: "a subtest's, indented once", outputs: []string{"    --- PASS: TestA/sub (0.20s)\n", "panic\n"}, test: "TestA/sub", result: 1, after: 1},
		{name: "indented twice", outputs: []string{"        --- PASS: TestA/b/c (0.20s)\n", "panic\n"}, test: "TestA/b/c", result: 1, after: 1},
		{name: "three spaces are no indent", outputs: []string{"   --- PASS: TestA (0.20s)\n", "panic\n"}, test: "TestA", before: 2},
		{name: "a line with a carriage return", outputs: []string{"--- PASS: TestA (0.00s)\r\n", "panic\n"}, test: "TestA", result: 1, after: 1},
		// A benchmark's skip line holds no duration.
		{name: "a line with no duration", outputs: []string{"why\n", "--- SKIP: BenchmarkX\n", "panic\n"}, test: "BenchmarkX", before: 1, result: 1, after: 1},
		{name: "a line with no duration and a carriage return", outputs: []string{"--- SKIP: BenchmarkX\r\n", "panic\n"}, test: "BenchmarkX", result: 1, after: 1},
		{name: "another test's line", outputs: []string{"--- PASS: TestAB (0.00s)\n", "panic\n"}, test: "TestA", before: 2},
		{name: "a longer name that begins alike", outputs: []string{"--- PASS: TestA (0.00s)\n"}, test: "TestA (0.00s)", before: 1},
		{name: "the last of two", outputs: []string{"--- PASS: TestA (0.00s)\n", "stray\n", "--- PASS: TestA (0.00s)\n", "late\n"}, test: "TestA", before: 2, result: 1, after: 1},
		{name: "the text inside an event", outputs: []string{"--- PASS: TestA (0.00s)\n", "kept\n", "note --- PASS: TestA (0.00s)\n"}, test: "TestA", result: 1, after: 2},
		{name: "a bench line", outputs: []string{"--- BENCH: TestA\n"}, test: "TestA", before: 1},
		{name: "cut across events", outputs: []string{"=== RUN\n", long[:1024], long[1024:], "panic\n"}, test: "TestLong/" + strings.Repeat("x", 1100), before: 1, result: 2, after: 1},
		{name: "cut and never closed", outputs: []string{long[:1024], long[1024 : len(long)-1]}, test: "TestLong/" + strings.Repeat("x", 1100), result: 2},
		{name: "a cut line naming another test", outputs: []string{"--- PASS: TestA (0.00s)\n", long[:1024], long[1024:]}, test: "TestA", result: 1, after: 2},
	} {
		before, result, after := SplitAtResult(row.outputs, row.test)
		if len(before) != row.before || len(result) != row.result || len(after) != row.after {
			t.Errorf("%s: %d events before, %d of the line and %d after; want %d, %d and %d", row.name, len(before), len(result), len(after), row.before, row.result, row.after)
		}
		if joined := slices.Concat(before, result, after); !slices.Equal(joined, row.outputs) {
			t.Errorf("%s: the parts hold %q, want every event in order", row.name, joined)
		}
	}
}
