package source

import (
	"errors"
	"fmt"
	"path"
	"strings"
)

// errImportBackslash refuses an import path holding a backslash, which Windows
// reads as a separator and every other host as part of a file name, so the path
// would name a different file on each.
var errImportBackslash = errors.New("an import path separates its segments with / and holds no backslash")

// errImportNoFile refuses an import path that names a directory or nothing.
var errImportNoFile = errors.New("an import path ends in a file name, and this one ends in none")

// CheckImportPath refuses a path no host reads alike and a path whose last
// segment is absent or a dot segment, which the extension rule would turn into
// a dotfile. A trailing separator after a file name is read past.
func CheckImportPath(importPath string) error {
	if strings.ContainsRune(importPath, '\\') {
		return errImportBackslash
	}
	switch path.Base(importPath) {
	case ".", "..", "/":
		return errImportNoFile
	}
	return nil
}

// SyntheticImportKey returns the key a load under schema.WithSyntheticRoot looks
// an import up by: importPath resolved as text against importerKey, from the
// importer's directory for "./" and "../" and from the root otherwise, with
// ".yammm" added when absent and a leading ".." kept. A generator that keys its
// embedded sources by it writes the keys the re-load asks for. It refuses a key
// or an import path the loader refuses.
func SyntheticImportKey(importerKey, importPath string) (string, error) {
	if err := CheckImportPath(importPath); err != nil {
		return "", err
	}
	importer, err := NormalizeSyntheticKey(importerKey)
	if err != nil {
		return "", fmt.Errorf("importer: %w", err)
	}
	key, err := NormalizeSyntheticKey(ImportFileName(ImportKeyText(importer, importPath)))
	if err != nil {
		return "", fmt.Errorf("import %q: %w", importPath, err)
	}
	return key, nil
}

// ImportKeyText resolves an import path against its importer's key as text.
func ImportKeyText(importerKey, importPath string) string {
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		return path.Join(path.Dir(importerKey), importPath)
	}
	return strings.TrimRight(importPath, "/")
}

// ImportFileName returns the file name an import path resolves to: the path
// itself when it ends in ".yammm", else the path with ".yammm" added. An import
// never resolves to a file named by the bare path, of whatever content type, by
// the extension rule docs/SPEC.md states.
func ImportFileName(relativePath string) string {
	if strings.HasSuffix(relativePath, ".yammm") {
		return relativePath
	}
	return relativePath + ".yammm"
}
