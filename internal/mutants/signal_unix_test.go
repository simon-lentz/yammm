//go:build unix

package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// sleepingTest writes its process ID to the directory FX_PIDS names and
// sleeps. With FX_DEAF set it first ignores TERM, the signal a stopping run
// sends each go command's process group.
const sleepingTest = "package m\n\nimport (\n\t\"os\"\n\t\"os/signal\"\n\t\"path/filepath\"\n\t\"strconv\"\n\t\"syscall\"\n\t\"testing\"\n\t\"time\"\n)\n\n" +
	"func TestAdd(t *testing.T) {\n\tif os.Getenv(\"FX_DEAF\") != \"\" {\n\t\tsignal.Ignore(syscall.SIGTERM)\n\t}\n" +
	"\tif err := os.WriteFile(filepath.Join(os.Getenv(\"FX_PIDS\"), strconv.Itoa(os.Getpid())), nil, 0o600); err != nil {\n\t\tt.Fatal(err)\n\t}\n" +
	"\ttime.Sleep(time.Hour)\n\t_ = Add(1, 2)\n}\n"

// alive reports whether a process has the ID. A process that has exited and
// that nothing has reaped does not count.
func alive(pid int) bool {
	if syscall.Kill(pid, 0) != nil {
		return false
	}
	out, _ := exec.CommandContext(context.Background(), "ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output() //nolint:gosec // ps, with a process ID
	return len(bytes.TrimSpace(out)) > 0 && !bytes.HasPrefix(bytes.TrimSpace(out), []byte("Z"))
}

// writeFile gives a file its mode whatever the process's umask. Not parallel:
// the umask is the process's.
func TestWriteFile_GivesTheModeWhateverTheUmask(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	path := filepath.Join(t.TempDir(), "run.sh")
	if err := writeFile(path, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o755 {
		t.Errorf("the file: %v, %v; want mode 0755", info, err)
	}
}

// A file git lists untracked is read by the copy as a tracked one is, and is
// refused before anything is copied when it cannot be. git lists no FIFO, so
// one is no part of a copy and stops nothing.
func TestRun_RefusesAnUntrackedFileTheCopyCannotRead(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("needs a file its owner cannot read")
	}
	f := newFixture(t)
	path := filepath.Join(f.checkout, "untracked")
	if err := os.WriteFile(path, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")

	r := f.run()

	r.wantCode(t, 2)
	r.wantStderr(t, "the checkout holds untracked, which cannot be read: ")
	r.wantNothingStarted(t)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "mkfifo", path).CombinedOutput(); err != nil {
		t.Fatalf("mkfifo: %v\n%s", err, out)
	}
	r = f.run()
	r.wantCode(t, 0)
	r.wantVerdicts(t, map[string]string{"m01": verdictKilled})
}

// A tracked file the program cannot reach is refused as unreadable, not as a
// submodule: git diff reads no change through a directory it cannot search.
func TestRun_RefusesATrackedFileItCannotReach(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("needs a directory its owner cannot search")
	}
	f := newFixture(t)
	f.write("b/b.go", "package b\n")
	f.index()
	dir := filepath.Join(f.checkout, "b")
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o750) }) //nolint:gosec // restoring the test's own directory
	f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")

	r := f.run()

	r.wantCode(t, 2)
	r.wantStderr(t, "the checkout holds b/b.go, which cannot be read: ")
	r.wantNothingStarted(t)
}

// A copied file a test left unreadable through an access list, its mode as it
// was, is removed and copied again.
func TestTree_RestoreCopiesAgainAFileAnAccessListLeftUnreadable(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		t.Skip("needs darwin's chmod +a and a user it binds")
	}
	tree := copied(t)
	at := filepath.Join(tree.dir, "m", "m.go")
	if out, err := exec.CommandContext(t.Context(), "chmod", "+a", "everyone deny read", at).CombinedOutput(); err != nil {
		t.Fatalf("chmod +a: %v\n%s", err, out)
	}
	if differed, err := tree.restore(t.Context()); err != nil || !slices.Equal(differed, []string{"m/m.go"}) {
		t.Errorf("restore reports %q, %v; want m/m.go", differed, err)
	}
	if again, err := tree.restore(t.Context()); err != nil || len(again) != 0 {
		t.Errorf("the restored tree differs: %q, %v", again, err)
	}
}

