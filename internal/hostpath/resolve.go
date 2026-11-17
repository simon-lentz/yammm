package hostpath

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"unicode/utf8"
)

// maxLinkHops bounds how many dangling links one resolution follows. The kernel
// refuses a cycle with ELOOP before the walk sees it; the bound is for links
// rewritten while the walk runs.
const maxLinkHops = 255

// errTooManyLinks refuses a walk that followed more than maxLinkHops links.
var errTooManyLinks = errors.New("too many levels of symbolic links")

// ErrEmptyPath refuses an empty path: a caller that never set the file name.
// filepath.Abs("") names the working directory, so without it a directory
// would get a valid-looking identity for a file that was never named. Its text
// is location.ErrEmptyPath's, the public name it is returned under.
var ErrEmptyPath = errors.New("location: path is empty")

// ErrInvalidUTF8Path refuses a path that is not valid UTF-8, or that resolves to
// one. Its text is location.ErrInvalidUTF8Path's, the public name it is
// returned under.
var ErrInvalidUTF8Path = errors.New("location: path is not valid UTF-8")

// Resolve returns p as the filesystem spells it: absolute, clean,
// symlink-resolved, each existing component in its on-disk spelling. The
// result is a host path, not an identity. A path that does not exist resolves
// as typed past its last existing component, as does one the process cannot
// traverse past; a descriptor path is returned absolute and clean, its link not
// followed. A path that ends in a separator or a "." element names a directory:
// one whose last element exists and is not a directory is refused with ENOTDIR,
// as the kernel refuses it.
func Resolve(p string) (string, error) {
	host, err := walkHostPath(p)
	if err == nil && !utf8.ValidString(host) {
		// The working directory, a name on disk or a dangling link's target
		// can hold bytes the typed path does not.
		return "", fmt.Errorf("%w: %q resolves to %q", ErrInvalidUTF8Path, p, host)
	}
	return host, err
}

// walkHostPath resolves p as [Resolve] describes, before the result is
// judged.
func walkHostPath(p string) (string, error) {
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
	wantDir := NamesDirectory(p)

	var missing []string
	hops := 0
	current := abs
	for {
		if len(missing) == 0 && !wantDir && Descriptor(current) {
			return current, nil
		}
		info, err := os.Stat(current)
		if err == nil && len(missing) == 0 && wantDir && !info.IsDir() {
			return "", fmt.Errorf("resolve %q: %w", p, &fs.PathError{Op: "stat", Path: current, Err: syscall.ENOTDIR})
		}
		if err == nil && len(missing) == 0 && Descriptor(current) {
			return current, nil
		}
		if err == nil {
			// Windows reports a path under a regular file absent rather than
			// ENOTDIR, so this is where its half of that refusal lands, as the
			// same path error the Unix kernel returns.
			if len(missing) > 0 && !info.IsDir() {
				return "", fmt.Errorf("resolve %q: %w", p, &fs.PathError{Op: "stat", Path: current, Err: syscall.ENOTDIR})
			}
			spelled, serr := spellOnDisk(current)
			if serr == nil {
				return joinMissing(spelled, missing), nil
			}
			// Removed between the stat and the spelling: walk on as absent.
			err = serr
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

// NamesDirectory reports whether p's spelling makes the kernel require its last
// element to be a directory: p ends in a separator after its volume and root,
// or its last element is ".". [filepath.Clean] and [filepath.Abs] drop both, so
// a caller that cleans a path it hands on carries this answer with it. A root
// alone, and "." alone, name a directory already.
func NamesDirectory(p string) bool {
	isSep := func(r rune) bool { return r == '/' || r == filepath.Separator }
	rest := p[len(filepath.VolumeName(p)):]
	trimmed := strings.TrimRightFunc(rest, isSep)
	if trimmed == "" {
		return false
	}
	if len(trimmed) < len(rest) {
		return true
	}
	i := strings.LastIndexFunc(trimmed, isSep)
	return i >= 0 && trimmed[i+1:] == "."
}

// DirectoryForm returns cleaned, a cleaned or absolute form of p, ending in a
// separator when p names a directory, so [Resolve] reads cleaned's last element
// as the kernel reads p's.
func DirectoryForm(p, cleaned string) string {
	if NamesDirectory(p) && !strings.HasSuffix(cleaned, string(filepath.Separator)) {
		return cleaned + string(filepath.Separator)
	}
	return cleaned
}
