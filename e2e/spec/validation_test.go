package spec_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// ValidateOne — three outcomes
// =============================================================================

// TestValidation_ValidateOne_Success verifies that ValidateOne returns
// (valid, nil, nil) for a valid instance.
// Source: SPEC.md, "Instance Validation" — ValidateOne returns exactly one of
// three outcomes: valid, failure, or error.
func TestValidation_ValidateOne_Success(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name": "Alice",
		"age":  30,
	}))
	require.NoError(t, err)
	assert.Nil(t, failure, "expected no failure, got: %v", failureMessages(failure))
	require.NotNil(t, valid, "expected valid instance")
	assert.Equal(t, "Person", valid.TypeName())
}

// TestValidation_ValidateOne_Failure verifies that ValidateOne returns
// (nil, failure, nil) when validation fails (e.g., missing required property).
// Source: SPEC.md, "Instance Validation" — validation failure returns diagnostics.
func TestValidation_ValidateOne_Failure(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	// Missing required "name" property (which is also the primary key)
	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"age": 25,
	}))
	require.NoError(t, err)
	assert.Nil(t, valid, "expected nil valid instance")
	require.NotNil(t, failure, "expected validation failure")
	assert.True(t, failure.HasErrors(), "failure should have errors")
}

// TestValidation_ValidateOne_TypeNotFound verifies that ValidateOne returns
// a validation failure (not a system error) when the type name does not exist.
// Source: SPEC.md, "Instance Validation" — type resolution failure semantics.
func TestValidation_ValidateOne_TypeNotFound(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	// Non-existent type name returns (nil, failure, nil), not (nil, nil, error).
	// This is because type-not-found is a validation-level failure, not a system error.
	valid, failure, err := v.ValidateOne(ctx, "NonExistentType", raw(map[string]any{
		"name": "test",
	}))
	require.NoError(t, err, "type-not-found should not be a system error")
	assert.Nil(t, valid)
	require.NotNil(t, failure, "expected validation failure for nonexistent type")

	// Verify the diagnostic code
	issueCodes := map[string]bool{}
	for issue := range failure.Result.Issues() {
		issueCodes[issue.Code().String()] = true
	}
	assert.True(t, issueCodes[diag.E_INSTANCE_TYPE_NOT_FOUND.String()],
		"expected E_INSTANCE_TYPE_NOT_FOUND code, got issues: %v", failure.Result.Messages())
}

// =============================================================================
// Validate — batch API
// =============================================================================

// TestValidation_Validate_Batch verifies that Validate processes a batch of
// instances and separates valid from invalid results.
// Source: SPEC.md, "Instance Validation" — Validate batch returns valid + failures.
func TestValidation_Validate_Batch(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	raws := []instance.RawInstance{
		raw(map[string]any{"name": "Alice", "age": 30}), // valid
		raw(map[string]any{"name": "Bob"}),               // valid (age is optional)
		raw(map[string]any{"age": 200}),                  // invalid: missing required "name"
	}

	valid, failures, err := v.Validate(ctx, "Person", raws)
	require.NoError(t, err)
	assert.Len(t, valid, 2, "expected 2 valid instances")
	assert.Len(t, failures, 1, "expected 1 failure")

	// Verify the failure is for the missing required field
	assert.True(t, failures[0].HasErrors())
}

// TestValidation_Validate_NilInput verifies that Validate returns (nil, nil, nil)
// when passed a nil slice.
// Source: SPEC.md, "Instance Validation" — nil input handling.
func TestValidation_Validate_NilInput(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	valid, failures, err := v.Validate(ctx, "Person", nil)
	require.NoError(t, err)
	assert.Nil(t, valid)
	assert.Nil(t, failures)
}

// TestValidation_Validate_EmptyInput verifies that Validate returns
// (empty-slice, nil, nil) when passed an empty slice.
// Source: SPEC.md, "Instance Validation" — empty input handling.
func TestValidation_Validate_EmptyInput(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	valid, failures, err := v.Validate(ctx, "Person", []instance.RawInstance{})
	require.NoError(t, err)
	assert.NotNil(t, valid, "empty input should return non-nil empty slice")
	assert.Empty(t, valid)
	assert.Nil(t, failures)
}

// =============================================================================
// Validator options — WithLogger
// =============================================================================

