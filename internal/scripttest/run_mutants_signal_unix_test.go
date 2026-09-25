//go:build unix

package scripttest

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sleepingGo is a go whose every go test run that is not a pre-build records
// its PID under the worker tree's name and sleeps, so a worker stays busy
// until the test signals the run. In a worker tree named by GO_SHIM_DEAF it
// ignores SIGTERM first, which the sleep it execs keeps.
const sleepingGo = `#!/usr/bin/env bash
if [ "${1:-}" = "test" ]; then
	case " $* " in
	*" -exec=true "*) ;;
	*)
		w=$(basename "$PWD")
		case " ${GO_SHIM_DEAF:-} " in *" $w "*) trap '' TERM ;; esac
		printf '%s\n' "$$" >"$GO_SHIM_SLEEPERS/$w"
		exec sleep 60
		;;
	esac
fi
exec "$GO_SHIM_REAL" "$@"
`

// graceFloor is the least time run_mutants.sh gives a signalled group before
// it sends SIGKILL: five seconds as bash counts SECONDS, in whole seconds.
const graceFloor = 4 * time.Second

// signalOutlastsTheRun bounds the time from the signal to the run's exit. It
// is well under the shim's 60-second sleep, which a run that waits for its
// workers' children to finish outlasts.
const signalOutlastsTheRun = 30 * time.Second

// signalCase is one signalled run over two workers.
type signalCase struct {
	// both makes the second worker's mutant run a go test that sleeps too;
	// otherwise it matches nothing and the worker finishes at once.
	both bool
	// deaf names the worker trees whose go test ignores SIGTERM.
	deaf string
	// again sends a second SIGTERM one second after the first.
	again bool
}

// signalledRun is what a signalled run did.
type signalledRun struct {
	elapsed  time.Duration
	output   string
	kills    []groupKill
	sleepers map[string]int // worker tree name to its sleeping go test
	groups   map[string]int // worker tree name to that go test's process group
	work     string
}

// groupKill is one signal the run sent to a process group.
type groupKill struct {
	signal string
	group  int
}

// signalRun runs run_mutants.sh over two mutants with two workers and sends
// SIGTERM once every worker still running sleeps in its go test and every
// other worker has exited and been reaped. It records what the run did until
// it exited.
func signalRun(t *testing.T, c signalCase) signalledRun {
	t.Helper()
	f := runMutantsFixture(t)
	f.writeMutant("a_slow", "a - b")
	running := []string{"w1"}
	if c.both {
		f.writeMutant("b_slow", "b - a")
		running = append(running, "w2")
	} else {
		f.writeMutantSearching("b_nomatch", "a * b", "a - b")
	}
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "go"), []byte(sleepingGo), 0o700); err != nil { //nolint:gosec // a test shim the fixture executes
		t.Fatal(err)
	}
	sleepers := t.TempDir()
	env, log := killLogger(t)
	f.env = append(f.env, env...)
	f.env = append(f.env,
		"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		"GO_SHIM_REAL="+real,
		"GO_SHIM_SLEEPERS="+sleepers,
		"GO_SHIM_DEAF="+c.deaf,
	)
	t.Cleanup(func() {
		for _, w := range running {
			if pid, ok := readPID(filepath.Join(sleepers, w)); ok {
				_ = syscall.Kill(pid, syscall.SIGKILL)
			}
		}
	})

	// The out directory is outside the checkout: each worker copies the
	// checkout, and an out directory inside it carries the earlier workers'
	// copies into the later ones.
	out := t.TempDir()
	//nolint:gosec // runs the repository's script, copied into the fixture's own module
	cmd := exec.CommandContext(t.Context(), "bash", filepath.Join(f.dir, "scripts", "run_mutants.sh"), ".", "mutants", out, "2")
	cmd.Dir = f.dir
	cmd.Env = append(withoutRepositoryVars(t, fixtureEnv()), f.env...)
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Minute)
	pids := map[string]int{}
	for {
		for _, w := range running {
			if pid, ok := readPID(filepath.Join(sleepers, w)); ok {
				pids[w] = pid
			}
		}
		second, _ := os.ReadFile(filepath.Join(out, "work", "worker.2.out"))
		finished := c.both || bytes.Contains(second, []byte("NOMATCH"))
		// A worker prints its verdict before it exits, so the run is signalled
		// only once the main shell's children are the sleeping workers alone.
		if len(pids) == len(running) && finished && len(children(t, cmd.Process.Pid)) == len(running) {
			break
		}
		if time.Now().After(deadline) {
			stopRun(t, cmd, pids)
			t.Fatalf("the run never had %v sleeping alone\n%s", running, output.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	groups := map[string]int{}
	for w, pid := range pids {
		group, ok := processGroup(t, pid)
		if !ok {
			stopRun(t, cmd, pids)
			t.Fatalf("%s's go test (PID %d) is gone before the signal\n%s", w, pid, output.String())
		}
		groups[w] = group
	}
	signalled := time.Now()
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatal(err)
	}
	if c.again {
		time.Sleep(time.Second)
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatalf("the run exited within a second of the first signal: %v\n%s", err, output.String())
		}
	}
	// A run that never exits is stopped here, its workers with it, rather than
	// left to the package timeout, which would kill this binary and orphan them.
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	var waitErr error
	select {
	case waitErr = <-exited:
	case <-time.After(2 * signalOutlastsTheRun):
		stopRun(t, cmd, pids)
		<-exited
		t.Fatalf("the run had not exited %v after the signal\n%s", 2*signalOutlastsTheRun, output.String())
	}
	elapsed := time.Since(signalled)
	var exit *exec.ExitError
	if !errors.As(waitErr, &exit) || exit.ExitCode() != 143 {
		t.Fatalf("run ended with %v, want exit 143\n%s", waitErr, output.String())
	}

	// The script resolves its arguments with pwd -P, and a temporary directory
	// on darwin is reached through a symlink.
	dir, err := filepath.EvalSymlinks(out)
	if err != nil {
		t.Fatal(err)
	}
	return signalledRun{
		elapsed:  elapsed,
		output:   output.String(),
		kills:    groupKills(t, log),
		sleepers: pids,
		groups:   groups,
		work:     filepath.Join(dir, "work"),
	}
}

