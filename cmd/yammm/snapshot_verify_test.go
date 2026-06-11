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

// The tampering tests need byte surgery on the .ys payload, which the
// testscript scripts cannot express — they stay in-process alongside their
// exact exit-code claims.

func TestSnapshotVerify_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

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
