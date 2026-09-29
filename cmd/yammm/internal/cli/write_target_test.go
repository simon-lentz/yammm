package cli

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const targetPayload = "payload\n"

// Why a case or an assertion cannot run on Windows.
const (
	noModeBitsOnWindows       = "Windows has no owner, group or other permission bits: Go reports a writable file as 0666"
	noDirPermissionsOnWindows = "Windows does not honour a directory's permission bits, so a sealed directory still takes a write"
	noNULLinkOnWindows        = "a symlink cannot name NUL, Windows' null device"
)

// writeTargetCase is one kind of target a CLI write can be pointed at, with
// the outcome the operator is owed: what the reader of the target receives,
// what survives, and what an error names.
type writeTargetCase struct {
	name  string
	setup func(dir string) (target string, err error)
	check func(dir, target string, err error) string
	// windowsSkip says why Windows cannot set the case up; empty when it can.
	windowsSkip string
}

// writeTargetCases is the outcome table both write primitives are judged by.
// It asserts what a reader of the target sees, never how the bytes got there.
func writeTargetCases() []writeTargetCase {
	cases := []writeTargetCase{
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
			name:  "the null device is written through",
			setup: func(string) (string, error) { return os.DevNull, nil },
			check: func(_, target string, err error) string {
				if err != nil {
					return "writing to " + target + " failed: " + err.Error()
				}
				return expectDevice(target)
			},
		},
		{
			name:        "a link to a device is written through",
			windowsSkip: noNULLinkOnWindows,
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
			name:        "a writable file in a read-only directory is refused",
			windowsSkip: noDirPermissionsOnWindows,
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
			name:        "an error from a sealed directory names the operator's path",
			windowsSkip: noDirPermissionsOnWindows,
			setup: func(dir string) (string, error) {
				sub := filepath.Join(dir, "sealed")
				if err := os.Mkdir(sub, 0o750); err != nil {
					return "", err
				}
				return filepath.Join(sub, "x.json"), sealDir(sub)
			},
			check: func(_, target string, err error) string {
				return expectErrorNames(target, err)
			},
		},
		{
			name: "an error under a parent that is a file names the operator's path",
			setup: func(dir string) (string, error) {
				parent := filepath.Join(dir, "file.txt")
				return filepath.Join(parent, "x.json"), writeFixture(parent, 0o600)
			},
			check: func(_, target string, err error) string {
				return expectErrorNames(target, err)
			},
		},
	}
	return append(cases, platformTargetCases()...)
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
			if tc.windowsSkip != "" && runtime.GOOS == "windows" {
				t.Skip(tc.windowsSkip)
			}
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

// TestWriteFileSet_TargetKinds judges a one-file set by the same table. A set
// is replaced by renames, so a target only written through — a FIFO, a device,
// a descriptor path — is refused.
func TestWriteFileSet_TargetKinds(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("root ignores the write bit")
	}

	for _, tc := range writeTargetCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.windowsSkip != "" && runtime.GOOS == "windows" {
				t.Skip(tc.windowsSkip)
			}
			dir := unsealedTempDir(t)
			target, err := tc.setup(dir)
			if err != nil {
				t.Fatalf("setup: %v", err)
			}
			err = stageOne(target, targetPayload)
			var failure string
			switch tc.name {
			case "a FIFO receives the bytes and stays a FIFO",
				"the null device is written through",
				"a link to a device is written through":
				if !errors.Is(err, errNotReplaceable) {
					failure = fmt.Sprintf("staging a target no rename can replace: error %v, want %v", err, errNotReplaceable)
				}
			case "an error under a parent that is a file names the operator's path":
				// The set's directory is the operator's path, and creating it fails.
				failure = expectErrorNames(filepath.Dir(target), err)
			default:
				failure = tc.check(dir, target, err)
			}
			checkRepairState(t, "WriteFileSet: "+tc.name, failure)
		})
	}
}

