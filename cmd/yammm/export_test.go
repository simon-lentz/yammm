package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/cmd/yammm/internal/cli"
)

// multiTypeFixture writes a schema of three types and matching data.
//
// Three rather than two: the adapter walks a map, and at two types the emitted
// order was stable across eight runs — a test written on two types can pass by
// luck about an ordering that is not guaranteed.
func multiTypeFixture(t *testing.T) (schemaPath, dataPath string) {
	t.Helper()

	tmpDir := t.TempDir()
	schemaPath = filepath.Join(tmpDir, "multi.yammm")
	dataPath = filepath.Join(tmpDir, "multi.json")

	schemaContent := `schema "multi"

type Person {
	id   String primary
	name String required
}

type Pet {
	id   String primary
	name String required
}

type Place {
	id   String primary
	name String required
}
`
	dataContent := `{
	"Person": [{"id": "a", "name": "Alice"}],
	"Pet": [{"id": "p1", "name": "Fido"}],
	"Place": [{"id": "x", "name": "Rome"}]
}`
	require.NoError(t, os.WriteFile(schemaPath, []byte(schemaContent), 0o600))
	require.NoError(t, os.WriteFile(dataPath, []byte(dataContent), 0o600))
	return schemaPath, dataPath
}

// A CSV export of several types needs one file per type. --output does not
// change that, and used to be exactly what admitted the broken case: the
// command refused a multi-type export to stdout and accepted the same export
// into one file, where the per-type header rows interleave in an order that
// varies between runs.
func TestExport_CSVMultiTypeRequiresOutputDir(t *testing.T) {
	t.Parallel()

	schemaPath, dataPath := multiTypeFixture(t)

	t.Run("to stdout", func(t *testing.T) {
		t.Parallel()
		code := executeCmd(t, "export", "--to", "csv", schemaPath, dataPath)
		assert.Equal(t, cli.ExitUsage, code)
	})

	t.Run("to one --output file", func(t *testing.T) {
		t.Parallel()
		out := filepath.Join(t.TempDir(), "all.csv")
		code := executeCmd(t, "export", "--to", "csv", "--output", out, schemaPath, dataPath)
		assert.Equal(t, cli.ExitUsage, code)
		assert.NoFileExists(t, out, "a refused export writes nothing")
	})
}

// TestExport_CypherCarriesTheLabelFlags pins that `--to cypher` composes its
// labels from the same flags its sibling `neo4j` commands take. The plugin's
// CLI reference documents the three as one workflow; emitting data on
// `catalog__Book` while `neo4j constraints --prefix app_` guards
// `app_catalog__Book` writes rows no constraint covers, and nothing says so.
func TestExport_CypherCarriesTheLabelFlags(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	schemaPath := filepath.Join(dir, "catalog.yammm")
	dataPath := filepath.Join(dir, "data.json")
	require.NoError(t, os.WriteFile(schemaPath,
		[]byte("schema \"catalog\"\n\ntype Book {\n\tisbn String primary\n}\n"), 0o600))
	require.NoError(t, os.WriteFile(dataPath,
		[]byte(`{"Book":[{"isbn":"1"}]}`), 0o600))

	code, out, errOut := executeCmdOutput(t,
		"export", "--to", "cypher", "--prefix", "app_", "--separator", "_",
		schemaPath, dataPath)
	require.Equal(t, cli.ExitOK, code, "stderr:\n%s", errOut)
	assert.Contains(t, out, "app_catalog_Book",
		"the label flags did not reach the adapter that composes the label")
	assert.NotContains(t, out, "catalog__Book",
		"the default label survived beside the configured one")
}

// TestExport_LabelFlagsApplyOnlyToCypher pins the per-target refusal beside the
// one --output-dir already carries: json and csv compose no Neo4j label, so a
// label flag on either is a flag that would do nothing.
func TestExport_LabelFlagsApplyOnlyToCypher(t *testing.T) {
	t.Parallel()

	schemaPath, dataPath := multiTypeFixture(t)

	for _, flag := range []string{"--prefix", "--separator"} {
		t.Run(flag, func(t *testing.T) {
			t.Parallel()
			code := executeCmd(t, "export", "--to", "json", flag, "x", schemaPath, dataPath)
			assert.Equal(t, cli.ExitUsage, code)
		})
	}
}
