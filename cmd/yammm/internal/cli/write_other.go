//go:build !linux

package cli

// procDescriptorTable reports false: off Linux no procfs descriptor table is
// judged, as on illumos, Solaris or NetBSD, and no other host yammm ships a
// binary for keeps one.
func procDescriptorTable(string) bool { return false }
