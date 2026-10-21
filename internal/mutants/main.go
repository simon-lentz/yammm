// Command mutants judges a set of mutants: for each, one change to a copy of a
// checkout, and whether the named test packages then fail.
//
// Build it once from the checkout and run the binary. The process that runs is
// then the one a signal reaches, and no file it runs from is in a tree it
// mutates:
//
//	go build -o DIR/mutants ./internal/mutants
//	DIR/mutants <checkout> <mutants dir> <out dir> [workers, default 4]
//
// # A mutant
//
// Each <mutants dir>/<id>/ holds these files, each a regular file read byte for
// byte; an entry whose name begins with "." is not a mutant:
//
//   - file: the path of a regular file, from the checkout's root, spelled so
//     that the checkout holds a regular file there and the spelling cleans to
//     a path git lists.
//   - search and replace: every match of search in the file becomes replace. An
//     empty search is refused.
//   - spell: the TMPDIR spellings to run under, the words asis and folded
//     separated by spaces, tabs or newlines, at least one and none twice. folded is
//     TMPDIR's last component "T" spelled "t", the same directory only on a
//     volume that folds case; a host without one records NOFOLD.
//   - pkgs, optional: the packages to judge by, as go test takes them, each
//     word matching a package of the checkout. Without it the set is the
//     package in the file's directory, every tracked package whose code or
//     tests import it, and ./docs, which holds the documentation's own tests;
//     a file in a directory that holds no tracked package, as a file a package
//     embeds from below it, needs pkgs.
//
// A mutant directory that breaks a rule is refused by name before anything is
// copied, every such mutant in one report, and the run exits 2.
//
// # A run
//
// The checkout's unstaged tree must be clean, and the out directory empty or
// absent. A checkout is refused when git lists in it a submodule, a nested
// repository as untracked, or a file the program cannot read, as a file git
// marks skip-worktree and the checkout lacks; and when the checkout's
// scripts/packages.sh --check refuses it, as each gate script that takes its
// package list would: for an untracked Go file in a tracked package, ignored or
// not, or one git does not ignore in another directory go reads; a tracked one
// below a go.mod git does not track; a go.work in effect; or no tracked Go file. Each worker copies the files git lists in the checkout, tracked or
// untracked and not ignored, into <out dir>/work/wN with an index of the
// tracked ones, and judges its mutants there one at a time. Before each test
// run it holds the copy to what it copied, each file's bytes and mode and each
// directory's mode, and restores what a test or a stopped binary left changed.
//
// Every go command runs under the toolchain the checkout's go.mod names, with
// the caller's environment and GOFLAGS, less the variables that bind git to one
// repository, this program's own and GOEXPERIMENT, and with GODEBUG's
// gotestjsonbuildtext set to 0, so the go command names a failed build in its
// events. The jsonv2 configuration
// alone sets an experiment, and a run whose go env file sets one is refused, so
// every other configuration builds with the toolchain's default experiments. A
// caller's GOFLAGS reaches every configuration, and its CGO_ENABLED every one
// but race, which sets CGO_ENABLED=1. Every build, listing and test of a worker
// uses a build cache of the run's own; the toolchain and experiment checks,
// scripts/packages.sh --check and the listings that read the mutants run before
// that cache exists, under the caller's. A test run is
// `go test -json -count=1 -failfast=false -p=2` with the -timeout the
// configuration's gate job gives the go test that runs its tests
// (scripts/test.sh's 10m, the mermaid job's 5m, the integration job's 20m), or
// a minute past MUTATE_TIMEOUT when that is longer, so go test waits as long as
// in that job for a binary's output to close whenever the limit leaves room.
// Its test binaries are started by this program, as go test's -exec runner,
// which holds each to MUTATE_TIMEOUT (default 300s; a whole number of seconds,
// minutes or hours up to 24 hours): at the limit the binary is sent SIGQUIT, so
// it prints its stacks, and is killed five seconds later. A host with no
// SIGQUIT kills it at the limit.
//
// Under the plain and jsonv2 configurations a mutant the tests leave SURVIVED
// or NOTEST is judged again by its packages' benchmarks, each run once without
// the race detector, as scripts/test.sh runs them, by
// `go test -json -count=1 -p=2 -run=^$ -bench=. -benchtime=1x -timeout=2m`,
// each binary held to TEST_BENCH_LIMIT (default 2m, read as MUTATE_TIMEOUT is).
// Under -bench go test sets no bound on its wait for a binary's output to close,
// so the runner waits a tenth of the limit, at least five seconds, and then fails
// the binary. A benchmark that fails, a binary held to the limit and one whose
// output stays open each make the mutant KILLED, as each fails the gate. The
// benchmarks run before the change only for a mutant they judge, and must pass.
//
// A file the go command lists in its package is judged under the first
// configuration in which a test binary of the named packages compiles it: the
// plain build, -race, GOEXPERIMENT=jsonv2, -tags=mermaid, then
// -tags=neo4j_integration. A file go list names for no named package and no
// package their tests link is judged under the plain build when it is not Go
// or the go command ignores it: its name, or a directory's on its path, is
// testdata or begins with "." or "_", as a fixture's is. A data file only a
// test behind a build tag reads is therefore judged by tests that do not read
// it.
//
// # Verdicts
//
// A build failure and a timeout rest on what this program holds itself and on
// the result the go command reports for a package (see [testjson]). A package
// fails as the gate's summary fails it: its binary exits non-zero, or exits 0
// and leaves no pass or skip. A binary that exits 0 leaves its own package
// result when its last framing line opened a test, as one that exits inside a
// test does: the go command gives its pass to that test, and the last package
// event the binary printed, or none, stands. So a line the binary prints can
// decide a zero exit's verdict, for this program as for the gate. A skip result
// for a binary that ran is read as a pass, as the gate reads it. Whether a
// passing package ran a test is read from its tests' run events, which a test
// can print, so a printed event can move a mutant from NOTEST to SURVIVED. The
// words, in the order they are decided, but ERROR, which any step after NOFOLD
// can reach:
//
//   - NOFOLD: the spelling is folded and the host has none. No log is written.
//   - NOMATCH: the search is not in the file, or the replacement leaves the
//     file as it was.
//   - NOTCOMPILED: no test binary of the named packages compiles the file on
//     this host, before or after the change, under any configuration, and the
//     file is a Go file the go command does not ignore or a file go list
//     names for a named package or a package their tests link.
//     The tests cannot judge it here.
//   - RED: the named packages' tests do not pass before the change, or, for a
//     mutant the tests leave SURVIVED or NOTEST under a configuration that runs
//     benchmarks, their benchmarks do not.
//   - NOBUILD: the changed tree does not build, or a named package's test
//     binary was not built, failed go test's vet or did not start, or a
//     package whose binary did not run reported no result.
//   - KILLED: a named package's test binary ended in failure by itself, or
//     exited 0 and left a result the gate's summary fails: none or a printed
//     fail, from a binary whose last framing line opened a test, or the go
//     command's fail for one that left its output open; or the benchmark
//     run, after tests that left SURVIVED or NOTEST, failed.
//   - TIMEOUT: no package failed by itself and this program stopped one at the
//     limit, whatever its tests reported before. Run the mutant again alone
//     with a longer MUTATE_TIMEOUT.
//   - SURVIVED: every named package passed or has no test files, and at least
//     one reported a test start.
//   - NOTEST: every named package passed or has no test files, and none
//     reported a test start.
//   - ERROR: the run could not judge the mutant, at whatever step it stopped;
//     its log says why.
//
// # What a run leaves
//
// <out dir>/results.tsv holds one row for each mutant and spelling, written by
// encoding/csv with a tab between cells, under the header id, spelling,
// verdict, seconds, worker, failing tests, packages, configuration. The
// failing tests are the tests go test reported failed in a package that
// failed, and of one whose binary exited 0 the tests still open when it exited:
// those that started and reported nothing after, and those whose pass or skip
// has no result line of their own, which the go command gave them. For TIMEOUT
// they are the tests still open in a package stopped at the limit, in the same
// two kinds. Each is a name a test binary can also print.
// <out dir>/logs/<id>.<spelling>.log names, when a test run decides the
// verdict, each package that has test files and did not pass with a test, and
// holds what each that did not pass printed, less the output of each test that
// passed or skipped up to that test's own result line, when it has one.
// work/worker.N.out and work/results.wN.tsv are each worker's own record; with the logs already written they are all a
// run stopped by a signal leaves, since it writes no results.tsv. The worker
// trees, the runners' records and the build cache are removed at exit; each
// one the run cannot remove is named, and a run that would exit 0 exits 1.
//
// On unix, INT, TERM, HUP and QUIT each stop the run: every running go
// command's process group is sent TERM and killed five seconds later, and a
// second signal changes nothing. On another host an interrupt stops it and
// each running go command is killed. The exit status is 0 when no row is ERROR
// and the run completes; 1 when a row is ERROR or the run itself fails, as when a worker's
// copy or record cannot be written or its tree cannot be removed; 2 for a refusal, or when the program
// cannot find its own path; and 128 plus the signal's number for a run a
// signal stopped.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// superviseFlag is the first argument go test gives this program as its -exec
// runner.
const superviseFlag = "-supervise"

