package hostpath

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// Descriptor takes a descriptor path however the kernel reaches its
// directory: through two "..", the second after a link, each taken on disk;
// through a ".." climbing out of a working directory entered through a link;
// through a link to /dev, which the text never spells; and on Linux through
// every descriptor table procfs holds. Any other path under /dev, and a
// directory named fd anywhere else, is decided by its file.
func TestDescriptor_SeesAPathTheKernelTakesIntoDev(t *testing.T) {
	if resolved, err := filepath.EvalSymlinks("/usr"); err != nil || resolved != "/usr" {
		t.Skip("/usr is not a directory directly under /")
	}
	if info, err := os.Stat("/dev"); err != nil || !info.IsDir() {
		t.Skip("this host has no /dev")
	}
	root := t.TempDir()
	sep := string(filepath.Separator)
	if err := os.Mkdir(filepath.Join(root, "a"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "fd"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, target := range map[string]string{"u": "/usr", "devl": "/dev", "lnk": "/usr"} {
		if err := os.Symlink(target, filepath.Join(root, name)); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	descriptors := []string{
		root + sep + "a" + sep + ".." + sep + "u" + sep + ".." + sep + "dev" + sep + "stdout",
		root + sep + "a" + sep + ".." + sep + "u" + sep + ".." + sep + "dev" + sep + "fd" + sep + "0",
		root + sep + "devl" + sep + "stderr",
		// On Linux /dev/fd links out of /dev, so the link to /dev reaches /proc/self/fd.
		root + sep + "devl" + sep + "fd" + sep + "0",
		"/dev/stdin",
		"/dev/fd/12",
	}
	if paths := procDescriptorPaths(t, 0); paths != nil {
		descriptors = append(descriptors, paths...)
		// A link named other than fd, to a descriptor table /dev/fd is not.
		if err := os.Symlink("/proc/thread-self/fd", filepath.Join(root, "tfd")); err != nil {
			t.Fatal(err)
		}
		descriptors = append(descriptors, root+sep+"tfd"+sep+"0")
	}
	for _, p := range descriptors {
		if !Descriptor(p) {
			t.Errorf("Descriptor(%q) = false, where the kernel reaches a descriptor path", p)
		}
	}
	for _, p := range []string{
		root + sep + "a" + sep + "stdout",
		"/dev/null",
		"/dev/shm/x",
		"/dev/stdout.json",
		"/dev/fd",
		"/dev/fd/1a",
		"/dev/fd/-1",
		root + sep + "devl" + sep + "null",
		root + sep + "devl" + sep + "shm" + sep + "stdout",
		root + sep + "fd" + sep + "3",
		"/proc/self/fdinfo/0",
	} {
		if Descriptor(p) {
			t.Errorf("Descriptor(%q) = true for a path its file decides", p)
		}
	}
	t.Chdir(filepath.Join(root, "lnk"))
	if rel := "." + sep + ".." + sep + "dev" + sep + "stdout"; !Descriptor(rel) {
		t.Errorf("Descriptor(%q) from %s = false, where the kernel climbs from /usr into /dev", rel, filepath.Join(root, "lnk"))
	}
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

// A descriptor path in another case on a case-folding /dev names the descriptor
// the kernel reaches, so its name is spelled from the directory that judged it:
// one identity for /dev/stdin however it is typed.
func TestResolve_SpellsADescriptorPathFromItsDirectory(t *testing.T) {
	t.Parallel()
	for typed, want := range map[string]string{"/DEV/stdin": "/dev/stdin", "/DEV/FD/0": "/dev/fd/0", "/dev/FD/2": "/dev/fd/2"} {
		if _, err := os.Stat(filepath.Dir(typed)); err != nil {
			continue
		}
		if got, err := Resolve(typed); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, want)
		}
	}
}

// TestResolve_NamesEachDescriptorOnce holds every path the kernel reads as one
// descriptor to one name: a number with a leading zero, which darwin's fdesc
// reads as the number, and on Linux the process's own procfs table, which
// /dev/fd reaches. A number procfs does not name is no descriptor there.
func TestResolve_NamesEachDescriptorOnce(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	f, err := os.Open(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	n := fmt.Sprint(f.Fd())
	want := "/dev/fd/" + n
	typed := []string{want}
	if _, err := os.Stat("/dev/fd/0" + n); err == nil {
		typed = append(typed, "/dev/fd/0"+n)
	} else if Descriptor("/dev/fd/0" + n) {
		t.Errorf("Descriptor(%q) = true, where the kernel answers %v", "/dev/fd/0"+n, err)
	}
	if paths := procDescriptorPaths(t, f.Fd()); paths != nil {
		typed = append(typed, paths[:2]...)
	}
	for _, p := range typed {
		if got, err := Resolve(p); err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", p, got, err, want)
		}
	}
}

// TestResolve_ADescriptorPathNamesTheFileItHolds holds every answer for a
// descriptor path to one that opens the file the descriptor holds: one reached
// through a link to /dev takes /dev/fd's name, a directory's descriptor
// whatever the directory is called, and on Linux another process's entry,
// which this process's /dev/fd does not hold, keeps its spelling.
func TestResolve_ADescriptorPathNamesTheFileItHolds(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("Windows has no descriptor paths")
	}
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, "held"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	symlink(t, "/dev", filepath.Join(dir, "devl"))
	want := fmt.Sprintf("/dev/fd/%d", f.Fd())
	if got, err := Resolve(filepath.Join(dir, "devl", "fd", fmt.Sprint(f.Fd()))); err != nil || got != want {
		t.Errorf("Resolve(devl/fd/%d) = %q, %v; want %q", f.Fd(), got, err, want)
	}

	named := filepath.Join(dir, "d")
	mkdirAll(t, named)
	d, err := os.Open(named)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	heldDir, err := d.Stat()
	if err != nil {
		t.Fatal(err)
	}
	typed := fmt.Sprintf("/dev/fd/%d", d.Fd())
	if got, err := Resolve(typed); err != nil || !opensAs(t, got, heldDir) {
		t.Errorf("Resolve(%q) for the descriptor of a directory named d = %q, %v; want a path that opens it", typed, got, err)
	}

	if runtime.GOOS != "linux" {
		return
	}
	other, err := os.Create(filepath.Join(dir, "other"))
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	heldOther, err := other.Stat()
	if err != nil {
		t.Fatal(err)
	}
	holder := exec.CommandContext(t.Context(), "sleep", "60")
	holder.ExtraFiles = []*os.File{other}
	if err := holder.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		holder.Process.Kill() //nolint:gosec // ending the test's own process
		holder.Wait()         //nolint:gosec // its exit is the kill's
	})
	entry := fmt.Sprintf("/proc/%d/fd/3", holder.Process.Pid)
	if got, err := Resolve(entry); err != nil || got != entry || !opensAs(t, got, heldOther) {
		t.Errorf("Resolve(%q) for another process's descriptor = %q, %v; want its own spelling, which opens the file it holds", entry, got, err)
	}
}

