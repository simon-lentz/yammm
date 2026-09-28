package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func timed(t *testing.T, action, pkg, test string, seconds float64) string {
	t.Helper()
	b, err := json.Marshal(event{Action: action, Package: pkg, Test: test, Elapsed: &seconds})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(b)
}

func TestRun_WritesEveryTimedResult(t *testing.T) {
	lines := []string{
		ev(t, "run", "b", "TestSlow", ""),
		ev(t, "pause", "b", "TestSlow", ""),
		ev(t, "cont", "b", "TestSlow", ""),
		ev(t, "run", "b", "TestSlow/sub", ""),
		timed(t, "pass", "b", "TestSlow/sub", 2.5),
		timed(t, "pass", "b", "TestSlow", 2.75),
		ev(t, "run", "a", "TestSkip", ""),
		timed(t, "skip", "a", "TestSkip", 0),
		ev(t, "run", "a", "TestFail", ""),
		timed(t, "fail", "a", "TestFail", 0.125),
		timed(t, "fail", "a", "", 1.5),
		timed(t, "pass", "b", "", 3),
	}
	file := filepath.Join(t.TempDir(), "durations.tsv")
	if code, _, stderr := runSummary(t, lines, "-durations="+file, "a", "b"); code != 1 {
		t.Fatalf("exit %d, want 1: %s", code, stderr)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read durations: %v", err)
	}
	want := "package\ttest\tresult\tseconds\tscheduling\n" +
		"a\t\tfail\t1.5\tserial\n" +
		"a\tTestFail\tfail\t0.125\tserial\n" +
		"a\tTestSkip\tskip\t0\tserial\n" +
		"b\t\tpass\t3\tserial\n" +
		"b\tTestSlow\tpass\t2.75\tparallel\n" +
		"b\tTestSlow/sub\tpass\t2.5\tserial\n"
	if string(got) != want {
		t.Errorf("durations =\n%s\nwant\n%s", got, want)
	}
}

func TestRun_WritesNoDurationsForAnEmptyFileName(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if code, _, stderr := runSummary(t, passingTest(t, "a", "TestA"), "-durations=", "a"); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("an empty -durations wrote %v", entries)
	}
}

// A test run more than once keeps its runs' order, and a pause marks only the
// run it happened in.
func TestRun_WritesEachRunOfARepeatedTest(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestR", ""),
		ev(t, "pause", "a", "TestR", ""),
		timed(t, "pass", "a", "TestR", 2),
		ev(t, "run", "a", "TestR", ""),
		timed(t, "pass", "a", "TestR", 1),
		timed(t, "pass", "a", "", 3),
	}
	file := filepath.Join(t.TempDir(), "durations.tsv")
	if code, _, stderr := runSummary(t, lines, "-durations="+file, "a"); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read durations: %v", err)
	}
	want := "package\ttest\tresult\tseconds\tscheduling\n" +
		"a\t\tpass\t3\tserial\n" +
		"a\tTestR\tpass\t2\tparallel\n" +
		"a\tTestR\tpass\t1\tserial\n"
	if string(got) != want {
		t.Errorf("durations =\n%s\nwant\n%s", got, want)
	}
}