func main() {
	os.Exit(mainStatus(nil))
}

// mainStatus runs the program the arguments ask for, a run over a set or go
// test's -exec runner, and returns its exit status. seed is judgeSet's.
func mainStatus(seed fs.FS) int {
	if len(os.Args) > 1 && os.Args[1] == superviseFlag {
		return supervise(os.Args[2:], os.Environ(), os.Stderr)
	}
	self, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "mutants: %v\n", err)
		return 2
	}
	ctx, stopped := notifyContext(context.Background())
	code := judgeSet(ctx, os.Args[1:], os.Environ(), self, seed, os.Stdout, os.Stderr)
	if sig := stopped(); sig != 0 {
		return 128 + sig
	}
	return code
}

// notifyContext returns a context the first stop signal cancels, and a
// function that returns that signal's number, or 0 when none came. A later
// signal is taken and changes nothing, so it cannot cut the stop short.
func notifyContext(parent context.Context) (ctx context.Context, stopped func() int) {
	ctx, cancel := context.WithCancel(parent)
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, stopSignals()...)
	var mu sync.Mutex
	number := 0
	go func() {
		for sig := range signals {
			mu.Lock()
			if number == 0 {
				number = signalNumber(sig)
				cancel()
			}
			mu.Unlock()
		}
	}()
	return ctx, func() int {
		mu.Lock()
		defer mu.Unlock()
		return number
	}
}

