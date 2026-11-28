//go:build !darwin && !linux

package hostpath

import (
	"io/fs"
	"os"
)

// stageDir is a directory a staging file is created and renamed in, named by
// its path: these hosts open no directory for search alone, so each lookup
// spells the whole path.
type stageDir struct{ name string }

func openStageDir(dir string) (stageDir, error) {
	if _, err := os.Stat(dir); err != nil {
		return stageDir{}, err //nolint:wrapcheck // the path error names dir
	}
	return stageDir{name: dir}, nil
}

func (d stageDir) create(name string, perm fs.FileMode) (*os.File, error) {
	return os.OpenFile(d.path(name), os.O_RDWR|os.O_CREATE|os.O_EXCL, perm.Perm()) //nolint:wrapcheck // the path error names the file
}

func (d stageDir) rename(from, to string) error {
	return os.Rename(d.path(from), d.path(to)) //nolint:wrapcheck // the link error names both files
}

func (d stageDir) remove(name string) error {
	return os.Remove(d.path(name)) //nolint:wrapcheck // the path error names the file
}

func (stageDir) close() {}
