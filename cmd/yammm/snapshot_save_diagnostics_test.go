package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// TestSnapshotSave_ExtensionWarningDescribesAWrittenFile: the warning's
// registered meaning is that a snapshot WAS written to a path an
// extension-discovering reader will not find, so it names that file and is
// raised only once the write has succeeded.
func TestSnapshotSave_ExtensionWarningDescribesAWrittenFile(t *testing.T) {
	t.Parallel()

	t.Run("a failed write raises no extension warning", func(t *testing.T) {
		t.Parallel()
		// A parent path that is a regular file fails the write on every host and for root.
		parent := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(parent, nil, 0o600); err != nil {
			t.Fatalf("write the parent file: %v", err)
		}
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", filepath.Join(parent, "o.dat"),
			"testdata/valid.yammm", "testdata/data.json")
		var failure string
		switch {
		case code != cli.ExitRuntime:
			failure = fmt.Sprintf("exit code = %d, want %d", code, cli.ExitRuntime)
		case strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION"):
			failure = "a snapshot that was not written is reported as written to a non-.ys path:\n" + stderr
		}
		checkRepairState(t, "snapshot save: a failed write raises no extension warning", failure)
	})

	t.Run("the warning names the output path", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "made.dat")
		_, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
			"testdata/valid.yammm", "testdata/data.json")
		var failure string
		if !strings.Contains(stderr, "made.dat: warning[W_SNAPSHOT_PATH_EXTENSION]") {
			failure = "the warning does not locate itself at the file it describes:\n" + stderr
		}
		checkRepairState(t, "snapshot save: the extension warning names the output path", failure)
	})

	t.Run("a .ys link to a descriptor path warns", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" {
			t.Skip("Windows has no descriptor paths")
		}
		dir := t.TempDir()
		stream, err := os.Create(filepath.Join(dir, "r.bin"))
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()
		targets := map[string]string{"fd.ys": fmt.Sprintf("/dev/fd/%d", stream.Fd())}
		if runtime.GOOS == "linux" {
			// A thread's table holds the process's own descriptors.
			targets["thread.ys"] = fmt.Sprintf("/proc/thread-self/fd/%d", stream.Fd())
		}
		for name, target := range targets {
			link := filepath.Join(dir, name)
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", link,
				"testdata/valid.yammm", "testdata/data.json")
			if code != cli.ExitOK || !strings.Contains(stderr, name+": warning[W_SNAPSHOT_PATH_EXTENSION]") {
				t.Errorf("a .ys link to %s: exit %d, stderr:\n%s\nwant exit 0 and the extension warning: a scan in another process cannot reach the stream", target, code, stderr)
			}
		}
	})

	t.Run("a .ys link to another process's descriptor raises no warning", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS != "linux" {
			t.Skip("only procfs names another process's descriptor")
		}
		dir := t.TempDir()
		held, err := os.Create(filepath.Join(dir, "held.ys"))
		if err != nil {
			t.Fatal(err)
		}
		defer held.Close()
		holder := exec.CommandContext(t.Context(), "sleep", "60")
		holder.ExtraFiles = []*os.File{held}
		if err := holder.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			holder.Process.Kill() //nolint:gosec // ending the test's own process
			holder.Wait()         //nolint:gosec // its exit is the kill's
		})
		entry := fmt.Sprintf("/proc/%d/fd/3", holder.Process.Pid)
		link := filepath.Join(dir, "x.ys")
		if err := os.Symlink(entry, link); err != nil {
			t.Fatal(err)
		}
		for _, out := range []string{link, entry} {
			code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
				"testdata/valid.yammm", "testdata/data.json")
			if code != cli.ExitOK || strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION") {
				t.Errorf("-o %s, another process's descriptor of a .ys file: exit %d, stderr:\n%s\nwant exit 0 and no warning: a scan reads the file it holds", out, code, stderr)
			}
		}
	})

	t.Run("a .ys output in a directory a scan cannot list warns", func(t *testing.T) {
		t.Parallel()
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs a Unix directory mode the process obeys")
		}
		dir := filepath.Join(t.TempDir(), "d")
		if err := os.Mkdir(dir, 0o300); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.Chmod(dir, 0o700) }) //nolint:gosec // restoring the test's own directory
		code, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", filepath.Join(dir, "x.ys"),
			"testdata/valid.yammm", "testdata/data.json")
		if code != cli.ExitOK || !strings.Contains(stderr, "warning[W_SNAPSHOT_PATH_EXTENSION]") {
			t.Errorf("-o x.ys in a directory of mode 0300: exit %d, stderr:\n%s\nwant exit 0 and the extension warning", code, stderr)
		}
		link := filepath.Join(t.TempDir(), "x.bin")
		if err := os.Symlink(filepath.Join(dir, "y.ys"), link); err != nil {
			t.Fatal(err)
		}
		code, _, stderr = executeCmdOutput(t, "snapshot", "save", "-o", link,
			"testdata/valid.yammm", "testdata/data.json")
		if code != cli.ExitOK || !strings.Contains(stderr, "warning[W_SNAPSHOT_PATH_EXTENSION]") {
			t.Errorf("-o a link into a directory of mode 0300: exit %d, stderr:\n%s\nwant exit 0 and the extension warning", code, stderr)
		}
	})

	t.Run("a .ys output raises no warning", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "made.ys")
		_, _, stderr := executeCmdOutput(t, "snapshot", "save", "-o", out,
			"testdata/valid.yammm", "testdata/data.json")
		if strings.Contains(stderr, "W_SNAPSHOT_PATH_EXTENSION") {
			t.Errorf("a .ys output drew the extension warning:\n%s", stderr)
		}
	})
}

// TestSnapshotSaveHelp_StatesWhatIntoCarries: the Long text says when a
// created_at is written, and --into carrying the merged file's own is one of
// the two ways.
func TestSnapshotSaveHelp_StatesWhatIntoCarries(t *testing.T) {
	t.Parallel()

	long := newSnapshotSaveCmd().Long
	var failure string
	if !strings.Contains(long, "--into") || !strings.Contains(long, "created_at") {
		failure = "the Long text does not say that --into carries created_at forward:\n" + long
	}
	checkRepairState(t, "snapshot save: the Long text states what --into carries", failure)
}