// refusal is an error for which the run exits 2: nothing was copied or judged.
type refusal struct{ message string }

func (r refusal) Error() string { return r.message }

func refusef(format string, args ...any) error {
	return refusal{fmt.Sprintf(format, args...)}
}

// judgeSet judges the mutants args name and returns the exit status. self is
// the program go test starts as its -exec runner. seed, when not nil, holds
// the files the run's build cache starts with.
func judgeSet(ctx context.Context, args, environ []string, self string, seed fs.FS, stdout, stderr io.Writer) int {
	r, err := newRun(ctx, args, environ, self)
	if err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		if _, ok := err.(refusal); ok { //nolint:errorlint // newRun returns a refusal unwrapped
			return 2
		}
		return 1
	}
	status := 1
	if err := copySeed(r.cache, seed); err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
	} else {
		status = r.judge(ctx, stdout, stderr)
	}
	if !r.removeWork(stderr) && status == 0 {
		return 1
	}
	return status
}

// A run is one invocation: its checkout, mutants and out directory, and the
// environment its go and git commands start from.
type run struct {
	checkout string
	out      string
	work     string // out/work: the worker trees, the build cache, the runners' records and each worker's record
	cache    string
	mutants  []mutant
	workers  int
	limit    time.Duration
	bench    time.Duration // the limit of one benchmark binary
	asis     string        // TMPDIR as the host resolves it, with a trailing separator
	folded   string        // the folded spelling, empty on a host without one
	env      []string
	exec     string // go test's -exec value: this program as the runner
	tracked  []string
	others   []string // untracked files git does not ignore
}

