package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/internal/testjson"
)

// A configuration is one way a gate builds the module: flags of the go
// command and variables of its environment.
type configuration struct {
	name    string
	flags   []string
	env     []string      // name=value
	timeout time.Duration // the -timeout the gate job that makes it gives go test
}

// configurations are the test builds a gate makes on one host, one for each
// setting a gate job adds, in the order a file is tried under them: the plain
// build, the full gate's, the jsonv2 job's, the mermaid job's and the
// integration job's. None combines two, as the jsonv2 job's -race run does.
func configurations() []configuration {
	return []configuration{
		{name: "default", timeout: 10 * time.Minute},
		{name: "race", flags: []string{"-race"}, env: []string{"CGO_ENABLED=1"}, timeout: 10 * time.Minute},
		{name: "jsonv2", env: []string{"GOEXPERIMENT=jsonv2"}, timeout: 10 * time.Minute},
		{name: "mermaid", flags: []string{"-tags=mermaid"}, timeout: 5 * time.Minute},
		{name: "neo4j_integration", flags: []string{"-tags=neo4j_integration"}, timeout: 20 * time.Minute},
	}
}

func (c configuration) environment(env []string) []string {
	for _, kv := range c.env {
		name, value, _ := strings.Cut(kv, "=")
		env = setEnv(env, name, value)
	}
	return env
}

// The verdict words of results.tsv.
const (
	verdictKilled      = "KILLED"
	verdictSurvived    = "SURVIVED"
	verdictTimeout     = "TIMEOUT"
	verdictNoBuild     = "NOBUILD"
	verdictRed         = "RED"
	verdictNoMatch     = "NOMATCH"
	verdictNoFold      = "NOFOLD"
	verdictNoTest      = "NOTEST"
	verdictNotCompiled = "NOTCOMPILED"
	verdictError       = "ERROR"
)

// What one named package did in a test run.
const (
	packagePassed     = "passed"
	packageNoTests    = "passed and ran no test"
	packageNoFiles    = "has no test files"
	packageFailed     = "failed"
	packageExited     = "failed though its binary exited 0"
	packageTimedOut   = "was stopped at the limit"
	packageNotBuilt   = "was not built"
	packageNotStarted = "did not start"
	packageNoResult   = "reported no result"
)

// A testRun is one go test run over a mutant's packages.
type testRun struct {
	tally    testjson.Tally
	packages []listed             // the named packages, in import-path order
	state    map[string]string    // import path to what the package did
	output   map[string][]printed // import path to what its binary and the go command printed for it
	plain    []string             // what the go command printed outside any package
	exit     int
	fault    string // why the run cannot be read; empty when it can
}

// printed is the text of one output event and the test go test gave it to.
type printed struct{ test, text string }

// test runs go test over patterns in the tree under config, with tmp as
// TMPDIR, and classifies every named package.
func (w *worker) test(ctx context.Context, config configuration, tmp string, patterns []string) (*testRun, error) {
	env := setEnv(config.environment(w.tree.run.env), "TMPDIR", tmp)
	named, err := goList(ctx, w.tree.dir, env, slices.Concat(config.flags, []string{"--"}, patterns)...)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(named, func(a, b listed) int { return strings.Compare(a.ImportPath, b.ImportPath) })
	records := filepath.Join(w.tree.run.work, fmt.Sprintf("records.w%d", w.number))
	if err := removeAll(records); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(records, 0o750); err != nil {
		return nil, fmt.Errorf("the runners' records: %w", err)
	}
	limit := w.tree.run.limit
	args := slices.Concat([]string{
		"test", "-json", "-count=1", "-failfast=false", "-p=2",
		// The runner stops a binary at the limit, before this alarm of the
		// binary's own. go test waits a tenth of it for a binary's output to
		// close, and a test that asks for its deadline gets it, so it is the gate
		// job's whenever the limit leaves room.
		"-timeout=" + max(config.timeout, limit+time.Minute).String(),
		"-exec=" + w.tree.run.exec,
	}, config.flags, patterns)
	env = setEnv(setEnv(env, recordVar, records), superviseLimitVar, limit.String())
	var stdout, stderr bytes.Buffer
	run := &testRun{packages: named, state: map[string]string{}, output: map[string][]printed{}}
	if run.exit, err = goCommand(ctx, w.tree.dir, env, &stdout, &stderr, args...); err != nil {
		return nil, err
	}
	err = testjson.Read(&stdout, func(e testjson.Event) {
		run.tally.Add(e)
		switch e.Action {
		case "output":
			run.output[e.Package] = append(run.output[e.Package], printed{e.Test, e.Output})
		case "build-output":
			run.plain = append(run.plain, e.Output)
		}
	}, func(line string) { run.plain = append(run.plain, line+"\n") })
	if err != nil {
		return nil, fmt.Errorf("go test: %w", err)
	}
	run.plain = append(run.plain, stderr.String())
	seen, err := readRecords(records)
	if err != nil {
		return nil, err
	}
	run.classify(seen)
	return run, nil
}

