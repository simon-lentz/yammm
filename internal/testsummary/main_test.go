package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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

// timeoutEvents replays the events go test -json writes when a binary times out
// while TestHangs/r runs: the panic follows TestHangs/e's result line, so
// test2json gives it to TestHangs/e and reports that test's result after it.
func timeoutEvents(t *testing.T, result string) []string {
	t.Helper()
	return []string{
		ev(t, "output", "a", "", "-test.shuffle 7\n"),
		ev(t, "run", "a", "TestHangs", ""),
		ev(t, "output", "a", "TestHangs", "=== RUN   TestHangs\n"),
		ev(t, "run", "a", "TestHangs/e", ""),
		ev(t, "output", "a", "TestHangs/e", "=== RUN   TestHangs/e\n"),
		ev(t, "output", "a", "TestHangs/e", "=== PAUSE TestHangs/e\n"),
		ev(t, "pause", "a", "TestHangs/e", ""),
		ev(t, "run", "a", "TestHangs/r", ""),
		ev(t, "output", "a", "TestHangs/r", "=== RUN   TestHangs/r\n"),
		ev(t, "output", "a", "TestHangs/r", "=== PAUSE TestHangs/r\n"),
		ev(t, "pause", "a", "TestHangs/r", ""),
		ev(t, "cont", "a", "TestHangs/e", ""),
		ev(t, "output", "a", "TestHangs/e", "=== CONT  TestHangs/e\n"),
		ev(t, "cont", "a", "TestHangs/r", ""),
		ev(t, "output", "a", "TestHangs/r", "=== CONT  TestHangs/r\n"),
		ev(t, "output", "a", "TestHangs/e", "    e_test.go:4: "+result+" reason\n"),
		ev(t, "output", "a", "TestHangs/e", "--- "+strings.ToUpper(result)+": TestHangs/e (0.50s)\n"),
		ev(t, "output", "a", "TestHangs/e", "panic: test timed out after 3s\n"),
		ev(t, "output", "a", "TestHangs/e", "\trunning tests:\n"),
		ev(t, "output", "a", "TestHangs/e", "\t\tTestHangs/r (3s)\n"),
		ev(t, "output", "a", "TestHangs/e", "goroutine 24 [sleep]:\n"),
		ev(t, "output", "a", "TestHangs/e", "a.TestHangs.func1()\n"),
		ev(t, result, "a", "TestHangs/e", ""),
		ev(t, "output", "a", "", "FAIL\ta\t3.011s\n"),
		ev(t, "fail", "a", "", ""),
	}
}

