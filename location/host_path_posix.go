//go:build !windows

package location

import (
	"os"
	"path/filepath"
	"strings"
)

// linkTarget is walkedTarget: a Unix kernel takes a target's ".." on disk.
func linkTarget(dir, target string) string {
	return walkedTarget(dir, target)
}

// walkedTarget resolves the target of a link in the directory dir as a Unix
// kernel does: an absolute target from its root and a relative one from dir,
// each ".." taking the parent of the directory reached so far on disk.
func walkedTarget(dir, target string) string {
	if filepath.IsAbs(target) {
		volume := filepath.VolumeName(target)
		return walkTarget(volume+string(filepath.Separator), target[len(volume):])
	}
	return walkTarget(dir, target)
}

// walkTarget joins target's components to start, an existing directory that
// holds no link, so ".." takes the parent the kernel reaches. From the first
// component that is not an existing directory, the rest is joined as written.
func walkTarget(start, target string) string {
	current := start
	components := strings.FieldsFunc(target, func(r rune) bool {
		return r == '/' || r == filepath.Separator
	})
	for i, c := range components {
		switch c {
		case ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		next := filepath.Join(current, c)
		if info, err := os.Stat(next); err == nil && info.IsDir() {
			if spelled, err := spellOnDisk(next); err == nil {
				current = spelled
				continue
			}
		}
		return strings.Join(append([]string{next}, components[i+1:]...), string(filepath.Separator))
	}
	return current
}
