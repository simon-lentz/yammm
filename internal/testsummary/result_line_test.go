package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
)

// hangEvents are the events of a test binary that timed out after test, a
// parallel test, finished with result. before are the output events go test
// wrote for test just before its result line, and resultLine is that line as
// go test wrote it.
func hangEvents(t *testing.T, test, result, resultLine string, before ...string) []string {
	t.Helper()
	lines := []string{
		ev(t, "run", "a", test, ""),
		ev(t, "output", "a", test, "=== RUN   "+test+"\n"),
		ev(t, "run", "a", "TestSlow", ""),
		ev(t, "output", "a", "TestSlow", "=== RUN   TestSlow\n"),
		ev(t, "output", "a", test, "    x_test.go:3: no input on this host\n"),
	}
	for _, out := range before {
		lines = append(lines, ev(t, "output", "a", test, out))
	}
	return append(lines,
		ev(t, "output", "a", test, resultLine),
		ev(t, "output", "a", test, "panic: test timed out after 3s\n"),
		ev(t, "output", "a", test, "\trunning tests:\n"),
		ev(t, "output", "a", test, "\t\tTestSlow (3s)\n"),
		ev(t, "output", "a", test, "goroutine 9 [running]:\n"),
		ev(t, result, "a", test, ""),
		ev(t, "output", "a", "", "FAIL\ta\t3.009s\n"),
		ev(t, "fail", "a", "", ""),
	)
}

const hangTrace = "panic: test timed out after 3s\n\trunning tests:\n\t\tTestSlow (3s)\ngoroutine 9 [running]:\n"

// Without -v, go test -json writes a write that ended without a newline as an
// event of its own, and the result line that follows starts the next event.
func TestRun_PrintsTheTraceAfterAResultLineThatFollowsAnOpenLine(t *testing.T) {
	lines := hangEvents(t, "TestQuick", "pass", "--- PASS: TestQuick (0.30s)\n", "unterminated")
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, hangTrace)
	if strings.Contains(stdout, "unterminated") || strings.Contains(stdout, "--- PASS: TestQuick") {
		t.Errorf("a passed test's own output printed:\n%s", stdout)
	}
}

func TestRun_KeepsASkipsReasonApartFromATraceAfterAnOpenLine(t *testing.T) {
	lines := hangEvents(t, "TestSkipper", "skip", "--- SKIP: TestSkipper (0.30s)\n", "cleanup-partial")
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, hangTrace)
	assertContains(t, "stdout", stdout, "test: 1 test(s) skipped:\n  a TestSkipper: x_test.go:3: no input on this host cleanup-partial\n")
}

// Under go test -json -v a subtest's result line follows four spaces for each
// level it is nested.
func TestRun_PrintsTheTraceAfterAnIndentedResultLine(t *testing.T) {
	lines := hangEvents(t, "TestQuick/sub", "pass", "    --- PASS: TestQuick/sub (0.20s)\n")
	code, stdout, _ := runSummary(t, lines, "a")
	if code != 1 {
		t.Errorf("exit %d, want 1", code)
	}
	assertContains(t, "stdout", stdout, hangTrace)
}

// A result line is matched where an event starts. Without -v, the same text
// after other text in one event is a line the test printed.
func TestRun_ReadsNoResultLineInsideAnEvent(t *testing.T) {
	lines := []string{
		ev(t, "run", "a", "TestA", ""),
		ev(t, "output", "a", "TestA", "--- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "kept\n"),
		ev(t, "output", "a", "TestA", "note --- PASS: TestA (0.00s)\n"),
		ev(t, "output", "a", "TestA", "kept too\n"),
		ev(t, "pass", "a", "TestA", ""),
		ev(t, "fail", "a", "", ""),
	}
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "kept\nnote --- PASS: TestA (0.00s)\nkept too\n")
}

func TestRun_PrintsAnUnfinishedTestsOutputOnce(t *testing.T) {
	for name, lines := range map[string][]string{
		"the package reported its failure": {
			ev(t, "run", "a", "TestP", ""),
			ev(t, "output", "a", "TestP", "panic: nil map\n"),
			ev(t, "output", "a", "", "FAIL\ta\t0.1s\n"),
			ev(t, "fail", "a", "", ""),
		},
		"the package reported no result": {
			ev(t, "run", "a", "TestP", ""),
			ev(t, "output", "a", "TestP", "panic: nil map\n"),
		},
	} {
		_, stdout, _ := runSummary(t, lines, "a")
		if n := strings.Count(stdout, "panic: nil map\n"); n != 1 {
			t.Errorf("%s: the unfinished test's output printed %d times, want once:\n%s", name, n, stdout)
		}
	}
}

