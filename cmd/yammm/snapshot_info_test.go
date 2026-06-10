package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// createYSFixture uses snapshot save to create a .ys file from testdata.
func createYSFixture(t *testing.T, tmpDir string) string {
	t.Helper()
	ysPath := filepath.Join(tmpDir, "test.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code, "failed to create .ys fixture")

	return ysPath
}

func TestSnapshotInfo_MalformedFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`not json at all`), 0o600))

	code := executeCmd(t, "snapshot", "info", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotInfo_DirFlag_MutuallyExclusiveWithPositional(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	file := createYSFixture(t, tmpDir)

	code := executeCmd(t, "snapshot", "info", "--dir", tmpDir, file)
	assert.Equal(t, cli.ExitUsage, code)
}