// TestValidation_WithLogger verifies that creating a validator with WithLogger
// does not panic and validation proceeds normally.
// Source: SPEC.md, "Instance Validation" — WithLogger option.
func TestValidation_WithLogger(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	// Create validator with a logger — should not panic
	v := instance.NewValidator(s, instance.WithLogger(slog.Default()))

	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name": "Alice",
		"age":  30,
	}))
	require.NoError(t, err)
	assert.Nil(t, failure)
	assert.NotNil(t, valid)
}

// =============================================================================
// Validator options — WithStrictPropertyNames
// =============================================================================

// TestValidation_StrictPropertyNames verifies that WithStrictPropertyNames(true)
// rejects case-mismatched property names.
// Source: SPEC.md, "Instance Validation" — strict property name matching.
func TestValidation_StrictPropertyNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	v := instance.NewValidator(s, instance.WithStrictPropertyNames(true))

	// "Name" (capital N) should fail in strict mode — schema defines "name"
	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"Name": "Alice",
	}))
	require.NoError(t, err)
	assert.Nil(t, valid, "expected case-mismatched name to be rejected in strict mode")
	require.NotNil(t, failure, "expected validation failure for case mismatch")
}

// TestValidation_NonStrictPropertyNames verifies that the default (non-strict)
// mode accepts case-insensitive property name matching.
// Source: SPEC.md, "Instance Validation" — default case-insensitive property matching.
func TestValidation_NonStrictPropertyNames(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	// Default: non-strict, so "Name" should match "name"
	v := instance.NewValidator(s, instance.WithStrictPropertyNames(false))

	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"Name": "Alice",
	}))
	require.NoError(t, err)
	assert.Nil(t, failure, "non-strict mode should accept case-insensitive names, got: %v", failureMessages(failure))
	assert.NotNil(t, valid)
}

// =============================================================================
// Validator options — WithAllowUnknownFields
// =============================================================================

// TestValidation_AllowUnknownFields verifies that WithAllowUnknownFields(true)
// silently ignores extra fields that are not in the schema.
// Source: SPEC.md, "Instance Validation" — unknown field handling.
func TestValidation_AllowUnknownFields(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	v := instance.NewValidator(s, instance.WithAllowUnknownFields(true))

	// Extra field "email" is not in the schema — should be silently ignored
	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name":  "Alice",
		"age":   30,
		"email": "alice@example.com",
	}))
	require.NoError(t, err)
	assert.Nil(t, failure, "allowUnknownFields should ignore extra fields, got: %v", failureMessages(failure))
	assert.NotNil(t, valid)
}

// TestValidation_RejectUnknownFieldsByDefault verifies that the default
// (allowUnknownFields=false) rejects extra fields.
// Source: SPEC.md, "Instance Validation" — default rejects unknown fields.
func TestValidation_RejectUnknownFieldsByDefault(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	// Default: unknown fields not allowed
	v := instance.NewValidator(s)

	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name":  "Alice",
		"email": "alice@example.com",
	}))
	require.NoError(t, err)
	assert.Nil(t, valid, "default should reject unknown fields")
	require.NotNil(t, failure, "expected failure for unknown field")

	// Verify the diagnostic code is E_UNKNOWN_FIELD
	issueCodes := map[string]bool{}
	for issue := range failure.Result.Issues() {
		issueCodes[issue.Code().String()] = true
	}
	assert.True(t, issueCodes[diag.E_UNKNOWN_FIELD.String()],
		"expected E_UNKNOWN_FIELD, got: %v", failure.Result.Messages())
}

// =============================================================================
// Validator options — WithMaxIssuesPerInstance
// =============================================================================

// TestValidation_MaxIssuesPerInstance verifies that WithMaxIssuesPerInstance
// caps the number of diagnostics collected for a single instance.
// Source: SPEC.md, "Instance Validation" — issue count capping.
func TestValidation_MaxIssuesPerInstance(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Schema with many required properties to provoke multiple errors
	schemaStr := `schema "MultiReq"
type Record {
    id String primary
    a String required
    b String required
    c String required
    d String required
    e String required
}`
	schema, _ := loadSchemaStringRaw(t, schemaStr, "multi_req_capped")

	// Create validator with max issues capped at 1
	cappedValidator := instance.NewValidator(schema, instance.WithMaxIssuesPerInstance(1))

	// Provide an instance missing all required fields (a through e) — should
	// generate 5 errors but be capped at 1
	valid, failure, err := cappedValidator.ValidateOne(ctx, "Record", raw(map[string]any{
		"id": "test",
		// a, b, c, d, e all missing
	}))
	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)

	issueCount := failure.Result.Len()
	assert.LessOrEqual(t, issueCount, 1,
		"expected at most 1 issue with MaxIssuesPerInstance(1), got %d", issueCount)
}