// classify says what each named package did, from the result the go command
// reported for it, the runner's record of its binary, and whether it reported a
// test start.
func (t *testRun) classify(records map[string]record) {
	failures, refused := 0, 0
	for _, pkg := range t.packages {
		rec, ran := records[pkg.Dir]
		started := ran && rec.StartError == ""
		result := t.tally.Result(pkg.ImportPath)
		state := ""
		switch {
		// A binary stopped at the limit can still exit 0, as one whose test
		// catches SIGQUIT and returns or exits does, and then print any result
		// line or none.
		case started && rec.TimedOut:
			state = packageTimedOut
		// A binary that exited 0 with a test open, inside it or after a framing
		// line, gets no package result of the go command's, so whatever package
		// event it printed last, or none, stands; one that left its output open
		// gets the go command's fail. The gate's summary fails none and a fail.
		case started && rec.Exit == 0 && (result == "" || result == "fail"):
			state = packageExited
		case result == "":
			state = packageNoResult
		case result == "skip" && !ran:
			state = packageNoFiles
		case result == "fail" && !started:
			state = packageNotStarted
			if t.tally.FailedBuild(pkg.ImportPath) != "" {
				state = packageNotBuilt
			}
		case result == "fail" && rec.Exit != 0:
			state = packageFailed
		// A skip for a binary that ran is a line it printed; the gate passes it.
		case (result == "pass" || result == "skip") && started && rec.Exit == 0 && t.tally.Started(pkg.ImportPath) > 0:
			state = packagePassed
		case (result == "pass" || result == "skip") && started && rec.Exit == 0:
			state = packageNoTests
		default:
			t.fault = fmt.Sprintf("%s reported %s, and its test binary's record is %+v (present: %t)", pkg.ImportPath, result, rec, ran)
		}
		t.state[pkg.ImportPath] = state
		// go test fails every package that reports no pass or skip, and can pass
		// one whose binary ran and exited 0.
		if result != "pass" && result != "skip" {
			refused++
			if !started || rec.Exit != 0 {
				failures++
			}
		}
	}
	switch {
	case len(t.packages) == 0:
		t.fault = "the run named no package"
	case t.fault == "" && failures > 0 && t.exit == 0:
		t.fault = fmt.Sprintf("go test exited 0 and %d of its packages failed", failures)
	case t.fault == "" && refused == 0 && t.exit != 0:
		t.fault = fmt.Sprintf("go test exited %d and every package reported a pass or skip", t.exit)
	}
}

// count returns how many named packages are in one of states.
func (t *testRun) count(states ...string) int {
	n := 0
	for _, pkg := range t.packages {
		if slices.Contains(states, t.state[pkg.ImportPath]) {
			n++
		}
	}
	return n
}

// green reports whether the run can be read and every named package passed or
// has no test files.
func (t *testRun) green() bool {
	return t.fault == "" && t.count(packagePassed, packageNoTests, packageNoFiles) == len(t.packages)
}

// verdict judges a mutant by the run over its changed tree.
func (t *testRun) verdict() string {
	switch {
	case t.fault != "":
		return verdictError
	case t.count(packageNotBuilt, packageNotStarted, packageNoResult) > 0:
		return verdictNoBuild
	case t.count(packageFailed, packageExited) > 0:
		return verdictKilled
	case t.count(packageTimedOut) > 0:
		return verdictTimeout
	case t.count(packagePassed) > 0:
		return verdictSurvived
	default:
		return verdictNoTest
	}
}