func newRun(ctx context.Context, args, environ []string, self string) (*run, error) {
	if len(args) < 3 || len(args) > 4 {
		return nil, refusef("usage: mutants <checkout> <mutants dir> <out dir> [workers]")
	}
	r := &run{workers: 4}
	if len(args) == 4 {
		n, err := strconv.Atoi(args[3])
		if err != nil || n < 1 || strconv.Itoa(n) != args[3] {
			return nil, refusef("workers must be a positive integer, got %q", args[3])
		}
		r.workers = n
	}
	var err error
	if r.limit, err = parseLimit(limitVar, envValue(environ, limitVar), 300*time.Second); err != nil {
		return nil, err
	}
	if r.bench, err = parseLimit(benchLimitVar, envValue(environ, benchLimitVar), benchLimit); err != nil {
		return nil, err
	}
	if r.exec, err = execValue(self); err != nil {
		return nil, err
	}
	if r.checkout, err = realDir(args[0]); err != nil {
		return nil, refusef("the checkout: %v", err)
	}
	mutantsDir, err := realDir(args[1])
	if err != nil {
		return nil, refusef("the mutants directory: %v", err)
	}
	if r.env, err = cleanEnv(ctx, environ); err != nil {
		return nil, err
	}
	if err := r.prepareOut(args[2]); err != nil {
		return nil, err
	}
	if err := r.listFiles(ctx); err != nil {
		return nil, err
	}
	if r.env, err = pinToolchain(ctx, r.checkout, r.env); err != nil {
		return nil, err
	}
	if err := checkExperiments(ctx, r.checkout, r.env); err != nil {
		return nil, err
	}
	if err := r.refuseUntrackedGo(ctx); err != nil {
		return nil, err
	}
	r.asis, r.folded, err = tmpSpellings(envValue(r.env, "TMPDIR"))
	if err != nil {
		return nil, err
	}
	if r.mutants, err = readMutants(ctx, r, mutantsDir); err != nil {
		return nil, err
	}
	for _, dir := range []string{r.work, filepath.Join(r.out, "logs")} {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("the out directory: %w", err)
		}
	}
	r.cache = filepath.Join(r.work, "gocache")
	r.env = setEnv(r.env, "GOCACHE", r.cache)
	return r, nil
}

// The variables that hold the limit of one test binary, and of one benchmark
// binary, which scripts/test.sh reads too.
const (
	limitVar      = "MUTATE_TIMEOUT"
	benchLimitVar = "TEST_BENCH_LIMIT"
)

// parseLimit reads the limit variable name holds, unset when it is empty:
// digits with no leading zero, then s, m or h, from one second up to 24 hours.
func parseLimit(name, value string, unset time.Duration) (time.Duration, error) {
	if value == "" {
		return unset, nil
	}
	units := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour}
	unit, ok := units[value[len(value)-1]]
	n, err := strconv.Atoi(value[:len(value)-1])
	if !ok || err != nil || n < 1 || strconv.Itoa(n) != value[:len(value)-1] || time.Duration(n) > 24*time.Hour/unit {
		return 0, refusef("%s must be a whole number of seconds, minutes or hours from 1 up to 24 hours, with no leading zero, such as 900s or 15m, got %q", name, value)
	}
	return time.Duration(n) * unit, nil
}

// execValue spells self as go test's -exec flag reads it: fields split at
// white space, a field that opens on a quote kept whole to the same quote, and
// a quote inside an unquoted field read as itself.
func execValue(self string) (string, error) {
	for _, quote := range []string{"", "'", `"`} {
		if quote == "" && !strings.ContainsAny(self, " \t\n\r") && !strings.HasPrefix(self, "'") && !strings.HasPrefix(self, `"`) ||
			quote != "" && !strings.Contains(self, quote) {
			return quote + self + quote + " " + superviseFlag, nil
		}
	}
	return "", refusef("this program's path %q holds white space and both quote characters, so go test cannot be given it as its -exec runner", self)
}

// realDir returns dir with every symbolic link resolved.
func realDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("%s: %w", dir, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory", dir)
	}
	return resolved, nil
}

