package schema_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireOK fails the test when result carries errors, logging every
// collected issue first so load failures are diagnosable.
func requireOK(t *testing.T, result diag.Result) {
	t.Helper()
	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Unexpected issue: %v", issue)
		}
	}
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)
}

// loadOK loads source from a string and requires success: a non-nil
// schema with no error diagnostics (each issue logged on failure).
func loadOK(t *testing.T, source string) *schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), source, "test.yammm")
	requireOK(t, result)
	require.NotNil(t, s)
	return s
}

// loadErr loads source from a string and requires failure: a nil schema
// with error diagnostics. Returns the result for code-level assertions.
func loadErr(t *testing.T, source string) diag.Result {
	t.Helper()
	s, result := schema.LoadString(t.Context(), source, "test.yammm")
	require.Nil(t, s, "load should fail")
	require.True(t, result.HasErrors(), "load should produce error diagnostics")
	return result
}

func TestString_SimpleSchema(t *testing.T) {
	// Note: YAMMM grammar uses `name Type` syntax, not `name: Type`
	source := `schema "test" type Person { name String primary }`
	s := loadOK(t, source)
	assert.Equal(t, "test", s.Name())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, "Person", typ.Name())
}

func TestString_EmptySchema(t *testing.T) {
	source := `schema "empty"`
	s := loadOK(t, source)
	assert.Equal(t, "empty", s.Name())
}

func TestString_SyntaxError(t *testing.T) {
	// Completely invalid syntax that can't be recovered
	source := `not a valid schema at all!!!`
	loadErr(t, source)
}

func TestString_NilContextPanics(t *testing.T) {
	source := `schema "test"`

	assert.Panics(t, func() {
		_, _ = schema.LoadString(nil, source, "test.yammm") //nolint:staticcheck // intentional nil
	})
}

func TestString_DisallowsImports(t *testing.T) {
	source := `schema "test" import "./other"`
	loadErr(t, source)
}

func TestWithDisallowImports_SourcesWithEntry(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	tmpDir := t.TempDir()

	source := `schema "test"

import "./foo" as f

type Bar {
	id String primary
}`

	sources := map[string][]byte{
		filepath.Join(tmpDir, "test.yammm"): []byte(source),
	}

	s, result := schema.LoadSourcesWithEntry(
		ctx, sources, filepath.Join(tmpDir, "test.yammm"), tmpDir,
		schema.WithDisallowImports(),
	)

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify the specific diagnostic code
	var foundCode bool
	for issue := range result.Issues() {
		if issue.Code() == diag.E_IMPORT_NOT_ALLOWED {
			foundCode = true
			break
		}
	}
	assert.True(t, foundCode, "expected E_IMPORT_NOT_ALLOWED diagnostic")
}

func TestString_DataTypeResolution_PreservesCase(t *testing.T) {
	// Verify datatype references preserve declared case end-to-end
	source := `schema "test"

type Name = String[1, 100]
type Email = Pattern["^[^@]+@[^@]+$"]

type Person {
	id String primary
	fullName Name required
	email Email required
}`

	ctx := t.Context()
	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.False(t, result.HasErrors(), "result: %v", result)
	require.NotNil(t, s)

	// Verify datatypes are indexed with preserved case
	nameType, ok := s.DataType("Name")
	require.True(t, ok, "should find DataType 'Name' (not 'name')")
	assert.Equal(t, "Name", nameType.Name())

	emailType, ok := s.DataType("Email")
	require.True(t, ok, "should find DataType 'Email' (not 'email')")
	assert.Equal(t, "Email", emailType.Name())

	// Verify property constraints reference correct names
	person, ok := s.Type("Person")
	require.True(t, ok)

	fullName, ok := person.Property("fullName")
	require.True(t, ok)
	alias, ok := fullName.Constraint().(schema.AliasConstraint)
	require.True(t, ok)
	assert.Equal(t, "Name", alias.DataTypeName(), "should preserve 'Name' case")

	// Verify resolution works (lookup by name matches)
	dt, ok := s.DataType(alias.DataTypeName())
	require.True(t, ok, "alias name should match indexed datatype")
	assert.Equal(t, nameType, dt)
}

func TestSources_SimpleSchema(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main" type Person { name String primary }`),
	}
	ctx := t.Context()

	s, result := schema.LoadSources(ctx, sources, "/project")

	require.NotNil(t, s)
	assert.Equal(t, "main", s.Name())
	assert.False(t, result.HasErrors())
}

func TestSources_EmptySources(t *testing.T) {
	sources := map[string][]byte{}
	ctx := t.Context()

	_, result := schema.LoadSources(ctx, sources, "/project")

	require.True(t, result.HasFatal())
	assert.Contains(t, result.String(), "no sources provided")
}

func TestSources_NilContextPanics(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}

	assert.Panics(t, func() {
		_, _ = schema.LoadSources(nil, sources, "/project") //nolint:staticcheck // intentional nil
	})
}

func TestSourcesWithEntry_ExplicitEntry(t *testing.T) {
	// Create two schemas where "beta.yammm" would be selected lexicographically
	// but we explicitly select "gamma.yammm" as entry
	sources := map[string][]byte{
		"beta.yammm":  []byte(`schema "beta" type BetaType { id String primary }`),
		"gamma.yammm": []byte(`schema "gamma" type GammaType { name String primary }`),
	}
	ctx := t.Context()

	// Explicitly select gamma.yammm as entry (not beta which comes first lexicographically)
	s, result := schema.LoadSourcesWithEntry(ctx, sources, "gamma.yammm", "/project")

	require.NotNil(t, s)
	assert.Equal(t, "gamma", s.Name(), "should load gamma schema, not beta")
	assert.False(t, result.HasErrors())

	// Verify gamma's type is present
	_, ok := s.Type("GammaType")
	assert.True(t, ok, "GammaType should exist in loaded schema")
}

func TestSourcesWithEntry_FallbackToLexicographic(t *testing.T) {
	sources := map[string][]byte{
		"beta.yammm":  []byte(`schema "beta" type BetaType { id String primary }`),
		"alpha.yammm": []byte(`schema "alpha" type AlphaType { name String primary }`),
	}
	ctx := t.Context()

	// Empty entry path should fall back to lexicographic order (alpha comes first)
	s, result := schema.LoadSourcesWithEntry(ctx, sources, "", "/project")

	require.NotNil(t, s)
	assert.Equal(t, "alpha", s.Name(), "should load alpha schema (lexicographically first)")
	assert.False(t, result.HasErrors())
}

func TestSourcesWithEntry_EntryNotInSources(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}
	ctx := t.Context()

	// Request an entry that doesn't exist
	_, result := schema.LoadSourcesWithEntry(ctx, sources, "nonexistent.yammm", "/project")

	require.True(t, result.HasFatal())
	assert.Contains(t, result.String(), "not found in sources")
}

func TestSourcesWithEntry_NilContextPanics(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}

	assert.Panics(t, func() {
		_, _ = schema.LoadSourcesWithEntry(nil, sources, "main.yammm", "/project") //nolint:staticcheck // intentional nil
	})
}