// =============================================================================
// Input formats — map[string]any
// =============================================================================

// TestValidation_MapInput verifies that validation accepts map[string]any input
// via RawInstance.Properties.
// Source: SPEC.md, "Instance Validation" — map[string]any accepted.
func TestValidation_MapInput(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	// map[string]any is the native input format
	props := map[string]any{
		"name": "Alice",
		"age":  30,
	}
	valid, failure, err := v.ValidateOne(ctx, "Person", instance.RawInstance{
		Properties: props,
	})
	require.NoError(t, err)
	assert.Nil(t, failure, "map[string]any should be accepted, got: %v", failureMessages(failure))
	assert.NotNil(t, valid)
}

// =============================================================================
// Input formats — Go struct via JSON round-trip
// =============================================================================

// TestValidation_GoStructViaJSONRoundTrip verifies that Go structs with json
// tags can be used as input by marshaling to JSON and unmarshaling to
// map[string]any. This is the canonical pattern for using typed Go structs
// with the validation API.
//
// Note: RawInstance.Properties requires map[string]any. Go structs cannot be
// passed directly — they must be converted via JSON round-trip or reflection.
// Source: SPEC.md, "Instance Validation" — typed input handling.
func TestValidation_GoStructViaJSONRoundTrip(t *testing.T) {
	t.Parallel()

	type PersonInput struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}

	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	// Convert Go struct to map[string]any via JSON round-trip
	input := PersonInput{Name: "Alice", Age: 30}
	data, err := json.Marshal(input)
	require.NoError(t, err)

	var props map[string]any
	require.NoError(t, json.Unmarshal(data, &props))

	valid, failure, err := v.ValidateOne(ctx, "Person", instance.RawInstance{
		Properties: props,
	})
	require.NoError(t, err)
	assert.Nil(t, failure, "Go struct via JSON should validate, got: %v", failureMessages(failure))
	assert.NotNil(t, valid)
}

// =============================================================================
// Instance shape — JSON adapter top-level object keyed by type names
// =============================================================================

// TestValidation_JSONAdapterTopLevelKeys verifies that the JSON adapter parses
// a top-level object where each key is a type name mapping to an array of
// instances, and these instances validate correctly.
// Source: SPEC.md, "Instance Validation" — JSON top-level object keyed by type names.
func TestValidation_JSONAdapterTopLevelKeys(t *testing.T) {
	t.Parallel()

	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	// Load data through the JSON adapter
	dataBytes, err := os.ReadFile("testdata/validation/data.json")
	require.NoError(t, err)

	adapter, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err)

	sourceID := location.NewSourceID("test://validation/data.json")
	parsed, parseResult := adapter.ParseObject(sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse failed: %v", parseResult.Messages())

	// Verify the parsed map has type-keyed entries
	personRecords := parsed["Person"]
	require.NotEmpty(t, personRecords, "expected Person records in parsed data")

	// Validate each parsed record
	for i, rec := range personRecords {
		valid, failure, err := v.ValidateOne(ctx, "Person", rec)
		require.NoError(t, err, "record %d", i)
		assert.Nil(t, failure, "record %d should be valid, got: %v", i, failureMessages(failure))
		assert.NotNil(t, valid, "record %d should produce a valid instance", i)
	}

	// Also verify the invalid records fail
	invalidRecords := parsed["Person__invalid"]
	require.NotEmpty(t, invalidRecords, "expected Person__invalid records")

	for i, rec := range invalidRecords {
		valid, failure, err := v.ValidateOne(ctx, "Person", rec)
		require.NoError(t, err, "invalid record %d", i)
		assert.Nil(t, valid, "invalid record %d should not validate", i)
		assert.NotNil(t, failure, "invalid record %d should produce failure", i)
	}
}

// =============================================================================
// Instance shape — association edges with _target_ conventions
// =============================================================================

