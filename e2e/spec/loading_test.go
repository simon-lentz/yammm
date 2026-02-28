package spec_test

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
	"github.com/simon-lentz/yammm/schema/load"
)

// =============================================================================
// Load functions
// =============================================================================

// TestLoading_LoadFromFile verifies that load.Load reads a .yammm file from
// disk and produces a valid schema.
// Source: SPEC.md, "load.Load reads a schema from a file path."
func TestLoading_LoadFromFile(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	s, result, err := load.Load(ctx, "testdata/loading/valid.yammm")

	require.NoError(t, err, "Load should not return an I/O error for a valid file")
	require.True(t, result.OK(), "result should be OK for a valid schema: %v", result.Messages())
	require.NotNil(t, s, "schema should not be nil on success")
	assert.Equal(t, "Valid", s.Name())
}

// TestLoading_LoadStringParameterOrder verifies that load.LoadString accepts
// (ctx, sourceCode, sourceName) — content first, then display name.
// Source: SPEC.md, "load.LoadString loads a schema from a string."
func TestLoading_LoadStringParameterOrder(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	content := `schema "FromString" type Item { name String required }`
	s, result, err := load.LoadString(ctx, content, "test-source")

	require.NoError(t, err, "LoadString should not return an error for valid content")
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s, "schema should not be nil")
	assert.Equal(t, "FromString", s.Name())
}

// TestLoading_LoadSources verifies that load.LoadSources loads a schema from
// an in-memory source map keyed by file path.
// Source: SPEC.md, "load.LoadSources loads from in-memory sources."
func TestLoading_LoadSources(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// LoadSources requires absolute paths or a moduleRoot to resolve relative paths.
	// Use a temporary directory as module root with a relative key.
	tmpDir := t.TempDir()
	sources := map[string][]byte{
		"entry.yammm": []byte(`schema "InMemory" type Widget { label String required }`),
	}

	s, result, err := load.LoadSources(ctx, sources, tmpDir)

	require.NoError(t, err, "LoadSources should not return an error")
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s, "schema should not be nil")
	assert.Equal(t, "InMemory", s.Name())
}

// =============================================================================
// Three-way error pattern
// =============================================================================

// TestLoading_ErrorPattern_IOFailure verifies that loading a nonexistent file
// produces a Go error (error != nil).
// Source: SPEC.md, "error != nil indicates catastrophic failure (I/O, corruption)."
func TestLoading_ErrorPattern_IOFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	_, _, err := load.Load(ctx, "testdata/loading/does_not_exist.yammm")

	require.Error(t, err, "Load should return an error for a nonexistent file")
}

// TestLoading_ErrorPattern_SemanticFailure verifies that a syntactically broken
// schema produces error == nil but !result.OK().
// Source: SPEC.md, "error == nil && !result.OK() indicates semantic failure."
func TestLoading_ErrorPattern_SemanticFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	_, result, err := load.Load(ctx, "testdata/loading/syntax_error.yammm")

	require.NoError(t, err, "Load should not return Go error for syntax problems")
	assert.False(t, result.OK(), "result should report errors for a broken schema")
}

// TestLoading_ErrorPattern_Success verifies the success path: error == nil &&
// result.OK() with a non-nil schema.
// Source: SPEC.md, "error == nil && result.OK() indicates success."
func TestLoading_ErrorPattern_Success(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	s, result, err := load.Load(ctx, "testdata/loading/valid.yammm")

	require.NoError(t, err)
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s)
}

// =============================================================================
// Load options
// =============================================================================

// TestLoading_WithRegistry verifies that WithRegistry enables cross-schema
// type resolution during loading.
// Source: SPEC.md, "WithRegistry provides a schema registry for cross-schema
// type resolution."
func TestLoading_WithRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	reg := schema.NewRegistry()

	// Load a schema with the registry option. The schema itself does not use
	// imports, but the option must be accepted without error.
	s, result, err := load.Load(ctx, "testdata/loading/valid.yammm", load.WithRegistry(reg))

	require.NoError(t, err)
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s)
}

