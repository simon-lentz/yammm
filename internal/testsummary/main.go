// Command testsummary reads `go test -json` events on standard input and judges
// the run by what each package reported, not by the go command's exit status
// alone.
//
// It prints each package's result line, the output of every test that failed or
// never finished, and all of a failed package's own output, which holds the
// shuffle seed that reproduces its order. Then it names each package that
// reported no result, each package that passed or skipped and ran no test, and
// every skipped test with its reason. The arguments are the packages the run was asked for, and it exits
// 1 when one of them failed or reported no result.
//
// go test -json gives a line written outside the testing package's framing
// (a panic, a stack, a raw print from any test) to the test whose result line
// it read last, until it reads another line naming a test or the package's
// result. When a finished test's result line came last, a test binary's
// timeout panic, the tests it names as running and every goroutine's stack
// arrive that way, so a failed package also prints what followed a finished
// test's own result line, which [testjson.SplitAtResult] finds.
//
// A test skipped through [raceskip.Skip] is also collected, a subtest by its
// own name. With -race-skips=FILE, those tests are written to FILE, one line
// each: the import path, a tab, and the test's name. With -require=NAME, which
// may be given more than once and never with an empty name, the run fails
// unless each named test ran and passed.
//
// With -durations=FILE, every result go test timed is written to FILE as
// tab-separated rows under a header: the package, the test (empty for the
// package's own row), the result, the seconds go test reported, and "parallel"
// for a test that paused for t.Parallel or "serial" otherwise. Rows are sorted
// by package and test, and a test run more than once keeps its runs' order. A
// parallel test's seconds leave out the time it waited paused. An empty FILE
// writes nothing.
//
// With -slowest=N, the summary lists before its verdict the N packages whose
// results go test timed longest, longest first, each with its seconds to a
// tenth; packages timed alike are in import-path order. The seconds are a
// package's test binary's, so they leave out its build, and packages run at
// once, so they can sum to more than the run took. A run of fewer than N
// packages lists them all.
//
// Usage:
//
//	go test -json ./... | testsummary [-race-skips=FILE] [-durations=FILE] [-slowest=N] [-require=NAME]... pkg...
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/testjson"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("testsummary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	raceSkipsFile := fs.String("race-skips", "", "write the tests raceskip.Skip skipped to this file")
	durationsFile := fs.String("durations", "", "write every timed result to this file")
	slowest := fs.Int("slowest", 0, "list this many of the packages go test timed longest")
	var required []string
	fs.Func("require", "a test that must run and pass; may be given more than once", func(name string) error {
		if name == "" {
			return errors.New("names no test")
		}
		required = append(required, name)
		return nil
	})
	if err := fs.Parse(args); err != nil {
		return 2
	}
	want := fs.Args()
	if len(want) == 0 {
		fmt.Fprintln(stderr, "testsummary: name the packages the run was asked for")
		return 2
	}
	if *slowest < 0 {
		fmt.Fprintf(stderr, "testsummary: -slowest takes a count of zero or more, got %d\n", *slowest)
		return 2
	}

	s, err := read(stdin, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "testsummary: %v\n", err)
		return 1
	}
	// The report comes first, so a file that cannot be written does not hide
	// the run's result.
	status := s.report(want, required, *slowest, stdout, stderr)
	for _, out := range []struct {
		file    string
		content func() []byte
	}{
		{*raceSkipsFile, s.raceSkipLines},
		{*durationsFile, s.durationLines},
	} {
		if out.file == "" {
			continue
		}
		if err := os.WriteFile(out.file, out.content(), 0o600); err != nil {
			fmt.Fprintf(stderr, "testsummary: %v\n", err)
			status = 1
		}
	}
	return status
}

type summary struct {
	tally     testjson.Tally      // each package's result and each test's
	skipped   []string            // "package test: reason"
	raceSkips map[string][]string // package -> tests raceskip.Skip skipped
	pending   map[string][]string // package NUL test -> output of a test not yet finished
	framing   map[string][]string // package -> its own output, until it finishes
	after     map[string][]string // package -> output that followed a finished test's result line
	paused    map[string]bool     // package NUL test -> the test paused for t.Parallel
	timed     []timing            // every result go test timed, in arrival order
}

