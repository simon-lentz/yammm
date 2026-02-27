package spec_test

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/simon-lentz/yammm/diag"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// schemasDir returns the absolute path to testdata/schemas/.
func schemasDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", "schemas")
}

// TestSchemas_SchemaNameRequired verifies that omitting the schema declaration
// produces a syntax error (SPEC: SchemaName = [ DOC_COMMENT ] "schema" STRING).
func TestSchemas_SchemaNameRequired(t *testing.T) {
	t.Parallel()

	result := loadSchemaStringExpectError(t,
		`type Foo { name String }`,
		"no-schema-decl.yammm",
	)
	assertDiagHasCode(t, result, diag.E_SYNTAX)
}

// TestSchemas_DocCommentPreserved verifies that a doc comment preceding a type
// declaration is preserved in the parsed model (SPEC Section 3: Comments).
func TestSchemas_DocCommentPreserved(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "doc_comment.yammm")
	s, _ := loadSchemaRaw(t, path)

	typ, ok := s.Type("Documented")
	require.True(t, ok, "type Documented should exist in schema")
	assert.NotEmpty(t, typ.Documentation(), "doc comment should be preserved on type")
	assert.Contains(t, typ.Documentation(), "Documented type",
		"doc comment content should match what was written")
}

// TestSchemas_BasicCompilation verifies that a standalone schema with a simple
// type compiles and can validate instances.
func TestSchemas_BasicCompilation(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "basic.yammm")
	v := loadSchema(t, path)

	records := loadTestData(t,
		filepath.Join(schemasDir(), "data.json"),
		"Item",
	)
	for _, rec := range records {
		assertValid(t, v, "Item", rec)
	}
}

// TestSchemas_ImportRelativePath verifies that relative path imports
// (import "./parts") resolve correctly (SPEC Section 4.2: Imports).
func TestSchemas_ImportRelativePath(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "main_imports.yammm")
	s, _ := loadSchemaRaw(t, path)

	// If the schema compiled, relative import resolution worked.
	// Also verify the imported type is referenceable.
	_, ok := s.Type("Assembly")
	assert.True(t, ok, "Assembly type should exist in the compiled schema")
}

// TestSchemas_ImportExtensionAutoAppend verifies that importing without
// .yammm extension auto-appends it (import "./parts" resolves to parts.yammm).
func TestSchemas_ImportExtensionAutoAppend(t *testing.T) {
	t.Parallel()

	// import_no_ext.yammm imports "./parts" (no .yammm suffix).
	// The loader should auto-append .yammm and find parts.yammm.
	path := filepath.Join(schemasDir(), "import_no_ext.yammm")
	_ = loadSchema(t, path)
}

// TestSchemas_ImportCycleDetected verifies that circular imports are detected
// and produce a diagnostic error (SPEC Section 4.2: Imports).
func TestSchemas_ImportCycleDetected(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "cycle_a.yammm")
	result := loadSchemaExpectError(t, path)
	assertDiagHasCode(t, result, diag.E_IMPORT_CYCLE)
}

// TestSchemas_QualifiedTypeRef verifies that qualified type references
// (alias.TypeName) resolve correctly for cross-schema associations
// (SPEC Section 4.2: Imports, qualified type references).
func TestSchemas_QualifiedTypeRef(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "qualified_types.yammm")
	s, _ := loadSchemaRaw(t, path)

	machineType, ok := s.Type("Machine")
	require.True(t, ok, "Machine type should exist")

	// Verify the association to the imported type was resolved.
	assocs := machineType.AssociationsSlice()
	require.Len(t, assocs, 1, "Machine should have one association")
	assert.Equal(t, "component", assocs[0].Name(),
		"association name should be 'component'")
	assert.Equal(t, "Component", assocs[0].TargetID().Name(),
		"association target should reference Component type")
	assert.False(t, assocs[0].TargetID().IsZero(),
		"association targetID should be resolved (non-zero)")
}

// TestSchemas_PathTraversalRejected verifies that import paths attempting to
// escape the module root are rejected with E_PATH_ESCAPE.
func TestSchemas_PathTraversalRejected(t *testing.T) {
	t.Parallel()

	path := filepath.Join(schemasDir(), "path_traversal.yammm")
	result := loadSchemaExpectError(t, path)

	// The loader should detect the path escape attempt and emit E_PATH_ESCAPE
	// or E_IMPORT_RESOLVE (depending on how far resolution gets).
	hasPathEscape := false
	hasImportResolve := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_PATH_ESCAPE {
			hasPathEscape = true
		}
		if issue.Code() == diag.E_IMPORT_RESOLVE {
			hasImportResolve = true
		}
	}
	assert.True(t, hasPathEscape || hasImportResolve,
		"path traversal import should produce E_PATH_ESCAPE or E_IMPORT_RESOLVE, got: %v",
		result.Messages())
}

// TestSchemas_SchemaDocumentation verifies that doc comments on the schema
// declaration itself are preserved (SPEC Section 4.1: Schema Declaration).
func TestSchemas_SchemaDocumentation(t *testing.T) {
	t.Parallel()

	s, _ := loadSchemaStringRaw(t,
		"/* Schema-level documentation */\nschema \"DocTest\"\ntype Marker { id String primary }",
		"schema-doc-test.yammm",
	)
	assert.NotEmpty(t, s.Documentation(),
		"schema-level doc comment should be preserved")
	assert.Contains(t, s.Documentation(), "Schema-level documentation")
}

// TestSchemas_ImportNotAllowedInString verifies that LoadString always
// disallows imports, producing E_IMPORT_NOT_ALLOWED.
func TestSchemas_ImportNotAllowedInString(t *testing.T) {
	t.Parallel()

	result := loadSchemaStringExpectError(t,
		"schema \"Test\"\nimport \"./other\"\ntype Foo { name String }",
		"string-with-import.yammm",
	)
	assertDiagHasCode(t, result, diag.E_IMPORT_NOT_ALLOWED)
}
