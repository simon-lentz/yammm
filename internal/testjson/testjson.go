// Package testjson reads the event stream `go test -json` writes. The test
// summary and the mutation harness both judge a run through it.
//
// # What an event can be trusted for
//
// A package's own result is its last event with no Test whose Action is pass,
// fail or skip. The go command writes one when the test binary exits, and it
// is fail whenever the binary's exit status is not zero or its build failed,
// whatever the binary printed, and its FailedBuild is the go command's. On a
// zero exit the binary's lines can decide it: a printed line can turn pass into
// skip; a marked line a test prints can yield an earlier event with no Test,
// which [Tally] keeps when no later one comes; and when the binary's last
// framing line opened a test, as one that exits inside a test leaves it, or a
// test printed one, the go command gives its pass to that test and writes no
// package event.
//
// Every event that names a test is the go command's reading of the binary's
// output, which any code in the binary can print, and of the go command's own
// notes, such as how a killed binary exited, which it gives to the test it read
// last. Under -json the testing package starts each of its framing lines with
// byte 0x16; a line a test prints with that byte, or any framing line printed
// before the binary's first, yields a run, pass, fail or skip event for
// whatever test the line names, one that never ran included. So a test-level
// event is a report the binary chose to make: the mutation harness rests no
// kill on one, and the summary's -require trusts the pass a test reports.
//
// No Action says that a binary ran out of time: a package that timed out
// reports fail as any other, and only its output text differs.
//
// # Result lines
//
// The go command gives a line written outside the testing package's framing to
// the test whose framing line it read last, so the output a finished test
// holds can end with lines that are not its own. [SplitAtResult] finds where
// the test's own output ends.
package testjson

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Event is one line of `go test -json` output. A build error arrives with
// ImportPath rather than Package.
type Event struct {
	Action      string
	Package     string
	ImportPath  string
	Test        string
	Output      string
	Elapsed     *float64
	FailedBuild string
}

// maxLine bounds one line of the stream. An output event holds at most 1024
// bytes of text, and the go command writes some errors as plain text, one line
// at a time.
const maxLine = 1 << 26

// Read calls event for each event of r in order, and plain for each line that
// is not an event: the go command writes some errors as plain text.
func Read(r io.Reader, event func(Event), plain func(line string)) error {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), maxLine)
	for sc.Scan() {
		var e Event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Action == "" {
			plain(sc.Text())
			continue
		}
		event(e)
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("reading events: %w", err)
	}
	return nil
}

// Tally holds what a run reported for each package and each test.
type Tally struct {
	result      map[string]string
	failedBuild map[string]string
	started     map[string]int
	tests       map[string]map[string]string
}

// Add records e. It reads every Action and ignores the ones it does not hold.
func (t *Tally) Add(e Event) {
	if t.result == nil {
		t.result = map[string]string{}
		t.failedBuild = map[string]string{}
		t.started = map[string]int{}
		t.tests = map[string]map[string]string{}
	}
	switch e.Action {
	case "run":
		if e.Test == "" {
			return
		}
		if !strings.Contains(e.Test, "/") {
			t.started[e.Package]++
		}
		t.setTest(e.Package, e.Test, "")
	case "pass", "fail", "skip":
		if e.Test == "" {
			t.result[e.Package] = e.Action
			if e.FailedBuild != "" {
				t.failedBuild[e.Package] = e.FailedBuild
			}
			return
		}
		t.setTest(e.Package, e.Test, e.Action)
	}
}

func (t *Tally) setTest(pkg, test, result string) {
	if t.tests[pkg] == nil {
		t.tests[pkg] = map[string]string{}
	}
	t.tests[pkg][test] = result
}

// Result returns the result pkg reported for itself: pass, fail or skip, and
// the empty string when it reported none.
func (t *Tally) Result(pkg string) string { return t.result[pkg] }

// FailedBuild returns the build the go command named as the cause of pkg's
// failure, and the empty string when pkg's test binary was built.
func (t *Tally) FailedBuild(pkg string) string { return t.failedBuild[pkg] }

// Started returns how many run events the top-level tests of pkg reported: a
// test run twice counts twice.
func (t *Tally) Started(pkg string) int { return t.started[pkg] }

// Passed reports whether test's last event in pkg was a pass.
func (t *Tally) Passed(pkg, test string) bool { return t.tests[pkg][test] == "pass" }

// Tests returns, sorted, the tests of pkg whose last event was result. The
// empty result names the tests that started and reported nothing after.
func (t *Tally) Tests(pkg, result string) []string {
	var names []string
	for _, name := range slices.Sorted(maps.Keys(t.tests[pkg])) {
		if t.tests[pkg][name] == result {
			names = append(names, name)
		}
	}
	return names
}

// resultMarkers open the line the testing package prints for a finished test.
var resultMarkers = []string{"--- PASS: ", "--- FAIL: ", "--- SKIP: "}

// SplitAtResult splits outputs, the Output of each output event of one
// finished test in order, at the test's own result line: the events before
// the line, the events that hold it, and the events after it, which were
// written outside the testing package's framing. With no such line, every
// event is before.
//
// Without -v, the go command starts a new output event at every marked result
// line it reads, so the line is matched at the start of an event and never
// inside one: a write that ended without a newline just before it is an event
// of its own. Under -v such a write and the line can arrive as one event, which
// is not matched, and the line follows one group of four spaces for each level
// of nesting. A line longer than an event's 1024 bytes continues in
// the events that follow, up to the first that ends in a newline.
//
// A line of the same shape, printed after the real one by a test that still
// runs, starts an event of this test as the real line does, and nothing in the
// stream tells the two apart. The last such line is taken, since a test's own
// body can print one before its result.
func SplitAtResult(outputs []string, test string) (before, result, after []string) {
	for i, out := range slices.Backward(outputs) {
		rest, ok := afterMarker(out)
		if !ok {
			continue
		}
		end := i
		for !strings.HasSuffix(outputs[end], "\n") && end+1 < len(outputs) {
			end++
		}
		name, _, _ := strings.Cut(rest+strings.Join(outputs[i+1:end+1], ""), " (")
		if strings.TrimRight(name, "\r\n") == test {
			return outputs[:i], outputs[i : end+1], outputs[end+1:]
		}
	}
	return outputs, nil, nil
}

// afterMarker returns what follows a result marker that opens out, after any
// groups of four spaces.
func afterMarker(out string) (rest string, ok bool) {
	for strings.HasPrefix(out, "    ") {
		out = out[4:]
	}
	for _, marker := range resultMarkers {
		if rest, ok := strings.CutPrefix(out, marker); ok {
			return rest, true
		}
	}
	return "", false
}
