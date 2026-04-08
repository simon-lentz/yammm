package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

func TestSnapshotSave_SingleJSONFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", outPath)
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "yammm_snapshot")
	assert.Contains(t, raw, "types")
	assert.Contains(t, raw, "instances")
}

func TestSnapshotSave_MultipleDataFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "multi.ys")

	// Create a second JSON data file.
	data2Path := filepath.Join(tmpDir, "data2.json")
	require.NoError(t, os.WriteFile(data2Path, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", data2Path, "-o", outPath)
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	// Verify all instances are present.
	raw := string(data)
	assert.Contains(t, raw, `"alice"`)
	assert.Contains(t, raw, `"bob"`)
	assert.Contains(t, raw, `"eve"`)
}

func TestSnapshotSave_MetadataFlags(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "meta.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json",
		"-o", outPath, "-m", "pipeline=test", "-m", "env=ci")
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	header, _ := raw["yammm_snapshot"].(map[string]any)
	require.NotNil(t, header)
	meta, _ := header["metadata"].(map[string]any)
	require.NotNil(t, meta)
	assert.Equal(t, "test", meta["pipeline"])
	assert.Equal(t, "ci", meta["env"])
}

func TestSnapshotSave_TimestampFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "ts.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json",
		"-o", outPath, "--timestamp")
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	header, _ := raw["yammm_snapshot"].(map[string]any)
	require.NotNil(t, header)
	assert.NotEmpty(t, header["created_at"])
}

func TestSnapshotSave_NoTimestampDeterministic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	out1 := filepath.Join(tmpDir, "det1.ys")
	out2 := filepath.Join(tmpDir, "det2.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", out1)
	assert.Equal(t, cli.ExitOK, code)

	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", out2)
	assert.Equal(t, cli.ExitOK, code)

	data1, err := os.ReadFile(out1)
	require.NoError(t, err)
	data2, err := os.ReadFile(out2)
	require.NoError(t, err)

	assert.Equal(t, string(data1), string(data2), "output should be byte-level deterministic without --timestamp")
}

func TestSnapshotSave_IndentFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "indented.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json",
		"-o", outPath, "--indent")
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)

	// Indented output should contain newlines and tabs.
	assert.Contains(t, string(data), "\n")
	assert.Contains(t, string(data), "\t")

	// Should still be valid JSON with valid integrity hash.
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	assert.Contains(t, raw, "yammm_snapshot")
}

func TestSnapshotSave_NonYSExtensionWarning(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "out.json")

	cmd := newRootCmd("test")
	var errBuf bytes.Buffer
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", outPath})

	err := cmd.Execute()
	require.NoError(t, err)

	assert.Contains(t, errBuf.String(), "warning")
	assert.Contains(t, errBuf.String(), ".ys")
}

func TestSnapshotSave_MissingOutputFlag(t *testing.T) {
	t.Parallel()

	// Cobra enforces required flag — returns usage error.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json")
	assert.Equal(t, cli.ExitUsage, code)
}

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

func TestSnapshotSave_CSVWithTypeFlag(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "csv.ys")

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.csv",
		"--type", "Person", "-o", outPath)
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "yammm_snapshot")
}

func TestSnapshotSave_Into_BasicMerge(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	// Build initial snapshot.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// Create new data file with a different instance.
	newData := filepath.Join(tmpDir, "new.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	// Merge into existing snapshot.
	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData, "--into", ysPath)
	assert.Equal(t, cli.ExitOK, code)

	// Verify merged snapshot contains all instances.
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	raw := string(data)
	assert.Contains(t, raw, `"alice"`)
	assert.Contains(t, raw, `"bob"`)
	assert.Contains(t, raw, `"eve"`)
}

func TestSnapshotSave_Into_WithOutput(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	origPath := filepath.Join(tmpDir, "orig.ys")
	newPath := filepath.Join(tmpDir, "new.ys")

	// Build initial snapshot.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", origPath)
	require.Equal(t, cli.ExitOK, code)
	origData, err := os.ReadFile(origPath)
	require.NoError(t, err)

	// Create new data file.
	newData := filepath.Join(tmpDir, "extra.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	// Merge --into orig but write to new file.
	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData, "--into", origPath, "-o", newPath)
	assert.Equal(t, cli.ExitOK, code)

	// Original should be unchanged.
	origAfter, err := os.ReadFile(origPath)
	require.NoError(t, err)
	assert.Equal(t, string(origData), string(origAfter), "original file should not be modified")

	// New file should have merged content.
	newContent, err := os.ReadFile(newPath)
	require.NoError(t, err)
	assert.Contains(t, string(newContent), `"eve"`)
	assert.Contains(t, string(newContent), `"alice"`)
}

func TestSnapshotSave_Into_DefaultsToIntoPath(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	// Build initial snapshot.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// New data.
	newData := filepath.Join(tmpDir, "new.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	// --into without --output → writes back to --into path.
	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData, "--into", ysPath)
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), `"eve"`)
}

