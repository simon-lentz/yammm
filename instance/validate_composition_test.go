package instance_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// composedOf reads one composition relation through the Compositions
// iterator, the surviving accessor.
func composedOf(vi instance.ValidInstance, relation string) (immutable.Value, bool) {
	for rel, children := range vi.Compositions() {
		if rel == relation {
			return children, true
		}
	}
	return immutable.Value{}, false
}

// compositionSchema builds the canonical composition schema — part type
// Address(id) plus Person(id) with a many-valued "addresses" composition —
// parameterized on whether the composition is optional.
func compositionSchema(t *testing.T, optional bool) schema.Schema {
	t.Helper()
	multiplicity := "one:many"
	if optional {
		multiplicity = "many"
	}
	return mustLoad(t, fmt.Sprintf(`schema "test"

part type Address {
	id String primary
}

type Person {
	id String primary
	*-> ADDRESSES (%s) Address
}
`, multiplicity))
}

func TestValidateCompositions_Single(t *testing.T) {
	s := mustLoad(t, `schema "test"

part type Address {
	id String primary
	street String required
}

type Person {
	id String primary
	*-> ADDRESSES (many) Address
}
`)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				map[string]any{"id": "100", "street": "Main St"},
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.False(t, valid.IsZero())

	composed, ok := composedOf(valid, "ADDRESSES")
	require.True(t, ok)
	require.False(t, composed.IsNil())
}

func TestValidateCompositions_Multiple(t *testing.T) {
	s := mustLoad(t, `schema "test"

part type Address {
	id String primary
	street String required
}

type Person {
	id String primary
	*-> ADDRESSES (many) Address
}
`)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				map[string]any{"id": "100", "street": "Main St"},
				map[string]any{"id": "101", "street": "Oak Ave"},
				map[string]any{"id": "102", "street": "Pine Rd"},
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.False(t, valid.IsZero())

	composed, ok := composedOf(valid, "ADDRESSES")
	require.True(t, ok)
	require.False(t, composed.IsNil())
}

func TestValidateCompositions_Optional_Nil(t *testing.T) {
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// "addresses" is not present - optional composition
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.False(t, valid.IsZero())

	// No composition should be present
	composed, ok := composedOf(valid, "ADDRESSES")
	assert.False(t, ok)
	assert.True(t, composed.IsNil())
}

func TestValidateCompositions_Optional_Empty(t *testing.T) {
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": []any{}, // Empty array - valid for optional
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.False(t, valid.IsZero())

	// Composition should be present but empty
	composed, ok := composedOf(valid, "ADDRESSES")
	require.True(t, ok)
	require.False(t, composed.IsNil())
}

func TestValidateCompositions_Required_Missing(t *testing.T) {
	s := compositionSchema(t, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// "addresses" missing - required composition
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "missing required composition")
}

func TestValidateCompositions_Required_Empty(t *testing.T) {
	s := compositionSchema(t, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": []any{}, // Empty - not valid for required
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "required composition cannot be empty")
}

func TestValidateCompositions_DuplicatePK(t *testing.T) {
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				map[string]any{"id": "100"},
				map[string]any{"id": "100"}, // Duplicate PK
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "duplicate primary key")
}

func TestValidateCompositions_ChildValidationFails(t *testing.T) {
	s := mustLoad(t, `schema "test"

part type Address {
	id String primary
	street String required
}

type Person {
	id String primary
	*-> ADDRESSES (many) Address
}
`)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				map[string]any{"id": "100"}, // Missing required "street"
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "missing required")
}

func TestValidateCompositions_InvalidChildShape(t *testing.T) {
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				"not an object", // Invalid - should be object
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "composition child must be an object")
}

func TestValidateCompositions_NotArray(t *testing.T) {
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": map[string]any{"id": "100"}, // Object instead of array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected array")
}

func TestValidateCompositions_ExplicitNull_Optional(t *testing.T) {
	// Per architecture spec: null is always a shape error, even for optional compositions.
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": nil, // Explicit null - always a shape error
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid composition value")

	yammmtest.GoldenJSON(t, "composition_diag_null_optional", issueProjection(result))
}

