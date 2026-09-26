package location

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"unicode/utf8"
)

// maxLinkHops bounds how many dangling links one resolution follows. The kernel
// refuses a cycle with ELOOP before the walk sees it; the bound is for links
// rewritten while the walk runs.
const maxLinkHops = 255

// errTooManyLinks refuses a walk that followed more than maxLinkHops links.
var errTooManyLinks = errors.New("too many levels of symbolic links")

// ResolveHostPath returns p as the filesystem spells it: absolute, clean,
// symlink-resolved, each existing component in its on-disk spelling. The
// result is a host path, not an identity; [SourceIDFromPath] turns p into one.
// The package documentation states what it answers for a path that does not
// exist, one the process cannot traverse and one that can never exist.
func ResolveHostPath(p string) (string, error) {
	return resolveHostPath(p, false)
}

// ResolveSourcePath returns the identity of p and the host path it was derived
// from, out of one resolution, so the two cannot disagree. It is
// [SourceIDFromPath] for a caller that also reads the file, and it fails where
// [ResolveHostPath] does.
func ResolveSourcePath(p string) (SourceID, string, error) {
	host, err := resolveHostPath(p, false)
	if err != nil {
		return SourceID{}, "", err
	}
	return SourceID{cp: identityOf(host)}, host, nil
}

// resolveHostPath is ResolveHostPath, where requireExist refuses a path whose
// leaf the process cannot find rather than keeping it as typed.
func resolveHostPath(p string, requireExist bool) (string, error) {
	host, err := walkHostPath(p, requireExist)
	if err == nil && !utf8.ValidString(host) {
		// The working directory, a name on disk or a dangling link's target
		// can hold bytes the typed path does not.
		return "", fmt.Errorf("%w: %q resolves to %q", ErrInvalidUTF8Path, p, host)
	}
	return host, err
}

// walkHostPath resolves p as resolveHostPath describes, before the result is
// judged.
func walkHostPath(p string, requireExist bool) (string, error) {
	if p == "" {
		return "", ErrEmptyPath
	}
	if !utf8.ValidString(p) {
		return "", fmt.Errorf("%w: %q", ErrInvalidUTF8Path, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("absolute path: %w", err)
	}

	var missing []string
	hops := 0
	current := abs
	for {
		info, err := os.Stat(current)
		if err == nil {
			// Windows reports a path under a regular file absent rather than
			// ENOTDIR, so this is where its half of that refusal lands.
			if len(missing) > 0 && !info.IsDir() {
				return "", fmt.Errorf("resolve %q: %q is not a directory", p, current)
			}
			spelled, serr := spellOnDisk(current)
			if serr == nil {
				return joinMissing(spelled, missing), nil
			}
			// Removed between the stat and the spelling: walk on as absent.
			err = serr
		}
		if requireExist {
			return "", fmt.Errorf("resolve %q: %w", p, err)
		}
		switch {
		case errors.Is(err, fs.ErrNotExist):
			// os.Stat follows links, so a dangling link reads as absent;
			// resolving its target keeps its identity fixed as the target appears.
			target, isLink, lerr := danglingTarget(current)
			if lerr != nil {
				return "", fmt.Errorf("resolve %q: %w", p, lerr)
			}
			if isLink {
				if hops++; hops > maxLinkHops {
					return "", fmt.Errorf("resolve %q: %w", p, errTooManyLinks)
				}
				current = target
				continue
			}
		case errors.Is(err, fs.ErrPermission):
			// The process cannot look past this component, so the rest keeps
			// the spelling it was given, as a path that does not exist does.
		default:
			return "", fmt.Errorf("resolve %q: %w", p, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// filepath.Dir is its own fixed point at a volume root.
			return joinMissing(current, missing), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// danglingTarget reports whether p is a symbolic link and, when it is, the
// path the host's kernel reaches through it, by that host's rule for a ".."
// in a target. p's directory exists, because p's own entry does.
func danglingTarget(p string) (string, bool, error) {
	info, err := os.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("lstat %q: %w", p, err)
	case info.Mode()&fs.ModeSymlink == 0:
		return "", false, nil
	}
	target, err := os.Readlink(p)
	if err != nil {
		return "", false, fmt.Errorf("read link %q: %w", p, err)
	}
	if target == "" {
		return "", false, nil
	}
	dir, err := spellOnDisk(filepath.Dir(p))
	if err != nil {
		return "", false, fmt.Errorf("resolve the directory of link %q: %w", p, err)
	}
	resolved, err := linkTarget(dir, target)
	if err != nil {
		return "", false, err
	}
	return resolved, true, nil
}

// lexicalTarget resolves the target of a link in the directory dir as the
// Windows kernel does: the target replaces the link in the path's text, and
// the text's ".." is evaluated before any further component is looked up.
func lexicalTarget(dir, target string) string {
	switch {
	case filepath.IsAbs(target):
		return filepath.Clean(target)
	case os.IsPathSeparator(target[0]):
		return filepath.Join(filepath.VolumeName(dir)+string(filepath.Separator), target)
	default:
		return filepath.Join(dir, target)
	}
}

// joinMissing appends the components a resolution could not look up, in the
// order they were typed, to the part it spelled.
func joinMissing(spelled string, missing []string) string {
	parts := make([]string, 0, len(missing)+1)
	parts = append(parts, spelled)
	for _, m := range slices.Backward(missing) {
		parts = append(parts, m)
	}
	return filepath.Join(parts...)
}