// A subtest that skips through raceskip.Skip is recorded and required by its
// own name: its parent passing says nothing about it.
func TestRun_RecordsAndRequiresARaceSkippedSubtestByItsOwnName(t *testing.T) {
	subtest := func(result, output string) []string {
		return []string{
			ev(t, "run", "a", "TestRatios", ""),
			ev(t, "run", "a", "TestRatios/floor", ""),
			ev(t, "output", "a", "TestRatios/floor", output),
			ev(t, result, "a", "TestRatios/floor", ""),
			ev(t, "pass", "a", "TestRatios", ""),
			ev(t, "pass", "a", "", ""),
		}
	}
	file := filepath.Join(t.TempDir(), "race-skips")
	lines := subtest("skip", "    r_test.go:5: "+raceskip.Reason+"\n")
	if code, _, stderr := runSummary(t, lines, "-race-skips="+file, "a"); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	got, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("read race skips: %v", err)
	}
	if want := "a\tTestRatios/floor\n"; string(got) != want {
		t.Errorf("race skips = %q, want %q", got, want)
	}

	code, _, stderr := runSummary(t, subtest("skip", "    r_test.go:6: no input on this host\n"), "-require=TestRatios/floor", "a")
	if code != 1 {
		t.Errorf("the subtest skipped again: exit %d, want 1", code)
	}
	assertContains(t, "stderr", stderr, "test: TestRatios/floor did not run and pass\n")
	if code, _, stderr := runSummary(t, subtest("pass", "ok\n"), "-require=TestRatios/floor", "a"); code != 0 {
		t.Errorf("the subtest passed: exit %d, want 0: %s", code, stderr)
	}
}

// A test that skips through raceskip.Skip twice, as under -count=2, is one line.
func TestRun_WritesARaceSkippedTestOnce(t *testing.T) {
	var lines []string
	for range 2 {
		lines = append(lines, ev(t, "run", "a", "TestR", ""), ev(t, "output", "a", "TestR", "    r_test.go:5: "+raceskip.Reason+"\n"), ev(t, "skip", "a", "TestR", ""))
	}
	lines = append(lines, ev(t, "pass", "a", "", ""))
	file := filepath.Join(t.TempDir(), "race-skips")
	if code, _, stderr := runSummary(t, lines, "-race-skips="+file, "a"); code != 0 {
		t.Fatalf("exit %d, want 0: %s", code, stderr)
	}
	if got, err := os.ReadFile(file); err != nil || string(got) != "a\tTestR\n" {
		t.Errorf("race skips = %q, %v; want one line", got, err)
	}
}

func TestRun_RequiresEachNamedTest(t *testing.T) {
	lines := append(passingTest(t, "a", "TestR"), ev(t, "run", "a", "TestS", ""), ev(t, "skip", "a", "TestS", ""))
	for _, args := range [][]string{{"-require=TestR", "-require=TestS", "a"}, {"-require=TestS", "-require=TestR", "a"}} {
		code, _, stderr := runSummary(t, lines, args...)
		if code != 1 {
			t.Errorf("%v: exit %d, want 1", args, code)
		}
		assertContains(t, "stderr", stderr, "test: TestS did not run and pass\n")
		if strings.Contains(stderr, "TestR did not") {
			t.Errorf("%v: the passed test is reported:\n%s", args, stderr)
		}
	}
	if code, _, stderr := runSummary(t, lines, "-require=", "a"); code != 2 || !strings.Contains(stderr, "names no test") {
		t.Errorf("an empty -require: exit %d, %q; want 2 and the refusal", code, stderr)
	}
}

// realRun returns the events of go test -json over a module of one package
// holding source, run with flags, which must fail.
func realRun(t *testing.T, source string, flags ...string) []byte {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"go.mod": "module hang\n", "hang_test.go": source} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	args := append(append([]string{"test", "-json", "-count=1", "-timeout=5s"}, flags...), ".")
	cmd := exec.CommandContext(t.Context(), "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=", "GOWORK=off", "GOTOOLCHAIN=local", "GOPROXY=off")
	events, err := cmd.Output()
	if _, ok := errors.AsType[*exec.ExitError](err); !ok {
		t.Fatalf("go test -json: %v, want a failing exit\n%s", err, events)
	}
	return events
}