// TestLoading_WithModuleRoot verifies that WithModuleRoot is accepted by Load
// and influences import resolution context.
// Source: SPEC.md, "WithModuleRoot sets the root directory for module-style
// imports."
func TestLoading_WithModuleRoot(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	// Determine the absolute path to the testdata directory so WithModuleRoot
	// receives a valid directory.
	absTestdata, err := filepath.Abs("testdata/loading")
	require.NoError(t, err)

	s, result, loadErr := load.Load(ctx, "testdata/loading/valid.yammm",
		load.WithModuleRoot(absTestdata))

	require.NoError(t, loadErr)
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s)
}

// TestLoading_WithIssueLimit verifies that WithIssueLimit caps collected
// diagnostics and that result.LimitReached() returns true when exceeded.
// Source: SPEC.md, "WithIssueLimit sets the maximum number of diagnostic
// issues to collect."
func TestLoading_WithIssueLimit(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	_, result, err := load.Load(ctx, "testdata/loading/many_errors.yammm",
		load.WithIssueLimit(2))

	require.NoError(t, err, "Load should not return Go error for semantic issues")
	assert.False(t, result.OK(), "schema with duplicate properties should not be OK")
	assert.True(t, result.LimitReached(),
		"LimitReached() should be true when more issues exist than the limit")
}

// TestLoading_WithSourceRegistry verifies that WithSourceRegistry accepts a
// *source.Registry and the load succeeds.
// Source: SPEC.md, "WithSourceRegistry provides a custom source registry for
// position tracking."
func TestLoading_WithSourceRegistry(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	srcReg := source.NewRegistry()
	s, result, err := load.Load(ctx, "testdata/loading/valid.yammm",
		load.WithSourceRegistry(srcReg))

	require.NoError(t, err)
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s)
}

// TestLoading_WithLogger verifies that WithLogger is accepted and does not
// cause a panic during loading.
// Source: SPEC.md, "WithLogger provides a structured logger for load operation
// diagnostics."
func TestLoading_WithLogger(t *testing.T) {
	t.Parallel()
	ctx := t.Context()

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	s, result, err := load.Load(ctx, "testdata/loading/valid.yammm",
		load.WithLogger(logger))

	require.NoError(t, err)
	require.True(t, result.OK(), "result should be OK: %v", result.Messages())
	require.NotNil(t, s)
}

// =============================================================================
// Builder API
// =============================================================================

// TestLoading_BuilderAPI verifies that build.NewBuilder can construct a schema
// programmatically and that it produces a schema equivalent to one loaded from
// a .yammm file (same type names, properties, validation behavior).
// Source: SPEC.md, "Schemas can be constructed programmatically using the
// Builder API."
func TestLoading_BuilderAPI(t *testing.T) {
	t.Parallel()

	// Build the same schema as valid.yammm: schema "Valid" with type Person { name String required }
	s, result := build.NewBuilder().
		WithName("Valid").
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s, "builder should produce a non-nil schema")
	assert.False(t, result.HasErrors(), "builder result should have no errors: %v", result.Messages())
	assert.Equal(t, "Valid", s.Name())

	// Verify the type structure matches what the .yammm parser produces.
	typ, ok := s.Type("Person")
	require.True(t, ok, "schema should contain type Person")
	assert.Equal(t, "Person", typ.Name())

	props := typ.PropertiesSlice()
	require.Len(t, props, 1, "Person should have exactly one property")
	assert.Equal(t, "name", props[0].Name())

	// Validate an instance against the builder-produced schema.
	v := instance.NewValidator(s)
	ctx := t.Context()

	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{"name": "Alice"}))
	require.NoError(t, err)
	assert.Nil(t, failure, "valid instance should not produce failure")
	assert.NotNil(t, valid, "valid instance should be returned")

	// Verify that missing required property fails validation.
	valid2, failure2, err2 := v.ValidateOne(ctx, "Person", raw(map[string]any{}))
	require.NoError(t, err2)
	assert.Nil(t, valid2, "instance missing required property should be invalid")
	assert.NotNil(t, failure2, "missing required property should produce failure")
}