func TestValidateCompositions_ExplicitNull_Required(t *testing.T) {
	// Per architecture spec: null is always a shape error.
	s := compositionSchema(t, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": nil, // Explicit null - always a shape error
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid composition value")

	yammmtest.GoldenJSON(t, "composition_diag_null_required", issueProjection(result))
}

func TestValidateCompositions_ReasonDetail_Absent(t *testing.T) {
	// Verify E_UNRESOLVED_REQUIRED_COMPOSITION includes reason="absent" for missing field.
	s := compositionSchema(t, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// "addresses" absent
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "composition_diag_reason_absent", issueProjection(result))
}

func TestValidateCompositions_ReasonDetail_Empty(t *testing.T) {
	// Verify E_UNRESOLVED_REQUIRED_COMPOSITION includes reason="empty" for empty array.
	s := compositionSchema(t, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        "1",
			"addresses": []any{}, // Empty array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.True(t, valid.IsZero())
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "composition_diag_reason_empty", issueProjection(result))
}

func TestValidateCompositions_DuplicatePK_PathFormat(t *testing.T) {
	// Verify that duplicate PK errors use PK-based path format, not array index.
	s := compositionSchema(t, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"addresses": []any{
				map[string]any{"id": "100"},
				map[string]any{"id": "100"}, // Duplicate PK
			},
		},
	}

	_, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.False(t, result.OK())

	// Find the duplicate PK error and check its path
	var foundPath string
	for issue := range result.Issues() {
		if issue.Code() == instance.ErrDuplicateComposedPK {
			foundPath = issue.Path()
			break
		}
	}

	// Path should use PK format: $.addresses[id="100"], not $.addresses[1]
	assert.Contains(t, foundPath, `[id="100"]`, "path should use PK-based format")
	assert.NotContains(t, foundPath, "[1]", "path should not use array index")
}

func TestValidateCompositions_CompositePK_PathFormat(t *testing.T) {
	// Verify that composite PKs are properly formatted in paths.
	s := mustLoad(t, `schema "test"

part type Enrollment {
	region String primary
	studentId String primary
}

type School {
	id String primary
	*-> ENROLLMENTS (many) Enrollment
}
`)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollments": []any{
				map[string]any{"region": "us", "studentId": "123"},
				map[string]any{"region": "us", "studentId": "123"}, // Duplicate
			},
		},
	}

	_, result := validator.ValidateOne(t.Context(), "School", raw)

	require.False(t, result.OK())

	// Find the duplicate PK error and check its path
	var foundPath string
	for issue := range result.Issues() {
		if issue.Code() == instance.ErrDuplicateComposedPK {
			foundPath = issue.Path()
			break
		}
	}

	// Path should include composite PK: region="us",studentId=123
	assert.Contains(t, foundPath, `region="us"`, "path should include string PK field")
	assert.Contains(t, foundPath, `studentId="123"`, "path should include string PK field")
}

// TestValidateCompositions_OneCardinality pins this package's own enforcement
// of (one) composition cardinality. A composition always arrives as an array,
// so nothing about the shape settles multiplicity the way a (one) association's
// object form does, and the check has to be explicit.
func TestValidateCompositions_OneCardinality(t *testing.T) {
	s := mustLoad(t, `schema "test"

part type Address {
	id String primary
	street String required
}

type Person {
	id String primary
	*-> HOME (_:one) Address
}
`)

	validator := instance.NewValidator(s)
	person := func(homes ...any) instance.RawInstance {
		return instance.RawInstance{Properties: map[string]any{"id": "1", "home": homes}}
	}
	addr := func(id string) any { return map[string]any{"id": id, "street": "Main St"} }

	// One child is the whole of what a (one) slot may hold.
	valid, result := validator.ValidateOne(t.Context(), "Person", person(addr("100")))
	require.True(t, result.OK(), result.String())
	require.False(t, valid.IsZero())

	// Two is refused here, not deferred to the graph, under the code graph
	// assembly reports the same fact with.
	_, result = validator.ValidateOne(t.Context(), "Person", person(addr("100"), addr("200")))
	require.False(t, result.OK())
	assert.True(t, result.HasCode(instance.ErrDuplicateComposedPK),
		"want E_DUPLICATE_COMPOSED_PK, got %s", result.String())

	// An empty array on an optional (one) slot stays legal — the cardinality
	// check must not swallow the empty case the arm above it handles.
	_, result = validator.ValidateOne(t.Context(), "Person", person([]any{}...))
	require.True(t, result.OK(), result.String())

	// A nil slice is no empty array: it holds no value, so it is the null a
	// composition refuses, as encoding/json writes it.
	_, nullResult := validator.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "1", "home": nil}})
	_, result = validator.ValidateOne(t.Context(), "Person", person())
	require.False(t, result.OK())
	assert.Equal(t, nullResult.String(), result.String(), "a nil slice must draw what an explicit null draws")
}
