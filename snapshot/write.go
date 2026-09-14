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
)

// TmpSuffix ends the name of every staging file [WriteFile] creates, and
// [ScanDir] skips a file whose name ends in it. A crashed write can leave such
// a file beside its target. A sweep that removes that residue keys on this
// constant, not on the literal ".tmp".
const TmpSuffix = ".tmp"

// WriteFile writes data to path atomically: it stages the bytes beside path,
// fsyncs and closes the staging file, then renames it into place. Each call
// stages in a file of its own, named path's stem, a random token, path's
// extension and [TmpSuffix]. On an error, WriteFile removes its own staging
// file and returns the error wrapped with the failing step. The package
// documentation states the file mode, the durability limits and what a crash
// leaves behind.
func WriteFile(path string, data []byte) error {
	f, err := createStaging(path)
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
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp) //nolint:gosec // best-effort cleanup on rename failure
		return fmt.Errorf("rename temp to final: %w", err)
	}
	return nil
}

// createStaging creates a staging file for path that no other call shares. A
// name another file already holds is retried, as os.CreateTemp retries.
func createStaging(path string) (*os.File, error) {
	dir, base := filepath.Split(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for range 10000 {
		token := strconv.FormatUint(uint64(rand.Uint32()), 10) //nolint:gosec // O_EXCL makes the name unique; the token only spreads the attempts
		name := filepath.Join(dir, stem+"."+token+ext+TmpSuffix)
		f, err := os.OpenFile(name, os.O_RDWR|os.O_CREATE|os.O_EXCL, 0o666) //nolint:gosec // 0o666 under umask is the documented mode, as os.Create gives
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err //nolint:wrapcheck // WriteFile wraps the error with the failing step
	}
	return nil, fmt.Errorf("no unused staging name beside %s: %w", path, fs.ErrExist)
}
