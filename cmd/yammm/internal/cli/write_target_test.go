package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const targetPayload = "payload\n"

// writeTargetCase is one kind of target a CLI write can be pointed at, with
// the outcome the operator is owed: what the reader of the target receives,
// what survives, and what an error names.
type writeTargetCase struct {
	name  string
	setup func(dir string) (target string, err error)
	check func(dir, target string, err error) string
}

// writeTargetCases is the outcome table both write primitives are judged by.
// It asserts what a reader of the target sees, never how the bytes got there.
func writeTargetCases() []writeTargetCase {
	return []writeTargetCase{
		{
			name:  "a file that does not exist yet",
			setup: func(dir string) (string, error) { return filepath.Join(dir, "new.txt"), nil },
			check: func(_, target string, err error) string {
				return expectWritten(target, NewFileMode, err)
			},
		},
		{
			name: "an existing file keeps its mode",
			setup: func(dir string) (string, error) {
				target := filepath.Join(dir, "existing.txt")
				// 0o640 on purpose: the property under test is that a mode
				// wider than the default survives the write untouched.
				return target, writeFixture(target, 0o640)
			},
			check: func(_, target string, err error) string {
				return expectWritten(target, 0o640, err)
			},
		},
		{
			name: "a read-only file is refused and kept",
			setup: func(dir string) (string, error) {
				target := filepath.Join(dir, "protected.txt")
				return target, writeFixture(target, 0o400)
			},
			check: func(_, target string, err error) string {
				return expectRefused(target, err, fs.ErrPermission)
			},
		},
		{
			name: "a symlink survives and its target is written",
			setup: func(dir string) (string, error) {
				if err := writeFixture(filepath.Join(dir, "real.txt"), 0o600); err != nil {
					return "", err
				}
				link := filepath.Join(dir, "link.txt")
				return link, os.Symlink("real.txt", link)
			},
			check: func(dir, target string, err error) string {
				if err != nil {
					return "write through a symlink failed: " + err.Error()
				}
				if !isSymlink(target) {
					return "the link was replaced by a regular file"
				}
				if got, _ := os.ReadFile(filepath.Join(dir, "real.txt")); string(got) != targetPayload {
					return fmt.Sprintf("the link's target holds %q, want the payload", got)
				}
				return ""
			},
		},
		{
			name: "a dangling symlink is written through",
			setup: func(dir string) (string, error) {
				link := filepath.Join(dir, "dangling.txt")
				return link, os.Symlink("absent.txt", link)
			},
			check: func(dir, _ string, err error) string {
				if err != nil {
					return "write through a dangling symlink failed: " + err.Error()
				}
				if got, _ := os.ReadFile(filepath.Join(dir, "absent.txt")); string(got) != targetPayload {
					return fmt.Sprintf("the file the link names holds %q, want the payload", got)
				}
				return ""
			},
		},
		{
			name: "a looping symlink is refused",
			setup: func(dir string) (string, error) {
				link := filepath.Join(dir, "loop")
				return link, os.Symlink("loop", link)
			},
			check: func(_, target string, err error) string {
				if err == nil {
					return "a looping symlink was written"
				}
				if !isSymlink(target) {
					return "the looping symlink was replaced"
				}
				return ""
			},
		},
		{
			name: "a chain of two links resolves to the file",
			setup: func(dir string) (string, error) {
				if err := writeFixture(filepath.Join(dir, "real.txt"), 0o600); err != nil {
					return "", err
				}
				if err := os.Symlink("real.txt", filepath.Join(dir, "middle.txt")); err != nil {
					return "", err
				}
				link := filepath.Join(dir, "outer.txt")
				return link, os.Symlink("middle.txt", link)
			},
			check: func(dir, target string, err error) string {
				if err != nil {
					return "write through a chain of links failed: " + err.Error()
				}
				if !isSymlink(target) || !isSymlink(filepath.Join(dir, "middle.txt")) {
					return "a link in the chain was replaced by a regular file"
				}
				return expectPayload(filepath.Join(dir, "real.txt"))
			},
		},
		{
			name: "a relative link into another directory resolves from the link",
			setup: func(dir string) (string, error) {
				for _, sub := range []string{"a", "b"} {
					if err := os.Mkdir(filepath.Join(dir, sub), 0o750); err != nil {
						return "", err
					}
				}
				if err := writeFixture(filepath.Join(dir, "b", "real.txt"), 0o600); err != nil {
					return "", err
				}
				link := filepath.Join(dir, "a", "link.txt")
				return link, os.Symlink(filepath.Join("..", "b", "real.txt"), link)
			},
			check: func(dir, target string, err error) string {
				if err != nil {
					return "write through a relative link failed: " + err.Error()
				}
				if !isSymlink(target) {
					return "the link was replaced by a regular file"
				}
				return expectPayload(filepath.Join(dir, "b", "real.txt"))
			},
		},
		{
			name: "a link to a read-only file is refused and both are kept",
			setup: func(dir string) (string, error) {
				if err := writeFixture(filepath.Join(dir, "real.txt"), 0o400); err != nil {
					return "", err
				}
				link := filepath.Join(dir, "link.txt")
				return link, os.Symlink("real.txt", link)
			},
			check: func(dir, target string, err error) string {
				if !isSymlink(target) {
					return "the link was replaced"
				}
				return expectRefused(filepath.Join(dir, "real.txt"), err, fs.ErrPermission)
			},
		},
		{
			name:  "a path under /dev/ is written through",
			setup: func(string) (string, error) { return os.DevNull, nil },
			check: func(_, target string, err error) string {
				if err != nil {
					return "writing to " + target + " failed: " + err.Error()
				}
				return expectDevice(target)
			},
		},
		{
			name: "a link to a path under /dev/ is written through",
			setup: func(dir string) (string, error) {
				link := filepath.Join(dir, "null")
				return link, os.Symlink(os.DevNull, link)
			},
			check: func(_, target string, err error) string {
				if err != nil {
					return "writing through a link to " + os.DevNull + " failed: " + err.Error()
				}
				if !isSymlink(target) {
					return "the link was replaced by a regular file"
				}
				return expectDevice(os.DevNull)
			},
		},
		{
			name: "a directory is refused",
			setup: func(dir string) (string, error) {
				target := filepath.Join(dir, "adir")
				return target, os.Mkdir(target, 0o750)
			},
			check: func(_, _ string, err error) string {
				if err == nil {
					return "a directory target was written"
				}
				if !strings.Contains(err.Error(), "is a directory") {
					return fmt.Sprintf("error %q does not say the target is a directory", err)
				}
				return ""
			},
		},
		{
			name: "a FIFO receives the bytes and stays a FIFO",
			setup: func(dir string) (string, error) {
				target := filepath.Join(dir, "pipe")
				return target, syscall.Mkfifo(target, 0o600)
			},
			check: func(_, target string, err error) string {
				if err != nil {
					return "writing into a FIFO failed: " + err.Error()
				}
				if info, statErr := os.Lstat(target); statErr != nil || info.Mode()&fs.ModeNamedPipe == 0 {
					return "the FIFO was replaced by a regular file"
				}
				return ""
			},
		},
		{
			name: "a writable file in a read-only directory is refused",
			setup: func(dir string) (string, error) {
				sub := filepath.Join(dir, "ro")
				if err := os.Mkdir(sub, 0o750); err != nil {
					return "", err
				}
				target := filepath.Join(sub, "file.txt")
				if err := writeFixture(target, 0o600); err != nil {
					return "", err
				}
				return target, sealDir(sub)
			},
			check: func(_, target string, err error) string {
				if err == nil {
					return "a target whose directory cannot hold a staging file was written"
				}
				if got, _ := os.ReadFile(target); string(got) != "old\n" {
					return fmt.Sprintf("the file changed to %q on a refused write", got)
				}
				return ""
			},
		},
		{
			name: "a basename near the name limit is written",
			setup: func(dir string) (string, error) {
				return filepath.Join(dir, strings.Repeat("n", 240)+".json"), nil
			},
			check: func(_, target string, err error) string {
				return expectWritten(target, NewFileMode, err)
			},
		},
		{
			name: "an error names the operator's path",
			setup: func(dir string) (string, error) {
				sub := filepath.Join(dir, "sealed")
				if err := os.Mkdir(sub, 0o750); err != nil {
					return "", err
				}
				return filepath.Join(sub, "x.json"), sealDir(sub)
			},
			check: func(_, target string, err error) string {
				if err == nil {
					return "a write into a sealed directory succeeded"
				}
				if !strings.Contains(err.Error(), target) || strings.Contains(err.Error(), ".tmp") {
					return fmt.Sprintf("error %q must name %s and no staging file", err, target)
				}
				return ""
			},
		},
	}
}

