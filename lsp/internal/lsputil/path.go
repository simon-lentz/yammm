package lsputil

import "path/filepath"

// CanonicalPath mirrors the schema loader's own canonicalisation so a registry
// key the LSP mints matches the SourceID the loader mints for the same file.
// The order is load-bearing: cleaning before resolution is what makes a path
// containing ".." across a symlink land where the loader lands it.
//
// Resolution failure keeps the cleaned path rather than erroring, because two
// callers depend on it — a virtual markdown source (`<path>#block-N`) names no
// file, and a deleted file still needs a stable key.
func CanonicalPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	cleaned := filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}
