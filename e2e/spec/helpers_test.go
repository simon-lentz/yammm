package spec_test

import (
	"context"
	"os"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadSchema loads a .yammm file and returns a validator.
// Fails the test if the schema has errors.
func loadSchema(t *testing.T, path string) *instance.Validator {
	t.Helper()
	ctx := context.Background()
	s, result, err := load.Load(ctx, path)
	require.NoError(t, err, "load schema %s", path)
	require.True(t, result.OK(), "schema %s has errors: %v", path, result.Messages())
	return instance.NewValidator(s)
}

// loadSchemaRaw loads a .yammm file and returns both the schema and validator.
func loadSchemaRaw(t *testing.T, path string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	ctx := context.Background()
	s, result, err := load.Load(ctx, path)
	require.NoError(t, err, "load schema %s", path)
	require.True(t, result.OK(), "schema %s has errors: %v", path, result.Messages())
	return s, instance.NewValidator(s)
}

// loadSchemaExpectError loads a .yammm file and asserts it fails compilation.
// Returns the diagnostic result for inspection.
func loadSchemaExpectError(t *testing.T, path string) diag.Result {
	t.Helper()
	ctx := context.Background()
	_, result, err := load.Load(ctx, path)
	require.NoError(t, err, "load schema %s: unexpected I/O error", path)
	require.False(t, result.OK(), "schema %s should have errors but loaded cleanly", path)
	return result
}

// loadSchemaString loads a schema from inline string content.
func loadSchemaString(t *testing.T, content, name string) *instance.Validator {
	t.Helper()
	ctx := context.Background()
	s, result, err := load.LoadString(ctx, content, name)
	require.NoError(t, err, "load schema string %s", name)
	require.True(t, result.OK(), "schema string %s has errors: %v", name, result.Messages())
	return instance.NewValidator(s)
}

// loadSchemaStringRaw loads a schema from inline string content, returning both schema and validator.
func loadSchemaStringRaw(t *testing.T, content, name string) (*schema.Schema, *instance.Validator) { //nolint:unparam // test helper — second return used selectively
	t.Helper()
	ctx := context.Background()
	s, result, err := load.LoadString(ctx, content, name)
	require.NoError(t, err, "load schema string %s", name)
	require.True(t, result.OK(), "schema string %s has errors: %v", name, result.Messages())
	return s, instance.NewValidator(s)
}

// loadSchemaStringExpectError loads from string and asserts compilation failure.
func loadSchemaStringExpectError(t *testing.T, content, name string) diag.Result {
	t.Helper()
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, content, name)
	require.NoError(t, err, "load schema string %s: unexpected I/O error", name)
	require.False(t, result.OK(), "schema string %s should have errors but loaded cleanly", name)
	return result
}

// loadSchemaWithOpts loads a .yammm file with specific load options.
func loadSchemaWithOpts(t *testing.T, path string, opts ...load.Option) (*schema.Schema, diag.Result, error) { //nolint:unparam // test helper — path varies across test files
	t.Helper()
	ctx := context.Background()
	return load.Load(ctx, path, opts...)
}

// loadTestData reads a JSON data file and extracts instances by type key.
func loadTestData(t *testing.T, dataPath, typeKey string) []instance.RawInstance {
	t.Helper()
	dataBytes, err := os.ReadFile(dataPath)
	require.NoError(t, err, "read test data %s", dataPath)

	adapter, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://" + dataPath)
	parsed, parseResult := adapter.ParseObject(sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse %s failed: %v", dataPath, parseResult.Messages())

	records := parsed[typeKey]
	require.NotEmpty(t, records, "no %q records in %s", typeKey, dataPath)
	return records
}

// assertValid validates a single instance and asserts success.
func assertValid(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance) {
	t.Helper()
	ctx := context.Background()
	valid, failure, err := v.ValidateOne(ctx, typeName, raw)
	require.NoError(t, err)
	assert.Nil(t, failure, "expected valid %s instance, got: %v", typeName, failureMessages(failure))
	assert.NotNil(t, valid)
}

// assertInvalid validates a single instance and asserts failure with specific codes.
func assertInvalid(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance, wantCodes ...diag.Code) {
	t.Helper()
	ctx := context.Background()
	valid, failure, err := v.ValidateOne(ctx, typeName, raw)
	require.NoError(t, err)
	assert.Nil(t, valid, "expected invalid %s instance", typeName)
	require.NotNil(t, failure, "expected validation failure for %s", typeName)

	issueCodes := map[string]bool{}
	for issue := range failure.Result.Issues() {
		issueCodes[issue.Code().String()] = true
	}
	for _, wc := range wantCodes {
		assert.True(t, issueCodes[wc.String()],
			"expected code %s in diagnostics, got: %v", wc, failure.Result.Messages())
	}
}

// assertInvariantFails validates and asserts specific invariant failures by name.
func assertInvariantFails(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance, wantNames ...string) { //nolint:unparam // test helper — typeName varies by test file
	t.Helper()
	ctx := context.Background()
	valid, failure, err := v.ValidateOne(ctx, typeName, raw)
	require.NoError(t, err)
	assert.Nil(t, valid, "expected invariant failure for %s", typeName)
	require.NotNil(t, failure, "expected validation failure for %s", typeName)

	failedInvariants := map[string]bool{}
	for issue := range failure.Result.Issues() {
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
func buildGraph(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Result {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)
	for _, inst := range instances {
		result, err := g.Add(ctx, inst)
		require.NoError(t, err, "graph.Add failed")
		require.True(t, result.OK(), "graph.Add issues: %v", result.Messages())
	}
	return g.Snapshot()
}

// validateOne validates a single raw instance and returns the ValidInstance.
func validateOne(t *testing.T, v *instance.Validator, typeName string, raw instance.RawInstance) *instance.ValidInstance {
	t.Helper()
	ctx := context.Background()
	valid, failure, err := v.ValidateOne(ctx, typeName, raw)
	require.NoError(t, err)
	require.Nil(t, failure, "expected valid %s, got: %v", typeName, failureMessages(failure))
	require.NotNil(t, valid)
	return valid
}

// failureMessages extracts message strings from a validation failure.
func failureMessages(f *instance.ValidationFailure) []string {
	if f == nil {
		return nil
	}
	return f.Result.Messages()
}