// TestWriteFile_TargetKinds judges WriteFile by the outcome table.
func TestWriteFile_TargetKinds(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}

	for _, tc := range writeTargetCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := unsealedTempDir(t)
			target, err := tc.setup(dir)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			received := drainFIFO(target)
			err = WriteFile(target, []byte(targetPayload))
			checkRepairState(t, "WriteFile: "+tc.name, tc.check(dir, target, err)+received(err))
		})
	}
}

// TestStagedFiles_TargetKinds judges a one-file set by the same table. A set
// is replaced by renames, so a target only written through — a FIFO, a device,
// a path under /dev/ — is refused.
func TestStagedFiles_TargetKinds(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}

	for _, tc := range writeTargetCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := unsealedTempDir(t)
			target, err := tc.setup(dir)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			err = stageOne(target, targetPayload)
			var failure string
			switch tc.name {
			case "a FIFO receives the bytes and stays a FIFO",
				"a path under /dev/ is written through",
				"a link to a path under /dev/ is written through":
				if err == nil {
					failure = "a set staged a target no rename can replace"
				}
			default:
				failure = tc.check(dir, target, err)
			}
			checkRepairState(t, "StagedFiles: "+tc.name, failure)
		})
	}
}

// stageOne writes one file through a StagedFiles set and commits it.
func stageOne(target, content string) error {
	set, err := NewStagedFiles(filepath.Dir(target))
	if err != nil {
		return err
	}
	w, err := set.Create(filepath.Base(target))
	if err != nil {
		set.Rollback()
		return err
	}
	if _, err := io.WriteString(w, content); err != nil {
		set.Rollback()
		return err
	}
	return set.Commit()
}

