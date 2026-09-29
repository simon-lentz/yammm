package snapshot

import (
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
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
// file it names is written. On an error, WriteFile removes its own staging file
// and returns the error wrapped with the failing step. The package
// documentation states the staging name, the file mode, the durability limits
// and what a crash leaves behind.
func WriteFile(path string, data []byte) error {
	text, err := hostpath.Text(path)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	target, err := followFinalLinks(text)
	if err != nil {
		return fmt.Errorf("resolve target: %w", err)
	}
	f, err := createStaging(target)
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmp := f.Name()
	if _, err := f.Write(data); err != nil {
		f.Close()      //nolint:gosec // best-effort close before cleanup
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on write failure
		return fmt.Errorf("write temp: %w", err)
	}
	if err := f.Sync(); err != nil {
		f.Close()      //nolint:gosec // best-effort close before cleanup
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on sync failure
		return fmt.Errorf("sync temp: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on close failure
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on rename failure
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// followFinalLinks returns the path the kernel reaches through the chain of
// symbolic links at path, each target read uncleaned so the staging file and the
// rename name one directory. A path that is not a link, or cannot be examined,
// is returned as it is, and staging reports why.
func followFinalLinks(path string) (string, error) {
	for hops := 0; ; hops++ {
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&fs.ModeSymlink == 0 {
			return path, nil //nolint:nilerr // staging reports a path that cannot be examined
		}
		if hops == maxLinkHops {
			return "", &fs.PathError{Op: "readlink", Path: path, Err: syscall.ELOOP}
		}
		dest, err := os.Readlink(path)
		if err != nil {
			return "", err //nolint:wrapcheck // WriteFile wraps the error with the failing step
		}
		path = hostpath.FollowLink(path, dest)
	}
}

// createStaging creates a staging file for target that no other call shares,
// in the directory the kernel reaches for target. A name another file already
// holds is retried, as os.CreateTemp retries.
func createStaging(target string) (*os.File, error) {
	dir := hostpath.Parent(target)
	if !os.IsPathSeparator(dir[len(dir)-1]) {
		dir += string(filepath.Separator)
	}
	base := filepath.Base(target)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for range 10000 {
		token := strconv.FormatUint(uint64(rand.Uint32()), 10) //nolint:gosec // O_EXCL makes the name unique; the token only spreads the attempts
		name := dir + stem + "." + token + ext + TmpSuffix
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // 0o666 under umask is the documented mode, as os.Create gives
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err //nolint:wrapcheck // WriteFile wraps the error with the failing step
	}
	return nil, fmt.Errorf("no unused staging name beside %s: %w", target, fs.ErrExist)
}