// prepareOut resolves the out directory and refuses one the run cannot own: a
// used directory would keep an earlier run's rows and logs beside this run's.
// An empty one holds no file of the checkout, so leaving it out of every
// worker's copy leaves out nothing git lists.
func (r *run) prepareOut(out string) error {
	// The go command writes a path into files it reads back as text, and
	// cannot open one that is not.
	if !utf8.ValidString(out) {
		return refusef("the out directory %q is not named in UTF-8, and the go command cannot work under such a path", out)
	}
	if err := os.MkdirAll(out, 0o750); err != nil {
		return refusef("the out directory: %v", err)
	}
	var err error
	if r.out, err = realDir(out); err != nil {
		return refusef("the out directory: %v", err)
	}
	entries, err := os.ReadDir(r.out)
	if err != nil {
		return refusef("the out directory: %v", err)
	}
	if len(entries) > 0 {
		return refusef("the out directory %s is not empty: a run takes a directory no run has used", r.out)
	}
	r.work = filepath.Join(r.out, "work")
	return nil
}

// within reports whether dir is root or under it. Each directory is compared
// by identity, so a spelling a case-folding volume accepts does not hide that
// one holds the other.
func within(root, dir string) (bool, error) {
	rootInfo, err := os.Stat(root)
	if err != nil {
		return false, fmt.Errorf("%s: %w", root, err)
	}
	for at := dir; ; at = filepath.Dir(at) {
		info, err := os.Stat(at)
		if err != nil {
			return false, fmt.Errorf("%s: %w", at, err)
		}
		if os.SameFile(rootInfo, info) {
			return true, nil
		}
		if filepath.Dir(at) == at {
			return false, nil
		}
	}
}

// tmpSpellings returns the two spellings of the directory tmp names, each
// with a trailing separator. folded is empty unless the directory's last
// component is "T" and its sibling "t" is the same directory.
func tmpSpellings(tmp string) (asis, folded string, err error) {
	if tmp == "" {
		tmp = os.TempDir()
	}
	resolved, err := realDir(tmp)
	if err != nil {
		return "", "", refusef("TMPDIR: %v", err)
	}
	asis = resolved + string(filepath.Separator)
	if filepath.Base(resolved) != "T" {
		return asis, "", nil
	}
	candidate := filepath.Join(filepath.Dir(resolved), "t")
	realInfo, err := os.Stat(resolved)
	if err != nil {
		return "", "", refusef("TMPDIR: %v", err)
	}
	if info, err := os.Stat(candidate); err == nil && os.SameFile(realInfo, info) {
		folded = candidate + string(filepath.Separator)
	}
	return asis, folded, nil
}

// copySeed copies seed into the run's build cache, when there is one.
func copySeed(cache string, seed fs.FS) error {
	if seed == nil {
		return nil
	}
	if err := os.CopyFS(cache, seed); err != nil {
		return fmt.Errorf("the build cache seed: %w", err)
	}
	return nil
}

// refuseUntrackedGo refuses the checkout scripts/packages.sh --check refuses,
// as each gate script that takes its package list does; the package document
// names its refusals. A checkout without the script is not checked.
func (r *run) refuseUntrackedGo(ctx context.Context) error {
	script := filepath.Join(r.checkout, "scripts", "packages.sh")
	if _, err := os.Lstat(script); errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	cmd := exec.CommandContext(ctx, "bash", script, "--check")
	cmd.Dir = r.checkout
	cmd.Env = r.env
	if out, err := cmd.CombinedOutput(); err != nil {
		return refusef("scripts/packages.sh --check refuses the checkout, as each gate script that takes its package list would (%v):\n%s", err, bytes.TrimSpace(out))
	}
	return nil
}

// removeWork removes what only a running run needs: the worker trees, the
// build cache and the runners' records. It names each directory it cannot
// remove, which the run then leaves behind, and reports whether it removed all.
func (r *run) removeWork(stderr io.Writer) (removed bool) {
	entries, err := os.ReadDir(r.work)
	if err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return false
	}
	removed = true
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if err := removeAll(filepath.Join(r.work, e.Name())); err != nil {
			fmt.Fprintf(stderr, "mutants: %v, which the run leaves behind\n", err)
			removed = false
		}
	}
	return removed
}

// removeAll removes a tree that may hold directories without write permission,
// as a module cache a test made does.
func removeAll(dir string) error {
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil //nolint:nilerr // what cannot be walked is left for RemoveAll to report
		}
		return os.Chmod(path, 0o700) //nolint:gosec // a directory of the run's own, about to be removed
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return nil
}
