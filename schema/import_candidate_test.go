package schema_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// writeSchemaDir writes files into a temp dir and returns its path.
func writeSchemaDir(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		full := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	return dir
}

const importEntry = `schema "app"

import "./parts" as parts

type Widget {
    id String primary
}
`

const importedPart = `schema "parts"

type Part {
    id String primary
}
`

// An extensionless import resolves only to "<path>.yammm". The candidate list
// carried the bare path as a fallback, so `import "./parts"` would compile a
// file literally named "parts" — of any content type — against the extension
// rule docs/SPEC.md states.
//
// Mutation: restoring the bare candidate in importCandidates turns this red;
// the extensionless file compiles and the schema loads.
func TestImportCandidates_ExtensionlessFileIsNotAnImport(t *testing.T) {
	dir := writeSchemaDir(t, map[string]string{
		"entry.yammm": importEntry,
		"parts":       importedPart, // no extension
	})

	_, res := schema.Load(t.Context(), filepath.Join(dir, "entry.yammm"))
	if !res.HasErrors() {
		t.Fatal("a file named \"parts\" resolved an import written as \"./parts\"")
	}
}

// The suffixed file still resolves, which is the whole point: the tightening
// removes a fallback, not the ordinary form.
func TestImportCandidates_SuffixedFileStillResolves(t *testing.T) {
	dir := writeSchemaDir(t, map[string]string{
		"entry.yammm": importEntry,
		"parts.yammm": importedPart,
	})

	if _, res := schema.Load(t.Context(), filepath.Join(dir, "entry.yammm")); res.HasErrors() {
		t.Errorf("an extensionless import did not resolve its .yammm file: %s", res)
	}
}

// An import written WITH the suffix is untouched.
func TestImportCandidates_ExplicitSuffixIsUnchanged(t *testing.T) {
	dir := writeSchemaDir(t, map[string]string{
		"entry.yammm": strings.Replace(importEntry, `"./parts"`, `"./parts.yammm"`, 1),
		"parts.yammm": importedPart,
	})

	if _, res := schema.Load(t.Context(), filepath.Join(dir, "entry.yammm")); res.HasErrors() {
		t.Errorf("an explicit .yammm import did not resolve: %s", res)
	}
}

// The cross-Load short-circuit and the reader must admit the same candidates.
// They derived their lists independently, so tightening only the reader would
// have left the short-circuit able to bind an import from the process-wide
// Registry under a key the reader can no longer resolve — a loose result that
// depends on load order, which is the worst shape a tightening can take.
//
// Loading the same entry twice through one Registry exercises the short-circuit
// on the second pass.
func TestImportCandidates_ShortCircuitAgreesWithTheReader(t *testing.T) {
	dir := writeSchemaDir(t, map[string]string{
		"entry.yammm": importEntry,
		"parts":       importedPart,
	})
	entry := filepath.Join(dir, "entry.yammm")
	reg := schema.NewRegistry()

	_, first := schema.Load(t.Context(), entry, schema.WithRegistry(reg))
	if !first.HasErrors() {
		t.Fatal("first load resolved an extensionless file")
	}
	_, second := schema.Load(t.Context(), entry, schema.WithRegistry(reg))
	if !second.HasErrors() {
		t.Error("the second load bound the import through the short-circuit that the reader refuses")
	}
}