// tests returns, sorted and once each, the tests go test reported with result
// in the packages that are in state. A test binary can print such a report
// for any name, so the names are for a reader and no verdict rests on them.
func (t *testRun) tests(state, result string) []string {
	var names []string
	for _, pkg := range t.packages {
		if t.state[pkg.ImportPath] == state {
			names = append(names, t.tally.Tests(pkg.ImportPath, result)...)
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// failing returns, sorted and once each, the tests a failed package reported
// failed and, of a package whose binary exited 0, those it reported failed and
// those it exited inside.
func (t *testRun) failing() []string {
	names := slices.Concat(t.tests(packageFailed, "fail"), t.tests(packageExited, "fail"), t.unfinished(packageExited))
	slices.Sort(names)
	return slices.Compact(names)
}

// own returns, for each test of pkg, the output events go test gave it.
func (t *testRun) own(pkg string) map[string][]string {
	own := map[string][]string{}
	for _, l := range t.output[pkg] {
		own[l.test] = append(own[l.test], l.text)
	}
	return own
}

// unfinished returns, sorted and once each, the tests in the packages that are
// in state whose last event is a start, or a pass or skip with no result line
// in the test's output. go test gives the pass of a binary that exits 0 inside
// a test to that test, so such a pass is not the test's own.
func (t *testRun) unfinished(state string) []string {
	var names []string
	for _, pkg := range t.packages {
		if t.state[pkg.ImportPath] != state {
			continue
		}
		names = append(names, t.tally.Tests(pkg.ImportPath, "")...)
		own := t.own(pkg.ImportPath)
		for _, result := range []string{"pass", "skip"} {
			for _, test := range t.tally.Tests(pkg.ImportPath, result) {
				if _, line, _ := testjson.SplitAtResult(own[test], test); line == nil {
					names = append(names, test)
				}
			}
		}
	}
	slices.Sort(names)
	return slices.Compact(names)
}

// report writes the run's fault, if any, then the state of each package that
// has test files and did not pass with a test, with what it printed unless it
// passed with no test, then what the go command printed outside any package.
// Of a test that passed or skipped it keeps only what followed the test's own
// result line, all of it when there is none: a stopped binary's stacks arrive
// under whichever test reported last.
func (t *testRun) report(b *strings.Builder) {
	if t.fault != "" {
		fmt.Fprintf(b, "the run cannot be read: %s\n", t.fault)
	}
	for _, pkg := range t.packages {
		state := t.state[pkg.ImportPath]
		if state == packagePassed || state == packageNoFiles {
			continue
		}
		fmt.Fprintf(b, "=== %s %s\n", pkg.ImportPath, state)
		if state == packageNoTests {
			continue
		}
		lines, own := t.output[pkg.ImportPath], t.own(pkg.ImportPath)
		// dropped counts, for each test that passed or skipped, its output up to its
		// result line.
		dropped := map[string]int{}
		for _, result := range []string{"pass", "skip"} {
			for _, test := range t.tally.Tests(pkg.ImportPath, result) {
				if before, line, _ := testjson.SplitAtResult(own[test], test); line != nil {
					dropped[test] = len(before) + len(line)
				}
			}
		}
		for _, l := range lines {
			if dropped[l.test] > 0 {
				dropped[l.test]--
				continue
			}
			b.WriteString(l.text)
		}
	}
	b.WriteString(strings.Join(t.plain, ""))
}

// listing returns go list's entries for the named packages, their tests and
// everything those link, under config. A listing of the tree as it was copied
// is kept; one of a changed tree is made each time.
func (w *worker) listing(ctx context.Context, config configuration, patterns []string, changed bool) ([]listed, error) {
	key := config.name + "\x00" + strings.Join(patterns, " ")
	if entries, ok := w.listings[key]; ok && !changed {
		return entries, nil
	}
	args := slices.Concat([]string{"-deps", "-test"}, config.flags, []string{"--"}, patterns)
	entries, err := goList(ctx, w.tree.dir, config.environment(w.tree.run.env), args...)
	if err == nil && !changed {
		w.listings[key] = entries
	}
	return entries, err
}

// compiled reports whether a test binary among entries compiles file, a path
// from the tree's root: the file is among the files go list reports a package
// builds from, an embedded file below the package's directory and a test
// package's test files among them, and a listed test binary links that
// package. known reports whether go list names the file in an entry's files.
func (w *worker) compiled(entries []listed, file string) (compiled, known bool) {
	path := filepath.Join(w.tree.dir, file)
	// go list names each file from its package's directory.
	names := func(e listed, lists ...[]string) bool {
		return slices.ContainsFunc(slices.Concat(lists...), func(name string) bool {
			return filepath.Join(e.Dir, filepath.FromSlash(name)) == path
		})
	}
	var holders []string
	for _, e := range entries {
		if e.Dir == "" || !strings.HasPrefix(path, e.Dir+string(filepath.Separator)) {
			continue
		}
		// A package that fails to load still holds the file: a mutant that
		// brings a broken file into the build is then judged, and reads NOBUILD.
		if names(e, e.GoFiles, e.CgoFiles, e.SFiles, e.CFiles, e.CXXFiles, e.HFiles, e.SysoFiles, e.MFiles, e.FFiles, e.SwigFiles, e.SwigCXXFiles, e.EmbedFiles, e.InvalidGoFiles) {
			holders = append(holders, e.ImportPath)
		}
		if names(e, e.TestGoFiles, e.XTestGoFiles, e.IgnoredGoFiles, e.IgnoredOtherFiles) {
			known = true
		}
	}
	known = known || len(holders) > 0
	// A test binary that fails to load still compiles the file, as one whose
	// test file names no package does: the mutant is judged.
	for _, e := range entries {
		if e.Name == "main" && strings.HasSuffix(e.ImportPath, ".test") &&
			slices.ContainsFunc(holders, func(holder string) bool { return slices.Contains(e.Deps, holder) }) {
			return true, true
		}
	}
	return false, known
}
