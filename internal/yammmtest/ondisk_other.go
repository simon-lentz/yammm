//go:build !windows

package yammmtest

import "path/filepath"

// afterLink continues a walk past a link in the directory dir whose target is
// target, as a Unix kernel does: an absolute target restarts at its root, a
// relative one goes on from dir, and each ".." is taken on disk as it is reached.
func afterLink(dir, target string, rest []string) (string, []string) {
	if filepath.IsAbs(target) {
		root, parts := rootAndComponents(target)
		return root, append(parts, rest...)
	}
	return dir, append(components(target), rest...)
}