// TestDescriptorNumber_AThreadTableHoldsTheProcesssDescriptors holds every
// procfs spelling of the process's own table to its descriptors, a thread's
// table among them: the process's threads share one table.
func TestDescriptorNumber_AThreadTableHoldsTheProcesssDescriptors(t *testing.T) {
	t.Parallel()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	paths := procDescriptorPaths(t, 0)
	if paths == nil {
		t.Skip("needs a procfs descriptor table")
	}
	for _, p := range paths {
		if n, own := DescriptorNumber(p); !own || n != 0 {
			t.Errorf("DescriptorNumber(%q) = %d, %t; want 0, true", p, n, own)
		}
	}
}

// TestResolve_AMissingDescriptorEntryIsNamedAsAnExistingOne holds a name no
// descriptor holds to its table's name, as an existing entry takes it: procfs
// names no "06", and /proc/self/fd/06 resolves to /dev/fd/06, never to a name
// holding the process ID.
func TestResolve_AMissingDescriptorEntryIsNamedAsAnExistingOne(t *testing.T) {
	t.Parallel()
	if procDescriptorPaths(t, 0) == nil {
		t.Skip("needs a procfs descriptor table")
	}
	for _, typed := range []string{"/dev/fd/06", "/proc/self/fd/06", "/proc/" + strconv.Itoa(os.Getpid()) + "/fd/06"} {
		if got, err := Resolve(typed); err != nil || got != "/dev/fd/06" {
			t.Errorf("Resolve(%q) = %q, %v; want %q", typed, got, err, "/dev/fd/06")
		}
	}
}
