package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

// createYSFixtureWithMetadata builds a .ys file pre-populated with one
// metadata key so tests can exercise both --set and --unset paths.
func createYSFixtureWithMetadata(t *testing.T, tmpDir string) string {
	t.Helper()
	ysPath := filepath.Join(tmpDir, "test.ys")
	code := executeCmd(t, "snapshot", "save",
		"testdata/valid.yammm", "testdata/data.json",
		"-o", ysPath,
		"-m", "phase=extract",
	)
	require.Equal(t, cli.ExitOK, code, "failed to create .ys fixture with metadata")
	return ysPath
}

func TestSnapshotUpdateMetadata_SetSingleKey(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "phase=link", ysPath)
	require.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	header, res := snapshot.HeaderOnly(context.Background(), data)
	require.NoError(t, res.Err())
	assert.Equal(t, "link", header.Metadata["phase"])
}

func TestSnapshotUpdateMetadata_MultipleSet(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata",
		"-s", "phase=link",
		"-s", "pipeline_completed=true",
		ysPath,
	)
	require.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	header, res := snapshot.HeaderOnly(context.Background(), data)
	require.NoError(t, res.Err())
	assert.Equal(t, "link", header.Metadata["phase"])
	assert.Equal(t, "true", header.Metadata["pipeline_completed"])
}

func TestSnapshotUpdateMetadata_UnsetKey(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())
	// Fixture starts with phase=extract; remove it.
	code := executeCmd(t, "snapshot", "update-metadata", "--unset", "phase", ysPath)
	require.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	header, res := snapshot.HeaderOnly(context.Background(), data)
	require.NoError(t, res.Err())
	_, present := header.Metadata["phase"]
	assert.False(t, present, "unset key must be gone")
}

func TestSnapshotUpdateMetadata_SetAndUnset(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata",
		"-s", "phase=link",
		"--unset", "does_not_exist", // tolerated no-op
		ysPath,
	)
	require.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	header, res := snapshot.HeaderOnly(context.Background(), data)
	require.NoError(t, res.Err())
	assert.Equal(t, "link", header.Metadata["phase"])
}

func TestSnapshotUpdateMetadata_NoFlags(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", ysPath)
	assert.Equal(t, cli.ExitUsage, code, "neither --set nor --unset should surface usage error")
}

func TestSnapshotUpdateMetadata_MissingFileArg(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "k=v")
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotUpdateMetadata_BadSetFormat(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "no_equals_sign", ysPath)
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotUpdateMetadata_NonexistentFile(t *testing.T) {
	t.Parallel()
	code := executeCmd(t, "snapshot", "update-metadata", "-s", "k=v", "/nonexistent/path.ys")
	assert.Equal(t, cli.ExitRuntime, code)
}

func TestSnapshotUpdateMetadata_MalformedInput(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte("not a yammm snapshot"), 0o600))

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "k=v", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotUpdateMetadata_IntegrityPreserved(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	// Update.
	code := executeCmd(t, "snapshot", "update-metadata", "-s", "phase=link", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// Verify the file still passes full integrity + structural verify.
	code = executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitOK, code, "update should preserve integrity verification")
}

func TestSnapshotUpdateMetadata_HappyPathOutputLine(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"snapshot", "update-metadata", "-s", "phase=link", ysPath})
	require.NoError(t, cmd.Execute())
	assert.Contains(t, outBuf.String(), "updated metadata")
}