// expectWritten reports the target not holding the payload at mode after a
// successful write.
func expectWritten(target string, mode fs.FileMode, err error) string {
	if err != nil {
		return "write failed: " + err.Error()
	}
	got, readErr := os.ReadFile(target)
	if readErr != nil {
		return "read back: " + readErr.Error()
	}
	if string(got) != targetPayload {
		return fmt.Sprintf("the target holds %q, want the payload", got)
	}
	if info, _ := os.Stat(target); info != nil && info.Mode().Perm() != mode {
		return fmt.Sprintf("mode %v, want %v", info.Mode().Perm(), mode)
	}
	return ""
}

// expectPayload reports path not holding the payload.
func expectPayload(path string) string {
	if got, _ := os.ReadFile(path); string(got) != targetPayload {
		return fmt.Sprintf("%s holds %q, want the payload", filepath.Base(path), got)
	}
	return ""
}

// expectDevice reports path no longer being a device.
func expectDevice(path string) string {
	if info, err := os.Stat(path); err != nil || info.Mode()&fs.ModeDevice == 0 {
		return path + " is no longer a device"
	}
	return ""
}

// TestWriteFile_DescriptorPathContinuesItsStream pins the /dev/ rule where it
// is needed: /dev/fd/N stats as whatever the descriptor holds — a regular file
// here, as /dev/stdout does under a shell redirect — and the bytes must continue
// that stream, in the file the descriptor holds, after what it already wrote.
func TestWriteFile_DescriptorPathContinuesItsStream(t *testing.T) {
	t.Parallel()

	f, err := os.CreateTemp(t.TempDir(), "held-*.txt")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	defer f.Close()
	const earlier = "earlier output\n"
	if _, err := f.WriteString(earlier); err != nil {
		t.Fatalf("write earlier output: %v", err)
	}
	path := fmt.Sprintf("/dev/fd/%d", f.Fd())
	if _, err := os.Stat(path); err != nil {
		t.Skipf("%s is not available here: %v", path, err)
	}
	before, err := os.Stat(f.Name())
	if err != nil {
		t.Fatalf("stat: %v", err)
	}

	if err := WriteFile(path, []byte(targetPayload)); err != nil {
		t.Fatalf("WriteFile(%s): %v", path, err)
	}
	after, err := os.Stat(f.Name())
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !os.SameFile(before, after) {
		t.Error("the file the descriptor holds was replaced, so the descriptor now names a file no path reaches")
	}
	if got, _ := os.ReadFile(f.Name()); string(got) != earlier+targetPayload {
		t.Errorf("the descriptor's file holds %q, want %q: the payload must continue the stream", got, earlier+targetPayload)
	}
}

// expectRefused reports a write that did not fail with want, or one that
// changed the target.
func expectRefused(target string, err, want error) string {
	if err == nil {
		return "the write succeeded"
	}
	if !errors.Is(err, want) {
		return fmt.Sprintf("error %v, want %v", err, want)
	}
	if got, _ := os.ReadFile(target); string(got) != "old\n" {
		return fmt.Sprintf("the target changed to %q on a refused write", got)
	}
	return ""
}

// writeFixture writes the pre-existing content "old\n" at path with mode.
func writeFixture(path string, mode fs.FileMode) error {
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		return err
	}
	return os.Chmod(path, mode)
}

// sealDir removes write permission from dir for the rest of the process.
func sealDir(dir string) error {
	return os.Chmod(dir, 0o550) //nolint:gosec // a directory the test must not be able to write
}

// unsealedTempDir is a TempDir whose sealed subdirectories are made writable
// again before the directory is removed.
func unsealedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Cleanup(func() {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() {
				_ = os.Chmod(path, 0o750) //nolint:gosec // restoring the test's own directories
			}
			return nil
		})
	})
	return dir
}

func isSymlink(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info.Mode()&fs.ModeSymlink != 0
}

// drainFIFO attaches a reader to target when it is a FIFO and returns a
// function reporting what the reader received; for any other target it
// returns a no-op.
func drainFIFO(target string) func(error) string {
	info, err := os.Lstat(target)
	if err != nil || info.Mode()&fs.ModeNamedPipe == 0 {
		return func(error) string { return "" }
	}
	got := make(chan string, 1)
	go func() {
		f, err := os.Open(target)
		if err != nil {
			got <- "reader: " + err.Error()
			return
		}
		defer f.Close()
		b, _ := io.ReadAll(f)
		got <- string(b)
	}()
	return func(writeErr error) string {
		if writeErr != nil {
			return ""
		}
		if received := <-got; received != targetPayload {
			return fmt.Sprintf("; the FIFO's reader received %q, want the payload", received)
		}
		return ""
	}
}