// timing is one result go test timed.
type timing struct {
	pkg, test, result string
	seconds           float64
	parallel          bool
}

func read(r io.Reader, w io.Writer) (*summary, error) {
	s := &summary{
		raceSkips: map[string][]string{},
		pending:   map[string][]string{},
		framing:   map[string][]string{},
		after:     map[string][]string{},
		paused:    map[string]bool{},
	}
	err := testjson.Read(r, func(e testjson.Event) { s.apply(e, w) }, func(line string) { fmt.Fprintln(w, line) })
	if err != nil {
		return nil, err //nolint:wrapcheck // testjson.Read's error says what failed
	}
	// A package that never finished is judged as failed, so all of its output shows.
	for _, pkg := range s.unfinished() {
		s.finish(pkg, true, w)
	}
	return s, nil
}

func (s *summary) apply(e testjson.Event, w io.Writer) {
	s.tally.Add(e)
	key := e.Package + "\x00" + e.Test
	if e.Elapsed != nil && (e.Action == "pass" || e.Action == "fail" || e.Action == "skip") {
		s.timed = append(s.timed, timing{e.Package, e.Test, e.Action, *e.Elapsed, s.paused[key]})
	}
	switch e.Action {
	case "build-output":
		fmt.Fprint(w, e.Output)
	case "output":
		if e.Test == "" {
			s.framing[e.Package] = append(s.framing[e.Package], e.Output)
		} else {
			s.pending[key] = append(s.pending[key], e.Output)
		}
	case "run":
		delete(s.paused, key)
	case "pause":
		s.paused[key] = true
	case "skip":
		if e.Test == "" {
			s.finish(e.Package, false, w)
			return
		}
		before, _, trailing := testjson.SplitAtResult(s.pending[key], e.Test)
		s.keepAfter(e.Package, trailing)
		why := reason(before)
		delete(s.pending, key)
		s.skipped = append(s.skipped, e.Package+" "+e.Test+": "+why)
		if strings.Contains(why, raceskip.Reason) && !slices.Contains(s.raceSkips[e.Package], e.Test) {
			s.raceSkips[e.Package] = append(s.raceSkips[e.Package], e.Test)
		}
	case "pass", "fail":
		if e.Test == "" {
			s.finish(e.Package, e.Action == "fail", w)
			return
		}
		if e.Action == "fail" {
			fmt.Fprint(w, strings.Join(s.pending[key], ""))
		} else {
			_, _, trailing := testjson.SplitAtResult(s.pending[key], e.Test)
			s.keepAfter(e.Package, trailing)
		}
		delete(s.pending, key)
	}
}

// keepAfter holds the output that followed a finished test's result line until
// its package finishes.
func (s *summary) keepAfter(pkg string, lines []string) {
	if len(lines) > 0 {
		s.after[pkg] = append(s.after[pkg], lines...)
	}
}

// finish prints a finished package's own output: only its result line when it
// did not fail, and otherwise all of it, then every line that followed a
// finished test's result line, then the output of each test that started and
// never finished. A panic or a timeout leaves its trace in the last two.
func (s *summary) finish(pkg string, failed bool, w io.Writer) {
	for _, line := range s.framing[pkg] {
		if failed || strings.HasPrefix(line, "ok ") || strings.HasPrefix(line, "? ") {
			fmt.Fprint(w, line)
		}
	}
	delete(s.framing, pkg)
	after := s.after[pkg]
	delete(s.after, pkg)
	prefix := pkg + "\x00"
	keys := slices.Sorted(maps.Keys(s.pending))
	if !failed {
		for _, key := range keys {
			if strings.HasPrefix(key, prefix) {
				delete(s.pending, key)
			}
		}
		return
	}
	fmt.Fprint(w, strings.Join(after, ""))
	for _, key := range keys {
		if strings.HasPrefix(key, prefix) {
			fmt.Fprint(w, strings.Join(s.pending[key], ""))
			delete(s.pending, key)
		}
	}
}

// unfinished returns, sorted, each package that holds output and reported no
// result: its own lines, a test's, or lines after a finished test's result.
func (s *summary) unfinished() []string {
	pkgs := map[string]bool{}
	for pkg := range s.framing {
		pkgs[pkg] = true
	}
	for pkg := range s.after {
		pkgs[pkg] = true
	}
	for key := range s.pending {
		pkg, _, _ := strings.Cut(key, "\x00")
		pkgs[pkg] = true
	}
	return slices.Sorted(maps.Keys(pkgs))
}

