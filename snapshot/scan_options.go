package snapshot

import (
	"io/fs"
	"os"
)

// ScanOption configures a directory scan. [ScanDirWith] and [ScanDirSliceWith]
// take the same options: the slice form materializes the iterator, so an
// option means one thing on both.
type ScanOption func(*scanConfig)

type scanConfig struct {
	keep func(ScanCandidate) bool
	open func(path string) (*os.File, error)
}

func applyScanOptions(opts []ScanOption) scanConfig {
	cfg := scanConfig{open: os.Open}
	for _, o := range opts {
		o(&cfg)
	}
	return cfg
}

// ScanCandidate is a file the scan has admitted and is about to open.
//
// It carries what the directory read already answered and defers everything
// costing a syscall to [ScanCandidate.Info]. A caller-built value works — Info
// falls back to stat'ing Path — so a predicate is unit-testable without a
// directory. Use keyed fields: a later release may add a trailing one.
type ScanCandidate struct {
	// Name is the basename of the file (e.g., "CA.ys").
	Name string
	// Path is the full filesystem path, equivalent to filepath.Join(dir, Name).
	Path string

	// stat is the scan's resolve-once target stat, nil in a caller-built value.
	stat func() (fs.FileInfo, error)
}

// Info describes the file the open would read: the stat follows a symlink, so
// it answers for the target rather than the link — the same file the fstat
// behind [ScanEntry.FileSize] and [ScanEntry.ModTime] answers for. One
// candidate costs at most one stat however often Info is called, and nothing
// at all when no filter asks. A failure is returned rather than swallowed; a
// filter that admits the entry anyway gets the documented per-file
// E_SNAPSHOT_IO entry from the open.
func (c ScanCandidate) Info() (fs.FileInfo, error) {
	if c.stat != nil {
		return c.stat()
	}
	info, err := os.Stat(c.Path)
	if err != nil {
		return nil, err //nolint:wrapcheck // the os error is the answer; a caller tests it with errors.Is
	}
	return info, nil
}

// WithScanFilter admits only the files keep returns true for, and decides
// before the file is opened: a rejected file yields no [ScanEntry], costs no
// open, and parses no header. keep sees exactly the files the unfiltered scan
// would yield — the .ys, regular-file and [TmpSuffix] rules run first — one at
// a time, in [os.ReadDir] order, on the iterating goroutine. A nil keep, or no
// option at all, admits every file; the last WithScanFilter passed wins.
func WithScanFilter(keep func(ScanCandidate) bool) ScanOption {
	return func(c *scanConfig) {
		c.keep = keep
	}
}

// withScanOpen replaces the file-open primitive. Unexported: the pre-open
// contract is only provable by observing what the scan opens, and an exported
// seam typed on *os.File would fix a choice no consumer has asked for yet.
func withScanOpen(open func(path string) (*os.File, error)) ScanOption {
	return func(c *scanConfig) {
		c.open = open
	}
}
