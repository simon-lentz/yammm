package main

import (
	"strings"
	"testing"
)

// nearTheLimit reports, against a 10m -timeout, b and e past 70% of it and
// alike, e first, f half a second past 70%, a exactly at 70%, c just past the
// limit, d far under it, and a test of a that ran past 70% alone.
func nearTheLimit(t *testing.T, cResult string) []string {
	t.Helper()
	return []string{
		ev(t, "run", "a", "TestLong", ""),
		timed(t, "pass", "a", "TestLong", 500),
		timed(t, "pass", "a", "", 420),
		timed(t, "pass", "e", "", 540.5),
		timed(t, "pass", "b", "", 540.5),
		timed(t, cResult, "c", "", 600.5),
		timed(t, "pass", "d", "", 12),
		timed(t, "pass", "f", "", 420.5),
	}
}

func TestRun_NamesEveryPackageOverSeventyPercentOfTheTimeout(t *testing.T) {
	code, stdout, stderr := runSummary(t, nearTheLimit(t, "pass"), "-timeout=10m", "a", "b", "c", "d", "e", "f")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	const list = "test: 4 package(s) took over 70% of the 10m0s -timeout:\n" +
		"   600.5s  c\n" +
		"   540.5s  b\n" +
		"   540.5s  e\n" +
		"   420.5s  f\n" +
		"test: 6 of 6 packages reported a result, and none failed\n"
	assertContains(t, "stdout", stdout, list)
	if strings.Contains(stdout, "::warning") {
		t.Errorf("a run that asked for no annotation printed one:\n%s", stdout)
	}
}

// The list is a warning: it changes no exit status, and a failed run prints
// it too.
func TestRun_NamesThePackagesOverTheBudgetOfAFailedRun(t *testing.T) {
	code, stdout, _ := runSummary(t, nearTheLimit(t, "fail"), "-timeout=10m", "a", "b", "c", "d", "e", "f")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "test: 4 package(s) took over 70% of the 10m0s -timeout:\n   600.5s  c\n   540.5s  b\n   540.5s  e\n   420.5s  f\n")
}

func TestRun_AnnotatesEachPackageOverTheBudget(t *testing.T) {
	code, stdout, stderr := runSummary(t, nearTheLimit(t, "pass"), "-timeout=10m", "-annotate", "a", "b", "c", "d", "e", "f")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	for _, want := range []string{
		"::warning title=Test budget::c took 600.5s, over 70% of the 10m0s -timeout\n",
		"::warning title=Test budget::b took 540.5s, over 70% of the 10m0s -timeout\n" +
			"::warning title=Test budget::e took 540.5s, over 70% of the 10m0s -timeout\n" +
			"::warning title=Test budget::f took 420.5s, over 70% of the 10m0s -timeout\n",
	} {
		assertContains(t, "stdout", stdout, want)
	}
	if n := strings.Count(stdout, "::warning"); n != 4 {
		t.Errorf("%d annotations, want 4:\n%s", n, stdout)
	}
}

func TestRun_NamesNoPackageUnderTheBudget(t *testing.T) {
	for _, args := range [][]string{nil, {"-timeout=0s"}, {"-timeout=20m", "-annotate"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runSummary(t, nearTheLimit(t, "pass"), append(args, "a", "b", "c", "d", "e", "f")...)
			if code != 0 {
				t.Fatalf("exit %d, want 0: %s", code, stderr)
			}
			if strings.Contains(stdout, "-timeout") || strings.Contains(stdout, "::warning") {
				t.Errorf("a run with no package over its budget named one:\n%s", stdout)
			}
		})
	}
}

// The refusal comes before any event is read, as -slowest's does.
func TestRun_RefusesANegativeTimeout(t *testing.T) {
	lines := append([]string{"go: cannot find main module"}, nearTheLimit(t, "pass")...)
	code, stdout, stderr := runSummary(t, lines, "-timeout=-1m", "a", "b", "c", "d", "e", "f")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	assertContains(t, "stderr", stderr, "testsummary: -timeout takes a duration of zero or more, got -1m0s\n")
	if stdout != "" {
		t.Errorf("a refused run printed a report:\n%s", stdout)
	}
}