func TestSnapshotSave_Into_SchemaMismatch(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	// Build initial snapshot with testdata schema.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// Create a different schema.
	altSchema := filepath.Join(tmpDir, "alt.yammm")
	require.NoError(t, os.WriteFile(altSchema, []byte(`schema "alt"

type Widget {
	id   String primary
	name String required
}
`), 0o600))

	altData := filepath.Join(tmpDir, "alt.json")
	require.NoError(t, os.WriteFile(altData, []byte(`{"Widget": [{"id": "w1", "name": "Foo"}]}`), 0o600))

	// --into with wrong schema → should fail.
	code = executeCmd(t, "snapshot", "save", altSchema, altData, "--into", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotSave_Into_WithMetadata(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	// Build initial snapshot with metadata.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json",
		"-o", ysPath, "-m", "env=prod")
	require.Equal(t, cli.ExitOK, code)

	// New data.
	newData := filepath.Join(tmpDir, "new.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	// --into with different metadata → new metadata replaces old.
	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData,
		"--into", ysPath, "-m", "env=staging")
	assert.Equal(t, cli.ExitOK, code)

	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	var raw map[string]any
	require.NoError(t, json.Unmarshal(data, &raw))
	header, _ := raw["yammm_snapshot"].(map[string]any)
	meta, _ := header["metadata"].(map[string]any)
	assert.Equal(t, "staging", meta["env"])
}

func TestSnapshotSave_Into_CorruptedFile(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := createYSFixture(t, tmpDir)

	// Tamper with the file.
	data, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	tampered := bytes.Replace(data, []byte(`"alice"`), []byte(`"alicX"`), 1)
	require.NoError(t, os.WriteFile(ysPath, tampered, 0o600))

	newData := filepath.Join(tmpDir, "new.json")
	require.NoError(t, os.WriteFile(newData, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", newData, "--into", ysPath)
	assert.Equal(t, cli.ExitValidation, code)
}

func TestSnapshotSave_Into_MultipleNewFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ysPath := filepath.Join(tmpDir, "base.ys")

	// Build initial snapshot.
	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json", "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// Create two new data files.
	data1 := filepath.Join(tmpDir, "d1.json")
	data2 := filepath.Join(tmpDir, "d2.json")
	require.NoError(t, os.WriteFile(data1, []byte(`{"Person": [{"id": "eve", "name": "Eve", "age": 40}]}`), 0o600))
	require.NoError(t, os.WriteFile(data2, []byte(`{"Person": [{"id": "frank", "name": "Frank", "age": 50}]}`), 0o600))

	// --into with two new files.
	code = executeCmd(t, "snapshot", "save", "testdata/valid.yammm", data1, data2, "--into", ysPath)
	assert.Equal(t, cli.ExitOK, code)

	content, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	raw := string(content)
	assert.Contains(t, raw, `"alice"`)
	assert.Contains(t, raw, `"eve"`)
	assert.Contains(t, raw, `"frank"`)
}

func TestSnapshotSave_Into_ResolvesUnresolved(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a schema with Person → WORKS_AT → Company.
	schemaPath := filepath.Join(tmpDir, "hr.yammm")
	require.NoError(t, os.WriteFile(schemaPath, []byte(`schema "hr"

type Company {
	id    String primary
	title String required
}

type Person {
	id   String primary
	name String required
	--> WORKS_AT Company
}
`), 0o600))

	// Create Person data referencing Company["c1"] which doesn't exist yet.
	personData := filepath.Join(tmpDir, "people.json")
	require.NoError(t, os.WriteFile(personData, []byte(`{
		"Person": [{"id": "p1", "name": "Alice", "works_at": {"_target_id": "c1"}}]
	}`), 0o600))

	// Build initial snapshot — Person→Company edge is unresolved.
	ysPath := filepath.Join(tmpDir, "hr.ys")
	code := executeCmd(t, "snapshot", "save", schemaPath, personData, "-o", ysPath)
	require.Equal(t, cli.ExitOK, code)

	// Verify unresolved edge exists in the initial snapshot.
	initial, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	assert.Contains(t, string(initial), `"target_missing"`)

	// Create Company data that will resolve the forward reference.
	companyData := filepath.Join(tmpDir, "companies.json")
	require.NoError(t, os.WriteFile(companyData, []byte(`{
		"Company": [{"id": "c1", "title": "Acme"}]
	}`), 0o600))

	// --into adds the Company, resolving the unresolved edge.
	code = executeCmd(t, "snapshot", "save", schemaPath, companyData, "--into", ysPath)
	assert.Equal(t, cli.ExitOK, code)

	// Verify the edge is now resolved (no more "target_missing").
	merged, err := os.ReadFile(ysPath)
	require.NoError(t, err)
	mergedStr := string(merged)
	assert.NotContains(t, mergedStr, `"target_missing"`)
	assert.Contains(t, mergedStr, `"Company"`)
	assert.Contains(t, mergedStr, `"c1"`)
}

func TestSnapshotSave_NeitherOutputNorInto(t *testing.T) {
	t.Parallel()

	code := executeCmd(t, "snapshot", "save", "testdata/valid.yammm", "testdata/data.json")
	assert.Equal(t, cli.ExitUsage, code)
}
