package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestExport_CSVMultiTypeRequiresOutputDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "multi.yammm")
	dataPath := filepath.Join(tmpDir, "multi.json")

	schemaContent := `schema "multi"

type Person {
	id   String primary
	name String required
}

type Pet {
	id   String primary
	name String required
}
`
	dataContent := `{
	"Person": [{"id": "a", "name": "Alice"}],
	"Pet": [{"id": "p1", "name": "Fido"}]
}`
	require.NoError(t, os.WriteFile(schemaPath, []byte(schemaContent), 0o600))
	require.NoError(t, os.WriteFile(dataPath, []byte(dataContent), 0o600))

	// Multi-type CSV export cannot go to a single stream — usage error.
	code := executeCmd(t, "export", "--to", "csv", schemaPath, dataPath)
	assert.Equal(t, cli.ExitUsage, code)
}
