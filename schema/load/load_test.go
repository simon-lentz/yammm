package load_test

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"
)

func TestLoadString_SimpleSchema(t *testing.T) {
	// Note: YAMMM grammar uses `name Type` syntax, not `name: Type`
	source := `schema "test" type Person { name String }`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Unexpected issue: %v", issue)
		}
	}
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, "Person", typ.Name())
}

func TestLoadString_EmptySchema(t *testing.T) {
	source := `schema "empty"`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "empty.yammm")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "empty", s.Name())
	assert.False(t, result.HasErrors())
}

func TestLoadString_SyntaxError(t *testing.T) {
	// Completely invalid syntax that can't be recovered
	source := `not a valid schema at all!!!`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "syntax.yammm")

	require.NoError(t, err) // No Go error, but diagnostics
	assert.Nil(t, s)        // No valid schema could be produced
	assert.True(t, result.HasErrors())
}

func TestLoadString_NilContextPanics(t *testing.T) {
	source := `schema "test"`

	assert.Panics(t, func() {
		_, _, _ = load.LoadString(nil, source, "test.yammm") //nolint:staticcheck // intentional nil
	})
}

func TestLoadString_DisallowsImports(t *testing.T) {
	source := `schema "test" import "./other"`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestWithDisallowImports_LoadSourcesWithEntry(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	tmpDir := t.TempDir()

	source := `schema "test"

import "./foo" as f

type Bar {
	id String primary
}`

	sources := map[string][]byte{
		filepath.Join(tmpDir, "test.yammm"): []byte(source),
	}

	s, result, err := load.LoadSourcesWithEntry(
		ctx, sources, filepath.Join(tmpDir, "test.yammm"), tmpDir,
		load.WithDisallowImports(),
	)

	require.NoError(t, err)
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

func TestLoadString_DataTypeResolution_PreservesCase(t *testing.T) {
	// M3 fix: Verify datatype references preserve declared case end-to-end
	source := `schema "test"

type Name = String[1, 100]
type Email = Pattern["^[^@]+@[^@]+$"]

type Person {
	fullName Name required
	email Email required
}`

	ctx := context.Background()
	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	require.False(t, result.HasErrors(), "result: %v", result.Messages())
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

func TestLoadSources_SimpleSchema(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main" type Person { name String }`),
	}
	ctx := context.Background()

	s, result, err := load.LoadSources(ctx, sources, "/project")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "main", s.Name())
	assert.False(t, result.HasErrors())
}

func TestLoadSources_EmptySources(t *testing.T) {
	sources := map[string][]byte{}
	ctx := context.Background()

	_, _, err := load.LoadSources(ctx, sources, "/project")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sources provided")
}

func TestLoadSources_NilContextPanics(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}

	assert.Panics(t, func() {
		_, _, _ = load.LoadSources(nil, sources, "/project") //nolint:staticcheck // intentional nil
	})
}

func TestLoadSourcesWithEntry_ExplicitEntry(t *testing.T) {
	// Create two schemas where "beta.yammm" would be selected lexicographically
	// but we explicitly select "gamma.yammm" as entry
	sources := map[string][]byte{
		"beta.yammm":  []byte(`schema "beta" type BetaType { id String }`),
		"gamma.yammm": []byte(`schema "gamma" type GammaType { name String }`),
	}
	ctx := context.Background()

	// Explicitly select gamma.yammm as entry (not beta which comes first lexicographically)
	s, result, err := load.LoadSourcesWithEntry(ctx, sources, "gamma.yammm", "/project")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "gamma", s.Name(), "should load gamma schema, not beta")
	assert.False(t, result.HasErrors())

	// Verify gamma's type is present
	_, ok := s.Type("GammaType")
	assert.True(t, ok, "GammaType should exist in loaded schema")
}

