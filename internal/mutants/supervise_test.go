package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// supervised runs the runner, with limit, over this test binary acting as mode,
// or over no binary for the mode "no such binary". It returns the runner's
// status, its record, whether it wrote one, and what it printed.
func supervised(t *testing.T, mode, limit string) (code int, rec record, found bool, stderr string) {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir, wd := t.TempDir(), t.TempDir()
	t.Chdir(wd)
	var errOut bytes.Buffer
	args := []string{self}
	if mode == "no such binary" {
		args = []string{filepath.Join(dir, "no-such-binary")}
	}
	code = supervise(args, append(os.Environ(), recordVar+"="+dir, superviseLimitVar+"="+limit, childVar+"="+mode), &errOut)
	records, err := readRecords(dir)
	if err != nil {
		t.Fatal(err)
	}
	for at, r := range records {
		if !sameDirectory(at, wd) {
			t.Errorf("the record names the directory %s, want %s", at, wd)
		}
		rec, found = r, true
	}
	return code, rec, found, errOut.String()
}

func TestSupervise_RecordsWhatTheBinaryDid(t *testing.T) {
	for _, row := range []struct {
		name, mode, limit string
		code              int
		want              record
		atLeast, atMost   time.Duration
	}{
		{name: "an exit status", mode: "exit3", limit: "1m", code: 3, want: record{Exit: 3}, atMost: 30 * time.Second},
		{name: "a binary at the limit", mode: "sleep", limit: "4s", code: 2, want: record{Exit: 2, TimedOut: true}, atLeast: 4 * time.Second, atMost: 7 * time.Second},
		{name: "a binary that ignores the request for its stacks", mode: "deaf", limit: "1s", code: 1, want: record{Exit: 1, TimedOut: true}, atLeast: time.Second + stopGrace, atMost: 4*time.Second + stopGrace},
		// A binary stopped at the limit fails though it exits 0.
		{name: "a binary that exits 0 at the request for its stacks", mode: "exit0", limit: "2s", code: 1, want: record{Exit: 0, TimedOut: true}, atLeast: 2 * time.Second, atMost: 6 * time.Second},
		{name: "a binary that cannot start", mode: "no such binary", limit: "1m", code: 1, atMost: 30 * time.Second},
	} {
		t.Run(row.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && row.want.TimedOut {
				row.code, row.want.Exit = 1, 1
				row.atLeast, _ = time.ParseDuration(row.limit)
			}
			began := time.Now()
			code, rec, found, stderr := supervised(t, row.mode, row.limit)
			if took := time.Since(began); took < row.atLeast || took > row.atMost {
				t.Errorf("the runner returned after %v, want %v to %v", took, row.atLeast, row.atMost)
			}
			if code != row.code || !found {
				t.Fatalf("the runner exited %d with a record: %t; want %d and a record\n%s", code, found, row.code, stderr)
			}
			if row.mode == "no such binary" {
				if rec.StartError == "" || !strings.Contains(stderr, "no-such-binary") {
					t.Errorf("the record's start error is %q and stderr %q", rec.StartError, stderr)
				}
				return
			}
			if rec.Exit != row.want.Exit || rec.TimedOut != row.want.TimedOut || rec.StartError != "" {
				t.Errorf("the record is %+v, want %+v", rec, row.want)
			}
		})
	}
}

func TestSupervise_RefusesToRunWithoutItsVariables(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, environ := range map[string][]string{
		"no record directory":  {superviseLimitVar + "=1m", childVar + "=exit3"},
		"no limit":             {recordVar + "=" + dir, childVar + "=exit3"},
		"a limit that is none": {recordVar + "=" + dir, superviseLimitVar + "=soon", childVar + "=exit3"},
	} {
		var stderr bytes.Buffer
		if code := supervise([]string{self}, environ, &stderr); code != 2 || !strings.Contains(stderr.String(), "-exec runner") {
			t.Errorf("%s: exit %d, %q; want 2 and a usage line", name, code, stderr.String())
		}
	}
	var stderr bytes.Buffer
	if code := supervise(nil, []string{recordVar + "=" + dir, superviseLimitVar + "=1m"}, &stderr); code != 2 {
		t.Errorf("no binary: exit %d, want 2", code)
	}
	if records, err := readRecords(dir); err != nil || len(records) != 0 {
		t.Errorf("a refused runner left %d records, %v", len(records), err)
	}
}

func TestReadRecords_RefusesTwoBinariesInOneDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"1.json", "2.json", "3.tmp"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(`{"Dir":"/a"}`), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if records, err := readRecords(dir); err == nil || !strings.Contains(err.Error(), "two test binaries ran in /a") {
		t.Errorf("readRecords returned %v, %v", records, err)
	}
	// A name that does not end in .json is a record its runner did not finish.
	if err := os.Remove(filepath.Join(dir, "2.json")); err != nil {
		t.Fatal(err)
	}
	if records, err := readRecords(dir); err != nil || len(records) != 1 {
		t.Errorf("readRecords returned %v, %v; want the one finished record", records, err)
	}
}

// A benchmark binary's stdout and stderr reach go test in the order it wrote
// them, as they do without the runner: go test gives a binary one file or pipe
// for both.
func TestSupervise_KeepsTheOrderOfABenchmarkBinarysOutput(t *testing.T) {
	t.Parallel()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), self, superviseFlag, self, "-test.bench=.")
	cmd.Env = append(os.Environ(), recordVar+"="+t.TempDir(), superviseLimitVar+"=1m", childVar+"=interleave")
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		t.Fatalf("the runner: %v\n%s", err, out.String())
	}
	var want strings.Builder
	for i := range 200 {
		fmt.Fprintf(&want, "out %03d\nerr %03d\n", i, i)
	}
	if out.String() != want.String() {
		t.Errorf("the binary's output reached go test out of order:\n%s", out.String())
	}
}