func TestRun_PrintsTheLinesAfterAPassedTestsResultWhenItsPackageFails(t *testing.T) {
	code, stdout, _ := runSummary(t, timeoutEvents(t, "pass"), "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "panic: test timed out after 3s\n\trunning tests:\n\t\tTestHangs/r (3s)\ngoroutine 24 [sleep]:\na.TestHangs.func1()\n")
	assertContains(t, "stdout", stdout, "=== CONT  TestHangs/r\n")
	if strings.Contains(stdout, "--- PASS: TestHangs/e") || strings.Contains(stdout, "pass reason") {
		t.Errorf("a passed test's own lines printed:\n%s", stdout)
	}
	own, kept, unfinished := strings.Index(stdout, "FAIL\ta\t"), strings.Index(stdout, "panic: "), strings.Index(stdout, "=== CONT  TestHangs/r")
	if own < 0 || kept < 0 || unfinished < 0 || own > kept || kept > unfinished {
		t.Errorf("want the package's own lines, then the kept lines, then the unfinished test's lines:\n%s", stdout)
	}
}

func TestRun_KeepsASkipsReasonApartFromTheLinesAfterIt(t *testing.T) {
	code, stdout, _ := runSummary(t, timeoutEvents(t, "skip"), "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, "  a TestHangs/e: e_test.go:4: skip reason\n")
	assertContains(t, "stdout", stdout, "panic: test timed out after 3s\n\trunning tests:\n")
	if strings.Contains(stdout, "--- SKIP: TestHangs/e") || strings.Count(stdout, "skip reason") != 1 {
		t.Errorf("a skipped test's own lines printed beside its reason:\n%s", stdout)
	}
}

func TestRun_PrintsNoLineAfterAResultWhenItsPackagePasses(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestA", ""),
		ev(t, "output", "a", "TestA", "--- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "a goroutine's late line\n"),
		ev(t, "pass", "a", "TestA", ""),
		ev(t, "output", "a", "", "ok  \ta\t0.1s\n"),
		ev(t, "pass", "a", "", ""),
	}
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 0 {
		t.Errorf("exit %d, want 0", code)
	}
	if strings.Contains(stdout, "late line") {
		t.Errorf("a passing package printed a line after a result:\n%s", stdout)
	}
}

// A package whose binary died before it printed a line of its own is still
// judged unfinished, whether a kept line or an unfinished test's line is all it
// left, and those lines print.
func TestRun_PrintsAPackageThatEndedBeforeItsOwnOutput(t *testing.T) {
	passed := []string{
		ev(t, "run", "a", "TestA", ""),
		ev(t, "output", "a", "TestA", "--- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "fatal error: out of memory\n"),
		ev(t, "pass", "a", "TestA", ""),
	}
	running := []string{
		ev(t, "run", "a", "TestB", ""),
		ev(t, "output", "a", "TestB", "=== RUN   TestB\n"),
	}
	for _, tt := range []struct {
		name  string
		lines []string
		want  []string
	}{
		{"a kept line alone", passed, []string{"fatal error: out of memory\n"}},
		{"an unfinished test alone", running, []string{"=== RUN   TestB\n"}},
		{"both", append(slices.Clone(passed), running...), []string{"fatal error: out of memory\n", "=== RUN   TestB\n"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runSummary(t, tt.lines, "a")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			for _, want := range tt.want {
				assertContains(t, "stdout", stdout, want)
			}
			assertContains(t, "stderr", stderr, "1 of 1 packages reported no result:\n  a\n")
		})
	}
}

// A benchmark's lines stay pending, since it reports no result event of its
// own; its package's pass ends them, so they neither print nor make the
// package unfinished.
func TestRun_DropsAPassedPackagesLeftoverLines(t *testing.T) {
	lines := []string{
		ev(t, "output", "a", "BenchmarkX", "=== RUN   BenchmarkX\n"),
		ev(t, "output", "a", "BenchmarkX", "BenchmarkX-8   \t1000\t   12 ns/op\n"),
		ev(t, "output", "a", "", "ok  \ta\t0.1s\n"),
		ev(t, "pass", "a", "", ""),
	}
	code, stdout, stderr := runSummary(t, lines, "a")
	if code != 0 {
		t.Errorf("exit %d, want 0: %s", code, stderr)
	}
	if strings.Contains(stdout, "BenchmarkX") {
		t.Errorf("a passed package's leftover lines printed:\n%s", stdout)
	}
}

// The split is at the last line reporting this test's own result: a line
// naming another test, or an earlier copy of its own result line, is a kept
// line like any other.
func TestRun_SplitsAtTheTestsOwnLastResultLine(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestA", ""),
		ev(t, "output", "a", "TestA", "--- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "early stray\n"),
		ev(t, "output", "a", "TestA", "--- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "--- FAIL: TestOther (0.00s)\n"),
		ev(t, "output", "a", "TestA", "late stray\n"),
		ev(t, "pass", "a", "TestA", ""),
		ev(t, "fail", "a", "", ""),
	}
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "--- FAIL: TestOther (0.00s)\nlate stray\n")
	if strings.Contains(stdout, "early stray") {
		t.Errorf("the split fell before the test's last result line:\n%s", stdout)
	}
}

// go test -json cuts a line longer than its 1024-byte output buffer into
// several events; a long test name's result line arrives that way.
func TestRun_FindsAResultLineCutAcrossEvents(t *testing.T) {
	name := "TestLong/" + strings.Repeat("x", 1100)
	result := "--- PASS: " + name + " (1.50s)\n"
	lines := []string{
		ev(t, "run", "a", name, ""),
		ev(t, "output", "a", name, "=== RUN   "+name+"\n"),
		ev(t, "output", "a", name, result[:1024]),
		ev(t, "output", "a", name, result[1024:]),
		ev(t, "output", "a", name, "panic: test timed out after 4s\n"),
		ev(t, "pass", "a", name, ""),
		ev(t, "fail", "a", "", ""),
	}
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "panic: test timed out after 4s\n")
}

func TestRun_FailsWhenItCannotWriteAFile(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "no such directory", "out")
	for _, flag := range []string{"-durations=", "-race-skips="} {
		t.Run(flag, func(t *testing.T) {
			code, stdout, stderr := runSummary(t, passingTest(t, "a", "TestA"), flag+missing, "a")
			if code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			assertContains(t, "stderr", stderr, "testsummary: ")
			assertContains(t, "stdout", stdout, "test: 1 of 1 packages reported a result")
		})
	}
}

// The premise of the timeout rule, measured on the running toolchain: go test
// -json gives a timed-out binary's panic to a test that then passes, and the
// summary prints it with the test that was still running. The passing test's
// name is long enough that go test -json cuts its result line in two.
func TestRun_PrintsARealTimeoutsTrace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	files := map[string]string{
		"go.mod": "module hang\n",
		"hang_test.go": "package hang\n\nimport (\n\t\"strings\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
			"func TestHangs(t *testing.T) {\n" +
			"\tt.Run(strings.Repeat(\"e\", 1100), func(t *testing.T) {\n\t\tt.Parallel()\n\t\ttime.Sleep(500 * time.Millisecond)\n\t})\n" +
			"\tt.Run(\"r\", func(t *testing.T) {\n\t\tt.Parallel()\n\t\ttime.Sleep(time.Hour)\n\t})\n}\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.CommandContext(t.Context(), "go", "test", "-json", "-count=1", "-timeout=5s", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	events, err := cmd.Output()
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Fatalf("go test -json: %v, want a failing exit\n%s", err, events)
	}
	var out, errOut bytes.Buffer
	if code := run([]string{"hang"}, bytes.NewReader(events), &out, &errOut); code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", out.String(), "panic: test timed out after 5s\n\trunning tests:\n\t\tTestHangs/r (")
	if !bytes.Contains(events, []byte(`"Action":"pass","Package":"hang","Test":"TestHangs/`+strings.Repeat("e", 1100)+`"`)) {
		t.Errorf("the timed-out run reported no pass for the long-named subtest, so it no longer holds the case this pins:\n%s", events)
	}
}