func TestLoadSourcesWithEntry_FallbackToLexicographic(t *testing.T) {
	sources := map[string][]byte{
		"beta.yammm":  []byte(`schema "beta" type BetaType { id String }`),
		"alpha.yammm": []byte(`schema "alpha" type AlphaType { name String }`),
	}
	ctx := context.Background()

	// Empty entry path should fall back to lexicographic order (alpha comes first)
	s, result, err := load.LoadSourcesWithEntry(ctx, sources, "", "/project")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.Equal(t, "alpha", s.Name(), "should load alpha schema (lexicographically first)")
	assert.False(t, result.HasErrors())
}

func TestLoadSourcesWithEntry_EntryNotInSources(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}
	ctx := context.Background()

	// Request an entry that doesn't exist
	_, _, err := load.LoadSourcesWithEntry(ctx, sources, "nonexistent.yammm", "/project")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found in sources")
}

func TestLoadSourcesWithEntry_NilContextPanics(t *testing.T) {
	sources := map[string][]byte{
		"main.yammm": []byte(`schema "main"`),
	}

	assert.Panics(t, func() {
		_, _, _ = load.LoadSourcesWithEntry(nil, sources, "main.yammm", "/project") //nolint:staticcheck // intentional nil
	})
}

func TestLoadSourcesWithEntry_EmptySources(t *testing.T) {
	sources := map[string][]byte{}
	ctx := context.Background()

	_, _, err := load.LoadSourcesWithEntry(ctx, sources, "main.yammm", "/project")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no sources provided")
}

func TestLoad_FileNotFound(t *testing.T) {
	ctx := context.Background()

	_, _, err := load.Load(ctx, "/nonexistent/path/schema.yammm")

	require.Error(t, err)
}

func TestLoad_NilContextPanics(t *testing.T) {
	assert.Panics(t, func() {
		_, _, _ = load.Load(nil, "/some/path.yammm") //nolint:staticcheck // intentional nil
	})
}

func TestLoad_RealFile(t *testing.T) {
	// Create a temporary directory and file
	tmpDir := t.TempDir()
	schemaPath := filepath.Join(tmpDir, "test.yammm")
	content := `schema "test" type User { id String }`
	err := os.WriteFile(schemaPath, []byte(content), 0o600)
	require.NoError(t, err)

	ctx := context.Background()
	s, result, err := load.Load(ctx, schemaPath)

	require.NoError(t, err)
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
	commonContent := `schema "common" type Base { id String }`
	err := os.WriteFile(commonPath, []byte(commonContent), 0o600)
	require.NoError(t, err)

	// Create main.yammm that imports common
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./common" type User extends common.Base { name String }`
	err = os.WriteFile(mainPath, []byte(mainContent), 0o600)
	require.NoError(t, err)

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.Equal(t, "main", s.Name())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err) // No Go error
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestWithRegistry(t *testing.T) {
	source := `schema "test" type Person { name String }`
	ctx := context.Background()
	registry := schema.NewRegistry()

	s, result, err := load.LoadString(ctx, source, "test.yammm", load.WithRegistry(registry))

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	// The schema should be in the provided registry
	found, status := registry.LookupByName("test")
	assert.True(t, status.Found())
	assert.Same(t, s, found)
}

func TestWithIssueLimit(t *testing.T) {
	// Test that issue limit option is respected
	source := `schema "test" type Person { name String }`
	ctx := context.Background()

	// Test with issue limit (should still work for valid schema)
	s, result, err := load.LoadString(ctx, source, "test.yammm", load.WithIssueLimit(1))

	require.NoError(t, err)
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err) // No Go error
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify the error is about path escape
	found := false
	for _, issue := range result.IssuesSlice() {
		if strings.Contains(issue.Message(), "escapes module root") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected path escape error, got: %v", result.Messages())
}

