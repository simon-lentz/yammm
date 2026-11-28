package hostpath

import (
	"path/filepath"
	"syscall"
)

// procfsEntries is whether /dev/fd is a procfs table, whose entries are named
// in one decimal form alone.
const procfsEntries = true

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

// ownDescriptorTable reports whether dir is the process's own descriptor
// table: the one /dev/fd reaches, or a table of one of its threads,
// /proc/<pid>/task/<tid>/fd, which /proc/thread-self/fd reaches. Go's threads
// share the process's one table.
func ownDescriptorTable(dir string) bool {
	if sameFile(dir, "/dev/fd") {
		return true
	}
	if !procDescriptorTable(dir) {
		return false
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return false
	}
	task := filepath.Dir(filepath.Dir(resolved))
	return filepath.Base(task) == "task" && sameFile(filepath.Dir(task), "/proc/self")
}
