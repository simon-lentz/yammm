//go:build !darwin && !windows

package hostpath

import (
	"fmt"
	"path/filepath"
)

// spellOnDisk returns the existing path p as filepath.EvalSymlinks spells it,
// on Linux and every other Unix host but darwin: it keeps the typed case, so on a
// case-folding filesystem, or a bind mount of one, each spelling of a file has
// its own answer, and it takes a ".." on disk as the kernel takes it.
func spellOnDisk(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks %q: %w", p, err)
	}
	return resolved, nil
}
