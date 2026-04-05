package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExport_ToJSON(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"export", "--to", "json", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), `"Person"`)
	assert.Contains(t, outBuf.String(), `"alice"`)
}

func TestExport_ToCSVOutputDir(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"export", "--to", "csv", "--output-dir", tmpDir, "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)

	// Should have created Person.csv
	personCSV := filepath.Join(tmpDir, "Person.csv")
	data, err := os.ReadFile(personCSV)
	require.NoError(t, err)
	assert.Contains(t, string(data), "alice")
}

func TestExport_ToJSONOutputFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "output.json")

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"export", "--to", "json", "--output", outFile, "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"Person"`)
}

func TestExport_ToCypher(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	var outBuf bytes.Buffer
	cmd.SetOut(&outBuf)
	cmd.SetArgs([]string{"export", "--to", "cypher", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)
	assert.Contains(t, outBuf.String(), "MERGE")
}

func TestExport_ToCSVOutputFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outFile := filepath.Join(tmpDir, "output.csv")

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"export", "--to", "csv", "--output", outFile, "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.NoError(t, err)

	data, err := os.ReadFile(outFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "alice")
}

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

	code := executeCmd(t, "export", "--to", "csv", schemaPath, dataPath)
	assert.Equal(t, 2, code) // ExitUsage — multi-type CSV without --output-dir
}

func TestExport_RequiresToFlag(t *testing.T) {
	t.Parallel()

	cmd := newRootCmd("test")
	cmd.SetArgs([]string{"export", "testdata/valid.yammm", "testdata/data.json"})

	err := cmd.Execute()
	require.Error(t, err) // --to is required
}