// stopRun kills a run that failed the test, with the process group of each
// go test in pids that still runs: SIGKILL to the main shell alone skips the
// cleanup that would stop its workers. A group this binary belongs to is
// skipped, which is where every worker is when the script gives none a group.
func stopRun(t *testing.T, cmd *exec.Cmd, pids map[string]int) {
	t.Helper()
	own, _ := processGroup(t, os.Getpid())
	for _, pid := range pids {
		if g, ok := processGroup(t, pid); ok && g != own {
			_ = syscall.Kill(-g, syscall.SIGKILL)
		}
	}
	_ = cmd.Process.Kill()
}

// readPID reads the PID the go shim recorded.
func readPID(path string) (int, bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	return pid, err == nil
}

// children returns the PIDs of the processes whose parent is pid, an
// unreaped one included.
func children(t *testing.T, pid int) []int {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	var kids []int
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == strconv.Itoa(pid) {
			if kid, err := strconv.Atoi(fields[0]); err == nil {
				kids = append(kids, kid)
			}
		}
	}
	return kids
}

// processGroup returns the process group of pid as ps reports it, and false
// when ps lists no such process.
func processGroup(t *testing.T, pid int) (int, bool) {
	t.Helper()
	out, err := exec.CommandContext(t.Context(), "ps", "-A", "-o", "pid=,pgid=").Output()
	if err != nil {
		t.Fatalf("ps: %v", err)
	}
	for line := range strings.Lines(string(out)) {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == strconv.Itoa(pid) {
			if group, err := strconv.Atoi(fields[1]); err == nil {
				return group, true
			}
		}
	}
	return 0, false
}

// groupKills reads the kill log. A signal-0 probe sends nothing and is left
// out; any other kill must name a process group, as kill -SIG -- -PGID. Once a
// probe finds a group empty, no later kill may name it: its ID may since have
// been given to another process.
func groupKills(t *testing.T, log string) []groupKill {
	t.Helper()
	b, err := os.ReadFile(log)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var kills []groupKill
	empty := map[int]bool{}
	for line := range strings.Lines(string(b)) {
		fields := strings.Fields(line)
		var group int
		if len(fields) == 4 && fields[2] == "--" && strings.HasPrefix(fields[3], "-") {
			group, err = strconv.Atoi(fields[3][1:])
		}
		if group <= 0 || err != nil {
			t.Errorf("the run signalled something other than a process group: %q", line)
			continue
		}
		if empty[group] {
			t.Errorf("the run named group %d after a probe found it empty: %q", group, line)
		}
		if fields[1] == "-0" {
			empty[group] = fields[0] != "0"
			continue
		}
		kills = append(kills, groupKill{signal: fields[1], group: group})
	}
	return kills
}

