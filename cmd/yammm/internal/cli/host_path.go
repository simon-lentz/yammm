package cli

import (
	"errors"
	"io/fs"
	"os"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/hostpath"
	"github.com/simon-lentz/yammm/location"
)

// CheckOperand refuses an empty path operand before any lookup: it names no
// file, so it is a usage error (exit 2) carrying location.ErrEmptyPath.
// Everything the filesystem answers about a path it admits exits 3. what names
// the operand in the message.
func CheckOperand(what, path string) error {
	if path == "" {
		return Usagef("resolve %s %q: %w", what, path, location.ErrEmptyPath)
	}
	return nil
}

// CheckSourceOperand is [CheckOperand] for a path that names a source, a
// schema or data file or a module root: its identity reaches both JSON wires,
// so location refuses one that is not valid UTF-8 before any lookup, and so
// does this, with location.ErrInvalidUTF8Path. A path that only names a file
// to read takes [CheckOperand], since its name may be any bytes the filesystem
// holds.
func CheckSourceOperand(what, path string) error {
	if err := CheckOperand(what, path); err != nil {
		return err
	}
	if !utf8.ValidString(path) {
		return Usagef("resolve %s %q: %w: %q", what, path, location.ErrInvalidUTF8Path, path)
	}
	return nil
}

// HostPath returns the path the CLI reads and writes for a path the operator
// named, by the module's one rule for a "..": it is evaluated on the text, as
// the schema loader and [location.ResolveHostPath] evaluate it, and every other
// component, each symbolic link included, is left to the kernel. So every
// command reaches the file `yammm check` reads for that spelling.
func HostPath(p string) (string, error) {
	return hostpath.Text(p)
}

// ReadFile reads the file [HostPath] gives for path. An error names path as
// the operator spelled it.
func ReadFile(path string) ([]byte, error) {
	host, err := HostPath(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	data, err := os.ReadFile(host)
	return data, renamePathError(err, path)
}

// Open opens the file [HostPath] gives for path for reading. An error names
// path as the operator spelled it.
func Open(path string) (*os.File, error) {
	host, err := HostPath(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: path, Err: err}
	}
	f, err := os.Open(host)
	return f, renamePathError(err, path)
}

// renamePathError makes an [fs.PathError] name path.
func renamePathError(err error, path string) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return &fs.PathError{Op: pe.Op, Path: path, Err: pe.Err}
	}
	return err
}
