// Command testsummary reads `go test -json` events on standard input and judges
// the run by what each package reported, not by the go command's exit status
// alone.
//
// It prints each package's result line, the output of every test that failed or
// never finished, and all of a failed package's own output, which holds the
// shuffle seed that reproduces its order. Then it names each package that
// reported no result, each package that ran no test, and every skipped test with
// its reason. The arguments are the packages the run was asked for, and it exits
// 1 when one of them failed or reported no result.
//
// A test skipped through [raceskip.Skip] is also collected. With
// -race-skips=FILE, those tests are written to FILE, one line per package: the
// import path, a tab, and the top-level test names joined by commas. With
// -require=A,B, the run fails unless each named top-level test ran and passed.
//
// Usage:
//
//	go test -json ./... | testsummary [-race-skips=FILE] [-require=A,B] pkg...
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"

	"github.com/simon-lentz/yammm/internal/raceskip"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("testsummary", flag.ContinueOnError)
	fs.SetOutput(stderr)
	raceSkipsFile := fs.String("race-skips", "", "write the tests raceskip.Skip skipped to this file")
	require := fs.String("require", "", "comma-separated top-level tests that must run and pass")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	want := fs.Args()
	if len(want) == 0 {
		fmt.Fprintln(stderr, "testsummary: name the packages the run was asked for")
		return 2
	}

	s, err := read(stdin, stdout)
	if err != nil {
		fmt.Fprintf(stderr, "testsummary: %v\n", err)
		return 1
	}
	if *raceSkipsFile != "" {
		if err := os.WriteFile(*raceSkipsFile, s.raceSkipLines(), 0o600); err != nil {
			fmt.Fprintf(stderr, "testsummary: %v\n", err)
			return 1
		}
	}
	var required []string
	if *require != "" {
		required = strings.Split(*require, ",")
	}
	return s.report(want, required, stdout, stderr)
}

// event is one line of `go test -json` output. A build error arrives with
// ImportPath rather than Package.
type event struct {
	Action  string
	Package string
	Test    string
	Output  string
}

type summary struct {
	result    map[string]string          // package -> pass, fail or skip
	ran       map[string]int             // package -> top-level tests started
	passed    map[string]map[string]bool // package -> top-level test -> passed
	skipped   []string                   // "package test: reason"
	raceSkips map[string][]string        // package -> top-level tests raceskip.Skip skipped
	pending   map[string][]string        // package NUL test -> output of a test not yet finished
	framing   map[string][]string        // package -> its own output, until it finishes
}

func read(r io.Reader, w io.Writer) (*summary, error) {
	s := &summary{
		result:    map[string]string{},
		ran:       map[string]int{},
		passed:    map[string]map[string]bool{},
		raceSkips: map[string][]string{},
		pending:   map[string][]string{},
		framing:   map[string][]string{},
	}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		var e event
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil || e.Action == "" {
			// The go command writes a setup error as plain text.
			fmt.Fprintln(w, sc.Text())
			continue
		}
		s.apply(e, w)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading events: %w", err)
	}
	// A package that never finished is judged as failed, so all of its output shows.
	for _, pkg := range slices.Sorted(maps.Keys(s.framing)) {
		s.finish(pkg, true, w)
	}
	return s, nil
}

func (s *summary) apply(e event, w io.Writer) {
	key := e.Package + "\x00" + e.Test
	top, _, _ := strings.Cut(e.Test, "/")
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
		if e.Test != "" && top == e.Test {
			s.ran[e.Package]++
		}
	case "skip":
		if e.Test == "" {
			s.result[e.Package] = e.Action
			s.finish(e.Package, false, w)
			return
		}
		why := reason(s.pending[key])
		delete(s.pending, key)
		s.skipped = append(s.skipped, e.Package+" "+e.Test+": "+why)
		if strings.Contains(why, raceskip.Reason) && !slices.Contains(s.raceSkips[e.Package], top) {
			s.raceSkips[e.Package] = append(s.raceSkips[e.Package], top)
		}
	case "pass", "fail":
		if e.Test == "" {
			s.result[e.Package] = e.Action
			s.finish(e.Package, e.Action == "fail", w)
			return
		}
		if e.Action == "fail" {
			fmt.Fprint(w, strings.Join(s.pending[key], ""))
		}
		delete(s.pending, key)
		if top == e.Test {
			if s.passed[e.Package] == nil {
				s.passed[e.Package] = map[string]bool{}
			}
			s.passed[e.Package][e.Test] = e.Action == "pass"
		}
	}
}

// finish prints a finished package's own output: only its result line when it
// did not fail, and otherwise all of it and then the output of each test that
// started and never finished, which is where a panic or a timeout leaves its trace.
func (s *summary) finish(pkg string, failed bool, w io.Writer) {
	for _, line := range s.framing[pkg] {
		if failed || strings.HasPrefix(line, "ok ") || strings.HasPrefix(line, "? ") {
			fmt.Fprint(w, line)
		}
	}
	delete(s.framing, pkg)
	if !failed {
		return
	}
	prefix := pkg + "\x00"
	for _, key := range slices.Sorted(maps.Keys(s.pending)) {
		if strings.HasPrefix(key, prefix) {
			fmt.Fprint(w, strings.Join(s.pending[key], ""))
			delete(s.pending, key)
		}
	}
}

func (s *summary) raceSkipLines() []byte {
	var b strings.Builder
	for _, pkg := range slices.Sorted(maps.Keys(s.raceSkips)) {
		names := slices.Sorted(slices.Values(s.raceSkips[pkg]))
		b.WriteString(pkg + "\t" + strings.Join(names, ",") + "\n")
	}
	return []byte(b.String())
}

func (s *summary) report(want, required []string, stdout, stderr io.Writer) int {
	var missing, failed, noTests []string
	for _, pkg := range want {
		switch s.result[pkg] {
		case "":
			missing = append(missing, pkg)
		case "fail":
			failed = append(failed, pkg)
		default:
			if s.ran[pkg] == 0 {
				noTests = append(noTests, pkg)
			}
		}
	}
	slices.Sort(s.skipped)
	list(stdout, fmt.Sprintf("%d package(s) ran no test", len(noTests)), noTests)
	list(stdout, fmt.Sprintf("%d test(s) skipped", len(s.skipped)), s.skipped)

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
		if !slices.ContainsFunc(want, func(pkg string) bool { return s.passed[pkg][name] }) {
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
// without go test's RUN, NAME and SKIP framing.
func reason(lines []string) string {
	var out []string
	for _, l := range lines {
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