func TestLoad_SymlinkCanonicalization(t *testing.T) {
	// Create a temporary directory structure
	tmpDir := t.TempDir()
	subDir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0o750))

	// Create a schema file in subdir
	schemaPath := filepath.Join(subDir, "test.yammm")
	content := `schema "test" type Person { name String }`
	require.NoError(t, os.WriteFile(schemaPath, []byte(content), 0o600))

	// Create a symlink to the schema
	linkPath := filepath.Join(tmpDir, "link.yammm")
	if err := os.Symlink(schemaPath, linkPath); err != nil {
		t.Skip("symlinks not supported: " + err.Error())
	}

	ctx := context.Background()

	// Load via symlink
	s1, result1, err := load.Load(ctx, linkPath)
	require.NoError(t, err)
	require.NotNil(t, s1)
	assert.False(t, result1.HasErrors())

	// Load directly
	s2, result2, err := load.Load(ctx, schemaPath)
	require.NoError(t, err)
	require.NotNil(t, s2)
	assert.False(t, result2.HasErrors())

	// Both should resolve to the same SourceID
	assert.Equal(t, s1.SourceID(), s2.SourceID(), "Symlink and direct path should resolve to same SourceID")
}

func TestLoadString_InvariantExpression(t *testing.T) {
	source := `schema "test"

type Item {
	quantity Integer
	! "Quantity must be non-negative" quantity >= 0
}`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

	itemType, ok := s.Type("Item")
	require.True(t, ok)

	invs := itemType.InvariantsSlice()
	require.Len(t, invs, 1)

	e := invs[0].Expression()
	require.NotNil(t, e, "invariant expression should survive load pipeline")

	// Verify the expression operator (Expression() now returns expr.Expression directly)
	assert.Equal(t, ">=", e.Op(), "expression should be a >= comparison")
}

func TestLoadString_RelationTargetIDResolved(t *testing.T) {
	source := `schema "test"

type Target { id String }

type Owner {
	--> rel Target
}`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

	ownerType, ok := s.Type("Owner")
	require.True(t, ok)

	assocs := ownerType.AssociationsSlice()
	require.Len(t, assocs, 1)

	rel := assocs[0]
	assert.False(t, rel.TargetID().IsZero(), "relation targetID should be resolved")
	assert.Equal(t, "Target", rel.TargetID().Name())
	assert.Equal(t, s.SourceID(), rel.TargetID().SchemaPath())
}

