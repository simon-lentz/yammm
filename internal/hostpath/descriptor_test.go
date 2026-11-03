package hostpath

import (
	"fmt"
	"os"
	"path/filepath"
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