// stageOne writes one file as a set into its directory.
func stageOne(target, content string) error {
	return WriteFileSet(filepath.Dir(target), []NamedFile{{Name: filepath.Base(target), Data: []byte(content)}})
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
	if runtime.GOOS == "windows" {
		return "" // noModeBitsOnWindows: the payload is the whole outcome there.
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

// earlierOutput is what a held descriptor's file holds before a write reaches it.
const earlierOutput = "earlier output\n"

// heldDescriptor opens a regular file for appending, as a shell's ">>" opens
// stdout, writes earlierOutput to it and returns it with the spellings of its
// descriptor's path the tests judge: /dev/fd/N, the same through a link to /dev,
// and those [procDescriptorPaths] returns. It skips where /dev/fd/N does not
// exist.
func heldDescriptor(t *testing.T) (f *os.File, spellings []string) {
	t.Helper()
	dir := t.TempDir()
	f, err := os.OpenFile(filepath.Join(dir, "held.txt"), os.O_CREATE|os.O_EXCL|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Errorf("close %s: %v", f.Name(), err)
		}
	})
	if _, err := f.WriteString(earlierOutput); err != nil {
		t.Fatalf("write earlier output: %v", err)
	}
	fdPath := fmt.Sprintf("/dev/fd/%d", f.Fd())
	if _, err := os.Stat(fdPath); err != nil {
		t.Skipf("%s is not available here: %v", fdPath, err)
	}
	spellings = []string{fdPath}
	devl := filepath.Join(dir, "devl")
	if err := os.Symlink("/dev", devl); err != nil {
		t.Fatalf("link to /dev: %v", err)
	}
	spellings = append(spellings, filepath.Join(devl, "fd", fmt.Sprint(f.Fd())))
	return f, append(spellings, procDescriptorPaths(t, f.Fd())...)
}

// procDescriptorPaths returns every spelling of descriptor fd's path through a
// procfs descriptor table that exists here: /proc/self/fd/N, /proc/<pid>/fd/N,
// /proc/thread-self/fd/N and /proc/<pid>/task/<tid>/fd/N. It returns none
// where there is no procfs.
func procDescriptorPaths(t *testing.T, fd uintptr) []string {
	t.Helper()
	if _, err := os.Stat("/proc/self/fd"); err != nil {
		return nil
	}
	n := fmt.Sprint(fd)
	paths := []string{
		filepath.Join("/proc/self/fd", n),
		filepath.Join("/proc", strconv.Itoa(os.Getpid()), "fd", n),
	}
	thread, err := filepath.EvalSymlinks("/proc/thread-self")
	if err != nil {
		return paths
	}
	return append(paths, filepath.Join("/proc/thread-self/fd", n), filepath.Join(thread, "fd", n))
}

// TestWriteFile_DescriptorPathContinuesItsStream pins the descriptor rule
// where it is needed: /dev/fd/N stats as whatever the descriptor holds — a
// regular file here, as /dev/stdout does under a shell redirect — and the bytes
// must continue that stream, in the file the descriptor holds, after what it
// already wrote, under each spelling [heldDescriptor] returns.
func TestWriteFile_DescriptorPathContinuesItsStream(t *testing.T) {
	t.Parallel()
	_, spellings := heldDescriptor(t)
	for i := range spellings {
		f, paths := heldDescriptor(t)
		path := paths[i]
		before, err := os.Stat(f.Name())
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if err := WriteFile(path, []byte(targetPayload)); err != nil {
			t.Errorf("WriteFile(%s): %v", path, err)
			continue
		}
		if _, err := f.WriteString("later output\n"); err != nil {
			t.Fatalf("write later output: %v", err)
		}
		after, err := os.Stat(f.Name())
		if err != nil {
			t.Fatalf("stat after: %v", err)
		}
		if !os.SameFile(before, after) {
			t.Errorf("WriteFile(%s) replaced the file the descriptor holds, so the descriptor now names a file no path reaches", path)
		}
		if got, _ := os.ReadFile(f.Name()); string(got) != earlierOutput+targetPayload+"later output\n" {
			t.Errorf("after WriteFile(%s) the descriptor's file holds %q, want %q: the payload must continue the stream", path, got, earlierOutput+targetPayload+"later output\n")
		}
	}
}