func TestLoadString_CompositionTargetIDResolved(t *testing.T) {
	source := `schema "test"

part type Component { id String }

type Container {
	*-> parts Component
}`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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
	targetContent := `schema "target" type Entity { id String }`
	require.NoError(t, os.WriteFile(targetPath, []byte(targetContent), 0o600))

	// Create main.yammm with a relation to the imported type
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main"
import "./target" as t
type Owner { --> rel t.Entity }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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
type Container { *-> widgets c.Widget }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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

// TestLoadString_Contract19_SchemaNilWhenErrorsExist verifies:
// Schema must be nil whenever Result.HasErrors() is true.
// This test uses a validation error (undefined type reference) rather than a parse error
// to ensure the invariant holds through the completion phase.
func TestLoadString_Contract19_SchemaNilWhenErrorsExist(t *testing.T) {
	// Schema with an undefined type reference in a relation
	source := `schema "test"
type Person {
	name String
	--> friend NonExistentType
}`
	ctx := context.Background()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	// error should be nil for content issues
	require.NoError(t, err, "Go error should be nil for content/validation issues")

	// schema must be nil when errors exist
	require.True(t, result.HasErrors(), "should have validation errors for undefined type")
	require.Nil(t, s, "schema must be nil when Result.HasErrors() is true")
}

// TestLoadSources_RecoveryAfterParseFailure verifies that the loader properly cleans up
// internal state after a parse failure, allowing subsequent loads to succeed.
// This tests the fix for Executive Summary Item 6: loadingSchemas markers must be
// cleared on all exit paths to prevent false "import cycle" errors.
func TestLoadSources_RecoveryAfterParseFailure(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	// Step 1: Attempt to load schema with parse error
	brokenSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
type Person {
	INVALID SYNTAX HERE
}`),
	}

	s1, result1, err1 := load.LoadSources(ctx, brokenSources, tmpDir)
	require.NoError(t, err1, "I/O errors should not occur")
	require.True(t, result1.HasErrors(), "should have parse errors")
	require.Nil(t, s1, "schema should be nil on errors")

	// Step 2: Fix the schema and reload (simulating user fixing the file)
	// This MUST succeed - if loadingSchemas was not cleaned up properly,
	// this would fail with a false "import cycle" error.
	fixedSources := map[string][]byte{
		"main.yammm": []byte(`schema "main"
type Person {
	name String
}`),
	}

	s2, result2, err2 := load.LoadSources(ctx, fixedSources, tmpDir)
	require.NoError(t, err2, "I/O errors should not occur")
	require.False(t, result2.HasErrors(), "fixed schema should have no errors: %v", result2.Messages())
	require.NotNil(t, s2, "fixed schema should load successfully")
}

// TestLoad_CrossSchemaInheritance_AllProperties verifies that cross-schema
// inheritance properly inherits properties via AllProperties().
// This tests that CC1 (AllProperties flattens inherited) and CC4 (cross-schema
// supertype resolution) work together end-to-end.
func TestLoad_CrossSchemaInheritance_AllProperties(t *testing.T) {
	tmpDir := t.TempDir()

	// Create base.yammm with base type
	basePath := filepath.Join(tmpDir, "base.yammm")
	baseContent := `schema "base"
type Base {
	id String
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, derivedPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
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

// TestLoadSources_RecoveryAfterImportFailure verifies that the loader cleans up
// loadingSchemas markers for both the main schema and its imports when an import
// fails. This tests cascading cleanup.
func TestLoadSources_RecoveryAfterImportFailure(t *testing.T) {
	ctx := context.Background()
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

	s1, result1, err1 := load.LoadSources(ctx, brokenSources, tmpDir)
	require.NoError(t, err1)
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
	id Uuid
}`),
	}

	s2, result2, err2 := load.LoadSources(ctx, fixedSources, tmpDir)
	require.NoError(t, err2)
	require.False(t, result2.HasErrors(), "should not report false import cycle: %v", result2.Messages())
	require.NotNil(t, s2)
}

// TestLoad_MultiImportSameLevel verifies that a schema importing multiple other
// schemas at the same level correctly resolves references to all of them.
// This tests the fix for C1: resolvedImports recursion safety.
func TestLoad_MultiImportSameLevel(t *testing.T) {
	tmpDir := t.TempDir()

	// Create b.yammm
	bPath := filepath.Join(tmpDir, "b.yammm")
	bContent := `schema "b" type Entity { id String }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create c.yammm
	cPath := filepath.Join(tmpDir, "c.yammm")
	cContent := `schema "c" type Other { name String }`
	require.NoError(t, os.WriteFile(cPath, []byte(cContent), 0o600))

	// Create a.yammm that imports both
	aPath := filepath.Join(tmpDir, "a.yammm")
	aContent := `schema "a"
import "./b" as b
import "./c" as c
type Connector {
	--> toB b.Entity
	--> toC c.Other
}`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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
// This tests the fix for C1: resolvedImports recursion safety.
func TestLoad_NestedImports(t *testing.T) {
	tmpDir := t.TempDir()

	// Create d.yammm (deepest)
	dPath := filepath.Join(tmpDir, "d.yammm")
	dContent := `schema "d" type Deep { id String }`
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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
// This tests the fix for C1: resolvedImports recursion safety.
func TestLoad_DiamondImportPattern(t *testing.T) {
	tmpDir := t.TempDir()

	// Create d.yammm (common dependency)
	dPath := filepath.Join(tmpDir, "d.yammm")
	dContent := `schema "d" type Shared { id String }`
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
	--> toB b.BType
	--> toC c.CType
}`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should not be nil. Errors: %v", result.Messages())
	assert.False(t, result.HasErrors(), "Unexpected errors: %v", result.Messages())

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

// =============================================================================
// A7: rootLoader Edge Case Tests
// =============================================================================

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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err)
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Verify the error is about path escape
	found := false
	for _, issue := range result.IssuesSlice() {
		if strings.Contains(issue.Message(), "escapes module root") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected path escape error for multi-level escape, got: %v", result.Messages())
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err)
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())

	// Should fail due to symlink escape - os.Root blocks symlink escapes
	found := false
	for _, issue := range result.IssuesSlice() {
		// os.Root may report "escapes" or "not found" depending on OS behavior
		if strings.Contains(issue.Message(), "escapes") || strings.Contains(issue.Message(), "not found") {
			found = true
			break
		}
	}
	assert.True(t, found, "Expected symlink escape to be blocked, got: %v", result.Messages())
}

// TestLoad_BoundaryPath_AtRoot verifies that imports at the exact module root
// boundary work correctly.
func TestLoad_BoundaryPath_AtRoot(t *testing.T) {
	tmpDir := t.TempDir()

	// Create helper.yammm at the root level
	helperPath := filepath.Join(tmpDir, "helper.yammm")
	require.NoError(t, os.WriteFile(helperPath, []byte(`schema "helper" type Base { id String }`), 0o600))

	// Create main.yammm also at root level, importing helper
	mainPath := filepath.Join(tmpDir, "main.yammm")
	mainContent := `schema "main" import "./helper" type User extends helper.Base { name String }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	require.NotNil(t, s, "Schema should load: %v", result.Messages())
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	// os.Root blocks ALL symlinks, even internal ones - this is security-correct
	require.NoError(t, err)
	assert.Nil(t, s, "symlink imports should be blocked by os.Root")
	assert.True(t, result.HasErrors())

	// Verify the error mentions escape (os.Root treats symlinks as escapes)
	found := false
	for _, issue := range result.IssuesSlice() {
		if strings.Contains(issue.Message(), "escapes") {
			found = true
			break
		}
	}
	assert.True(t, found, "os.Root should report symlink as path escape: %v", result.Messages())
}

// TestLoad_DotDotInMiddleOfPath verifies that ".." components in the middle
// of a path are properly handled.
func TestLoad_DotDotInMiddleOfPath(t *testing.T) {
	tmpDir := t.TempDir()
	subdir := filepath.Join(tmpDir, "subdir")
	require.NoError(t, os.Mkdir(subdir, 0o750))

	// Create helper.yammm at root level
	helperPath := filepath.Join(tmpDir, "helper.yammm")
	require.NoError(t, os.WriteFile(helperPath, []byte(`schema "helper" type Base { id String }`), 0o600))

	// Create main.yammm in subdir that imports ../helper
	mainPath := filepath.Join(subdir, "main.yammm")
	mainContent := `schema "main" import "../helper" type User extends helper.Base { name String }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(tmpDir))

	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	// This should work because "../helper" from subdir/ stays within module root
	require.NotNil(t, s, "Relative import staying within root should work: %v", result.Messages())
	assert.False(t, result.HasErrors())
}

// =============================================================================
// Context Cancellation Tests (/19)
// =============================================================================

func TestLoadString_CancellationReturnsError(t *testing.T) {
	source := `schema "test" type Person { name String }`

	// Create already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, result, err := load.LoadString(ctx, source, "test.yammm")

	// Cancellation returns error, not diagnostic
	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, s)

	// Result should NOT contain E_INTERNAL for cancellation
	for issue := range result.Issues() {
		assert.NotEqual(t, diag.E_INTERNAL, issue.Code(),
			"cancellation should not be reported as E_INTERNAL diagnostic")
	}
}

func TestLoadString_DeadlineExceededReturnsError(t *testing.T) {
	source := `schema "test"`

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-1*time.Second))
	defer cancel()

	s, _, err := load.LoadString(ctx, source, "test.yammm")

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, s)
}

func TestLoad_CancellationDuringImport(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a.yammm and b.yammm
	aPath := filepath.Join(tmpDir, "a.yammm")
	require.NoError(t, os.WriteFile(aPath, []byte(`schema "a" import "./b.yammm"`), 0o600))

	bPath := filepath.Join(tmpDir, "b.yammm")
	require.NoError(t, os.WriteFile(bPath, []byte(`schema "b" type X { id String }`), 0o600))

	// Create context that gets cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, _, err := load.Load(ctx, aPath, load.WithModuleRoot(tmpDir))

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	assert.Nil(t, s)
}

// mockSourceStore implements SourceStore but is not *source.Registry.
// Used to test ErrSourceStoreNotSupported behavior.
type mockSourceStore struct{}

func (m *mockSourceStore) Register(_ location.SourceID, _ []byte) error {
	return nil
}

func (m *mockSourceStore) PositionAt(_ location.SourceID, _ int) location.Position {
	return location.Position{}
}

func (m *mockSourceStore) RuneToByteOffset(_ location.SourceID, _ int) (int, bool) {
	return 0, false
}

func TestLoad_ErrSourceStoreNotSupported(t *testing.T) {
	// When a custom SourceStore that isn't *source.Registry is provided,
	// the loader should fail fast with ErrSourceStoreNotSupported.
	source := `schema "test" type Person { name String }`
	ctx := context.Background()

	s, _, err := load.LoadString(ctx, source, "test.yammm", load.WithSourceRegistry(&mockSourceStore{}))

	require.Error(t, err)
	require.ErrorIs(t, err, load.ErrSourceStoreNotSupported)
	assert.Nil(t, s)
}

func TestLoad_TransitiveImportsChain(t *testing.T) {
	// Test A imports B, B imports C (transitive imports)
	moduleRoot := t.TempDir()

	// Create C (base)
	cPath := filepath.Join(moduleRoot, "c.yammm")
	require.NoError(t, os.WriteFile(cPath, []byte(`schema "c" type BaseType { name String }`), 0o600))

	// Create B (imports C)
	bPath := filepath.Join(moduleRoot, "b.yammm")
	bContent := `schema "b"
import "./c" as c
type Middle { base c.BaseType }`
	require.NoError(t, os.WriteFile(bPath, []byte(bContent), 0o600))

	// Create A (imports B)
	aPath := filepath.Join(moduleRoot, "a.yammm")
	aContent := `schema "a"
import "./b" as b
type Top { middle b.Middle }`
	require.NoError(t, os.WriteFile(aPath, []byte(aContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err)
	require.NotNil(t, s)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
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

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(moduleRoot))

	// Cycle should be detected
	require.NoError(t, err)
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
	// Check for cycle error
	hasImportCycle := false
	for _, issue := range result.IssuesSlice() {
		if strings.Contains(issue.Message(), "cycle") {
			hasImportCycle = true
			break
		}
	}
	assert.True(t, hasImportCycle, "expected import cycle error")
}

func TestLoad_NonExistentPath(t *testing.T) {
	// Test loading from a path that doesn't exist
	ctx := context.Background()

	s, _, err := load.Load(ctx, "/nonexistent/path/to/schema.yammm")

	require.Error(t, err)
	assert.Nil(t, s)
}

func TestLoadSources_ImportBetweenSources(t *testing.T) {
	// Test LoadSources with one in-memory schema importing another
	ctx := context.Background()
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

	s, result, err := load.LoadSources(ctx, sources, moduleRoot)
	require.NoError(t, err)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
		t.FailNow()
	}
	require.NotNil(t, s)
}

func TestLoadSources_ContextCancellation(t *testing.T) {
	// Test that LoadSources handles context cancellation
	moduleRoot := t.TempDir()

	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := []byte(`schema "main" type Person { name String }`)

	sources := map[string][]byte{
		mainPath: mainContent,
	}

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	s, _, err := load.LoadSources(ctx, sources, moduleRoot)

	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "cancel")
}

func TestLoad_WithLogger(t *testing.T) {
	// Test that WithLogger option works
	moduleRoot := t.TempDir()

	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := `schema "main" type Person { name String }`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath,
		load.WithModuleRoot(moduleRoot),
		load.WithLogger(logger))

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestLoad_MultipleImports(t *testing.T) {
	// Test loading schema with multiple imports
	moduleRoot := t.TempDir()

	// Create three helper schemas
	helper1Path := filepath.Join(moduleRoot, "helper1.yammm")
	require.NoError(t, os.WriteFile(helper1Path, []byte(`schema "helper1" type Type1 { name String }`), 0o600))

	helper2Path := filepath.Join(moduleRoot, "helper2.yammm")
	require.NoError(t, os.WriteFile(helper2Path, []byte(`schema "helper2" type Type2 { value Integer }`), 0o600))

	helper3Path := filepath.Join(moduleRoot, "helper3.yammm")
	require.NoError(t, os.WriteFile(helper3Path, []byte(`schema "helper3" type Type3 { flag Boolean }`), 0o600))

	// Main imports all three
	mainPath := filepath.Join(moduleRoot, "main.yammm")
	mainContent := `schema "main"
import "./helper1" as h1
import "./helper2" as h2
import "./helper3" as h3
type Combined {
	ref1 h1.Type1
	ref2 h2.Type2
	ref3 h3.Type3
}`
	require.NoError(t, os.WriteFile(mainPath, []byte(mainContent), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, mainPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors())
}

func TestLoadString_InvalidSyntax(t *testing.T) {
	// Test LoadString with invalid syntax
	ctx := context.Background()

	source := `schema "broken"
type Entity {
	name String
	// missing closing brace`

	s, result, err := load.LoadString(ctx, source, "test://broken.yammm")

	require.NoError(t, err) // Parse errors are collected, not returned as error
	assert.Nil(t, s)
	assert.True(t, result.HasErrors())
}

func TestLoadString_ContextCancellation(t *testing.T) {
	// Test that LoadString handles context cancellation
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	source := `schema "test" type Test { name String }`
	s, _, err := load.LoadString(ctx, source, "test.yammm")

	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "cancel")
}

func TestLoad_WithSchemaRegistry(t *testing.T) {
	// Test loading with a pre-populated schema registry
	tmpDir := t.TempDir()

	schemaPath := filepath.Join(tmpDir, "test.yammm")
	content := `schema "test" type Test { name String }`
	require.NoError(t, os.WriteFile(schemaPath, []byte(content), 0o600))

	// Create a registry
	reg := schema.NewRegistry()

	ctx := context.Background()
	s, result, err := load.Load(ctx, schemaPath, load.WithRegistry(reg))

	require.NoError(t, err)
	require.NotNil(t, s)
	assert.False(t, result.HasErrors())

	// Verify schema was registered
	found, status := reg.LookupBySourceID(s.SourceID())
	assert.NotNil(t, found)
	assert.Equal(t, schema.LookupFound, status)
}

func TestLoad_DiamondImport(t *testing.T) {
	// Test diamond import pattern: A->B, A->C, B->D, C->D
	// This tests that D is only loaded once
	moduleRoot := t.TempDir()

	// D is the base
	dPath := filepath.Join(moduleRoot, "d.yammm")
	require.NoError(t, os.WriteFile(dPath, []byte(`schema "d" type BaseD { name String }`), 0o600))

	// B imports D
	bPath := filepath.Join(moduleRoot, "b.yammm")
	require.NoError(t, os.WriteFile(bPath, []byte(`schema "b"
import "./d" as d
type TypeB { ref d.BaseD }`), 0o600))

	// C imports D
	cPath := filepath.Join(moduleRoot, "c.yammm")
	require.NoError(t, os.WriteFile(cPath, []byte(`schema "c"
import "./d" as d
type TypeC { ref d.BaseD }`), 0o600))

	// A imports B and C
	aPath := filepath.Join(moduleRoot, "a.yammm")
	require.NoError(t, os.WriteFile(aPath, []byte(`schema "a"
import "./b" as b
import "./c" as c
type TypeA {
	refB b.TypeB
	refC c.TypeC
}`), 0o600))

	ctx := context.Background()
	s, result, err := load.Load(ctx, aPath, load.WithModuleRoot(moduleRoot))

	require.NoError(t, err)
	require.NotNil(t, s)
	if result.HasErrors() {
		for _, issue := range result.IssuesSlice() {
			t.Logf("Error: %v", issue)
		}
	}
	assert.False(t, result.HasErrors())
}
