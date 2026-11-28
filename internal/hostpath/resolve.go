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

// ErrInvalidUTF8Path refuses a path that is not valid UTF-8 once its ".."
// elements are taken on the text, or that resolves to one. Its text is location.ErrInvalidUTF8Path's, the public name it is
// returned under.
var ErrInvalidUTF8Path = errors.New("location: path is not valid UTF-8")

// Resolve returns p as the filesystem spells it: absolute, clean,
// symlink-resolved, each existing component in its on-disk spelling. The
// result is a host path, not an identity. A path that does not exist resolves
// as typed past its last existing component, as does one the process cannot
// traverse past. A descriptor path of a file that is not a directory resolves
// to its name spelled from the directory that judged it ([Descriptor]), and a
// path whose links reach such a descriptor path, or that the host spells under a
// descriptor table, keeps its typed last element in its spelled directory, since
// the host's name for such a file may reach no file or another one. A path [Stat] refuses with
// [ErrNotDirectory] is refused with it.
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
// judged. Its first lookup is [Stat]'s judgement of p as typed; each lookup it
// makes is in the host's extended form ([lookupName]).
func walkHostPath(p string) (string, error) {
	if p == "" {
		return "", ErrEmptyPath
	}
	// Judged after the ".." text rule, which a writer applies first, so a
	// ".." that cancels an invalid element leaves one name for the file; and
	// before filepath.Abs, which on Windows would write an invalid byte as
	// U+FFFD.
	if !utf8.ValidString(filepath.Clean(p)) {
		return "", fmt.Errorf("%w: %q", ErrInvalidUTF8Path, p)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", resolveError(p, fmt.Errorf("absolute path: %w", err))
	}
	walk := hostLookups{extended: true}
	info, err := statIn(walk, DirectoryForm(p, abs))
	if errors.Is(err, ErrNotDirectory) {
		return "", resolveError(p, ErrNotDirectory)
	}
	// A directory reached through a descriptor is spelled: Linux names it
	// through procfs, and darwin's answer lies in the descriptor table, which
	// the walk refuses as a spelling. Any other file a descriptor holds has no
	// name the host is sure to give. A closed descriptor answers missing or
	// EBADF; a lookup the kernel refuses otherwise, as ELOOP, is no descriptor
	// path.
	if err == nil && !info.IsDir() || errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.EBADF) {
		if name, ok := descriptorName(abs); ok {
			return name, nil
		}
		if reachesDescriptor(abs) {
			return spelledDirectory(p, abs)
		}
	}

	var missing []string
	hops := 0
	current := abs
	for {
		if err == nil {
			spelled, serr := spellOnDisk(current)
			switch {
			case serr == nil && inDescriptorTable(spelled):
				// The host named the file through a descriptor: a table takes
				// its entries' name, and any other element stands, typed, in
				// its spelled directory.
				named, ok := tableName(current)
				if !ok {
					var derr error
					if named, derr = spelledDirectory(p, current); derr != nil {
						return "", derr
					}
				}
				return joinMissing(named, missing), nil
			case serr == nil:
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
				if current != abs {
					lerr = fmt.Errorf("link %q: %w", current, lerr)
				}
				return "", resolveError(p, lerr)
			}
			if isLink {
				if hops++; hops > maxLinkHops {
					return "", resolveError(p, errTooManyLinks)
				}
				current = target
				_, err = walk.Stat(current)
				continue
			}
		case errors.Is(err, fs.ErrPermission):
			// The process cannot look past this component, so the rest keeps
			// the spelling it was given, as a path that does not exist does.
		default:
			return "", resolveError(p, err)
		}
		parent := filepath.Dir(current)
		if parent == current {
			// filepath.Dir is its own fixed point at a volume root.
			return joinMissing(current, missing), nil
		}
		missing = append(missing, filepath.Base(current))
		current = parent
		_, err = walk.Stat(current)
	}
}

// spelledDirectory returns element, an absolute clean path whose last element
// has no name the host is sure to give, in its directory as [Resolve] spells
// that directory, so its identity agrees with its siblings'. A refusal of the
// directory refuses p.
func spelledDirectory(p, element string) (string, error) {
	dir := filepath.Dir(element)
	if dir == element {
		return element, nil
	}
	spelled, err := walkHostPath(dir)
	if err != nil {
		return "", resolveError(p, err)
	}
	return filepath.Join(spelled, filepath.Base(element)), nil
}

// resolveError is a refusal of the typed path p, which it names once: a
// lookup's own path error names p again, in its host form, so only the cause
// it wraps is kept.
func resolveError(p string, err error) error {
	return &fs.PathError{Op: "resolve", Path: p, Err: pathCause(err)}
}

// pathCause is the cause a path error wraps, or err when it is no path error.
func pathCause(err error) error {
	if pe, ok := err.(*fs.PathError); ok { //nolint:errorlint // a wrapped path error carries its wrapper's context, which is kept
		return pe.Err
	}
	return err
}

// danglingTarget reports whether p is a symbolic link and, when it is, the
// path the host's kernel reaches through it, by that host's rule for a ".."
// in a target. p's directory exists, because p's own entry does. A link that
// is gone by the time its target is read, or whose target is empty, reaches
// nothing and reads as absent. An error names the step and no path of p's: the
// caller names the link.
func danglingTarget(p string) (string, bool, error) {
	l := hostLookups{extended: true}
	info, err := l.Lstat(p)
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, fs.ErrPermission):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("lstat: %w", pathCause(err))
	case info.Mode()&fs.ModeSymlink == 0:
		return "", false, nil
	}
	target, err := readLinkIn(l, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("read link: %w", pathCause(err))
	}
	dir, err := spellOnDisk(filepath.Dir(p))
	if err != nil {
		return "", false, fmt.Errorf("spell the link's directory: %w", err)
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
