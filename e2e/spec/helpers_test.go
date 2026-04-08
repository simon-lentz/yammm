package spec_test

import (
	"os"
	"strings"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadSchema loads a .yammm file and returns a validator.
// Fails the test if the schema has errors.
func loadSchema(t *testing.T, path string) *instance.Validator {
	t.Helper()
	ctx := t.Context()
	s, result := schema.Load(ctx, path)
	require.True(t, result.OK(), "schema %s has errors: %v", path, result.Messages())
	return instance.NewValidator(s)
}

// loadSchemaRaw loads a .yammm file and returns both the schema and validator.
func loadSchemaRaw(t *testing.T, path string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	ctx := t.Context()
	s, result := schema.Load(ctx, path)
	require.True(t, result.OK(), "schema %s has errors: %v", path, result.Messages())
	return s, instance.NewValidator(s)
}

// loadSchemaExpectError loads a .yammm file and asserts it fails compilation.
// Returns the diagnostic result for inspection.
func loadSchemaExpectError(t *testing.T, path string) diag.Result {
	t.Helper()
	ctx := t.Context()
	_, result := schema.Load(ctx, path)
	require.False(t, result.OK(), "schema %s should have errors but loaded cleanly", path)
	return result
}

// loadSchemaString loads a schema from inline string content.
func loadSchemaString(t *testing.T, content, name string) *instance.Validator {
	t.Helper()
	ctx := t.Context()
	s, result := schema.LoadString(ctx, content, name)
	require.True(t, result.OK(), "schema string %s has errors: %v", name, result.Messages())
	return instance.NewValidator(s)
}

// loadSchemaStringRaw loads a schema from inline string content, returning both schema and validator.
func loadSchemaStringRaw(t *testing.T, content, name string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	ctx := t.Context()
	s, result := schema.LoadString(ctx, content, name)
	require.True(t, result.OK(), "schema string %s has errors: %v", name, result.Messages())
	return s, instance.NewValidator(s)
}

// loadSchemaStringExpectError loads from string and asserts compilation failure.
func loadSchemaStringExpectError(t *testing.T, content, name string) diag.Result {
	t.Helper()
	ctx := t.Context()
	_, result := schema.LoadString(ctx, content, name)
	require.False(t, result.OK(), "schema string %s should have errors but loaded cleanly", name)
	return result
}

// loadSchemaWithOpts loads a .yammm file with specific load options.
func loadSchemaWithOpts(t *testing.T, path string, opts ...schema.LoadOption) (*schema.Schema, diag.Result) { //nolint:unparam // test helper — path varies across test files
	t.Helper()
	ctx := t.Context()
	return schema.Load(ctx, path, opts...)
}

// loadTestData reads a JSON data file and extracts instances by type key.
func loadTestData(t *testing.T, dataPath, typeKey string) []instance.RawInstance {
	t.Helper()
	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read test data %s", dataPath)

	adapter, err := jsonadapter.New(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://" + dataPath)
	parsed, parseResult := adapter.ParseObject(t.Context(), sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse %s failed: %v", dataPath, parseResult.Messages())

	records := parsed[typeKey]
	require.NotEmpty(t, records, "no %q records in %s", typeKey, dataPath)
	return records
}

// assertValid validates a single instance and asserts success.
func assertValid(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance) {
	t.Helper()
	ctx := t.Context()
	valid, result := v.ValidateOne(ctx, typeName, raw)
	require.True(t, result.OK(), "expected valid %s instance, got: %v", typeName, result.Messages())
	assert.NotNil(t, valid)
}

// assertInvalid validates a single instance and asserts failure with specific codes.
func assertInvalid(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance, wantCodes ...diag.Code) {
	t.Helper()
	ctx := t.Context()
	valid, result := v.ValidateOne(ctx, typeName, raw)
	assert.Nil(t, valid, "expected invalid %s instance", typeName)
	require.False(t, result.OK(), "expected validation failure for %s", typeName)

	issueCodes := map[string]bool{}
	for issue := range result.Issues() {
		issueCodes[issue.Code().String()] = true
	}
	for _, wc := range wantCodes {
		assert.True(t, issueCodes[wc.String()],
			"expected code %s in diagnostics, got: %v", wc, result.Messages())
	}
}

// assertInvariantFails validates and asserts specific invariant failures by name.
func assertInvariantFails(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance, wantNames ...string) { //nolint:unparam // test helper — typeName varies by test file
	t.Helper()
	ctx := t.Context()
	valid, result := v.ValidateOne(ctx, typeName, raw)
	assert.Nil(t, valid, "expected invariant failure for %s", typeName)
	require.False(t, result.OK(), "expected validation failure for %s", typeName)

	failedInvariants := map[string]bool{}
	for issue := range result.Issues() {
		if issue.Code() == diag.E_INVARIANT_FAIL {
			failedInvariants[issue.Message()] = true
		}
	}

	for _, name := range wantNames {
		assert.True(t, failedInvariants[name],
			"invariant %q should have failed, got failures: %v", name, failedInvariants)
	}
	assert.Len(t, failedInvariants, len(wantNames),
		"expected exactly %d invariant failures, got: %v", len(wantNames), failedInvariants)
}

// assertDiagHasCode asserts a diagnostic result contains at least one issue with the given code.
func assertDiagHasCode(t *testing.T, result diag.Result, code diag.Code) {
	t.Helper()
	for issue := range result.Issues() {
		if issue.Code() == code {
			return
		}
	}
	t.Errorf("expected diagnostic code %s, got: %v", code, result.Messages())
}

// buildGraph builds a graph from a schema and validated instances, returns snapshot.
func buildGraph(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	ctx := t.Context()
	g := graph.New(s)
	for _, inst := range instances {
		result := g.Add(ctx, inst)
		require.True(t, result.OK(), "graph.Add issues: %v", result.Messages())
	}
	return g.Snapshot()
}

// validateOne validates a single raw instance and returns the ValidInstance.
func validateOne(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance) *instance.ValidInstance {
	t.Helper()
	ctx := t.Context()
	valid, result := v.ValidateOne(ctx, typeName, raw)
	require.True(t, result.OK(), "expected valid %s, got: %v", typeName, result.Messages())
	require.NotNil(t, valid)
	return valid
}

// =============================================================================
// Snapshot helpers
// =============================================================================

// marshalSnapshot marshals a snapshot to .ys bytes, failing on error.
func marshalSnapshot(t *testing.T, snap *graph.Snapshot, opts ...snapshot.Option) []byte {
	t.Helper()
	data, result := snapshot.Marshal(t.Context(), snap, opts...)
	require.Nil(t, result.Err(), "snapshot.Marshal failed: %v", result.Messages())
	return data
}

// loadSnapshot loads .ys bytes back into a snapshot, failing on error.
func loadSnapshot(t *testing.T, data []byte, s *schema.Schema, opts ...snapshot.LoadOption) *graph.Snapshot { //nolint:unparam // test helper — opts used by negative tests
	t.Helper()
	snap, result := snapshot.Load(t.Context(), data, s, opts...)
	require.Nil(t, result.Err(), "snapshot.Load failed: %v", result.Messages())
	require.NotNil(t, snap)
	return snap
}

// buildSnapshotFromData runs the full e2e pipeline: schema.Load -> JSON parse
// -> validate -> graph.Add -> Snapshot. typeKeys specifies which keys from
// data.json to include. Keys with "__" suffixes are negative variants and are
// NOT included unless explicitly listed.
func buildSnapshotFromData(t *testing.T, schemaPath, dataPath string, typeKeys []string) (*schema.Schema, *graph.Snapshot) {
	t.Helper()
	ctx := t.Context()

	s, result := schema.Load(ctx, schemaPath)
	require.True(t, result.OK(), "schema %s has errors: %v", schemaPath, result.Messages())

	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read %s", dataPath)

	adapter, err := jsonadapter.New(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://" + dataPath)
	parsed, parseResult := adapter.ParseObject(ctx, sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse %s failed: %v", dataPath, parseResult.Messages())

	v := instance.NewValidator(s)
	g := graph.New(s)

	for _, typeKey := range typeKeys {
		records := parsed[typeKey]
		require.NotEmpty(t, records, "no %q records in %s", typeKey, dataPath)

		// Resolve the schema type name (strip __ suffix variants).
		typeName := typeKey
		if idx := strings.Index(typeName, "__"); idx >= 0 {
			typeName = typeName[:idx]
		}

		for _, rec := range records {
			valid, valResult := v.ValidateOne(ctx, typeName, rec)
			require.True(t, valResult.OK(), "validate %s failed: %v", typeName, valResult.Messages())
			g.Add(ctx, valid)
		}
	}

	return s, g.Snapshot()
}

// buildSnapshotFromDataAllTypes is like buildSnapshotFromData but auto-discovers
// type keys from the JSON data, excluding keys with "__" suffixes.
func buildSnapshotFromDataAllTypes(t *testing.T, schemaPath, dataPath string) (*schema.Schema, *graph.Snapshot) {
	t.Helper()
	ctx := t.Context()

	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read %s", dataPath)

	adapter, err := jsonadapter.New(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://" + dataPath)
	parsed, parseResult := adapter.ParseObject(ctx, sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse %s failed: %v", dataPath, parseResult.Messages())

	var typeKeys []string
	for key := range parsed {
		if !strings.Contains(key, "__") {
			typeKeys = append(typeKeys, key)
		}
	}

	return buildSnapshotFromData(t, schemaPath, dataPath, typeKeys)
}

// verifySnapshot runs snapshot.Verify and asserts no errors. Warnings are allowed.
func verifySnapshot(t *testing.T, data []byte, s *schema.Schema, opts ...snapshot.LoadOption) {
	t.Helper()
	result := snapshot.Verify(t.Context(), data, s, opts...)
	require.False(t, result.HasErrors(), "snapshot.Verify had errors: %v", result.Messages())
}

// verifySnapshotFails runs snapshot.Verify and asserts the given diagnostic code.
func verifySnapshotFails(t *testing.T, data []byte, s *schema.Schema, code diag.Code, opts ...snapshot.LoadOption) {
	t.Helper()
	result := snapshot.Verify(t.Context(), data, s, opts...)
	require.True(t, result.HasErrors(), "snapshot.Verify should have errors")
	assertDiagHasCode(t, result, code)
}

// snapshotInfo calls snapshot.Info and returns the result, failing on error.
func snapshotInfo(t *testing.T, data []byte) *snapshot.SnapshotInfo {
	t.Helper()
	info, result := snapshot.Info(t.Context(), data)
	require.Nil(t, result.Err(), "snapshot.Info failed: %v", result.Messages())
	require.NotNil(t, info)
	return info
}

// corruptBytesAt returns a copy of data with a single bit flipped at the given offset.
func corruptBytesAt(data []byte, offset int) []byte {
	corrupted := make([]byte, len(data))
	copy(corrupted, data)
	corrupted[offset] ^= 0x01
	return corrupted
}
