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
	Exit       int    // the binary's exit status; not 0 when a signal ended it
	TimedOut   bool   // the runner stopped the binary at the limit
}

// supervise runs the test binary args name, as go test's -exec runner. It
// stops a binary that reaches the limit, records what happened, and exits with
// the binary's status, so go test reports the package as it would without it.
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
	if err := cmd.Start(); err != nil {
		rec.StartError = err.Error()
		fmt.Fprintf(stderr, "mutants: %v\n", err)
	} else {
		rec.Exit, rec.TimedOut = waitWithin(cmd, allowed)
	}
	if err := writeRecord(dir, rec); err != nil {
		fmt.Fprintf(stderr, "mutants: %v\n", err)
		return 2
	}
	if rec.StartError != "" {
		return 1
	}
	return rec.Exit
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
