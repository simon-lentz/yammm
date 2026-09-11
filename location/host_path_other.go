//go:build !darwin

package location

import "io/fs"

// spellOnDisk returns the existing path p as the filesystem spells it, which
// EvalSymlinks answers on every host but darwin. See the package doc.
func spellOnDisk(p string, _ fs.FileMode) (string, error) {
	return evalSymlinks(p)
}
