package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
)

func ev(t *testing.T, action, pkg, test, output string) string {
	t.Helper()
	b, err := json.Marshal(event{Action: action, Package: pkg, Test: test, Output: output})
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	return string(b)
}

func runSummary(t *testing.T, lines []string, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = run(args, strings.NewReader(strings.Join(lines, "\n")+"\n"), &out, &errOut)
	return code, out.String(), errOut.String()
}

func passingTest(t *testing.T, pkg, test string) []string {
	t.Helper()
	return []string{
		ev(t, "run", pkg, test, ""),
		ev(t, "pass", pkg, test, ""),
		ev(t, "output", pkg, "", "ok  \t"+pkg+"\t0.1s\n"),
		ev(t, "pass", pkg, "", ""),
	}
}

func assertContains(t *testing.T, stream, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Errorf("%s does not hold %q:\n%s", stream, want, got)
	}
}

func TestRun_NamesAPackageThatRanNoTest(t *testing.T) {
	lines := passingTest(t, "a", "TestA")
	lines = append(lines,
		ev(t, "output", "b", "", "?   \tb\t[no test files]\n"),
		ev(t, "skip", "b", "", ""),
	)
	code, stdout, _ := runSummary(t, lines, "a", "b")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	assertContains(t, "stdout", stdout, "test: 1 package(s) ran no test:\n  b\n")
	assertContains(t, "stdout", stdout, "?   \tb\t[no test files]\n")
	assertContains(t, "stdout", stdout, "2 of 2 packages reported a result, and none failed")
}

func TestRun_PrintsEverySkipWithItsReason(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestS", ""),
		ev(t, "output", "a", "TestS", "=== RUN   TestS\n"),
		ev(t, "output", "a", "TestS", "    s_test.go:9: needs a symlink\n"),
		ev(t, "output", "a", "TestS", "--- SKIP: TestS (0.00s)\n"),
		ev(t, "skip", "a", "TestS", ""),
		ev(t, "pass", "a", "", ""),
	}
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	assertContains(t, "stdout", stdout, "test: 1 test(s) skipped:\n  a TestS: s_test.go:9: needs a symlink\n")
}

func TestRun_FailsWhenAPackageReportsNoResult(t *testing.T) {
	code, _, stderr := runSummary(t, passingTest(t, "b", "TestB"), "a", "b")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stderr", stderr, "1 of 2 packages reported no result:\n  a\n")
}

func TestRun_PrintsAFailedTestsOutput(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestF", ""),
		ev(t, "output", "a", "TestF", "    f_test.go:3: boom\n"),
		ev(t, "fail", "a", "TestF", ""),
		ev(t, "fail", "a", "", ""),
	}
	code, stdout, stderr := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "f_test.go:3: boom")
	if n := strings.Count(stdout, "f_test.go:3: boom"); n != 1 {
		t.Errorf("the failed test's output printed %d times, want once:\n%s", n, stdout)
	}
	if strings.Contains(stdout, "none failed") {
		t.Errorf("a failed run printed the success line:\n%s", stdout)
	}
	assertContains(t, "stderr", stderr, "1 of 1 packages failed:\n  a\n")
}

func TestRun_PrintsAnUnfinishedTestsOutputWhenItsPackageFails(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestP", ""),
		ev(t, "output", "a", "TestP", "panic: nil map\n"),
		ev(t, "fail", "a", "", ""),
	}
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "panic: nil map")
}

func TestRun_PrintsAPackagesOwnOutputOnlyWhenItFails(t *testing.T) {
	lines := []string{
		ev(t, "output", "a", "", "-test.shuffle 7\n"),
		ev(t, "output", "a", "", "PASS\n"),
		ev(t, "output", "a", "", "ok  \ta\t0.1s\n"),
		ev(t, "pass", "a", "", ""),
		ev(t, "output", "b", "", "-test.shuffle 9\n"),
		ev(t, "output", "b", "", "FAIL\tb\t0.1s\n"),
		ev(t, "fail", "b", "", ""),
		ev(t, "output", "c", "", "-test.shuffle 11\n"),
	}
	_, stdout, _ := runSummary(t, lines, "a", "b", "c")
	if strings.Contains(stdout, "-test.shuffle 7") || strings.Contains(stdout, "PASS\n") {
		t.Errorf("a passing package printed more than its result line:\n%s", stdout)
	}
	assertContains(t, "stdout", stdout, "ok  \ta\t0.1s\n")
	assertContains(t, "stdout", stdout, "-test.shuffle 9\nFAIL\tb\t0.1s\n")
	assertContains(t, "stdout", stdout, "-test.shuffle 11\n")
}

func TestRun_PassesPlainTextThrough(t *testing.T) {
	lines := append([]string{"go: cannot find main module"}, passingTest(t, "a", "TestA")...)
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "go: cannot find main module\n")
}