func TestSourcesWithEntry_EmptySources(t *testing.T) {
	sources := map[string][]byte{}
	ctx := t.Context()

	_, result := schema.LoadSourcesWithEntry(ctx, sources, "main.yammm", "/project")

	require.True(t, result.HasFatal())
	assert.Contains(t, result.String(), "no sources provided")
}

func TestLoad_FileNotFound(t *testing.T) {
	ctx := t.Context()

	_, result := schema.Load(ctx, "/nonexistent/path/schema.yammm")

	require.True(t, result.HasFatal())
}

func TestLoad_NilContextPanics(t *testing.T) {
	assert.Panics(t, func() {
		_, _ = schema.Load(nil, "/some/path.yammm") //nolint:staticcheck // intentional nil
	})
}

func TestLoad_RealFile(t *testing.T) {
	// Create a temporary directory and file
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "test.yammm")
	content := `schema "test" type User { id String primary }`
	err := os.WriteFile(schemaPath, []byte(content), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, schemaPath)

	require.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
	assert.False(t, result.HasErrors())

	typ, ok := s.Type("User")
	require.True(t, ok)
	assert.Equal(t, "User", typ.Name())
}

func TestLoad_WithImport(t *testing.T) {
	// Create a temporary directory with multiple files
	tmpDir := t.TempDir()

	// Create common.yammm
	commonPath := filepath.Join(tmpDir, "common.yammm")
	commonContent := `schema "common" type Base { id String primary }`
	err := os.WriteFile(commonPath, []byte(commonContent), 0o600)
	require.NoError(t, err)

	// Create main.yammm that imports common
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./common" type User extends common.Base { name String }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.Equal(t, "main", s.Name())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	typ, ok := s.Type("User")
	require.True(t, ok)
	assert.Equal(t, "User", typ.Name())
}

func TestLoad_ImportCycle(t *testing.T) {
	// Create a temporary directory with cyclic imports
	tmpDir := t.TempDir()

	// Create a.yammm that imports b
	aPath := filepath.Join(tmpDir, "a.yammm")
	aContent := `schema "a" import "./b"`
	err := os.WriteFile(aPath, []byte(aContent), 0o600)
	require.NoError(t, err)

	// Create b.yammm that imports a (cycle)
	bPath := filepath.Join(tmpDir, "b.yammm")
	bContent := `schema "b" import "./a"`
	err = os.WriteFile(bPath, []byte(bContent), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(tmpDir))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestLoad_DuplicateImport(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create common.yammm
	commonPath := filepath.Join(tmpDir, "common.yammm")
	commonContent := `schema "common" type Base { id String }`
	err := os.WriteFile(commonPath, []byte(commonContent), 0o600)
	require.NoError(t, err)

	// Create main.yammm with duplicate import
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./common" import "./common"`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestWithRegistry(t *testing.T) {
	source := `schema "test" type Person { name String primary }`
	ctx := t.Context()
	registry := schema.NewRegistry()

	s, result := schema.LoadString(ctx, source, "test.yammm", schema.WithRegistry(registry))

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	// The schema should be in the provided registry
	found, ok := registry.LookupByName("test")
	assert.True(t, ok)
	assert.Same(t, s, found)
}

func TestWithIssueLimit(t *testing.T) {
	// Test that issue limit option is respected
	source := `schema "test" type Person { name String primary }`
	ctx := t.Context()

	// Test with issue limit (should still work for valid schema)
	s, result := schema.LoadString(ctx, source, "test.yammm", schema.WithIssueLimit(1))

	assert.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestLoad_ReservedKeywordAlias(t *testing.T) {
	// Create a temporary directory
	tmpDir := t.TempDir()

	// Create a schema file
	otherPath := filepath.Join(tmpDir, "other.yammm")
	err := os.WriteFile(otherPath, []byte(`schema "other"`), 0o600)
	require.NoError(t, err)

	// Create main.yammm with reserved keyword alias
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./other" as type` // "type" is reserved
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestLoad_PathEscape(t *testing.T) {
	// Create a temporary directory structure with nested folders
	tmpDir := t.TempDir()
	moduleRoot := filepath.Join(tmpDir, "project")
	require.NoError(t, os.Mkdir(moduleRoot, 0o750))

	// Create an "escaped" schema outside the module root but inside tmpDir
	outsidePath := filepath.Join(tmpDir, "escaped.yammm")
	escapedContent := `schema "escaped"`
	require.NoError(t, os.WriteFile(outsidePath, []byte(escapedContent), 0o600))

	// Create a schema file inside the module root
	mainPath := filepath.Join(moduleRoot, "main.yammm")
	// Import attempts to escape the module root
	mainContent := `schema "main" import "../escaped"`
	err := os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err)

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(moduleRoot))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify the error is about path escape
	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		if strings.Contains(issue.Message(), "escapes module root") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected path escape error, got: %v", result)
}

func TestLoad_SymlinkCanonicalization(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o750))

	// Create a schema file in subdir
	schemaPath := filepath.Join(subDir, "test.yammm")
	content := `schema "test" type Person { name String primary }`
	require.NoError(t, os.WriteFile(schemaPath, []byte(content), 0o600))

	// Create a symlink to the schema
	linkPath := filepath.Join(tmpDir, "link.yammm")
	if err := os.Symlink(schemaPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	ctx := t.Context()

	// Load via symlink
	s1, result1 := schema.Load(ctx, linkPath)
	require.NotNil(t, s1)
	assert.False(t, result1.HasErrors())

	// Load directly
	s2, result2 := schema.Load(ctx, schemaPath)
	require.NotNil(t, s2)
	assert.False(t, result2.HasErrors())

	// Both should resolve to the same SourceID
	assert.Equal(t, s1.SourceID(), s2.SourceID(), "Symlink and direct path should resolve to same SourceID")
}

