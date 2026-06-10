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

func TestSnapshotSave_InvalidData(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "bad.json")
	outPath := filepath.Join(tmpDir, "out.ys")

	// Missing required field "name".
	require.NoError(t, os.WriteFile(dataPath, []byte(`{"Person": [{"id": "x"}]}`), 0o600))

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", dataPath, "-o", outPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotSave_Into_SchemaMismatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	altSchema := filepath.Join(tmpDir, "alt.yammm")
	require.NoError(t, os.WriteFile(altSchema, []byte(`schema "alt"

type Widget {
	id   String primary
	name String required
}
`), 0o600))

	altData := filepath.Join(tmpDir, "alt.json")
	require.NoError(t, os.WriteFile(altData, []byte(`{"Widget": [{"id": "w1", "name": "Foo"}]}`), 0o600))

	// --into with a different schema must fail as a validation error.
	code = executeCmd(t, "snapshot", "save", altSchema, altData, "--into", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotSave_Into_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	// Tamper with the file so the integrity hash no longer matches.
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	tampered := bytes.Replace(data, []byte(`"alice"`), []byte(`"alicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	newData := filepath.Join(tmpDir, "new.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData, "--into", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}
