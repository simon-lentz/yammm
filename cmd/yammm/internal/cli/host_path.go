package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/simon-lentz/yammm/internal/hostpath"
)

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

// kernelText returns p with each ".." taken as the kernel takes it: the part up
// to its last ".." is resolved on disk and the rest kept as written, and on
// Windows, whose kernel takes a ".." on the text, p cleaned. p is returned
// unchanged when it holds no "..", and cleaned when that part cannot be
// resolved.
func kernelText(p string) string {
	if runtime.GOOS == "windows" {
		return filepath.Clean(p)
	}
	last := -1
	for i := 0; i+2 <= len(p); i++ {
		if p[i:i+2] == ".." && (i == 0 || os.IsPathSeparator(p[i-1])) && (i+2 == len(p) || os.IsPathSeparator(p[i+2])) {
			last = i + 2
		}
	}
	if last < 0 {
		return p
	}
	prefix := p[:last]
	if !filepath.IsAbs(prefix) {
		wd, err := os.Getwd()
		if err != nil {
			return filepath.Clean(p)
		}
		prefix = wd + string(filepath.Separator) + prefix
	}
	resolved, err := filepath.EvalSymlinks(prefix)
	if err != nil {
		return filepath.Clean(p)
	}
	return resolved + p[last:]
}
