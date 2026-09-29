package cli

import (
	"path/filepath"
	"syscall"
)

// procSuperMagic is PROC_SUPER_MAGIC, procfs's statfs type (linux/magic.h).
const procSuperMagic = 0x9fa0

// procDescriptorTable reports whether dir, as the kernel reaches it, is a
// descriptor table on procfs: /proc/<pid>/fd or /proc/<pid>/task/<tid>/fd,
// which /dev/fd, /proc/self/fd and /proc/thread-self/fd reach. Each entry is a
// link: for a descriptor holding a file its target names that file, which a
// write through the target's text would replace rather than continue, and for
// a pipe or a socket it names no file at all.
func procDescriptorTable(dir string) bool {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil || st.Type != procSuperMagic {
		return false
	}
	resolved, err := filepath.EvalSymlinks(dir)
	return err == nil && filepath.Base(resolved) == "fd"
}
