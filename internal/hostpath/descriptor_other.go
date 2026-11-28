//go:build !linux

package hostpath

// procDescriptorTable reports false: off Linux no procfs descriptor table is
// judged, as on illumos, Solaris or NetBSD, and no other host yammm ships a
// binary for keeps one.
func procDescriptorTable(string) bool { return false }

// procfsEntries is false: off Linux /dev/fd is no procfs table.
const procfsEntries = false

// ownDescriptorTable reports whether dir is the process's own descriptor
// table, the one /dev/fd reaches.
func ownDescriptorTable(dir string) bool { return sameFile(dir, "/dev/fd") }