// A copied file a test changed and made one the copy may not remove is
// written back in place; one it may neither remove nor read cannot be put
// back, and restore says why rather than blaming the checkout.
func TestTree_RestoreWritesInPlaceAFileItCannotRemove(t *testing.T) {
	t.Parallel()
	if runtime.GOOS != "darwin" || os.Geteuid() == 0 {
		t.Skip("needs darwin's chmod +a and a user it binds")
	}
	for name, deny := range map[string]string{"delete": "everyone deny delete", "read and delete": "everyone deny read,delete"} {
		tree := copied(t)
		at := filepath.Join(tree.dir, "m", "m.go")
		if err := os.WriteFile(at, []byte("package m\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.CommandContext(t.Context(), "chmod", "+a", deny, at).CombinedOutput(); err != nil {
			t.Fatalf("chmod +a: %v\n%s", err, out)
		}
		t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "chmod", "-N", at).Run() })
		differed, err := tree.restore(t.Context())
		switch name {
		case "delete":
			if err != nil || !slices.Equal(differed, []string{"m/m.go"}) {
				t.Errorf("%s: restore reports %q, %v; want m/m.go put back", name, differed, err)
			}
		default:
			if err == nil || !strings.Contains(err.Error(), "m/m.go cannot be read") {
				t.Errorf("%s: restore returned %v; want m/m.go named unreadable", name, err)
			}
		}
	}
	// A link cannot be written in place: the removal that failed is named.
	tree := copied(t)
	link := filepath.Join(tree.dir, "m", "link.go")
	if err := os.Remove(link); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("m_test.go", link); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.CommandContext(t.Context(), "chmod", "-h", "+a", "everyone deny delete", link).CombinedOutput(); err != nil {
		t.Fatalf("chmod -h +a: %v\n%s", err, out)
	}
	t.Cleanup(func() { _ = exec.CommandContext(context.Background(), "chmod", "-h", "-N", link).Run() })
	if _, err := tree.restore(t.Context()); err == nil || !strings.Contains(err.Error(), "copying the checkout: remove ") {
		t.Errorf("a link: restore returned %v; want the removal that failed", err)
	}
}

// Each signal stops the run: every test binary ends, the trees and the cache
// go, and the run exits with the signal's status. A binary that ignores the
// stop is killed when the grace ends, and a second signal changes nothing.
func TestProgram_StopsEveryWorkerAtASignal(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name   string
		signal syscall.Signal
		deaf   bool
		again  syscall.Signal // a second signal, sent a second later
	}{
		{name: "INT", signal: syscall.SIGINT},
		{name: "TERM", signal: syscall.SIGTERM},
		{name: "HUP", signal: syscall.SIGHUP},
		{name: "QUIT", signal: syscall.SIGQUIT},
		{name: "TERM to a binary that ignores it", signal: syscall.SIGTERM, deaf: true},
		{name: "INT twice to a binary that ignores TERM", signal: syscall.SIGINT, deaf: true, again: syscall.SIGINT},
		{name: "INT then TERM to a binary that ignores TERM", signal: syscall.SIGINT, deaf: true, again: syscall.SIGTERM},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f := newFixture(t)
			f.write("m/m_test.go", sleepingTest)
			f.index()
			f.mutant("m01", "m/m.go", "a + b", "a - b", "./m/")
			f.mutant("m02", "m/m.go", "a + b", "b - a", "./m/")
			pids, seedDir := t.TempDir(), t.TempDir()
			if err := os.CopyFS(seedDir, seed(t)); err != nil {
				t.Fatal(err)
			}
			self, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			args := f.args("2")
			cmd := exec.CommandContext(t.Context(), self, args...)
			cmd.Env = append(f.environment(), asProgramVar+"=1", seedVar+"="+seedDir, "FX_PIDS="+pids)
			if row.deaf {
				cmd.Env = append(cmd.Env, "FX_DEAF=1")
			}
			var stdout, stderr bytes.Buffer
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			exited := make(chan error, 1)
			go func() { exited <- cmd.Wait() }()
			var running []int
			for began := time.Now(); len(running) < 2; time.Sleep(100 * time.Millisecond) {
				entries, _ := os.ReadDir(pids)
				running = running[:0]
				for _, e := range entries {
					pid, _ := strconv.Atoi(e.Name())
					running = append(running, pid)
				}
				select {
				case err := <-exited:
					t.Fatalf("the run ended before both workers ran a test: %v\nstdout:\n%s\nstderr:\n%s", err, &stdout, &stderr)
				default:
				}
				if time.Since(began) > 5*time.Minute {
					_ = cmd.Process.Kill()
					t.Fatalf("%d of 2 workers ran a test in five minutes\nstderr:\n%s", len(running), &stderr)
				}
			}

			signalled := time.Now()
			if err := cmd.Process.Signal(row.signal); err != nil {
				t.Fatal(err)
			}
			if row.again != 0 {
				time.Sleep(time.Second)
				if err := cmd.Process.Signal(row.again); err != nil {
					t.Fatal(err)
				}
			}
			err = <-exited
			took := time.Since(signalled)

			exit, ok := errors.AsType[*exec.ExitError](err)
			if !ok || exit.ExitCode() != 128+int(row.signal) {
				t.Errorf("the run ended with %v, want exit status %d\nstderr:\n%s", err, 128+int(row.signal), &stderr)
			}
			if row.deaf && took < stopGrace-time.Second {
				t.Errorf("the run ended %v after the signal, before the grace of a binary that ignores it", took)
			}
			// Without a binary that ignores TERM, nothing waits out the grace.
			if !row.deaf && row.again == 0 && took >= stopGrace {
				t.Errorf("the run ended %v after the signal, which is the grace a binary that ignores TERM gets", took)
			}
			for _, pid := range running {
				if alive(pid) {
					t.Errorf("test binary %d outlives the run", pid)
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
			entries, err := os.ReadDir(filepath.Join(args[2], "work"))
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if e.IsDir() {
					t.Errorf("the stopped run left %s", e.Name())
				}
			}
			if _, err := os.Stat(filepath.Join(args[2], "results.tsv")); err == nil {
				t.Error("the stopped run wrote results.tsv")
			}
			if !bytes.Contains(stderr.Bytes(), []byte("stopped by a signal")) {
				t.Errorf("stderr does not say the run was stopped:\n%s", &stderr)
			}
		})
	}
}
