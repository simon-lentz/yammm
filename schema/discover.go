package schema

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/simon-lentz/yammm/diag"
)

// ModuleRootMarker is the file name that marks a directory as a module root.
//
// The directory holding the nearest such file above a schema is the root that
// module-style imports resolve against and the boundary the import sandbox is
// opened at. The name is exported so the CLI help, the editor's file watcher
// and a consumer's tooling all spell it one way.
const ModuleRootMarker = "yammm.mod"

// maxModuleRootMarkerSize bounds the read of a marker file. The marker holds
// comment lines only, so anything larger is malformed by size rather than
// worth parsing — and the bound keeps a hostile or mistaken file from being
// read into memory during a walk that runs before any sandbox exists.
const maxModuleRootMarkerSize = 64 << 10

// utf8BOM is refused rather than trimmed. Refusing reserves the byte for a
// later marker format; a rule that silently trims it is a rule that format
// has to keep.
const utf8BOM = "\ufeff"

// MalformedModuleRootError reports a module-root marker whose content
// violates the marker rule: the file must be empty or hold only comment
// lines, where a comment line's first non-space byte is '#'.
//
// A malformed marker fails the load. A marker the author wrote and the loader
// ignored is the failure mode module-root discovery exists to remove, so the
// error is never downgraded to a fall-through.
type MalformedModuleRootError struct {
	// Path is the marker file's path.
	Path string
	// Line is the 1-based line number that violates the rule, or 0 when the
	// violation is the file as a whole (a directory, or an over-size file).
	Line int
	// Reason states the violation in one clause, for rendering.
	Reason string
}

// Error implements the error interface.
func (e *MalformedModuleRootError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("malformed module root marker %s: line %d: %s", e.Path, e.Line, e.Reason)
	}
	return fmt.Sprintf("malformed module root marker %s: %s", e.Path, e.Reason)
}

// FindModuleRoot walks from dir to the filesystem root looking for a
// [ModuleRootMarker] file. It returns the canonical directory holding the
// nearest one and true; "" and false when no ancestor holds one; and an error
// when a marker exists but is malformed or cannot be read.
//
// A miss returns "" rather than dir so a caller can insert its own tier
// between discovery and its default — the editor's workspace folder does
// exactly that. dir must be a directory: pass filepath.Dir of a file path.
//
// The walk runs on the canonical (symlink-resolved) ancestor chain where
// resolution succeeds, so a marker reachable only through a symlinked spelling
// is normally not consulted. Resolution is best-effort: makeCanonicalPath falls
// back to the cleaned absolute path when it fails — a directory that does not
// exist, or a permission error under an editor — and the walk then runs on the
// unresolved chain. Discovery itself is not sandboxed and cannot be: it runs
// before any root exists, and costs one os.Lstat per ancestor level.
func FindModuleRoot(dir string) (string, bool, error) {
	canonical, err := makeCanonicalPath(dir)
	if err != nil {
		return "", false, fmt.Errorf("canonicalize %q: %w", dir, err)
	}

	for current := canonical; ; {
		marker := filepath.Join(current, ModuleRootMarker)
		switch ok, err := checkModuleRootMarker(marker); {
		case err != nil:
			return "", false, err
		case ok:
			return current, true, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			// filepath.Dir is its own fixed point at a volume root on every
			// platform, which is the walk's terminator.
			return "", false, nil
		}
		current = parent
	}
}

// checkModuleRootMarker reports whether path is a well-formed marker. It
// returns false and no error when nothing is there, and an error for a marker
// that exists but cannot serve: a directory, a dangling symlink, an over-size
// or non-comment file, or an unreadable one.
func checkModuleRootMarker(path string) (bool, error) {
	// Lstat first: os.Stat reports a dangling symlink as absent, which would
	// silently skip a marker the author committed.
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("stat module root marker %q: %w", path, err)
	}

	if info.Mode()&os.ModeSymlink != 0 {
		info, err = os.Stat(path)
		if err != nil {
			return false, &MalformedModuleRootError{
				Path:   path,
				Reason: "symlink does not resolve to a readable file",
			}
		}
	}

	if info.IsDir() {
		return false, &MalformedModuleRootError{
			Path:   path,
			Reason: "is a directory, not a marker file",
		}
	}

	content, err := readBounded(path, maxModuleRootMarkerSize)
	if err != nil {
		if _, ok := errors.AsType[*MalformedModuleRootError](err); ok { //nolint:errcheck // type check only, value unused
			return false, err
		}
		return false, fmt.Errorf("read module root marker %q: %w", path, err)
	}

	if err := checkMarkerContent(path, content); err != nil {
		return false, err
	}
	return true, nil
}

// readBounded reads at most limit bytes from path and reports a marker larger
// than that as malformed by size.
func readBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open %q: %w", path, err)
	}
	defer f.Close()

	content, tooLong, err := readAtMost(f, limit)
	if err != nil {
		return nil, fmt.Errorf("%q: %w", path, err)
	}
	if tooLong {
		return nil, &MalformedModuleRootError{
			Path:   path,
			Reason: fmt.Sprintf("larger than %d bytes; a marker holds comment lines only", limit),
		}
	}
	return content, nil
}

// checkMarkerContent applies the marker rule: every line must be empty or a
// comment. A UTF-8 BOM is refused rather than trimmed.
func checkMarkerContent(path string, content []byte) error {
	text := string(content)
	if strings.HasPrefix(text, utf8BOM) {
		return &MalformedModuleRootError{
			Path:   path,
			Line:   1,
			Reason: "starts with a UTF-8 byte order mark",
		}
	}

	// SplitSeq, not Split: the first offending line ends the scan, so
	// materializing every line to reject line one is waste up to the size bound.
	line := 0
	for raw := range strings.SplitSeq(text, "\n") {
		line++
		trimmed := strings.TrimSpace(strings.TrimSuffix(raw, "\r"))
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		return &MalformedModuleRootError{
			Path:   path,
			Line:   line,
			Reason: "is neither empty nor a comment; a marker may hold only # comment lines",
		}
	}
	return nil
}

// ModuleRootIssue converts a [FindModuleRoot] error into the diagnostic the
// load reports for it.
//
// A [MalformedModuleRootError] becomes an Error-severity
// E_LOAD_MODULE_ROOT_MALFORMED carrying the marker's directory and the origin
// "discovered"; any other error becomes a Fatal E_LOAD_IO_FAILURE, the shape
// every other I/O failure in the loader takes. The issue carries no span: the
// source registry holds .yammm sources only.
//
// Exported because the editor reports the same failure without going through
// [Load], and one code built in two places is a code with two shapes.
func ModuleRootIssue(err error) diag.Issue {
	if malformed, ok := errors.AsType[*MalformedModuleRootError](err); ok {
		return diag.NewIssue(diag.Error, diag.E_LOAD_MODULE_ROOT_MALFORMED, malformed.Error()).
			WithDetail(diag.DetailKeyModuleRoot, filepath.Dir(malformed.Path)).
			WithDetail(diag.DetailKeyModuleRootOrigin, diag.ModuleRootDiscovered).Build()
	}
	return errorToFatalIssue(err)
}
