package snapshot_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// On Linux a removed working directory with PWD unset has no name, so a dir
// that climbs out of it cannot be made absolute: the scan fails with the
// cause, under the dir as passed, and lists nothing.
func TestScanDir_AClimbFromARemovedWorkingDirectoryFailsWithItsCause(t *testing.T) {
	wd := filepath.Join(t.TempDir(), "gone")
	require.NoError(t, os.Mkdir(wd, 0o700))
	t.Chdir(wd)
	t.Setenv("PWD", "")
	require.NoError(t, os.Remove(wd))
	entries, err := collectScanDir(t.Context(), "../x")
	assert.Empty(t, entries)
	var pathErr *fs.PathError
	require.ErrorAs(t, err, &pathErr)
	assert.Equal(t, "open", pathErr.Op)
	assert.Equal(t, "../x", pathErr.Path)
	require.ErrorIs(t, err, syscall.ENOENT)
	var inner *fs.PathError
	assert.NotErrorAs(t, pathErr.Err, &inner, "the cause wraps a second path: %v", err)
}