// wantStopped checks what every signalled run owes: it sends each running
// worker's group the signals want names for that worker and nothing else, it
// exits within the bound, and once it has exited every sleeping go test, every
// member of those groups, the cache and the worker trees are gone.
func (r signalledRun) wantStopped(t *testing.T, want map[string][]string) {
	t.Helper()
	if r.elapsed > signalOutlastsTheRun {
		t.Errorf("the run exited %v after the signal, want under %v: it waited for a worker's child\n%s", r.elapsed, signalOutlastsTheRun, r.output)
	}
	sent := map[string][]string{}
	for _, k := range r.kills {
		worker := ""
		for w, g := range r.groups {
			if g == k.group {
				worker = w
			}
		}
		if worker == "" {
			t.Errorf("the run sent %s to group %d, which no running worker leads: a finished worker's PID may name another process", k.signal, k.group)
			continue
		}
		sent[worker] = append(sent[worker], k.signal)
	}
	for w, signals := range want {
		if strings.Join(sent[w], " ") != strings.Join(signals, " ") {
			t.Errorf("the run sent %q to %s's group, want %q", sent[w], w, signals)
		}
	}
	for w, pid := range r.sleepers {
		if syscall.Kill(pid, 0) == nil {
			t.Errorf("%s's go test (PID %d) still runs after the run exited", w, pid)
		}
		if syscall.Kill(-r.groups[w], 0) == nil {
			t.Errorf("%s's process group %d still has a member after the run exited", w, r.groups[w])
		}
	}
	for _, gone := range []string{"gocache", "w1", "w2"} {
		if _, err := os.Stat(filepath.Join(r.work, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("work/%s survives the signal: stat says %v", gone, err)
		}
	}
}

// needsRsync skips a test on a host with no rsync, which run_mutants.sh uses
// to give each worker its own copy.
func needsRsync(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("rsync"); err != nil {
		t.Skip("needs rsync, which run_mutants.sh uses to give each worker its own copy")
	}
}

// A signalled run stops the running worker with every process it started,
// removes its cache and worker trees, and exits with the signal's status. A
// group that empties on SIGTERM is not waited on for the grace. The second
// worker has finished and been reaped when the signal comes, and is not
// signalled: its PID may already belong to another process.
func TestRunMutantsScript_StopsItsWorkersWhenSignalled(t *testing.T) {
	t.Parallel()
	needsRsync(t)

	r := signalRun(t, signalCase{})

	r.wantStopped(t, map[string][]string{"w1": {"-TERM"}})
	if r.elapsed >= graceFloor {
		t.Errorf("the run exited %v after the signal, want under %v: it waited out the grace on an empty group", r.elapsed, graceFloor)
	}
}

// A worker's process that ignores SIGTERM is killed once the grace ends, and
// not before, so the run still stops everything it started before it removes
// its trees.
func TestRunMutantsScript_KillsAWorkerThatIgnoresTheSignal(t *testing.T) {
	t.Parallel()
	needsRsync(t)

	r := signalRun(t, signalCase{deaf: "w1"})

	r.wantStopped(t, map[string][]string{"w1": {"-TERM", "-KILL"}})
	if r.elapsed < graceFloor {
		t.Errorf("the run exited %v after the signal, want at least %v: the grace was cut short", r.elapsed, graceFloor)
	}
}

// Every running worker is stopped: a group that empties on SIGTERM is not
// killed, and each group that ignores it is killed once the grace ends.
func TestRunMutantsScript_StopsEveryRunningWorker(t *testing.T) {
	t.Parallel()
	needsRsync(t)
	for _, tc := range []struct {
		name string
		deaf string
		want map[string][]string
	}{
		{"one ignores the signal", "w2", map[string][]string{"w1": {"-TERM"}, "w2": {"-TERM", "-KILL"}}},
		{"both ignore the signal", "w1 w2", map[string][]string{"w1": {"-TERM", "-KILL"}, "w2": {"-TERM", "-KILL"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r := signalRun(t, signalCase{both: true, deaf: tc.deaf})

			r.wantStopped(t, tc.want)
			if r.elapsed < graceFloor {
				t.Errorf("the run exited %v after the signal, want at least %v: the grace was cut short", r.elapsed, graceFloor)
			}
		})
	}
}

// A second signal during the stop is ignored: the run still kills what
// ignores SIGTERM and removes its trees.
func TestRunMutantsScript_FinishesItsStopWhenSignalledAgain(t *testing.T) {
	t.Parallel()
	needsRsync(t)

	r := signalRun(t, signalCase{deaf: "w1", again: true})

	r.wantStopped(t, map[string][]string{"w1": {"-TERM", "-KILL"}})
}

// processGroupAwk is an awk that logs its own process group and its parent's
// before it runs.
const processGroupAwk = `#!/usr/bin/env bash
printf '%s %s\n' "$(ps -o pgid= -p "$$" | tr -d ' ')" "$(ps -o pgid= -p "$PPID" | tr -d ' ')" >>"$AWK_SHIM_LOG"
exec "$AWK_SHIM_REAL" "$@"
`

// Job control is on only while the workers start: every command the run runs
// after them, such as the summary's awk, stays in its parent's process group.
func TestRunMutantsScript_KeepsJobControlToTheWorkerLaunch(t *testing.T) {
	t.Parallel()
	needsRsync(t)
	f := runMutantsFixture(t)
	f.writeMutant("m01", "a - b")
	real, err := exec.LookPath("awk")
	if err != nil {
		t.Fatal(err)
	}
	shim := t.TempDir()
	if err := os.WriteFile(filepath.Join(shim, "awk"), []byte(processGroupAwk), 0o700); err != nil { //nolint:gosec // a test shim the fixture executes
		t.Fatal(err)
	}
	log := filepath.Join(shim, "groups")
	f.env = append(f.env,
		"PATH="+shim+string(os.PathListSeparator)+os.Getenv("PATH"),
		"AWK_SHIM_REAL="+real,
		"AWK_SHIM_LOG="+log,
	)

	r := f.run("run_mutants.sh", ".", "mutants", "out", "1")

	r.wantCode(t, 0)
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the run ran no awk: %v", err)
	}
	for line := range strings.Lines(string(b)) {
		if fields := strings.Fields(line); len(fields) != 2 || fields[0] != fields[1] {
			t.Errorf("an awk ran in a process group of its own (own, parent's): %q", line)
		}
	}
}
