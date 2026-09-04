package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// fkSchema builds the canonical two-type FK schema — Company(id) plus
// Person(id) with an "employer" relation to Company — parameterized on the
// relation's (optional, many) multiplicity.
func fkSchema(t *testing.T, optional, many bool) *schema.Schema {
	t.Helper()
	return mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), optional, many).
		Done())
}

// compositeFKSchema builds the canonical composite-PK FK schema —
// Enrollment(region, studentId) plus Person(id) with an optional-one
// "enrollment" relation to Enrollment.
func compositeFKSchema(t *testing.T) *schema.Schema {
	t.Helper()
	return mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("ENROLLMENT", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())
}

// issueProjection renders a result's issues as a stable, JSON-serializable
// shape — code, severity, and structured details; messages are presentation
// and deliberately excluded.
func issueProjection(result diag.Result) []map[string]any {
	out := make([]map[string]any, 0)
	for issue := range result.Issues() {
		details := make([]map[string]string, 0, len(issue.Details()))
		for _, d := range issue.Details() {
			details = append(details, map[string]string{"Key": d.Key, "Value": d.Value})
		}
		out = append(out, map[string]any{
			"Code":     issue.Code().String(),
			"Severity": issue.Severity().String(),
			"Details":  details,
		})
	}
	return out
}

func TestValidateEdges_SingleFK(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": map[string]any{"_target_id": "42"},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	edge, ok := valid.Edge("EMPLOYER")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Len(t, edge.Targets(), 1)
	assert.Equal(t, `["42"]`, edge.Targets()[0].TargetKey().String())
}

func TestValidateEdges_Many(t *testing.T) {
	s := fkSchema(t, true, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": []any{
				map[string]any{"_target_id": "10"},
				map[string]any{"_target_id": "20"},
				map[string]any{"_target_id": "30"},
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	edge, ok := valid.Edge("EMPLOYER")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Len(t, edge.Targets(), 3)
}

func TestValidateEdges_Optional_Nil(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// "employer" is not present - optional edge
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// No edge should be present
	edge, ok := valid.Edge("EMPLOYER")
	assert.False(t, ok)
	assert.Nil(t, edge)
}

func TestValidateEdges_Required_Absent(t *testing.T) {
	// Per architecture spec: association presence is a graph-layer concern.
	// Absent required associations are valid at instance layer; validated at graph.Check()
	// via E_UNRESOLVED_REQUIRED.
	s := fkSchema(t, false, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			// "employer" absent - valid at instance layer
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// No edge data should be present
	edge, ok := valid.Edge("EMPLOYER")
	assert.False(t, ok)
	assert.Nil(t, edge)
}

func TestValidateEdges_ShapeMismatch_ArrayForSingle(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": []any{map[string]any{"_target_id": "42"}}, // Array when expecting object
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected object")
}

func TestValidateEdges_ShapeMismatch_ObjectForMany(t *testing.T) {
	s := fkSchema(t, true, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": map[string]any{"_target_id": "10"}, // Object when expecting array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected array")
}

func TestValidateEdges_MissingFK(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": map[string]any{}, // Missing _target_id
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "_target_id")
}

func TestValidateEdges_UnknownField(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s) // Default: don't allow unknown fields

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id":    "42",
				"unknown_field": "value",
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "unknown field")
}

func TestValidateEdges_UnknownField_Allowed(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s, instance.WithAllowUnknownFields(true))

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id":    "42",
				"unknown_field": "value", // Should be ignored
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)
}

func TestValidateEdges_EdgePropertyValidation(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "test"
type Company {
    id String primary
}
type Person {
    id String primary
    --> EMPLOYER (_) Company {
        role String required
    }
}`, "test.yammm")
	require.False(t, result.HasErrors(), "schema: %s", result)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id": "42",
				"role":       "engineer",
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	edge, ok := valid.Edge("EMPLOYER")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Len(t, edge.Targets(), 1)

	// Check edge property
	roleVal, ok := edge.Targets()[0].Properties().Get("role")
	require.True(t, ok)
	role, ok := roleVal.String()
	require.True(t, ok)
	assert.Equal(t, "engineer", role)
}

func TestValidateEdges_MissingRequiredEdgeProperty(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "test"
type Company {
    id String primary
}
type Person {
    id String primary
    --> EMPLOYER (_) Company {
        role String required
    }
}`, "test.yammm")
	require.False(t, result.HasErrors(), "schema: %s", result)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id": "42",
				// Missing "role"
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "missing required edge property")
}