// TestValidation_AssociationEdgeTargetConvention verifies that association edge
// data uses the _target_ prefix convention for foreign key references.
// Source: SPEC.md, "Instance Validation" — _target_ convention for edges.
func TestValidation_AssociationEdgeTargetConvention(t *testing.T) {
	t.Parallel()

	schemaContent := `schema "EdgeConvention"

type Company {
    title String primary
}

type Person {
    name String primary
    --> WORKS_AT Company
}
`
	v := loadSchemaString(t, schemaContent, "edge_convention")
	ctx := context.Background()

	// Default multiplicity is optional/one (not many), so edge data is a single
	// object (not an array). The _target_ prefix names the FK fields referencing
	// the target type's primary key properties.
	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name":     "Alice",
		"works_at": map[string]any{"_target_title": "Acme Inc"},
	}))
	require.NoError(t, err)
	assert.Nil(t, failure, "expected valid instance with edge, got: %v", failureMessages(failure))
	require.NotNil(t, valid)

	// Verify the edge was captured
	edge, found := valid.Edge("WORKS_AT")
	assert.True(t, found, "expected WORKS_AT edge to be present")
	assert.Equal(t, 1, edge.TargetCount(), "expected 1 edge target")
}

// TestValidation_AssociationEdgeManyTargets verifies that a many-multiplicity
// association accepts an array of edge target objects.
// Source: SPEC.md, "Instance Validation" — many-edge array shape.
func TestValidation_AssociationEdgeManyTargets(t *testing.T) {
	t.Parallel()

	schemaContent := `schema "ManyEdge"

type Tag {
    label String primary
}

type Article {
    title String primary
    --> TAGGED_WITH (many) Tag
}
`
	v := loadSchemaString(t, schemaContent, "many_edge")
	ctx := context.Background()

	// Many-multiplicity edge uses an array of objects
	valid, failure, err := v.ValidateOne(ctx, "Article", raw(map[string]any{
		"title": "Hello World",
		"tagged_with": []any{
			map[string]any{"_target_label": "golang"},
			map[string]any{"_target_label": "testing"},
		},
	}))
	require.NoError(t, err)
	assert.Nil(t, failure, "expected valid instance with many-edge, got: %v", failureMessages(failure))
	require.NotNil(t, valid)

	edge, found := valid.Edge("TAGGED_WITH")
	assert.True(t, found, "expected TAGGED_WITH edge to be present")
	assert.Equal(t, 2, edge.TargetCount(), "expected 2 edge targets")
}

// =============================================================================
// RecommendedValidatorOptions
// =============================================================================

// TestValidation_RecommendedOptions verifies that RecommendedValidatorOptions
// enables strict property names and disallows unknown fields.
// Source: SPEC.md, "Instance Validation" — recommended defaults.
func TestValidation_RecommendedOptions(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result, err := loadSchemaWithOpts(t, "testdata/validation/basic.yammm")
	require.NoError(t, err)
	require.True(t, result.OK())

	v := instance.NewValidator(s, instance.RecommendedValidatorOptions()...)

	// Strict mode: "Name" (wrong case) should fail
	valid, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"Name": "Alice",
	}))
	require.NoError(t, err)
	assert.Nil(t, valid, "recommended options should reject case-mismatched names")
	assert.NotNil(t, failure)

	// Unknown fields should also be rejected
	valid2, failure2, err2 := v.ValidateOne(ctx, "Person", raw(map[string]any{
		"name":    "Bob",
		"unknown": "field",
	}))
	require.NoError(t, err2)
	assert.Nil(t, valid2, "recommended options should reject unknown fields")
	assert.NotNil(t, failure2)
}

// =============================================================================
// Validator concurrency safety
// =============================================================================

// TestValidation_ConcurrentUse verifies that a Validator is safe for concurrent
// use from multiple goroutines.
// Source: SPEC.md, "Instance Validation" — Validator is stateless and concurrent-safe.
func TestValidation_ConcurrentUse(t *testing.T) {
	t.Parallel()
	v := loadSchema(t, "testdata/validation/basic.yammm")
	ctx := context.Background()

	const goroutines = 10
	errs := make(chan error, goroutines)

	for i := range goroutines {
		go func(idx int) {
			_, failure, err := v.ValidateOne(ctx, "Person", raw(map[string]any{
				"name": "Person",
				"age":  idx,
			}))
			if err != nil {
				errs <- err
				return
			}
			if failure != nil {
				errs <- failure
				return
			}
			errs <- nil
		}(i)
	}

	for range goroutines {
		err := <-errs
		assert.NoError(t, err)
	}
}
