//go:build !windows

package hostpath

import (
	"fmt"
	"path/filepath"
	"strings"
)

// linkTarget is walkedTarget: a Unix kernel takes a target's ".." on disk.
func linkTarget(dir, target string) (string, error) {
	return walkedTarget(dir, target)
}

// walkedTarget resolves the target of a link in the directory dir as a Unix
// kernel does: an absolute target from its root and a relative one from dir.
// A target with no ".." is joined on its text, and the walk spells what of it
// exists. Each ".." takes the parent of the directory reached on disk, so the
// part of a target up to its last ".." is spelled once by the host's speller,
// which takes a ".." as the kernel does. The speller's refusal refuses the
// target with its error: a missing component, through which the kernel reaches
// no file, or a directory on the way whose resolved form is past the host's
// path limit, which the kernel enters and the speller cannot write.
func walkedTarget(dir, target string) (string, error) {
	start := dir
	if filepath.IsAbs(target) {
		start = filepath.VolumeName(target) + string(filepath.Separator)
	}
	components := strings.FieldsFunc(target, isSeparator)
	cut := 0
	for i, c := range components {
		if c == ".." {
			cut = i + 1
		}
	}
	if cut == 0 {
		return filepath.Join(append([]string{start}, components...)...), nil
	}
	head := start + string(filepath.Separator) + strings.Join(components[:cut], string(filepath.Separator))
	spelled, err := spellOnDisk(head)
	if err != nil {
		return "", fmt.Errorf("link target %q: %w", target, err)
	}
	return filepath.Join(append([]string{spelled}, components[cut:]...)...), nil
}

// lookupName is p: off Windows a lookup reads the path as typed.
func lookupName(p string) string { return p }
