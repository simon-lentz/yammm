//go:build unix

package cli

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

// platformTargetCases returns the target kinds only a Unix host can create.
func platformTargetCases() []writeTargetCase {
	return []writeTargetCase{{
		name: "a FIFO receives the bytes and stays a FIFO",
		setup: func(dir string) (string, error) {
			target := filepath.Join(dir, "pipe")
			return target, syscall.Mkfifo(target, 0o600)
		},
		check: func(_, target string, err error) string {
			if err != nil {
				return "writing into a FIFO failed: " + err.Error()
			}
			if info, statErr := os.Lstat(target); statErr != nil || info.Mode()&fs.ModeNamedPipe == 0 {
				return "the FIFO was replaced by a regular file"
			}
			return ""
		},
	}}
}
