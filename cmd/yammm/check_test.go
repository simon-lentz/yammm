package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestCheck_InvalidData_ExitCode(t *testing.T) {
	t.Parallel()

	// Person missing required "name" field — a data failure must exit with
	// the validation code, not usage.
	tmpDir := t.TempDir()
	dataPath := filepath.Join(tmpDir, "bad.json")
	require.NoError(t, os.WriteFile(dataPath, []byte(`{"Person": [{"id": "x"}]}`), 0o600))

	code := executeCmd(t, "check", "testdata/valid.yammm", dataPath)
	assert.Equal(t, cli.ExitValidation, code)
}
