package snapshot

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/simon-lentz/yammm/internal/hostpath"
)

// TmpSuffix ends the name of every staging file [WriteFile] creates, and
// [ScanDir] skips a file whose name ends in it. A crashed write can leave such
// a file beside its target. A sweep that removes that residue keys on this
// constant, not on the literal ".tmp".
const TmpSuffix = ".tmp"

// maxLinkHops is how many symbolic links [WriteFile] follows from path before
// it refuses the next, the bound Linux sets (MAXSYMLINKS), so a loop is refused
// rather than followed.
const maxLinkHops = 40

// WriteFile writes data to path atomically: it stages the bytes beside the file
// it replaces, fsyncs and closes the staging file, then renames it into place.
// A ".." in path is evaluated on its text first, as the schema loader evaluates
// it, and a symbolic link at it is then followed, so the link survives and the
// file it names is written. A path that ends in a separator or a "." element
// names a directory: when its last element is a file, sits under one, or does
// not exist in an existing directory, WriteFile refuses it before staging with
// an error matching syscall.ENOTDIR. On an error, WriteFile
// removes its own staging file and returns the error wrapped with the failing
// step. The package
// documentation states the staging name, the file mode, the durability limits
// and what a crash leaves behind.
func WriteFile(path string, data []byte) error {
	text, err := hostpath.Text(path)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	if err := hostpath.RefusedWrite(text); err != nil {
		return fmt.Errorf("resolve target: %w", &fs.PathError{Op: "stat", Path: path, Err: err})
	}
	target, err := followFinalLinks(text)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	stage, err := createStaging(target)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	f := stage.File
	if _, err := f.Write(data); err != nil {
		stage.Discard()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		stage.Discard()
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		stage.Discard()
		return fmt.Errorf("close temp: %w", err)
	}
	if err := stage.Commit(); err != nil {
		stage.Discard()
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// followFinalLinks returns the path the kernel reaches through the chain of
// symbolic links at path, each target read uncleaned so the staging file and the
// rename name one directory. A target in directory form is judged as the path
// itself is ([hostpath.RefusedWrite]). A path that is not a link, or cannot be
// examined, is returned as it is, and staging reports why.
func followFinalLinks(path string) (string, error) {
	for hops := 0; ; hops++ {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return path, nil //nolint:nilerr // staging reports a path that cannot be examined
		}
		if hops == maxLinkHops {
			return "", &fs.PathError{Op: "readlink", Path: path, Err: syscall.ELOOP}
		}
		next, err := hostpath.Hop(path)
		if err != nil {
			return "", err //nolint:wrapcheck // WriteFile wraps the error with the failing step
		}
		path = next
		if err := hostpath.RefusedWrite(path); err != nil {
			return "", &fs.PathError{Op: "stat", Path: path, Err: err}
		}
	}
}

// createStaging creates a staging file for target that no other call shares,
// in the directory the kernel reaches for target: the target's stem, a token,
// then its extension and [TmpSuffix].
func createStaging(target string) (*hostpath.Staging, error) {
	base := filepath.Base(target)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return hostpath.Stage(target, 0o666, func(token string) string { return stem + "." + token + ext + TmpSuffix }) //nolint:wrapcheck // WriteFile wraps the error with the failing step
}