// Each row is a real binary that times out after a parallel test finished, so
// the trace is given to the finished test.
func TestRun_PrintsARealTimeoutsTraceAfterEachFormOfResultLine(t *testing.T) {
	t.Parallel()
	const head = "package hang\n\nimport (\n\t\"fmt\"\n\t\"testing\"\n\t\"time\"\n)\n\nvar _ = fmt.Sprint\n\n"
	const slow = "func TestSlow(t *testing.T) {\n\tt.Parallel()\n\ttime.Sleep(time.Hour)\n}\n"
	for _, row := range []struct {
		name, source string
		flags        []string
		events       []string // each must be in the stream, or the row no longer holds its case
		stdout       []string
	}{
		{
			name:   "an open line before a pass",
			source: head + "func TestQuick(t *testing.T) {\n\tt.Parallel()\n\ttime.Sleep(300 * time.Millisecond)\n\tt.Log(\"about to finish\")\n\tfmt.Print(\"unterminated\")\n}\n\n" + slow,
			events: []string{`"Test":"TestQuick","Output":"unterminated"}`, `"Action":"pass","Package":"hang","Test":"TestQuick"`},
			stdout: []string{"panic: test timed out after 5s\n\trunning tests:\n\t\tTestSlow ("},
		},
		{
			name: "an open line before a skip",
			source: head + "func TestQuick(t *testing.T) {\n\tt.Parallel()\n\tt.Cleanup(func() { fmt.Print(\"cleanup-partial\") })\n" +
				"\ttime.Sleep(300 * time.Millisecond)\n\tt.Skip(\"no input on this host\")\n}\n\n" + slow,
			events: []string{`"Test":"TestQuick","Output":"cleanup-partial"}`, `"Action":"skip","Package":"hang","Test":"TestQuick"`},
			stdout: []string{
				"panic: test timed out after 5s\n\trunning tests:\n\t\tTestSlow (",
				": no input on this host cleanup-partial\n",
			},
		},
		{
			name: "an indented line under -v",
			source: head + "func TestQuick(t *testing.T) {\n\tt.Parallel()\n" +
				"\tt.Run(\"sub\", func(t *testing.T) { time.Sleep(300 * time.Millisecond) })\n}\n\n" + slow,
			flags:  []string{"-v"},
			events: []string{`"Test":"TestQuick/sub","Output":"    --- PASS: TestQuick/sub (`, `"Action":"pass","Package":"hang","Test":"TestQuick/sub"`},
			stdout: []string{"panic: test timed out after 5s\n\trunning tests:\n\t\tTestSlow ("},
		},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			events := realRun(t, row.source, row.flags...)
			for _, want := range row.events {
				if !bytes.Contains(events, []byte(want)) {
					t.Fatalf("the run's events do not hold %s, so it no longer holds the case this pins:\n%s", want, events)
				}
			}
			var out, errOut bytes.Buffer
			if code := run([]string{"hang"}, bytes.NewReader(events), &out, &errOut); code != 1 {
				t.Errorf("exit %d, want 1", code)
			}
			for _, want := range row.stdout {
				assertContains(t, "stdout", out.String(), want)
			}
		})
	}
}

// go test -json cuts a long line into several events, and a skip's reason is
// its lines, not its events.
func TestRun_JoinsASkipsReasonCutAcrossEvents(t *testing.T) {
	long := "    x_test.go:3: " + strings.Repeat("y", 1100) + " end\n"
	lines := []string{
		ev(t, "run", "a", "TestS", ""),
		ev(t, "output", "a", "TestS", long[:1024]),
		ev(t, "output", "a", "TestS", long[1024:]),
		ev(t, "output", "a", "TestS", "--- SKIP: TestS (0.00s)\n"),
		ev(t, "skip", "a", "TestS", ""),
		ev(t, "pass", "a", "", ""),
	}
	_, stdout, _ := runSummary(t, lines, "a")
	assertContains(t, "stdout", stdout, "  a TestS: x_test.go:3: "+strings.Repeat("y", 1100)+" end\n")
}
