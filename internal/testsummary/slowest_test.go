package main

import (
	"strings"
	"testing"
)

// fourPackages reports b and d at the same seconds, a below them and c last. A
// test of c ran longer than every package, and a test's seconds are no
// package's.
func fourPackages(t *testing.T) []string {
	t.Helper()
	return []string{
		ev(t, "run", "c", "TestLong", ""),
		timed(t, "pass", "c", "TestLong", 99),
		timed(t, "pass", "d", "", 12.5),
		timed(t, "pass", "a", "", 1.5),
		timed(t, "pass", "c", "", 0.04),
		timed(t, "pass", "b", "", 12.5),
	}
}

func TestRun_PrintsTheSlowestPackagesMostFirst(t *testing.T) {
	code, stdout, stderr := runSummary(t, fourPackages(t), "-slowest=3", "a", "b", "c", "d")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	const list = "test: the 3 slowest of 4 packages:\n" +
		"    12.5s  b\n" +
		"    12.5s  d\n" +
		"     1.5s  a\n" +
		"test: 4 of 4 packages reported a result, and none failed\n"
	assertContains(t, "stdout", stdout, list)
	if strings.Contains(stdout, "99") || strings.Contains(stdout, "s  c\n") {
		t.Errorf("the list holds a test's seconds or a fourth package:\n%s", stdout)
	}
}

func TestRun_PrintsNoSlowestPackagesUnlessAsked(t *testing.T) {
	for _, args := range [][]string{nil, {"-slowest=0"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			code, stdout, stderr := runSummary(t, fourPackages(t), append(args, "a", "b", "c", "d")...)
			if code != 0 {
				t.Fatalf("exit %d, want 0: %s", code, stderr)
			}
			if strings.Contains(stdout, "slowest") {
				t.Errorf("a run that asked for no list printed one:\n%s", stdout)
			}
		})
	}
}

func TestRun_PrintsEveryPackageWhenFewerRanThanAsked(t *testing.T) {
	lines := []string{timed(t, "pass", "a", "", 0.25), timed(t, "skip", "b", "", 0)}
	code, stdout, stderr := runSummary(t, lines, "-slowest=5", "a", "b")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	assertContains(t, "stdout", stdout, "test: the 2 slowest of 2 packages:\n     0.2s  a\n     0.0s  b\n")
}

// The report prints the list whichever of its three findings fails the run: a
// package that ran out of time is the one its reader looks for.
func TestRun_PrintsTheSlowestPackagesOfAFailedRun(t *testing.T) {
	const list = "test: the 1 slowest of 2 packages:\n   600.5s  b\n"
	for _, tt := range []struct {
		name   string
		b      string
		args   []string
		stderr string
	}{
		{"a package failed", "fail", []string{"a", "b"}, "test: 1 of 2 packages failed:\n  b\n"},
		{"a package reported no result", "pass", []string{"a", "b", "c"}, "test: 1 of 3 packages reported no result:\n  c\n"},
		{"a required test did not pass", "pass", []string{"-require=TestA", "a", "b"}, "test: TestA did not run and pass\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			lines := []string{
				ev(t, "run", "a", "TestA", ""),
				ev(t, "skip", "a", "TestA", ""),
				timed(t, "pass", "a", "", 2),
				timed(t, tt.b, "b", "", 600.5),
			}
			code, stdout, stderr := runSummary(t, lines, append([]string{"-slowest=1"}, tt.args...)...)
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			assertContains(t, "stdout", stdout, list)
			if stderr != tt.stderr {
				t.Errorf("stderr = %q, want this finding alone: %q", stderr, tt.stderr)
			}
		})
	}
}

// The order is by the seconds go test reported, which the list shows to a
// tenth: two packages can print alike and one still precede the other.
func TestRun_OrdersTheSlowestPackagesByTheirReportedSeconds(t *testing.T) {
	lines := []string{
		timed(t, "pass", "a", "", 1.21),
		timed(t, "pass", "b", "", 1.29),
		timed(t, "pass", "c", "", 12.46),
		timed(t, "pass", "d", "", 12.54),
	}
	code, stdout, stderr := runSummary(t, lines, "-slowest=4", "a", "b", "c", "d")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	assertContains(t, "stdout", stdout, "    12.5s  d\n    12.5s  c\n     1.3s  b\n     1.2s  a\n")
}

func TestRun_PrintsTheSlowestPackagesAfterTheSkippedTests(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestS", ""),
		ev(t, "output", "a", "TestS", "    s_test.go:9: needs a symlink\n"),
		ev(t, "skip", "a", "TestS", ""),
		timed(t, "pass", "a", "", 0.5),
	}
	code, stdout, stderr := runSummary(t, lines, "-slowest=1", "a")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	assertContains(t, "stdout", stdout, "test: 1 test(s) skipped:\n  a TestS: s_test.go:9: needs a symlink\n"+
		"test: the 1 slowest of 1 packages:\n     0.5s  a\n"+
		"test: 1 of 1 packages reported a result, and none failed\n")
}

func TestRun_PrintsNoSlowestPackagesWhenNoneReportedSeconds(t *testing.T) {
	code, stdout, stderr := runSummary(t, passingTest(t, "a", "TestA"), "-slowest=3", "a")
	if code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	if strings.Contains(stdout, "slowest") {
		t.Errorf("a run with no timed package printed a list:\n%s", stdout)
	}
}

// The refusal comes before any event is read: a plain-text line would
// otherwise pass through to stdout.
func TestRun_RefusesANegativeSlowestCount(t *testing.T) {
	lines := append([]string{"go: cannot find main module"}, fourPackages(t)...)
	code, stdout, stderr := runSummary(t, lines, "-slowest=-1", "a", "b", "c", "d")
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	assertContains(t, "stderr", stderr, "testsummary: -slowest takes a count of zero or more, got -1\n")
	if stdout != "" {
		t.Errorf("a refused run printed a report:\n%s", stdout)
	}
}