func TestString_InvariantExpression(t *testing.T) {
	source := `schema "test"

type Item {
	id String primary
	quantity Integer
	! "Quantity must be non-negative" quantity >= 0
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	itemType, ok := s.Type("Item")
	require.True(t, ok)

	invs := itemType.InvariantsSlice()
	require.Len(t, invs, 1)

	e := invs[0].Expression()
	require.NotNil(t, e, "invariant expression should survive load pipeline")

	// Verify the expression operator (Expression() now returns expr.Expression directly)
	assert.Equal(t, ">=", e.Op(), "expression should be a >= comparison")
}

func TestString_RelationTargetIDResolved(t *testing.T) {
	source := `schema "test"

type Target { id String primary }

type Owner {
	id String primary
	--> rel Target
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	ownerType, ok := s.Type("Owner")
	require.True(t, ok)

	assocs := ownerType.AssociationsSlice()
	require.Len(t, assocs, 1)

	rel := assocs[0]
	assert.False(t, rel.TargetID().IsZero(), "relation targetID should be resolved")
	assert.Equal(t, "Target", rel.TargetID().Name())
	assert.Equal(t, s.SourceID(), rel.TargetID().SchemaPath())
}

func TestString_CompositionTargetIDResolved(t *testing.T) {
	source := `schema "test"

part type Component { id String }

type Container {
	id String primary
	*-> parts Component
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	containerType, ok := s.Type("Container")
	require.True(t, ok)

	comps := containerType.CompositionsSlice()
	require.Len(t, comps, 1)

	rel := comps[0]
	assert.False(t, rel.TargetID().IsZero(), "composition targetID should be resolved")
	assert.Equal(t, "Component", rel.TargetID().Name())
	assert.Equal(t, s.SourceID(), rel.TargetID().SchemaPath())
}

func TestLoad_CrossSchemaRelationTargetID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create target.yammm
	targetPath := filepath.Join(tmpDir, "target.yammm")
	targetContent := `schema "target" type Entity { id String primary }`
	require.NoError(t, os.WriteFile(targetPath, []byte(targetContent), 0o600))

	// Create main.yammm with a relation to the imported type
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main"
import "./target" as t
type Owner { id String primary --> rel t.Entity }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	ownerType, ok := s.Type("Owner")
	require.True(t, ok)

	assocs := ownerType.AssociationsSlice()
	require.Len(t, assocs, 1)

	rel := assocs[0]
	assert.False(t, rel.TargetID().IsZero(), "relation targetID should be resolved")
	assert.Equal(t, "Entity", rel.TargetID().Name())

	// TargetID.SchemaPath should be the imported schema, not main
	assert.NotEqual(t, s.SourceID(), rel.TargetID().SchemaPath(), "targetID should point to imported schema")
}

func TestLoad_CrossSchemaCompositionTargetID(t *testing.T) {
	tmpDir := t.TempDir()

	// Create component.yammm with a part type
	componentPath := filepath.Join(tmpDir, "component.yammm")
	componentContent := `schema "component" part type Widget { id String }`
	require.NoError(t, os.WriteFile(componentPath, []byte(componentContent), 0o600))

	// Create main.yammm with a composition to the imported part type
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main"
import "./component" as c
type Container { id String primary *-> widgets c.Widget }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	containerType, ok := s.Type("Container")
	require.True(t, ok)

	comps := containerType.CompositionsSlice()
	require.Len(t, comps, 1)

	rel := comps[0]
	assert.False(t, rel.TargetID().IsZero(), "composition targetID should be resolved")
	assert.Equal(t, "Widget", rel.TargetID().Name())

	// TargetID.SchemaPath should be the imported schema, not main
	assert.NotEqual(t, s.SourceID(), rel.TargetID().SchemaPath(), "targetID should point to imported schema")
}

// TestString_Contract19_SchemaNilWhenErrorsExist verifies:
// Schema must be nil whenever Result.HasErrors() is true.
// This test uses a validation error (undefined type reference) rather than a parse error
// to ensure the invariant holds through the completion phase.
func TestString_Contract19_SchemaNilWhenErrorsExist(t *testing.T) {
	// Schema with an undefined type reference in a relation
	source := `schema "test"
type Person {
	name String
	--> friend NonExistentType
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	// schema must be nil when errors exist
	require.True(t, result.HasErrors(), "should have validation errors for undefined type")
	require.Nil(t, s, "schema must be nil when Result.HasErrors() is true")
}

// TestSources_RecoveryAfterParseFailure verifies that the loader properly cleans up
// internal state after a parse failure, allowing subsequent loads to succeed.
// Regression: loadingSchemas markers must be cleared on all exit paths to prevent
// false "import cycle" errors.
func TestSources_RecoveryAfterParseFailure(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	// Step 1: Attempt to load schema with parse error
	brokenSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
type Person {
	INVALID SYNTAX HERE
}`),
	}

	s1, result1 := schema.LoadSources(ctx, brokenSources, tmpDir)
	require.True(t, result1.HasErrors(), "should have parse errors")
	require.Nil(t, s1, "schema should be nil on errors")

	// Step 2: Fix the schema and reload (simulating user fixing the file)
	// This MUST succeed - if loadingSchemas was not cleaned up properly,
	// this would fail with a false "import cycle" error.
	fixedSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
type Person {
	name String primary
}`),
	}

	s2, result2 := schema.LoadSources(ctx, fixedSources, tmpDir)
	require.False(t, result2.HasErrors(), "fixed schema should have no errors: %v", result2)
	require.NotNil(t, s2, "fixed schema should load successfully")
}

// TestLoad_CrossSchemaInheritance_AllProperties verifies that cross-schema
// inheritance properly inherits properties via AllProperties().
func TestLoad_CrossSchemaInheritance_AllProperties(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base.yammm with base type
	basePath := filepath.Join(tmpDir, "base.yammm")
	baseContent := `schema "base"
type Base {
	id String primary
	name String
}`
	require.NoError(t, os.WriteFile(basePath, []byte(baseContent), 0o600))

	// Create derived.yammm that imports and extends
	derivedPath := filepath.Join(tmpDir, "derived.yammm")
	derivedContent := `schema "derived"
import "./base" as b
type Child extends b.Base {
	age Integer
}`
	require.NoError(t, os.WriteFile(derivedPath, []byte(derivedContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, derivedPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors())

	child, ok := s.Type("Child")
	require.True(t, ok)

	// Verify inherited properties are visible
	allProps := child.AllPropertiesSlice()
	propNames := make([]string, len(allProps))
	for i, p := range allProps {
		propNames[i] = p.Name()
	}

	assert.Contains(t, propNames, "id", "should inherit 'id' from Base")
	assert.Contains(t, propNames, "name", "should inherit 'name' from Base")
	assert.Contains(t, propNames, "age", "should have own 'age' property")
	assert.Len(t, allProps, 3, "should have exactly 3 properties (2 inherited + 1 own)")

	// Verify own properties are accessible
	props := child.PropertiesSlice()
	assert.Len(t, props, 1, "should have 1 own property")
	assert.Equal(t, "age", props[0].Name())

	// Verify supertype relationship
	superTypes := child.SuperTypesSlice()
	require.Len(t, superTypes, 1)
	assert.Equal(t, "Base", superTypes[0].Name())
}

// TestSources_RecoveryAfterImportFailure verifies that the loader cleans up
// loadingSchemas markers for both the main schema and its imports when an import
// fails. This tests cascading cleanup.
func TestSources_RecoveryAfterImportFailure(t *testing.T) {
	ctx := t.Context()
	tmpDir := t.TempDir()

	// Step 1: Load with broken import
	brokenSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./helper"
type Person {
	name String
}`),
		"helper.yammm": []byte(`INVALID`),
	}

	s1, result1 := schema.LoadSources(ctx, brokenSources, tmpDir)
	require.True(t, result1.HasErrors())
	require.Nil(t, s1)

	// Step 2: Fix the helper and reload
	// Both main.yammm AND helper.yammm markers must have been cleaned up
	fixedSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
import "./helper"
type Person {
	name String
}`),
		"helper.yammm": []byte(`schema "helper"
type Base {
	id UUID primary
}`),
	}

	s2, result2 := schema.LoadSources(ctx, fixedSources, tmpDir)
	require.False(t, result2.HasErrors(), "should not report false import cycle: %v", result2)
	require.NotNil(t, s2)
}