func TestRun_WritesRaceSkipsByPackage(t *testing.T) {
	skip := func(pkg, test string) []string {
		return []string{
			ev(t, "run", pkg, test, ""),
			ev(t, "output", pkg, test, "    r_test.go:5: "+raceskip.Reason+"\n"),
			ev(t, "skip", pkg, test, ""),
		}
	}
	var lines []string
	lines = append(lines, skip("a", "TestR")...)
	lines = append(lines, skip("a", "TestQ/sub")...)
	lines = append(lines, skip("a", "TestQ/other")...)
	lines = append(lines, skip("b", "TestOther")...)
	lines = append(lines,
		ev(t, "run", "b", "TestPlainSkip", ""),
		ev(t, "output", "b", "TestPlainSkip", "    p_test.go:2: not on this host\n"),
		ev(t, "skip", "b", "TestPlainSkip", ""),
		ev(t, "pass", "a", "", ""),
		ev(t, "pass", "b", "", ""),
	)
	file := filepath.Join(t.TempDir(), "race-skips")
	if code, _, stderr := runSummary(t, lines, "-race-skips="+file, "a", "b"); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read race skips: %v", err)
	}
	if want := "a\tTestQ,TestR\nb\tTestOther\n"; string(got) != want {
		t.Errorf("race skips = %q, want %q", got, want)
	}
}

func TestRun_RequireFailsUnlessTheTestPassed(t *testing.T) {
	skipped := []string{
		ev(t, "run", "a", "TestR", ""),
		ev(t, "skip", "a", "TestR", ""),
		ev(t, "pass", "a", "", ""),
	}
	code, _, stderr := runSummary(t, skipped, "-require=TestR", "a")
	if code != 1 {
		t.Errorf("a skipped required test: exit %d, want 1", code)
	}
	assertContains(t, "stderr", stderr, "test: TestR did not run and pass")

	if code, _, stderr := runSummary(t, passingTest(t, "a", "TestR"), "-require=TestR", "a"); code != 0 {
		t.Errorf("a passed required test: exit %d, want 0: %s", code, stderr)
	}
}

func TestRun_RequireFailsWhenTheTestRanAndFailed(t *testing.T) {
	failed := []string{
		ev(t, "run", "a", "TestR", ""),
		ev(t, "fail", "a", "TestR", ""),
		ev(t, "fail", "a", "", ""),
	}
	code, _, stderr := runSummary(t, failed, "-require=TestR", "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stderr", stderr, "test: TestR did not run and pass")
}

func TestRun_NamesASkipThatGaveNoReason(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestS", ""),
		ev(t, "output", "a", "TestS", "=== RUN   TestS\n"),
		ev(t, "output", "a", "TestS", "--- SKIP: TestS (0.00s)\n"),
		ev(t, "skip", "a", "TestS", ""),
		ev(t, "pass", "a", "", ""),
	}
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "  a TestS: no reason given\n")
}

func TestRun_RefusesARunNamingNoPackage(t *testing.T) {
	code, _, stderr := runSummary(t, passingTest(t, "a", "TestA"))
	if code != 2 {
		t.Errorf("exit %d, want 2", code)
	}
	assertContains(t, "stderr", stderr, "testsummary: name the packages the run was asked for")
}

func TestRun_PrintsBuildOutput(t *testing.T) {
	lines := []string{
		ev(t, "build-output", "", "", "# a\n./a.go:3:2: undefined: x\n"),
		ev(t, "output", "a", "", "FAIL\ta [build failed]\n"),
		ev(t, "fail", "a", "", ""),
	}
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "./a.go:3:2: undefined: x\n")
}

func TestRun_ListsSkipsInOrder(t *testing.T) {
	skip := func(pkg, test, why string) []string {
		return []string{
			ev(t, "run", pkg, test, ""),
			ev(t, "output", pkg, test, "    s_test.go:1: "+why+"\n"),
			ev(t, "skip", pkg, test, ""),
		}
	}
	lines := append(skip("b", "TestT", "later"), skip("a", "TestS", "earlier")...)
	lines = append(lines, ev(t, "pass", "a", "", ""), ev(t, "pass", "b", "", ""))
	_, stdout, _ := runSummary(t, lines, "a", "b")
	assertContains(t, "stdout", stdout, "  a TestS: s_test.go:1: earlier\n  b TestT: s_test.go:1: later\n")
}

func TestRun_PassesAJSONLineWithNoActionThrough(t *testing.T) {
	lines := append([]string{`{"Output":"stray"}`}, passingTest(t, "a", "TestA")...)
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, `{"Output":"stray"}`+"\n")
}

// A go command error can arrive as one plain-text line far longer than a
// scanner's default 64 KB token.
func TestRun_PassesALongPlainTextLineThrough(t *testing.T) {
	long := "go: " + strings.Repeat("x", 200_000)
	code, stdout, stderr := runSummary(t, append([]string{long}, passingTest(t, "a", "TestA")...), "a")
	if code != 0 {
		t.Errorf("exit %d, want 0: %s", code, stderr)
	}
	assertContains(t, "stdout", stdout, long+"\n")
}