func (s *summary) raceSkipLines() []byte {
	var b strings.Builder
	for _, pkg := range slices.Sorted(maps.Keys(s.raceSkips)) {
		for _, name := range slices.Sorted(slices.Values(s.raceSkips[pkg])) {
			b.WriteString(pkg + "\t" + name + "\n")
		}
	}
	return []byte(b.String())
}

func (s *summary) durationLines() []byte {
	var b strings.Builder
	b.WriteString("package\ttest\tresult\tseconds\tscheduling\n")
	for _, r := range slices.SortedStableFunc(slices.Values(s.timed), func(a, b timing) int {
		return cmp.Or(strings.Compare(a.pkg, b.pkg), strings.Compare(a.test, b.test))
	}) {
		scheduling := "serial"
		if r.parallel {
			scheduling = "parallel"
		}
		b.WriteString(r.pkg + "\t" + r.test + "\t" + r.result + "\t" + strconv.FormatFloat(r.seconds, 'f', -1, 64) + "\t" + scheduling + "\n")
	}
	return []byte(b.String())
}

// slowestPackages returns the n package results with the most seconds, most
// first and ties by import path, each as its seconds and package, and a heading.
func (s *summary) slowestPackages(n int) (heading string, items []string) {
	var pkgs []timing
	for _, r := range s.timed {
		if r.test == "" {
			pkgs = append(pkgs, r)
		}
	}
	slices.SortFunc(pkgs, func(a, b timing) int {
		return cmp.Or(cmp.Compare(b.seconds, a.seconds), strings.Compare(a.pkg, b.pkg))
	})
	n = min(n, len(pkgs))
	for _, r := range pkgs[:n] {
		items = append(items, fmt.Sprintf("%6.1fs  %s", r.seconds, r.pkg))
	}
	return fmt.Sprintf("the %d slowest of %d packages", n, len(pkgs)), items
}

func (s *summary) report(want, required []string, slowest int, stdout, stderr io.Writer) int {
	var missing, failed, noTests []string
	for _, pkg := range want {
		switch s.tally.Result(pkg) {
		case "":
			missing = append(missing, pkg)
		case "fail":
			failed = append(failed, pkg)
		default:
			if s.tally.Started(pkg) == 0 {
				noTests = append(noTests, pkg)
			}
		}
	}
	slices.Sort(s.skipped)
	list(stdout, fmt.Sprintf("%d package(s) ran no test", len(noTests)), noTests)
	list(stdout, fmt.Sprintf("%d test(s) skipped", len(s.skipped)), s.skipped)
	heading, items := s.slowestPackages(slowest)
	list(stdout, heading, items)

	status := 0
	if len(missing) > 0 {
		list(stderr, fmt.Sprintf("%d of %d packages reported no result", len(missing), len(want)), missing)
		status = 1
	}
	if len(failed) > 0 {
		list(stderr, fmt.Sprintf("%d of %d packages failed", len(failed), len(want)), failed)
		status = 1
	}
	for _, name := range required {
		if !slices.ContainsFunc(want, func(pkg string) bool { return s.tally.Passed(pkg, name) }) {
			fmt.Fprintf(stderr, "test: %s did not run and pass\n", name)
			status = 1
		}
	}
	if status == 0 {
		fmt.Fprintf(stdout, "test: %d of %d packages reported a result, and none failed\n", len(want), len(want))
	}
	return status
}

func list(w io.Writer, heading string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(w, "test: %s:\n", heading)
	for _, item := range items {
		fmt.Fprintf(w, "  %s\n", item)
	}
}

// reason returns a skipped test's own output lines, joined: its skip message
// without the framing lines that begin "=== " or "--- ". outputs are the events
// before the test's result line, and an event can hold part of a line.
func reason(outputs []string) string {
	var out []string
	for l := range strings.Lines(strings.Join(outputs, "")) {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "=== ") || strings.HasPrefix(t, "--- ") {
			continue
		}
		out = append(out, t)
	}
	if len(out) == 0 {
		return "no reason given"
	}
	return strings.Join(out, " ")
}