// TestLoad_MultiImportSameLevel verifies that a schema importing multiple other
// schemas at the same level correctly resolves references to all of them.
func TestLoad_MultiImportSameLevel(t *testing.T) {
	tmpDir := t.TempDir()

	// Create b.yammm
	bPath := filepath.Join(tmpDir, "b.yammm")
	bContent := `schema "b" type Entity { id String primary }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create c.yammm
	cPath := filepath.Join(tmpDir, "c.yammm")
	cContent := `schema "c" type Other { name String primary }`
	require.NoError(t, os.WriteFile(cPath, []byte(cContent), 0o600))

	// Create a.yammm that imports both
	aPath := filepath.Join(tmpDir, "a.yammm")
	aContent := `schema "a"
import "./b" as b
import "./c" as c
type Connector {
	id String primary
	--> toB b.Entity
	--> toC c.Other
}`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	conn, ok := s.Type("Connector")
	require.True(t, ok)

	assocs := conn.AssociationsSlice()
	require.Len(t, assocs, 2)

	// Verify both cross-schema relations are properly resolved
	for _, rel := range assocs {
		assert.False(t, rel.TargetID().IsZero(),
			"relation %s targetID should be resolved", rel.Name())
	}

	// Verify imports are present
	imports := s.ImportsSlice()
	require.Len(t, imports, 2, "should have 2 imports")

	aliases := make([]string, len(imports))
	for i, imp := range imports {
		aliases[i] = imp.Alias()
	}
	assert.Contains(t, aliases, "b")
	assert.Contains(t, aliases, "c")
}

// TestLoad_NestedImports verifies that nested import chains work correctly:
// A imports B, B imports D. A should only see its own imports (b), not transitive (d).
func TestLoad_NestedImports(t *testing.T) {
	tmpDir := t.TempDir()

	// Create d.yammm (deepest)
	dPath := filepath.Join(tmpDir, "d.yammm")
	dContent := `schema "d" type Deep { id String primary }`
	require.NoError(t, os.WriteFile(dPath, []byte(dContent), 0o600))

	// Create b.yammm that imports d
	bPath := filepath.Join(tmpDir, "b.yammm")
	bContent := `schema "b"
import "./d" as d
type Middle extends d.Deep { name String }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create a.yammm that imports b
	aPath := filepath.Join(tmpDir, "a.yammm")
	aContent := `schema "a"
import "./b" as b
type Top extends b.Middle { age Integer }`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	// Verify A only has import for B, not D (D is B's transitive dependency)
	imports := s.ImportsSlice()
	require.Len(t, imports, 1, "A should only have 1 import (b), not transitive (d)")
	assert.Equal(t, "b", imports[0].Alias())

	// Verify Top type inherits correctly through the chain
	top, ok := s.Type("Top")
	require.True(t, ok)

	allProps := top.AllPropertiesSlice()
	propNames := make([]string, len(allProps))
	for i, p := range allProps {
		propNames[i] = p.Name()
	}
	assert.Contains(t, propNames, "id", "should inherit 'id' from Deep via Middle")
	assert.Contains(t, propNames, "name", "should inherit 'name' from Middle")
	assert.Contains(t, propNames, "age", "should have own 'age' property")
}

// TestLoad_DiamondImportPattern verifies that diamond-shaped import graphs work:
// A imports B and C, both B and C import D.
func TestLoad_DiamondImportPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create d.yammm (common dependency)
	dPath := filepath.Join(tmpDir, "d.yammm")
	dContent := `schema "d" type Shared { id String primary }`
	require.NoError(t, os.WriteFile(dPath, []byte(dContent), 0o600))

	// Create b.yammm that imports d
	bPath := filepath.Join(tmpDir, "b.yammm")
	bContent := `schema "b"
import "./d" as d
type BType extends d.Shared { bProp String }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create c.yammm that also imports d
	cPath := filepath.Join(tmpDir, "c.yammm")
	cContent := `schema "c"
import "./d" as d
type CType extends d.Shared { cProp String }`
	require.NoError(t, os.WriteFile(cPath, []byte(cContent), 0o600))

	// Create a.yammm that imports both b and c
	aPath := filepath.Join(tmpDir, "a.yammm")
	aContent := `schema "a"
import "./b" as b
import "./c" as c
type AType {
	id String primary
	--> toB b.BType
	--> toC c.CType
}`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result)

	atype, ok := s.Type("AType")
	require.True(t, ok)

	assocs := atype.AssociationsSlice()
	require.Len(t, assocs, 2)

	// Both relations should be resolved
	for _, rel := range assocs {
		assert.False(t, rel.TargetID().IsZero(),
			"relation %s targetID should be resolved", rel.Name())
	}

	// Verify A has 2 imports (b and c)
	imports := s.ImportsSlice()
	require.Len(t, imports, 2, "A should have 2 imports (b and c)")
}

// TestLoad_PathEscape_MultiLevel verifies that multiple levels of ".." path
// escape attempts are blocked by the rootLoader.
func TestLoad_PathEscape_MultiLevel(t *testing.T) {
	// Create a temporary directory structure: tmpDir/project/subdir
	tmpDir := t.TempDir()
	moduleRoot := filepath.Join(tmpDir, "project")
	subdir := filepath.Join(moduleRoot, "subdir")
	require.NoError(t, os.MkdirAll(subdir, 0o750))

	// Create an "escaped" schema two levels up (outside tmpDir/project)
	outsidePath := filepath.Join(tmpDir, "escaped.yammm")
	require.NoError(t, os.WriteFile(outsidePath, []byte(`schema "escaped"`), 0o600))

	// Create a schema file in subdir that tries ../../escaped
	mainPath := filepath.Join(subdir, "main.yammm")
	mainContent := `schema "main" import "../../escaped"`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(moduleRoot))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify the error is about path escape
	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		if strings.Contains(issue.Message(), "escapes module root") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected path escape error for multi-level escape, got: %v", result)
}

// TestLoad_SymlinkEscape verifies that symlinks pointing outside the module
// root are properly blocked when used in import paths.
func TestLoad_SymlinkEscape(t *testing.T) {
	// Create directory structure: tmpDir/project (module root), tmpDir/outside
	tmpDir := t.TempDir()
	moduleRoot := filepath.Join(tmpDir, "project")
	require.NoError(t, os.Mkdir(moduleRoot, 0o750))

	outsideDir := filepath.Join(tmpDir, "outside")
	require.NoError(t, os.Mkdir(outsideDir, 0o750))

	// Create target schema outside the module root
	targetPath := filepath.Join(outsideDir, "secret.yammm")
	require.NoError(t, os.WriteFile(targetPath, []byte(`schema "secret"`), 0o600))

	// Create a symlink inside module root that points outside
	symlinkPath := filepath.Join(moduleRoot, "link.yammm")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Create main schema that imports via the symlink
	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := `schema "main" import "./link"`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(moduleRoot))

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Should fail due to symlink escape - os.Root blocks symlink escapes
	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		// os.Root may report "escapes" or "not found" depending on OS behavior
		if strings.Contains(issue.Message(), "escapes") || strings.Contains(issue.Message(), "not found") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected symlink escape to be blocked, got: %v", result)
}

// TestLoad_BoundaryPath_AtRoot verifies that imports at the exact module root
// boundary work correctly.
func TestLoad_BoundaryPath_AtRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create helper.yammm at the root level
	helperPath := filepath.Join(tmpDir, "helper.yammm")
	require.NoError(t, os.WriteFile(helperPath, []byte(`schema "helper" type Base { id String primary }`), 0o600))

	// Create main.yammm also at root level, importing helper
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./helper" type User extends helper.Base { name String }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should load: %v", result)
	assert.False(t, result.HasErrors())

	// Verify the import worked
	user, ok := s.Type("User")
	require.True(t, ok)
	allProps := user.AllPropertiesSlice()
	assert.Len(t, allProps, 2) // id from Base + name from User
}

// TestLoad_SymlinkWithinModuleRoot_Blocked verifies that os.Root blocks symlinks
// even when they point to files within the module root. This is a security feature:
// os.Root does not follow symlinks to prevent potential path escape attacks.
// Note: Entry point files (via Load()) are handled differently - makeCanonicalPath
// resolves symlinks BEFORE opening the root, which is why TestLoad_SymlinkCanonicalization works.
func TestLoad_SymlinkWithinModuleRoot_Blocked(t *testing.T) {
	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o750))

	// Create target schema inside module root
	targetPath := filepath.Join(subdir, "target.yammm")
	require.NoError(t, os.WriteFile(targetPath, []byte(`schema "target" type Base { id String }`), 0o600))

	// Create a symlink at root level pointing to subdir target (within module root)
	symlinkPath := filepath.Join(tmpDir, "linked.yammm")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	// Create main schema that imports via the symlink
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./linked"`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	// os.Root blocks ALL symlinks, even internal ones - this is security-correct
	assert.Nil(t, s, "symlink imports should be blocked by os.Root")
	assert.True(t, result.HasErrors())

	// Verify the error mentions escape (os.Root treats symlinks as escapes)
	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		if strings.Contains(issue.Message(), "escapes") {
			found = true
			break
		}
	}
	assert.True(t, found, "os.Root should report symlink as path escape: %v", result)
}

