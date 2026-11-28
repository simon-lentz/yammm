package cli

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strconv"
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
// so location refuses one that is not valid UTF-8 once a ".." in it is taken
// on the text, before any lookup, and so does this, with
// location.ErrInvalidUTF8Path. A path that only names a file to read takes
// [CheckOperand], since its name may be any bytes the filesystem holds.
func CheckSourceOperand(what, path string) error {
	if err := CheckOperand(what, path); err != nil {
		return err
	}
	if !utf8.ValidString(filepath.Clean(path)) {
		return Usagef("resolve %s %q: %w: %q", what, path, location.ErrInvalidUTF8Path, path)
	}
	return nil
}

// HostPath returns the path the CLI reads and writes for a path the operator
// named, by the module's one rule for a "..": it is evaluated on the text, as
// the schema loader and [location.SourceIDFromPath] evaluate it, and every other
// component, each symbolic link included, is left to the kernel. So every
// command reaches the file `yammm check` reads for that spelling.
func HostPath(p string) (string, error) {
	return hostpath.Text(p)
}

// ReadFile reads the file [HostPath] gives for path, judged as
// [hostpath.OpenFile] judges it. An error names path as the operator spelled it.
func ReadFile(path string) ([]byte, error) {
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	// Sized from the file, as os.ReadFile sizes it, so a read allocates the
	// file once; a stream, or a size below zero or too large for one buffer,
	// reads as it grows.
	size := 0
	if info, err := f.Stat(); err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() < int64(math.MaxInt-bytes.MinRead) {
		size = int(info.Size())
	}
	buf := bytes.NewBuffer(make([]byte, 0, size+bytes.MinRead))
	_, err = buf.ReadFrom(SpelledReader(f, path))
	return buf.Bytes(), err
}

// Open opens the file [HostPath] gives for path for reading, as
// [hostpath.OpenFile] opens it. An error names path as the operator spelled it.
func Open(path string) (*os.File, error) {
	host, err := HostPath(path)
	if err != nil {
		return nil, &fs.PathError{Op: "open", Path: operandName(path), Err: err}
	}
	f, err := hostpath.OpenFile(host, os.O_RDONLY)
	return f, renamePathError(err, path)
}

// SpelledReader returns a reader of f, which [Open] opened for path, whose
// errors name path as the operator spelled it, as Open's own do. A read of f
// itself names the host's path.
func SpelledReader(f *os.File, path string) io.Reader {
	return spelledReader{f: f, path: path}
}

type spelledReader struct {
	f    *os.File
	path string
}

func (r spelledReader) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	return n, renamePathError(err, r.path)
}

// renamePathError makes an [fs.PathError] name path, by [operandName].
func renamePathError(err error, path string) error {
	if pe, ok := errors.AsType[*fs.PathError](err); ok {
		return &fs.PathError{Op: pe.Op, Path: operandName(path), Err: pe.Err}
	}
	return err
}

// operandName is how an error names the operand path: as typed, or quoted
// when it is not valid UTF-8, as [SourceOf] names it, since a JSON wire writes
// an invalid byte as U+FFFD.
func operandName(path string) string {
	if utf8.ValidString(path) {
		return path
	}
	return strconv.Quote(path)
}
