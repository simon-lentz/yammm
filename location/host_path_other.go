//go:build !darwin

package location

import (
	"fmt"
	"path/filepath"
)

// spellOnDisk returns the existing path p as the filesystem spells it, which
// EvalSymlinks answers on every host but darwin: Linux is case-sensitive, and
// Go's Windows implementation spells each component through FindFirstFile.
func spellOnDisk(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks %q: %w", p, err)
	}
	return resolved, nil
}