// TestLoad_DotDotInMiddleOfPath verifies that ".." components in the middle
// of a path are properly handled.
func TestLoad_DotDotInMiddleOfPath(t *testing.T) {
	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o750))

	// Create helper.yammm at root level
	helperPath := filepath.Join(tmpDir, "helper.yammm")
	require.NoError(t, os.WriteFile(helperPath, []byte(`schema "helper" type Base { id String primary }`), 0o600))

	// Create main.yammm in subdir that imports ../helper
	mainPath := filepath.Join(subdir, "main.yammm")
	mainContent := `schema "main" import "../helper" type User extends helper.Base { name String }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(tmpDir))

	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	// This should work because "../helper" from subdir/ stays within module root
	require.NotNil(t, s, "Relative import staying within root should work: %v", result)
	assert.False(t, result.HasErrors())
}

func TestString_CancellationReturnsError(t *testing.T) {
	source := `schema "test" type Person { name String }`

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	// Cancellation produces a fatal diagnostic with E_CONTEXT_CANCELLED
	assert.Nil(t, s)
	require.True(t, result.HasFatal(), "cancellation should produce fatal diagnostic")

	// Verify the diagnostic uses E_CONTEXT_CANCELLED, not E_INTERNAL
	var foundCancelled bool
	for issue := range result.Issues() {
		assert.NotEqual(t, diag.E_INTERNAL, issue.Code(),
			"cancellation should not be reported as E_INTERNAL diagnostic")
		if issue.Code() == diag.E_CONTEXT_CANCELLED {
			foundCancelled = true
		}
	}
	assert.True(t, foundCancelled, "expected E_CONTEXT_CANCELLED diagnostic")
}

func TestString_DeadlineExceededReturnsError(t *testing.T) {
	source := `schema "test"`

	ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-1*time.Second))
	defer cancel()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	assert.Nil(t, s)
	require.True(t, result.HasFatal(), "deadline exceeded should produce fatal diagnostic")

	var foundCancelled bool
	for issue := range result.Issues() {
		if issue.Code() == diag.E_CONTEXT_CANCELLED {
			foundCancelled = true
		}
	}
	assert.True(t, foundCancelled, "expected E_CONTEXT_CANCELLED diagnostic for deadline exceeded")
}

func TestLoad_CancellationDuringImport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a.yammm and b.yammm
	aPath := filepath.Join(tmpDir, "a.yammm")
	require.NoError(t, os.WriteFile(aPath, []byte(`schema "a" import "./b.yammm"`), 0o600))

	bPath := filepath.Join(tmpDir, "b.yammm")
	require.NoError(t, os.WriteFile(bPath, []byte(`schema "b" type X { id String }`), 0o600))

	// Create context that gets cancelled
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(tmpDir))

	assert.Nil(t, s)
	require.True(t, result.HasFatal(), "cancellation should produce fatal diagnostic")
}

func TestLoad_TransitiveImportsChain(t *testing.T) {
	// Test A imports B, B imports C (transitive imports)
	moduleRoot := t.TempDir()

	// Create C (base)
	cPath := filepath.Join(moduleRoot, "c.yammm")
	require.NoError(t, os.WriteFile(cPath, []byte(`schema "c" type BaseType { name String primary }`), 0o600))

	// Create B (imports C)
	bPath := filepath.Join(moduleRoot, "b.yammm")
	bContent := `schema "b"
import "./c" as c
type Middle { id String primary base c.BaseType }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create A (imports B)
	aPath := filepath.Join(moduleRoot, "a.yammm")
	aContent := `schema "a"
import "./b" as b
type Top { id String primary middle b.Middle }`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(moduleRoot))

	require.NotNil(t, s)
	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	assert.False(t, result.HasErrors())
}

