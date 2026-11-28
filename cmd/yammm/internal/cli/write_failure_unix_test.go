//go:build darwin || linux

package cli

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

const failedWriteDirEnv = "YAMMM_CLI_FAILED_WRITE_DIR"

// TestWriteFile_AFailedWriteLeavesNoStagingFile holds a write that fails after
// staging, here past the process's file size limit, to an empty directory: the
// staging file is removed. The limit is set in a child process, so this test's
// own output is never cut by it.
func TestWriteFile_AFailedWriteLeavesNoStagingFile(t *testing.T) {
	if dir, ok := os.LookupEnv(failedWriteDirEnv); ok {
		signal.Ignore(syscall.SIGXFSZ)
		lim := syscall.Rlimit{Cur: 4, Max: 4}
		if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &lim); err != nil {
			t.Fatal(err)
		}
		if err := WriteFile(filepath.Join(dir, "out.json"), []byte("0123456789")); err == nil {
			t.Fatal("a write past the file size limit reported success")
		}
		return
	}
	t.Parallel()
	dir := t.TempDir()
	child := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestWriteFile_AFailedWriteLeavesNoStagingFile$", "-test.count=1") //nolint:gosec // runs this test binary again
	child.Env = append(os.Environ(), failedWriteDirEnv+"="+dir)
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("the child write failed: %v\n%s", err, out)
	}
	assertNoDebris(t, dir)
}