// TestWriteFileSet_DescriptorPathIsRefused pins the descriptor rule for a set:
// /dev/fd/N stats as the regular file its descriptor holds, and a rename over
// that file would leave the descriptor writing to a file no path reaches.
func TestWriteFileSet_DescriptorPathIsRefused(t *testing.T) {
	t.Parallel()
	f, spellings := heldDescriptor(t)
	for _, path := range spellings {
		if err := stageOne(path, targetPayload); !errors.Is(err, errNotReplaceable) {
			t.Errorf("staging %s: error %v, want %v", path, err, errNotReplaceable)
		}
	}
	if got, _ := os.ReadFile(f.Name()); string(got) != earlierOutput {
		t.Errorf("the descriptor's file holds %q after a refused set, want %q", got, earlierOutput)
	}
}

// devDirectory returns a new directory under Linux's /dev/shm, a directory in
// /dev that holds regular files, skipping where /dev/shm is missing or cannot be
// written.
func devDirectory(t *testing.T) string {
	t.Helper()
	info, err := os.Stat("/dev/shm")
	if err != nil || !info.IsDir() {
		t.Skip("this host has no /dev/shm")
	}
	dir, err := os.MkdirTemp("/dev/shm", "yammm-test-*") //nolint:usetesting // t.TempDir cannot place a directory under /dev/shm
	if err != nil {
		t.Skipf("/dev/shm is not writable here: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Errorf("remove %s: %v", dir, err)
		}
	})
	return dir
}

// TestWriteFile_ARegularFileUnderDevIsReplaced pins that a path under /dev is
// decided by its file unless it names a descriptor: a regular file there is
// replaced like any other, and a file that does not exist there is created.
func TestWriteFile_ARegularFileUnderDevIsReplaced(t *testing.T) {
	t.Parallel()
	dir := devDirectory(t)
	paths := []string{filepath.Join(dir, "new.json")}
	// "1" and "stdout" are a descriptor's names in another directory.
	for _, name := range []string{"existing.json", "1", "stdout"} {
		existing := filepath.Join(dir, name)
		if err := os.WriteFile(existing, []byte(earlierOutput), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, existing)
	}
	for _, path := range paths {
		if err := WriteFile(path, []byte(targetPayload)); err != nil {
			t.Errorf("WriteFile(%s): %v", path, err)
			continue
		}
		if got := expectPayload(path); got != "" {
			t.Error(got)
		}
	}
}

// TestWriteFileSet_WritesIntoADirectoryUnderDev pins the same rule for a set:
// a directory under /dev that holds regular files takes a set of them.
func TestWriteFileSet_WritesIntoADirectoryUnderDev(t *testing.T) {
	t.Parallel()
	dir := devDirectory(t)
	if err := os.WriteFile(filepath.Join(dir, "a.csv"), []byte(earlierOutput), 0o600); err != nil {
		t.Fatal(err)
	}
	files := []NamedFile{{Name: "a.csv", Data: []byte(targetPayload)}, {Name: "b.csv", Data: []byte(targetPayload)}}
	if err := WriteFileSet(dir, files); err != nil {
		t.Fatalf("WriteFileSet(%s): %v", dir, err)
	}
	for _, nf := range files {
		if got := expectPayload(filepath.Join(dir, nf.Name)); got != "" {
			t.Error(got)
		}
	}
}

// expectErrorNames reports a write that succeeded, or an error that does not
// name path or that names a staging file.
func expectErrorNames(path string, err error) string {
	if err == nil {
		return "the write succeeded"
	}
	if !strings.Contains(err.Error(), path) || strings.Contains(err.Error(), ".tmp") {
		return fmt.Sprintf("error %q must name %s and no staging file", err, path)
	}
	return ""
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