func TestLoad_CycleDetection(t *testing.T) {
	// Test import cycle detection: A imports B, B imports A
	moduleRoot := t.TempDir()

	// Create A
	aPath := filepath.Join(moduleRoot, "a.yammm")
	aContent := `schema "a"
import "./b" as b
type TypeA { ref b.TypeB }`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	// Create B (imports A, creating cycle)
	bPath := filepath.Join(moduleRoot, "b.yammm")
	bContent := `schema "b"
import "./a" as a
type TypeB { ref a.TypeA }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(moduleRoot))

	// Cycle should be detected
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
	// Check for cycle error
	hasImportCycle := false
	for _, issue := range slices.Collect(result.Issues()) {
		if strings.Contains(issue.Message(), "cycle") {
			hasImportCycle = true
			break
		}
	}
	assert.True(t, hasImportCycle, "expected import cycle error")
}

func TestLoad_NonExistentPath(t *testing.T) {
	// Test loading from a path that doesn't exist
	ctx := t.Context()

	s, result := schema.Load(ctx, "/nonexistent/path/to/schema.yammm")

	require.True(t, result.HasFatal())
	assert.Nil(t, s)
}

func TestSources_ImportBetweenSources(t *testing.T) {
	// Test Sources with one in-memory schema importing another
	ctx := t.Context()
	moduleRoot := t.TempDir()

	basePath := filepath.Join(moduleRoot, "base.yammm")
	baseContent := []byte(`schema "base"
type BaseEntity {
	id UUID primary
	name String
}`)

	derivedPath := filepath.Join(moduleRoot, "derived.yammm")
	derivedContent := []byte(`schema "derived"
import "./base" as b
type Derived extends b.BaseEntity {
	extra String
}`)

	sources := map[string][]byte{
		basePath:    baseContent,
		derivedPath: derivedContent,
	}

	s, result := schema.LoadSources(ctx, sources, moduleRoot)
	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
		t.FailNow()
	}
	require.NotNil(t, s)
}

func TestSources_ContextCancellation(t *testing.T) {
	// Test that Sources handles context cancellation
	moduleRoot := t.TempDir()

	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := []byte(`schema "main" type Person { name String }`)

	sources := map[string][]byte{
		mainPath: mainContent,
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	s, result := schema.LoadSources(ctx, sources, moduleRoot)

	require.True(t, result.HasFatal(), "cancellation should produce fatal diagnostic")
	assert.Nil(t, s)
}

func TestLoad_WithLogger(t *testing.T) {
	// Test that WithLogger option works
	moduleRoot := t.TempDir()

	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := `schema "main" type Person { name String primary }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath,
		schema.WithModuleRoot(moduleRoot),
		schema.WithLogger(logger))

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestLoad_MultipleImports(t *testing.T) {
	// Test loading schema with multiple imports
	moduleRoot := t.TempDir()

	// Create three helper schemas
	helper1Path := filepath.Join(moduleRoot, "helper1.yammm")
	require.NoError(t, os.WriteFile(helper1Path, []byte(`schema "helper1" type Type1 { name String primary }`), 0o600))

	helper2Path := filepath.Join(moduleRoot, "helper2.yammm")
	require.NoError(t, os.WriteFile(helper2Path, []byte(`schema "helper2" type Type2 { id String primary value Integer }`), 0o600))

	helper3Path := filepath.Join(moduleRoot, "helper3.yammm")
	require.NoError(t, os.WriteFile(helper3Path, []byte(`schema "helper3" type Type3 { id String primary flag Boolean }`), 0o600))

	// Main imports all three
	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := `schema "main"
import "./helper1" as h1
import "./helper2" as h2
import "./helper3" as h3
type Combined {
	id String primary
	ref1 h1.Type1
	ref2 h2.Type2
	ref3 h3.Type3
}`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, mainPath, schema.WithModuleRoot(moduleRoot))

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestString_InvalidSyntax(t *testing.T) {
	// Test String with invalid syntax
	ctx := t.Context()

	source := `schema "broken"
type Entity {
	name String
	// missing closing brace`

	s, result := schema.LoadString(ctx, source, "test://broken.yammm")

	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestString_ContextCancellation(t *testing.T) {
	// Test that String handles context cancellation
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	source := `schema "test" type Test { name String }`
	s, result := schema.LoadString(ctx, source, "test.yammm")

	assert.Nil(t, s)
	require.True(t, result.HasFatal(), "cancellation should produce fatal diagnostic")
}

func TestLoad_WithSchemaRegistry(t *testing.T) {
	// Test loading with a pre-populated schema registry
	tmpDir := t.TempDir()

	schemaPath := filepath.Join(tmpDir, "test.yammm")
	content := `schema "test" type Test { name String primary }`
	require.NoError(t, os.WriteFile(schemaPath, []byte(content), 0o600))

	// Create a registry
	reg := schema.NewRegistry()

	ctx := t.Context()
	s, result := schema.Load(ctx, schemaPath, schema.WithRegistry(reg))

	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	// Verify schema was registered
	found, ok := reg.LookupBySourceID(s.SourceID())
	assert.NotNil(t, found)
	assert.True(t, ok)
}

func TestLoad_DiamondImport(t *testing.T) {
	// Test diamond import pattern: A->B, A->C, B->D, C->D
	// This tests that D is only loaded once
	moduleRoot := t.TempDir()

	// D is the base
	dPath := filepath.Join(moduleRoot, "d.yammm")
	require.NoError(t, os.WriteFile(dPath, []byte(`schema "d" type BaseD { name String primary }`), 0o600))

	// B imports D
	bPath := filepath.Join(moduleRoot, "b.yammm")
	require.NoError(t, os.WriteFile(bPath, []byte(`schema "b"
import "./d" as d
type TypeB { id String primary ref d.BaseD }`), 0o600))

	// C imports D
	cPath := filepath.Join(moduleRoot, "c.yammm")
	require.NoError(t, os.WriteFile(cPath, []byte(`schema "c"
import "./d" as d
type TypeC { id String primary ref d.BaseD }`), 0o600))

	// A imports B and C
	aPath := filepath.Join(moduleRoot, "a.yammm")
	require.NoError(t, os.WriteFile(aPath, []byte(`schema "a"
import "./b" as b
import "./c" as c
type TypeA {
	id String primary
	refB b.TypeB
	refC c.TypeC
}`), 0o600))

	ctx := t.Context()
	s, result := schema.Load(ctx, aPath, schema.WithModuleRoot(moduleRoot))

	require.NotNil(t, s)
	if result.HasErrors() {
		for _, issue := range slices.Collect(result.Issues()) {
			t.Logf("Error: %v", issue)
		}
	}
	assert.False(t, result.HasErrors())
}

// TestString_InvariantUnknownProperty verifies that an invariant referencing
// a property that does not exist on the type produces E_UNKNOWN_PROPERTY when
// loaded end-to-end from source text.
func TestString_InvariantUnknownProperty(t *testing.T) {
	t.Parallel()

	source := `schema "test"

type Person {
	name String
	! "bad_check" fake_property != ""
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.True(t, result.HasErrors(), "should have validation errors for unknown property")
	require.Nil(t, s, "schema must be nil when Result.HasErrors() is true")

	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "fake_property",
				"error message should mention the unknown property name")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY diagnostic")
}

// TestString_InvariantValidProperty verifies that a schema with valid
// invariants referencing existing properties compiles successfully end-to-end.
func TestString_InvariantValidProperty(t *testing.T) {
	t.Parallel()

	source := `schema "test"

type Person {
	name String primary
	age Integer

	! "name_not_empty" name != ""
	! "age_positive" age >= 0
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.NotNil(t, s, "schema should compile successfully")
	assert.False(t, result.HasErrors(), "unexpected errors: %v", result)

	personType, ok := s.Type("Person")
	require.True(t, ok, "Person type should exist")

	invs := personType.InvariantsSlice()
	require.Len(t, invs, 2, "should have two invariants")

	// Verify both invariant expressions survived the pipeline
	for _, inv := range invs {
		require.NotNil(t, inv.Expression(),
			"invariant %q expression should survive load pipeline", inv.Name())
	}
}

// TestString_InvariantLambdaUnknownProperty verifies that a lambda parameter
// accessing a nonexistent property on a composition target type produces
// E_UNKNOWN_PROPERTY when loaded end-to-end from source text.
func TestString_InvariantLambdaUnknownProperty(t *testing.T) {
	t.Parallel()

	source := `schema "test"

part type LineItem {
	quantity Integer
	price Float
}

type Order {
	id String primary

	*-> ITEMS (many) LineItem

	! "bad_field" ITEMS -> All |$item| { $item.nonexistent > 0 }
}`
	ctx := t.Context()

	s, result := schema.LoadString(ctx, source, "test.yammm")

	require.True(t, result.HasErrors(), "should have validation errors for unknown property on LineItem")
	require.Nil(t, s, "schema must be nil when Result.HasErrors() is true")

	found := false
	for _, issue := range slices.Collect(result.Issues()) {
		if issue.Code() == diag.E_UNKNOWN_PROPERTY {
			found = true
			assert.Contains(t, issue.Message(), "nonexistent",
				"error message should mention the unknown property name")
			assert.Contains(t, issue.Message(), "LineItem",
				"error message should mention the target type name")
		}
	}
	assert.True(t, found, "expected E_UNKNOWN_PROPERTY for nonexistent on LineItem")
}

// --- Shared-Registry cross-Load tests ---

// writeSharedRegistryFixtures sets up a tempdir with:
//   - common.yammm — a leaf schema shared as a transitive import
//   - root_a.yammm, root_b.yammm — two independent roots both importing common
//
// Returns the tempdir path. Fixtures live only for the test's duration via t.TempDir().
func writeSharedRegistryFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "common.yammm"),
		[]byte(`schema "common" type BaseCommon { id String primary }`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "root_a.yammm"),
		[]byte(`schema "root_a"
import "./common" as c
type HolderA { id String primary ref c.BaseCommon }`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "root_b.yammm"),
		[]byte(`schema "root_b"
import "./common" as c
type HolderB { id String primary ref c.BaseCommon }`),
		0o600,
	))

	return dir
}

