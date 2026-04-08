package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestSnapshotVerify_ValidSnapshot(t *testing.T) {
	t.Parallel()

	ysPath := createYSFixture(t, t.TempDir())

	code := executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitOK, code)
}

func TestSnapshotVerify_WrongSchema(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	// Create a different schema.
	altSchema := filepath.Join(tmpDir, "alt.yammm")
	require.NoError(t, os.WriteFile(altSchema, []byte(`schema "alt"

type Widget {
	id   String primary
	name String required
}
`), 0o600))

	cmd := newRootCmd("test")
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"snapshot", "verify", altSchema, ysPath})

	err := cmd.Execute()
	require.Error(t, err)

	assert.Contains(t, errBuf.String(), "E_SNAPSHOT_INCOMPATIBLE_SCHEMA")
}

func TestSnapshotVerify_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	// Tamper with the file contents.
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)

	// Replace a character in the payload to break the integrity hash.
	tampered := bytes.Replace(data, []byte(`"alice"`), []byte(`"alicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	code := executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotVerify_SkipIntegrityCheck(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	// Tamper with the file but skip integrity check.
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)

	tampered := bytes.Replace(data, []byte(`"alice"`), []byte(`"alicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	// With --skip-integrity-check, structural validation still runs but
	// the integrity hash mismatch is not an error.
	code := executeCmd(t, "snapshot", "verify", "--skip-integrity-check", "testdata/valid.yammm", ysPath)
	assert.Equal(t, cli.ExitOK, code)
}

func TestSnapshotVerify_MalformedFile(t *testing.T) {
	t.Parallel()

	tmp := filepath.Join(t.TempDir(), "bad.ys")
	require.NoError(t, os.WriteFile(tmp, []byte(`{"yammm_snapshot": "not an object"}`), 0o600))

	code := executeCmd(t, "snapshot", "verify", "testdata/valid.yammm", tmp)
	assert.Equal(t, cli.ExitValidation, code)
}
