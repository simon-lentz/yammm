package yammmtest

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

// maxLinks bounds the symbolic links one [DiskSpelling] call follows, so a
// cycle of links is an error rather than a hang.
const maxLinks = 255

// errTooManyLinks is what [DiskSpelling] returns for a path that follows more
// than maxLinks symbolic links.
var errTooManyLinks = errors.New("too many levels of symbolic links")

// CaseFoldingFilesystem reports whether dir's filesystem finds a file by
// another spelling of its name. A test about two spellings of one file skips
// where it reports false, because a case-sensitive filesystem cannot produce
// them. The probe file it writes into dir is removed when the test ends.
func CaseFoldingFilesystem(tb testing.TB, dir string) bool {
	tb.Helper()
	probe := filepath.Join(dir, "CaseProbe")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = os.Remove(probe) })
	_, err := os.Stat(filepath.Join(dir, "caseprobe"))
	return err == nil
}

// DiskSpelling returns path as the filesystem lists it: absolute, every
// symbolic link resolved, and each component written as its parent directory
// lists it. It reads directory listings and never calls the location package,
// so a test can hold that package's resolver to it. A ".." in path is removed
// lexically first, as filepath.Abs removes it, and a ".." in a link target is
// walked on disk; path must exist.
func DiskSpelling(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path of %q: %w", path, err)
	}
	current := volumeRoot(abs)
	pending := components(abs[len(filepath.VolumeName(abs)):])
	links := 0
	for len(pending) > 0 {
		name := pending[0]
		pending = pending[1:]
		switch name {
		case ".":
			continue
		case "..":
			current = filepath.Dir(current)
			continue
		}
		info, err := os.Lstat(filepath.Join(current, name))
		if err != nil {
			return "", fmt.Errorf("spell %q: %w", path, err)
		}
		listed, err := listedName(current, name, info)
		if err != nil {
			return "", fmt.Errorf("spell %q: %w", path, err)
		}
		entry := filepath.Join(current, listed)
		if info.Mode()&fs.ModeSymlink == 0 {
			current = entry
			continue
		}
		if links++; links > maxLinks {
			return "", fmt.Errorf("spell %q: %w", path, errTooManyLinks)
		}
		target, err := os.Readlink(entry)
		if err != nil {
			return "", fmt.Errorf("spell %q: %w", path, err)
		}
		switch {
		case filepath.IsAbs(target):
			current = volumeRoot(target)
			target = target[len(filepath.VolumeName(target)):]
		case target != "" && os.IsPathSeparator(target[0]):
			// Only Windows reaches this: a rooted target names the link's volume.
			current = volumeRoot(current)
		}
		pending = append(components(target), pending...)
	}
	return current, nil
}

// volumeRoot returns the root directory of p's volume, with a drive letter in
// upper case, as Windows spells a drive.
func volumeRoot(p string) string {
	vol := filepath.VolumeName(p)
	if len(vol) == 2 && vol[1] == ':' {
		vol = strings.ToUpper(vol)
	}
	return vol + string(filepath.Separator)
}

// components splits p at every separator the host reads, dropping empty names:
// a slash everywhere, and a backslash as well on Windows.
func components(p string) []string {
	return strings.FieldsFunc(p, func(r rune) bool {
		return r == '/' || r == filepath.Separator
	})
}

// listedName returns the name dir lists for the entry typed as name, whose
// Lstat is info. An exact name wins, then a name equal under case folding and
// NFC, then any entry that is the same file, as a Windows short name is.
func listedName(dir, name string, info fs.FileInfo) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("list %q: %w", dir, err)
	}
	for _, e := range entries {
		if e.Name() == name {
			return name, nil
		}
	}
	composed := norm.NFC.String(name)
	for _, e := range entries {
		if strings.EqualFold(norm.NFC.String(e.Name()), composed) {
			return e.Name(), nil
		}
	}
	for _, e := range entries {
		other, err := os.Lstat(filepath.Join(dir, e.Name()))
		if err == nil && os.SameFile(info, other) {
			return e.Name(), nil
		}
	}
	return "", fmt.Errorf("%q lists no entry for %q", dir, name)
}
