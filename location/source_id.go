package location

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/hostpath"
)

// SourceID identifies a source within a build. File-backed identities compare
// up to NFC, and synthetic ones as written.
//
// A SourceID can represent:
//   - File-backed source: Created via SourceIDFromPath or ResolveSourcePath,
//     and cut to its directory's by SourceID.Dir
//   - Synthetic source: Created via NewSourceID, such as
//     "<stdin>", "inline:test", or "test://unit/person.yammm"
//
// SourceID is a value type with unexported fields. Always pass by value.
// The zero value is invalid; use IsZero() to check.
//
// SourceID is comparable and safe for use as map keys. Equality is structural
// (field-wise comparison).
type SourceID struct {
	cp        canonicalPath
	synthetic string
}

// NewSourceID returns the identity of a synthetic (non-file) source named name.
// It refuses a name that is empty ([ErrEmptySourceID]), not valid UTF-8
// ([ErrInvalidUTF8Path]), holding a character [strconv.IsPrint] reports does
// not print ([ErrUnprintableSourceID]), or starting as an absolute path does on
// some host: with a slash, two backslashes, or an ASCII letter, a colon and a
// separator ([ErrAbsolutePathSourceID]).
//
// Recommended synthetic identifier patterns:
//   - test://unit/person.yammm (unit tests)
//   - inline:fixture_schema (inline schemas)
//   - embedded://app/builtin.yammm (embedded content)
//   - <stdin> (standard input)
func NewSourceID(name string) (SourceID, error) {
	if err := validateSynthetic(name); err != nil {
		return SourceID{}, err
	}
	return SourceID{synthetic: name}, nil
}

// validateSynthetic is the rule [NewSourceID] judges a name by.
func validateSynthetic(name string) error {
	if name == "" {
		return ErrEmptySourceID
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidUTF8Path, name)
	}
	for _, r := range name {
		if !strconv.IsPrint(r) {
			return fmt.Errorf("%w: %q holds %U", ErrUnprintableSourceID, name, r)
		}
	}
	if looksLikeAbsolute(name) {
		return fmt.Errorf("%w: %q; it starts as a file-backed source's path does: with a slash, two backslashes, or a drive letter, a colon and a separator", ErrAbsolutePathSourceID, name)
	}
	return nil
}

// SourceIDFromPath returns the file-backed identity of path: the host path the
// package documentation's resolution gives, in NFC with forward slashes. Two
// spellings that resolve to one host path give one identity. A path that does
// not exist yet is allowed. A path that does not resolve returns a plain error.
func SourceIDFromPath(path string) (SourceID, error) {
	host, err := hostpath.Resolve(path)
	if err != nil {
		return SourceID{}, fmt.Errorf("create source ID from path %q: %w", path, err)
	}
	return SourceID{cp: identityOf(host)}, nil
}

// ResolveSourcePath returns the identity of p and the host path it was derived
// from, out of one resolution, so the two cannot disagree. It is
// [SourceIDFromPath] for a caller that also reads the file, and it fails where
// SourceIDFromPath does. A caller that may be given a link to a descriptor reads
// through p instead, since the host path may reach no file or another file.
func ResolveSourcePath(p string) (SourceID, string, error) {
	host, err := hostpath.Resolve(p)
	if err != nil {
		return SourceID{}, "", err //nolint:wrapcheck // the error names p
	}
	return SourceID{cp: identityOf(host)}, host, nil
}

// String returns the source identifier: the identity of a file-backed source,
// or a synthetic source's name.
func (s SourceID) String() string {
	if s.synthetic != "" {
		return s.synthetic
	}
	return s.cp.String()
}

// Dir returns the identity of the directory that holds s. At a volume root it
// returns the root. A synthetic or zero source has no directory, and returns
// the zero SourceID.
func (s SourceID) Dir() SourceID {
	if s.cp.IsZero() {
		return SourceID{}
	}
	return SourceID{cp: s.cp.Dir()}
}

// Rel returns s written relative to the directory base, climbing with ".."
// segments where s is not under base, and true, as [path/filepath.Rel] does
// for two absolute paths. Both are identities, so they are compared segment by
// segment, never through the host. A synthetic or zero source or base, s equal
// to base or to one of its ancestors, and a source on another drive or network
// share than base return false.
func (s SourceID) Rel(base SourceID) (string, bool) {
	if s.cp.IsZero() || base.cp.IsZero() {
		return "", false
	}
	sv, to := splitVolume(s.cp.path)
	dv, from := splitVolume(base.cp.path)
	if sv != dv {
		return "", false
	}
	to, from = strings.Trim(to, "/"), strings.Trim(from, "/")
	for from != "" && to != "" {
		f, fromRest, _ := strings.Cut(from, "/")
		t, toRest, _ := strings.Cut(to, "/")
		if f != t {
			break
		}
		from, to = fromRest, toRest
	}
	if to == "" {
		return "", false
	}
	if from == "" {
		return to, true
	}
	climb := strings.Count(from, "/") + 1
	var b strings.Builder
	b.Grow(3*climb + len(to))
	for range climb {
		b.WriteString("../")
	}
	b.WriteString(to)
	return b.String(), true
}

// splitVolume splits an identity into its volume — a network share
// "//server/share", a drive "C:", or "" — and the rest of the path.
func splitVolume(p string) (volume, rest string) {
	if strings.HasPrefix(p, "//") {
		i := strings.IndexByte(p[2:], '/')
		if i < 0 {
			return p, ""
		}
		j := strings.IndexByte(p[2+i+1:], '/')
		if j < 0 {
			return p, ""
		}
		return p[:2+i+1+j], p[2+i+1+j:]
	}
	if len(p) >= 2 && isLetter(p[0]) && p[1] == ':' {
		return p[:2], p[2:]
	}
	return "", p
}

// IsZero reports whether this is a zero-value SourceID.
// The zero value is invalid and should not be used.
func (s SourceID) IsZero() bool {
	return s.cp.IsZero() && s.synthetic == ""
}

// IsFilePath reports whether this SourceID represents a file-backed source.
func (s SourceID) IsFilePath() bool {
	return !s.cp.IsZero()
}
