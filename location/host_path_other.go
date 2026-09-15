//go:build !darwin && !windows

package location

import (
	"fmt"
	"path/filepath"
)

// spellOnDisk returns the existing path p as the filesystem spells it, which
// EvalSymlinks answers on Linux: the host is case-sensitive, and its kernel
// takes a link target's ".." on disk as EvalSymlinks takes it.
func spellOnDisk(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks %q: %w", p, err)
	}
	return resolved, nil
}