// resolveImportSchema returns the resolved *Schema for the import with the
// given alias on s. Fails the test if no such import is present. Reusable
// helper — kept general rather than hard-coding the "c" alias so future
// shared-Registry tests with different alias conventions can use it.
//
//nolint:unparam // kept general for future callers
func resolveImportSchema(t *testing.T, s *schema.Schema, alias string) *schema.Schema {
	t.Helper()
	imp, ok := s.ImportByAlias(alias)
	require.True(t, ok, "import alias %q not found on schema %q", alias, s.Name())
	resolved := imp.Schema()
	require.NotNil(t, resolved, "import %q on schema %q has no resolved schema", alias, s.Name())
	return resolved
}

func TestLoad_SharedRegistry_NonOverlapping(t *testing.T) {
	// Two independent schemas with no shared imports, one shared Registry.
	// Both Load calls must succeed; registry must end with exactly 2 schemas.
	dir := t.TempDir()

	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "alpha.yammm"),
		[]byte(`schema "alpha" type Alpha { id String primary }`),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "beta.yammm"),
		[]byte(`schema "beta" type Beta { id String primary }`),
		0o600,
	))

	ctx := t.Context()
	reg := schema.NewRegistry()

	sA, resA := schema.Load(ctx, filepath.Join(dir, "alpha.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sA)
	require.False(t, resA.HasErrors(), "errors: %v", resA)

	sB, resB := schema.Load(ctx, filepath.Join(dir, "beta.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sB)
	require.False(t, resB.HasErrors(), "errors: %v", resB)

	assert.Equal(t, 2, reg.Len(), "non-overlapping loads: registry holds exactly both roots")
}

func TestLoad_SharedRegistry_OverlappingImports(t *testing.T) {
	// The core cross-Load short-circuit test. Two roots both importing
	// common.yammm against a shared Registry: each root registers once,
	// common.yammm registers once, reg.Len() == 3.
	dir := writeSharedRegistryFixtures(t)

	ctx := t.Context()
	reg := schema.NewRegistry()

	sA, resA := schema.Load(ctx, filepath.Join(dir, "root_a.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sA)
	require.False(t, resA.HasErrors(), "errors: %v", resA)

	sB, resB := schema.Load(ctx, filepath.Join(dir, "root_b.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sB)
	require.False(t, resB.HasErrors(), "errors: %v", resB)

	assert.Equal(t, 3, reg.Len(), "expected registry: [common, root_a, root_b]")

	// The common-schema pointer must be identical across both root schemas.
	// This is the direct observation of the cross-Load short-circuit: both
	// Loads resolved the import to the same *Schema pointer.
	commonFromA := resolveImportSchema(t, sA, "c")
	commonFromB := resolveImportSchema(t, sB, "c")
	assert.Same(t, commonFromA, commonFromB,
		"cross-Load short-circuit must resolve overlapping imports to the same *Schema pointer")

	// And the registry's stored pointer must match both.
	regCommon, ok := reg.LookupBySourceID(commonFromA.SourceID())
	require.True(t, ok, "common schema must be in the registry")
	assert.Same(t, commonFromA, regCommon,
		"registry-stored pointer must match the pointer resolved through Import.Schema()")
}

func TestLoad_SharedRegistry_ObservesCrossLoadShortCircuit(t *testing.T) {
	// Stronger observation: delete the common-schema source file after the
	// first Load. The second Load must still succeed — which is only possible
	// if the cross-Load short-circuit fired before the parse path (which
	// would have tried to read the deleted file).
	dir := writeSharedRegistryFixtures(t)

	ctx := t.Context()
	reg := schema.NewRegistry()

	sA, resA := schema.Load(ctx, filepath.Join(dir, "root_a.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sA)
	require.False(t, resA.HasErrors(), "first load unexpectedly failed: %v", resA)

	// Remove the shared import source. A naive second Load that re-parses
	// the transitive import would fail with a file-not-found style error.
	require.NoError(t, os.Remove(filepath.Join(dir, "common.yammm")))

	sB, resB := schema.Load(ctx, filepath.Join(dir, "root_b.yammm"),
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, sB,
		"second Load must succeed via the cross-Load short-circuit; common.yammm was deleted")
	require.False(t, resB.HasErrors(),
		"no parse of common.yammm should occur on the second Load — errors: %v",
		resB)

	// Sanity: the second Load's resolved common pointer must equal the first's.
	assert.Same(t,
		resolveImportSchema(t, sA, "c"),
		resolveImportSchema(t, sB, "c"),
		"cross-Load short-circuit must reuse the first Load's common *Schema")
}

func TestLoad_DefaultFreshRegistryPerCall(t *testing.T) {
	// Pin the default: when WithRegistry is absent, each Load constructs its
	// own Registry, so two independent Loads do not share state. Guards against
	// an accidental future change that would flip the default to shared.
	dir := writeSharedRegistryFixtures(t)
	ctx := t.Context()

	sA, resA := schema.Load(ctx, filepath.Join(dir, "root_a.yammm"),
		schema.WithModuleRoot(dir))
	require.NotNil(t, sA)
	require.False(t, resA.HasErrors())

	sB, resB := schema.Load(ctx, filepath.Join(dir, "root_b.yammm"),
		schema.WithModuleRoot(dir))
	require.NotNil(t, sB)
	require.False(t, resB.HasErrors())

	// Each Load materialized its own common-schema instance (no Registry was
	// shared). Different pointers prove the per-Load default is intact.
	assert.NotSame(t,
		resolveImportSchema(t, sA, "c"),
		resolveImportSchema(t, sB, "c"),
		"fresh-Registry default: independent Loads must produce independent *Schema pointers for shared imports")
}

func TestLoad_SharedRegistry_TopLevelReparse(t *testing.T) {
	// Document the intentional top-level asymmetry. Load(A) twice against the
	// same Registry returns two distinct *Schema pointers, but the registry
	// retains the first pointer. Pins this so future refactors don't quietly
	// flip the short-circuit onto the top-level parse path (which is out of
	// scope for PR-7 per the spec).
	dir := t.TempDir()
	aPath := filepath.Join(dir, "a.yammm")
	require.NoError(t, os.WriteFile(aPath,
		[]byte(`schema "root" type Root { id String primary }`), 0o600))

	ctx := t.Context()
	reg := schema.NewRegistry()

	s1, res1 := schema.Load(ctx, aPath,
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, s1)
	require.False(t, res1.HasErrors())

	s2, res2 := schema.Load(ctx, aPath,
		schema.WithRegistry(reg), schema.WithModuleRoot(dir))
	require.NotNil(t, s2)
	require.False(t, res2.HasErrors(),
		"second Load must succeed — Register hits the idempotence path, not the error path: %v",
		res2)

	assert.NotSame(t, s1, s2,
		"top-level asymmetry: Load returns a freshly-compiled schema per call even under shared Registry")

	// The registry retained the first pointer (idempotent Register is a no-op).
	regStored, ok := reg.LookupBySourceID(s1.SourceID())
	require.True(t, ok)
	assert.Same(t, s1, regStored,
		"registry must retain the first Load's pointer; idempotent Register must not overwrite")
}

// canonicalPath mirrors the loader's path canonicalization (absolute,
// cleaned, symlinks resolved) so ModuleRoot expectations compare equal on
// systems where TempDir rides a symlink (e.g. macOS /var -> /private/var).
func canonicalPath(t *testing.T, path string) string {
	t.Helper()
	abs, err := filepath.Abs(path)
	require.NoError(t, err)
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return filepath.Clean(abs)
}

// writeModuleTree writes a two-file module-style layout under a fresh
// temp root: sub/entry.yammm imports lib/dep.yammm by module path.
// Returns (root, entryPath).
func writeModuleTree(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "lib"), 0o750))
	entry := filepath.Join(root, "sub", "entry.yammm")
	require.NoError(t, os.WriteFile(entry, []byte("schema \"entry\"\n\nimport \"lib/dep\" as dep\n\ntype Thing {\n\tid String primary\n\t--> USES (one) dep.Part\n}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(root, "lib", "dep.yammm"), []byte("schema \"dep\"\n\ntype Part {\n\tpart_id String primary\n}\n"), 0o600))
	return root, entry
}

// TestModuleRoot_Recorded pins Schema.ModuleRoot across every load form:
// the canonicalized WithModuleRoot value when given, the entry directory
// for a plain Load, the canonicalized root for the in-memory loaders
// (including the "." form), and "" for LoadString. Imported sub-schemas
// record the same root as their entry.
func TestModuleRoot_Recorded(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	t.Run("load default is entry dir", func(t *testing.T) {
		t.Parallel()
		dir := t.TempDir()
		path := filepath.Join(dir, "solo.yammm")
		require.NoError(t, os.WriteFile(path, []byte("schema \"solo\"\n\ntype T {\n\tid String primary\n}\n"), 0o600))
		s, res := schema.Load(ctx, path)
		requireOK(t, res)
		assert.Equal(t, canonicalPath(t, dir), s.ModuleRoot())
	})

	t.Run("with module root, entry and import record it", func(t *testing.T) {
		t.Parallel()
		root, entry := writeModuleTree(t)
		s, res := schema.Load(ctx, entry, schema.WithModuleRoot(root))
		requireOK(t, res)
		want := canonicalPath(t, root)
		assert.Equal(t, want, s.ModuleRoot())
		require.Len(t, s.ImportsSlice(), 1)
		sub := s.ImportsSlice()[0].Schema()
		require.NotNil(t, sub)
		assert.Equal(t, want, sub.ModuleRoot(), "imported sub-schema records the same root")
	})

	t.Run("load string records none", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadString(ctx, "schema \"s\"\n\ntype T {\n\tid String primary\n}\n", "inline.yammm")
		requireOK(t, res)
		assert.Empty(t, s.ModuleRoot())
	})

	t.Run("sources with entry records canonicalized dot", func(t *testing.T) {
		s, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
			"x.yammm": []byte("schema \"x\"\n\ntype T {\n\tid String primary\n}\n"),
		}, "x.yammm", ".")
		requireOK(t, res)
		cwd, err := os.Getwd()
		require.NoError(t, err)
		assert.Equal(t, canonicalPath(t, cwd), s.ModuleRoot())
	})
}

// TestWithSourcesOnly pins the hermetic-load contract: with the option, an
// import that misses the pre-registered set fails with E_IMPORT_RESOLVE even
// when a matching file exists on disk under the module root — the filesystem
// fallback is structurally skipped. Without the option the same layout loads
// via the disk fallback; with the full set pre-registered the option is
// satisfied entirely in memory.
func TestWithSourcesOnly(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	entrySrc := []byte("schema \"entry\"\n\nimport \"lib/dep\" as dep\n\ntype Thing {\n\tid String primary\n\t--> USES (one) dep.Part\n}\n")
	depSrc := []byte("schema \"dep\"\n\ntype Part {\n\tpart_id String primary\n}\n")

	t.Run("miss fails instead of reading disk", func(t *testing.T) {
		t.Parallel()
		root, _ := writeModuleTree(t) // dep exists ON DISK under root
		_, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
			"sub/entry.yammm": entrySrc,
		}, "sub/entry.yammm", root, schema.WithSourcesOnly())
		require.True(t, res.HasErrors(), "expected the import miss to fail under WithSourcesOnly")
		assert.Contains(t, res.Err().Error(), "not found in pre-registered sources")
	})

	t.Run("same layout loads via disk without the option", func(t *testing.T) {
		t.Parallel()
		root, _ := writeModuleTree(t)
		s, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
			"sub/entry.yammm": entrySrc,
		}, "sub/entry.yammm", root)
		requireOK(t, res)
		assert.Equal(t, "entry", s.Name())
	})

	t.Run("complete in-memory set satisfies the option", func(t *testing.T) {
		t.Parallel()
		s, res := schema.LoadSourcesWithEntry(ctx, map[string][]byte{
			"sub/entry.yammm": entrySrc,
			"lib/dep.yammm":   depSrc,
		}, "sub/entry.yammm", t.TempDir(), schema.WithSourcesOnly())
		requireOK(t, res)
		assert.Equal(t, "entry", s.Name())
	})
}

// TestSharedRegistry_CacheHitSourcesComplete pins that a cross-Load
// registry cache hit still yields a complete Sources() on the new load:
// the cached import's content is copied into the load's source registry,
// so closure-content consumers (diagnostics rendering, gogen's embedded
// SerializedModel) see every source even when the read+parse pipeline was
// short-circuited.
func TestSharedRegistry_CacheHitSourcesComplete(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	root, entry := writeModuleTree(t)
	registry := schema.NewRegistry()

	first, res := schema.Load(ctx, entry, schema.WithModuleRoot(root), schema.WithRegistry(registry))
	requireOK(t, res)
	second, res := schema.Load(ctx, entry, schema.WithModuleRoot(root), schema.WithRegistry(registry))
	requireOK(t, res)

	require.Len(t, second.ImportsSlice(), 1)
	imp := second.ImportsSlice()[0]
	assert.Same(t, first.ImportsSlice()[0].Schema(), imp.Schema(), "second load should cache-hit the import")

	srcs := second.Sources()
	require.NotNil(t, srcs)
	if _, ok := srcs.ContentBySource(imp.ResolvedSourceID()); !ok {
		t.Error("cache-hit import content missing from the new load's Sources()")
	}
}
