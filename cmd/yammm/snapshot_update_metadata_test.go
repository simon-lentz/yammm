package main

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
	"github.com/simon-lentz/yammm/snapshot"
)

// createYSFixtureWithMetadata builds a .ys file pre-populated with one
// metadata key so the exit-code tests have a valid file to operate on.
func createYSFixtureWithMetadata(t *testing.T, tmpDir string) string {
	t.Helper()
	ysPath := filepath.Join(tmpDir, "test.ys")
	code := executeCmd(
		t, "snapshot", "save",
		"testdata/valid.yammm", "testdata/data.json",
		"-o", ysPath,
		"-m", "phase=extract",
	)
	require.Equal(t, cli.ExitOK, code, "failed to create .ys fixture with metadata")
	return ysPath
}

func TestSnapshotUpdateMetadata_NoFlags(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", ysPath)
	assert.Equal(t, cli.ExitUsage, code, "neither --set nor --unset should surface usage error")
}

func TestSnapshotUpdateMetadata_BadSetFormat(t *testing.T) {
	t.Parallel()
	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "no_equals_sign", ysPath)
	assert.Equal(t, cli.ExitUsage, code)
}

func TestSnapshotUpdateMetadata_MalformedInput(t *testing.T) {
	t.Parallel()
	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte("not a yammm snapshot"), 0o600))

	code := executeCmd(t, "snapshot", "update-metadata", "-s", "k=v", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}

// TestSnapshotUpdateMetadata_SetAndUnsetLandInTheHeader pins what the header
// holds after a --set and after an --unset, and that the command prints
// nothing on success.
func TestSnapshotUpdateMetadata_SetAndUnsetLandInTheHeader(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixtureWithMetadata(t, t.TempDir())
	keys := func() []string {
		t.Helper()
		data, err := os.ReadFile(ysPath)
		require.NoError(t, err)
		header, result := snapshot.HeaderOnly(t.Context(), data)
		require.False(t, result.HasErrors(), "header: %s", result.String())
		return slices.Sorted(maps.Keys(header.Metadata))
	}
	before := keys()

	code, out, errOut := executeCmdOutput(t, "snapshot", "update-metadata",
		"-s", "env=prod", "-s", "owner=simon", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Empty(t, out+errOut, "a command that succeeds prints nothing")
	assert.Equal(t, sortedWith(before, "env", "owner"), keys())

	code, out, errOut = executeCmdOutput(t, "snapshot", "update-metadata",
		"--unset", "env", "--unset", "owner", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Empty(t, out+errOut, "a command that succeeds prints nothing")
	assert.Equal(t, before, keys())
}

// sortedWith returns keys and more, sorted.
func sortedWith(keys []string, more ...string) []string {
	all := append(slices.Clone(keys), more...)
	slices.Sort(all)
	return all
}
