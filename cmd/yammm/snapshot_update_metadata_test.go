package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
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

// TestSnapshotUpdateMetadata_SummaryCountsTheKeys pins B58. The summary's key
// count was asserted nowhere, so it could report any number: the operator's one
// confirmation that a --set landed and an --unset removed said nothing.
func TestSnapshotUpdateMetadata_SummaryCountsTheKeys(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixtureWithMetadata(t, t.TempDir())

	// The fixture carries one key; --set adds two more.
	code, _, errOut := executeCmdOutput(t, "snapshot", "update-metadata",
		"-s", "env=prod", "-s", "owner=simon", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Contains(t, errOut, "(3 keys)", "the summary counts what the header now holds")

	// Removing two leaves one, so a hard-coded count cannot satisfy both.
	code, _, errOut = executeCmdOutput(t, "snapshot", "update-metadata",
		"--unset", "env", "--unset", "owner", ysPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Contains(t, errOut, "(1 keys)", "the summary counts what remains after an unset")
}
