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

// ResolveHostPath returns p as the filesystem spells it: absolute, clean,
// symlink-resolved, each existing component in its on-disk spelling. The
// result is a host path, not an identity; pass it to [NewCanonicalPath] for
// that. A path that does not exist yet resolves as far as it exists and keeps
// its missing tail as typed; a path under a regular file is refused. It
// returns [ErrEmptyPath], [ErrInvalidUTF8Path], or the filesystem's own error.
// The package documentation states the rules.
func ResolveHostPath(p string) (string, error) {
	return resolveHostPath(p, false)
}

// resolveHostPath is ResolveHostPath, where requireExist refuses a path whose
// leaf is absent rather than keeping it.
func resolveHostPath(p string, requireExist bool) (string, error) {
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
	for current := abs; ; {
		info, err := os.Stat(current)
		switch {
		case err == nil:
			// Windows reports a path under a regular file absent rather than
			// ENOTDIR, so this is where its half of that refusal lands.
			if len(missing) > 0 && !info.IsDir() {
				return "", fmt.Errorf("resolve %q: %q is not a directory", p, current)
			}
			spelled, err := spellOnDisk(current, info.Mode())
			if err != nil {
				return "", fmt.Errorf("resolve %q: %w", p, err)
			}
			slices.Reverse(missing)
			return filepath.Join(append([]string{spelled}, missing...)...), nil
		case errors.Is(err, fs.ErrNotExist) && !requireExist:
			parent := filepath.Dir(current)
			if parent == current {
				// filepath.Dir is its own fixed point at a volume root.
				return abs, nil
			}
			missing = append(missing, filepath.Base(current))
			current = parent
		default:
			return "", fmt.Errorf("resolve %q: %w", p, err)
		}
	}
}

// evalSymlinks is filepath.EvalSymlinks with the path in its error, which is
// what a caller of a canonicalizer needs to see.
func evalSymlinks(p string) (string, error) {
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks %q: %w", p, err)
	}
	return resolved, nil
}
