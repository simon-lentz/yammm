package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The variables a test run gives its -exec runner. The runner keeps them from
// the test binary, so a test that runs this program starts from none.
const (
	recordVar         = "YAMMM_MUTANTS_RECORD" // the directory the runner writes its record in
	superviseLimitVar = "YAMMM_MUTANTS_LIMIT"  // how long the test binary may run
)

// A record is what the runner saw of one test binary: facts no output of the
// binary can change.
type record struct {
	Dir        string // the binary's working directory, its package's
	StartError string // why the binary did not start; empty when it did
	Exit       int    // the binary's own exit status; not 0 when a signal ended it
	TimedOut   bool   // the runner stopped the binary at the limit
	OutputOpen bool   // the binary's output stayed open past outputWait after it exited
}

// supervise runs the test binary args name, as go test's -exec runner. It
// stops a binary that reaches the limit, records what happened, and exits with
// the binary's status, so go test reports the package as it would without it;
// a binary it stopped fails, whatever its status.
// Under -bench go test waits for a binary's output with no bound, so for a
// benchmark run the runner bounds that wait itself and fails a binary whose
// output stays open, as go test fails a test binary's.
func supervise(args, environ []string, stderr io.Writer) int {
	dir, limit := envValue(environ, recordVar), envValue(environ, superviseLimitVar)
	allowed, err := time.ParseDuration(limit)
	if len(args) == 0 || dir == "" || err != nil {
		fmt.Fprintf(stderr, "mutants: %s is go test's -exec runner and needs a test binary, %s and %s\n", superviseFlag, recordVar, superviseLimitVar)
		return 2
	}
	rec := record{}
	if rec.Dir, err = os.Getwd(); err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return 2
	}
	cmd := exec.Command(args[0], args[1:]...) //nolint:gosec,noctx // the test binary go test built; the limit below stops it
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	cmd.Env = slices.DeleteFunc(slices.Clone(environ), func(kv string) bool {
		key, _, _ := strings.Cut(kv, "=")
		return key == recordVar || key == superviseLimitVar
	})
	var out *relayed
	if benchmarks(args[1:]) {
		if out, err = relay(cmd); err != nil {
			fmt.Fprintf(stderr, "mutants: %v\n", err)
			return 2
		}
	}
	err = cmd.Start()
	if out != nil {
		out.started()
	}
	if err != nil {
		rec.StartError = err.Error()
		fmt.Fprintf(stderr, "mutants: %v\n", err)
	} else {
		rec.Exit, rec.TimedOut = waitWithin(cmd, allowed)
	}
	if out != nil && rec.StartError == "" {
		if wait := outputWait(allowed); !out.closedWithin(wait) {
			rec.OutputOpen = true
			fmt.Fprintf(stderr, "mutants: the test binary's output was still open %v after it exited\n", wait)
		}
	}
	if rec.TimedOut {
		fmt.Fprintf(stderr, "mutants: the test binary was stopped at the limit of %v\n", allowed)
	}
	if err := writeRecord(dir, rec); err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return 2
	}
	if rec.StartError != "" {
		return 1
	}
	// A binary that catches the request for its stacks can exit 0.
	if rec.TimedOut || rec.OutputOpen {
		return max(rec.Exit, 1)
	}
	return rec.Exit
}

// benchmarks reports whether go test's arguments to a test binary run a
// benchmark.
func benchmarks(args []string) bool {
	return slices.ContainsFunc(args, func(arg string) bool {
		pattern, ok := strings.CutPrefix(arg, "-test.bench=")
		return ok && pattern != ""
	})
}

// outputWait is how long the runner waits for a benchmark binary's output to
// close after it exits: what go test waits for a test binary's under a
// -timeout of the limit, a tenth of it and at least 5 seconds.
func outputWait(limit time.Duration) time.Duration {
	return max(limit/10, 5*time.Second)
}

// relayed is a command's output, both streams on one pipe of the runner's own,
// copied to the runner's stdout, so they keep the order the binary wrote them
// in, as on the one file or pipe go test gives a binary for both. A process
// the binary leaves holding it holds the runner's pipe and not go test's.
type relayed struct {
	reader, writer *os.File
	copied         chan struct{} // closed when the copy ends: at end of file, or a read or write error
}

func relay(cmd *exec.Cmd) (*relayed, error) {
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("the binary's output: %w", err)
	}
	r := &relayed{reader: reader, writer: writer, copied: make(chan struct{})}
	cmd.Stdout, cmd.Stderr = writer, writer
	go func() {
		_, _ = io.Copy(os.Stdout, reader) //nolint:errcheck // a write error is go test's to report
		close(r.copied)
	}()
	return r, nil
}

// started closes the runner's copy of the pipe's write end, so the reader
// reaches end of file when the binary and what it started close theirs.
func (r *relayed) started() {
	_ = r.writer.Close()
}

// closedWithin reports whether the copy ended within wait, and closes the
// reader when it did not. A copy also ends when a write to the runner's stdout
// fails.
func (r *relayed) closedWithin(wait time.Duration) bool {
	select {
	case <-r.copied:
		return true
	case <-time.After(wait):
		_ = r.reader.Close()
		return false
	}
}

// waitWithin waits for cmd, and stops it when it has run for allowed: it asks
// for the stacks, or kills it on a host with no such request, then kills what
// has not exited stopGrace later.
func waitWithin(cmd *exec.Cmd, allowed time.Duration) (exit int, timedOut bool) {
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	var err error
	select {
	case err = <-done:
	case <-time.After(allowed):
		timedOut = true
		// A kill fails only for a process that has exited, which done then says.
		if dumpStacks(cmd.Process) != nil {
			_ = cmd.Process.Kill() //nolint:errcheck // see above
		}
		select {
		case err = <-done:
		case <-time.After(stopGrace):
			_ = cmd.Process.Kill() //nolint:errcheck // see above
			err = <-done
		}
	}
	switch exitErr, ok := errors.AsType[*exec.ExitError](err); {
	case err == nil:
		return 0, timedOut
	case ok && exitErr.ExitCode() > 0:
		return exitErr.ExitCode(), timedOut
	default:
		// A signal ended it, or its status could not be read.
		return 1, timedOut
	}
}

// writeRecord writes rec under a name of this process's own, whole or not at all.
func writeRecord(dir string, rec record) error {
	content, err := json.Marshal(rec) //nolint:errchkjson // the check holds for a field a later record gains
	if err != nil {
		return fmt.Errorf("the runner's record: %w", err)
	}
	name := filepath.Join(dir, strconv.Itoa(os.Getpid()))
	if err := os.WriteFile(name+".tmp", content, 0o600); err != nil {
		return fmt.Errorf("the runner's record: %w", err)
	}
	if err := os.Rename(name+".tmp", name+".json"); err != nil {
		return fmt.Errorf("the runner's record: %w", err)
	}
	return nil
}

// readRecords returns the records of dir by the directory each binary ran in.
// Two binaries in one directory are an error: go test runs one for a package.
func readRecords(dir string) (map[string]record, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("the runners' records: %w", err)
	}
	records := map[string]record{}
	for _, e := range entries {
		// A runner stopped while it wrote left a name that does not end so.
		if !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("the runners' records: %w", err)
		}
		var rec record
		if err := json.Unmarshal(content, &rec); err != nil {
			return nil, fmt.Errorf("the runners' records: %s: %w", e.Name(), err)
		}
		if _, twice := records[rec.Dir]; twice {
			return nil, fmt.Errorf("the runners' records: two test binaries ran in %s", rec.Dir)
		}
		records[rec.Dir] = rec
	}
	return records, nil
}
