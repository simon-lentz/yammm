//go:build !windows

package location

// linkTarget is walkedTarget: a Unix kernel takes a target's ".." on disk.
func linkTarget(dir, target string) string {
	return walkedTarget(dir, target)
}
