//go:build windows

package yammmtest

import (
	"os"
	"path/filepath"
)

// afterLink continues a walk past a link in the directory dir whose target is
// target, as the Windows kernel does: the target replaces the link in the
// path's text, the text's ".." is evaluated, and the walk restarts from the
// root of that text. A rooted target names the link's volume.
func afterLink(dir, target string, rest []string) (string, []string) {
	var resolved string
	switch {
	case filepath.IsAbs(target):
		resolved = filepath.Clean(target)
	case target != "" && os.IsPathSeparator(target[0]):
		resolved = filepath.Join(volumeRoot(dir), target)
	default:
		resolved = filepath.Join(dir, target)
	}
	root, parts := rootAndComponents(resolved)
	return root, append(parts, rest...)
}