func TestValidateEdges_EdgePropertyInvalid(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "test"
type Company {
    id String primary
}
type Person {
    id String primary
    --> EMPLOYER (_) Company {
        rating Integer required
    }
}`, "test.yammm")
	require.False(t, result.HasErrors(), "schema: %s", result)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id": "42",
				"rating":     "not an integer",
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "rating")
}

func TestValidateEdges_FKTypeMismatch(t *testing.T) {
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id": int64(999), // Wrong type - integer instead of string
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "_target_id")
}

func TestValidateEdges_EmptyTargetInElement(t *testing.T) {
	s := fkSchema(t, true, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": []any{
				"not an object", // Invalid element
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected object for edge target")
}

func TestValidateEdges_OptionalEdgeWithEmptyArray(t *testing.T) {
	s := fkSchema(t, true, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": []any{}, // Empty array - valid for optional
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// Edge should be present but empty
	edge, ok := valid.Edge("EMPLOYER")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.True(t, edge.IsEmpty())
}

func TestValidateEdges_RequiredEdgeWithEmptyArray(t *testing.T) {
	// Per architecture spec: association empty-array validation is a graph-layer concern.
	// Empty arrays for required associations are valid at instance layer; validated at graph.Check()
	// via E_UNRESOLVED_REQUIRED with reason="empty".
	s := fkSchema(t, false, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": []any{}, // Empty array - valid at instance layer
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// Edge should be present but empty
	edge, ok := valid.Edge("EMPLOYER")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.True(t, edge.IsEmpty())
}

func TestValidateEdges_CompositeFK(t *testing.T) {
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region":    "us-east",
				"_target_studentId": "12345",
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	edge, ok := valid.Edge("ENROLLMENT")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Len(t, edge.Targets(), 1)
	// Key should have both components
	assert.Equal(t, 2, edge.Targets()[0].TargetKey().Len())
}

func TestValidateEdges_PartialCompositeFK(t *testing.T) {
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region": "us-east",
				// Missing _target_studentId
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "incomplete composite FK")
}

// TestValidateEdges_FKFieldCase verifies that FK field matching follows the
// one fold rule every input key follows: a case variant matches under the
// default mode and does not under WithStrictPropertyNames.
func TestValidateEdges_FKFieldCase(t *testing.T) {
	s := fkSchema(t, true, false)

	t.Run("case_variant_matches_without_strict_mode", func(t *testing.T) {
		validator := instance.NewValidator(s)

		raw := instance.RawInstance{
			Properties: map[string]any{
				"id":       "1",
				"employer": map[string]any{"_target_ID": "42"},
			},
		}

		valid, result := validator.ValidateOne(t.Context(), "Person", raw)

		require.True(t, result.OK(), result.String())
		require.NotNil(t, valid, "_target_ID folds to _target_id under the default mode")
	})

	t.Run("wrong_case_fails_with_strict_mode", func(t *testing.T) {
		validator := instance.NewValidator(s, instance.WithStrictPropertyNames(true))

		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Wrong case: _Target_id instead of _target_id
				"employer": map[string]any{"_Target_id": "42"},
			},
		}

		valid, result := validator.ValidateOne(t.Context(), "Person", raw)

		assert.Nil(t, valid, "Should fail: _Target_id != _target_id (case-sensitive)")
		require.False(t, result.OK())
		assert.Contains(t, result.String(), "missing FK field")
	})

	t.Run("correct_case_succeeds", func(t *testing.T) {
		validator := instance.NewValidator(s)

		raw := instance.RawInstance{
			Properties: map[string]any{
				"id":       "1",
				"employer": map[string]any{"_target_id": "42"},
			},
		}

		valid, result := validator.ValidateOne(t.Context(), "Person", raw)

		require.True(t, result.OK())
		require.NotNil(t, valid, "Should succeed: exact case match")
	})
}

func TestValidateEdges_ExplicitNull_Optional(t *testing.T) {
	// Per architecture spec: null is always a shape error, even for optional associations.
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": nil, // Explicit null - always a shape error
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid edge value")

	yammmtest.GoldenJSON(t, "edge_diag_null_optional", issueProjection(result))
}

func TestValidateEdges_ExplicitNull_Required(t *testing.T) {
	// Per architecture spec: null is always a shape error.
	s := fkSchema(t, false, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": nil, // Explicit null - always a shape error
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid edge value")

	yammmtest.GoldenJSON(t, "edge_diag_null_required", issueProjection(result))
}

func TestValidateEdges_ExplicitNull_Many(t *testing.T) {
	// Per architecture spec: null is always a shape error (expects array).
	s := fkSchema(t, true, true)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": nil, // Explicit null - expects array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid edge value")

	yammmtest.GoldenJSON(t, "edge_diag_null_many", issueProjection(result))
}

func TestValidateEdges_FKDiagnosticDetails_MissingAll(t *testing.T) {
	// Verify E_MISSING_FK_TARGET includes 'relation' and 'expected' details.
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":       "1",
			"employer": map[string]any{"weight": 5}, // No _target_* fields
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "edge_diag_fk_missing_all", issueProjection(result))
}

func TestValidateEdges_FKDiagnosticDetails_Partial(t *testing.T) {
	// Verify E_PARTIAL_COMPOSITE_FK includes 'relation', 'expected', and 'got' details.
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region": "US", // Only one of two required FK fields
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "edge_diag_fk_partial", issueProjection(result))
}

func TestValidateEdges_SingleFK_NullValue(t *testing.T) {
	// Single-PK + _target_id: null
	// Spec: present=1 -> E_TYPE_MISMATCH for the null field (not E_MISSING_FK_TARGET)
	s := fkSchema(t, true, false)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"employer": map[string]any{
				"_target_id": nil, // null value - present but invalid
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	assert.Contains(t, result.String(), "null")
	yammmtest.GoldenJSON(t, "edge_diag_single_fk_null", issueProjection(result))
}

func TestValidateEdges_CompositeFK_OneNullOneValid(t *testing.T) {
	// Composite PK: all present, one null
	// Spec: present==expected -> E_TYPE_MISMATCH for null field only (not E_PARTIAL_COMPOSITE_FK)
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region":    nil,     // null - present but invalid
				"_target_studentId": "12345", // valid
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	assert.Contains(t, result.String(), "_target_region")
	yammmtest.GoldenJSON(t, "edge_diag_composite_one_null_one_valid", issueProjection(result))
}

func TestValidateEdges_CompositeFK_OneNullOneMissing(t *testing.T) {
	// Composite PK: one present (null), one missing
	// Spec: present=1 -> E_PARTIAL_COMPOSITE_FK + E_TYPE_MISMATCH for null
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region": nil, // present but null
				// _target_studentId is missing
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	assert.Contains(t, result.String(), "_target_region")
	yammmtest.GoldenJSON(t, "edge_diag_composite_one_null_one_missing", issueProjection(result))
}

func TestValidateEdges_CompositeFK_OneNullOneInvalidType(t *testing.T) {
	// Composite PK: both present, both invalid (one null, one wrong type)
	// Spec: present==expected -> E_TYPE_MISMATCH for each invalid field (not E_PARTIAL_COMPOSITE_FK)
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region":    nil,        // null - present but invalid
				"_target_studentId": int64(999), // wrong type (integer instead of string) - present but invalid
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "edge_diag_composite_both_invalid", issueProjection(result))
}

func TestValidateEdges_CompositeFK_OneInvalidOneMissing(t *testing.T) {
	// Composite PK: one present (invalid type), one missing
	// Spec: present=1 -> E_PARTIAL_COMPOSITE_FK + E_TYPE_MISMATCH
	// This tests that 'got' includes present keys even if invalid
	s := compositeFKSchema(t)

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"enrollment": map[string]any{
				"_target_region": 12345, // wrong type (integer instead of string)
				// _target_studentId is missing
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())

	yammmtest.GoldenJSON(t, "edge_diag_composite_one_invalid_one_missing", issueProjection(result))
}

// Tests all 4 combinations of (optional/required) x (one/many) multiplicity.
//
// Note: Required edge enforcement happens at the graph layer (Check()), not
// at instance validation. The instance layer validates edge _format_ (shape,
// FK fields), not presence requirements. This test verifies format validation
// works correctly for all multiplicity combinations.

func TestValidateEdges_MultiplicityMatrix(t *testing.T) {
	testCases := []struct {
		name          string
		optional      bool
		many          bool
		input         any    // The edge value to test
		expectSuccess bool   // Whether validation should succeed
		expectEdges   int    // Expected number of targets (if success)
		description   string // What we're testing
	}{
		// --- Optional One ---
		{
			name:          "optional_one_absent",
			optional:      true,
			many:          false,
			input:         nil, // Not present in properties
			expectSuccess: true,
			expectEdges:   0,
			description:   "(optional x one) absent edge is valid",
		},
		{
			name:          "optional_one_present",
			optional:      true,
			many:          false,
			input:         map[string]any{"_target_id": "1"},
			expectSuccess: true,
			expectEdges:   1,
			description:   "(optional x one) present edge is valid",
		},
		{
			name:          "optional_one_wrong_shape",
			optional:      true,
			many:          false,
			input:         []any{map[string]any{"_target_id": "1"}}, // Array instead of object
			expectSuccess: false,
			description:   "(optional x one) array shape fails",
		},

		// --- Required One ---
		// Note: absent required edge passes validation - enforced at graph.Check()
		{
			name:          "required_one_absent",
			optional:      false,
			many:          false,
			input:         nil, // Not present - validated later at graph.Check()
			expectSuccess: true,
			expectEdges:   0,
			description:   "(required x one) absent passes validation (enforced at graph.Check)",
		},
		{
			name:          "required_one_present",
			optional:      false,
			many:          false,
			input:         map[string]any{"_target_id": "1"},
			expectSuccess: true,
			expectEdges:   1,
			description:   "(required x one) present edge is valid",
		},
		{
			name:          "required_one_wrong_shape",
			optional:      false,
			many:          false,
			input:         []any{map[string]any{"_target_id": "1"}}, // Array instead of object
			expectSuccess: false,
			description:   "(required x one) array shape fails",
		},

		// --- Optional Many ---
		{
			name:          "optional_many_absent",
			optional:      true,
			many:          true,
			input:         nil, // Not present
			expectSuccess: true,
			expectEdges:   0,
			description:   "(optional x many) absent edge is valid",
		},
		{
			name:          "optional_many_empty",
			optional:      true,
			many:          true,
			input:         []any{},
			expectSuccess: true,
			expectEdges:   0,
			description:   "(optional x many) empty array is valid",
		},
		{
			name:          "optional_many_single",
			optional:      true,
			many:          true,
			input:         []any{map[string]any{"_target_id": "1"}},
			expectSuccess: true,
			expectEdges:   1,
			description:   "(optional x many) single element is valid",
		},
		{
			name:          "optional_many_multiple",
			optional:      true,
			many:          true,
			input:         []any{map[string]any{"_target_id": "1"}, map[string]any{"_target_id": "2"}},
			expectSuccess: true,
			expectEdges:   2,
			description:   "(optional x many) multiple elements are valid",
		},
		{
			name:          "optional_many_wrong_shape",
			optional:      true,
			many:          true,
			input:         map[string]any{"_target_id": "1"}, // Object instead of array
			expectSuccess: false,
			description:   "(optional x many) object shape fails",
		},

		// --- Required Many ---
		// Note: absent/empty required edge passes validation - enforced at graph.Check()
		{
			name:          "required_many_absent",
			optional:      false,
			many:          true,
			input:         nil, // Not present - validated later at graph.Check()
			expectSuccess: true,
			expectEdges:   0,
			description:   "(required x many) absent passes validation (enforced at graph.Check)",
		},
		{
			name:          "required_many_empty",
			optional:      false,
			many:          true,
			input:         []any{},
			expectSuccess: true,
			expectEdges:   0,
			description:   "(required x many) empty passes validation (enforced at graph.Check)",
		},
		{
			name:          "required_many_single",
			optional:      false,
			many:          true,
			input:         []any{map[string]any{"_target_id": "1"}},
			expectSuccess: true,
			expectEdges:   1,
			description:   "(required x many) single element is valid",
		},
		{
			name:          "required_many_multiple",
			optional:      false,
			many:          true,
			input:         []any{map[string]any{"_target_id": "1"}, map[string]any{"_target_id": "2"}, map[string]any{"_target_id": "3"}},
			expectSuccess: true,
			expectEdges:   3,
			description:   "(required x many) multiple elements are valid",
		},
		{
			name:          "required_many_wrong_shape",
			optional:      false,
			many:          true,
			input:         map[string]any{"_target_id": "1"}, // Object instead of array
			expectSuccess: false,
			description:   "(required x many) object shape fails",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create schema with the specific multiplicity configuration
			s := fkSchema(t, tc.optional, tc.many)

			validator := instance.NewValidator(s)

			// Build raw instance
			props := map[string]any{"id": "1"}
			if tc.input != nil {
				props["employer"] = tc.input
			}
			// Note: absent means not in props at all, handled by not adding to props

			raw := instance.RawInstance{
				Properties: props,
			}

			valid, result := validator.ValidateOne(t.Context(), "Person", raw)

			if tc.expectSuccess {
				require.True(t, result.OK(), "%s: should succeed but got failure", tc.description)
				require.NotNil(t, valid, "%s: should have valid instance", tc.description)

				edge, ok := valid.Edge("EMPLOYER")
				if tc.expectEdges > 0 {
					require.True(t, ok, "%s: should have edge", tc.description)
					assert.Len(t, edge.Targets(), tc.expectEdges, "%s: edge count", tc.description)
				} else if ok {
					// Zero edges means either no edge data or empty
					assert.Empty(t, edge.Targets(), "%s: should have 0 targets", tc.description)
				}
			} else {
				assert.Nil(t, valid, "%s: should fail but got valid", tc.description)
				require.False(t, result.OK(), "%s: should have failure", tc.description)
			}
		})
	}
}

// TestValidateEdges_DataTypeEdgeProperty pins that a DataType-typed edge
// property validates values against its resolved underlying constraint —
// valid values pass, constraint-violating values fail with E_CONSTRAINT_FAIL —
// exactly like a DataType-typed type property.
func TestValidateEdges_DataTypeEdgeProperty(t *testing.T) {
	s, result := schema.LoadString(t.Context(), `schema "geo"
type StateCode = String [2, 2]
type State {
    code StateCode primary
}
type County {
    fips String primary
    --> IN_STATE (one) State {
        code_tag StateCode
    }
}`, "test.yammm")
	require.False(t, result.HasErrors(), "schema: %s", result)

	validator := instance.NewValidator(s)

	t.Run("valid value passes", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"fips": "06001",
				"in_state": map[string]any{
					"_target_code": "CA",
					"code_tag":     "CA",
				},
			},
		}
		valid, result := validator.ValidateOne(t.Context(), "County", raw)
		require.True(t, result.OK(), "valid DataType edge-property value must validate: %s", result)
		require.NotNil(t, valid)

		edge, ok := valid.Edge("IN_STATE")
		require.True(t, ok)
		tagVal, ok := edge.Targets()[0].Properties().Get("code_tag")
		require.True(t, ok)
		tag, ok := tagVal.String()
		require.True(t, ok)
		assert.Equal(t, "CA", tag)
	})

	t.Run("constraint-violating value fails", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"fips": "06001",
				"in_state": map[string]any{
					"_target_code": "CA",
					"code_tag":     "C",
				},
			},
		}
		valid, result := validator.ValidateOne(t.Context(), "County", raw)
		assert.Nil(t, valid)
		require.False(t, result.OK())
		assert.True(t, result.HasCode(instance.ErrConstraintFail),
			"want E_CONSTRAINT_FAIL, got: %s", result)
	})
}
