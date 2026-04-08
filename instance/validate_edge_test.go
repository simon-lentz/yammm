package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// --- Edge Validation Tests ---

func TestValidateEdges_SingleFK(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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

	edge, ok := valid.Edge("employer")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Equal(t, 1, edge.TargetCount())
	assert.Equal(t, `["42"]`, edge.Targets()[0].TargetKey().String())
}

func TestValidateEdges_Many(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"tags": []any{
				map[string]any{"_target_id": "10"},
				map[string]any{"_target_id": "20"},
				map[string]any{"_target_id": "30"},
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	edge, ok := valid.Edge("tags")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Equal(t, 3, edge.TargetCount())
}

func TestValidateEdges_Optional_Nil(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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
	edge, ok := valid.Edge("employer")
	assert.False(t, ok)
	assert.Nil(t, edge)
}

func TestValidateEdges_Required_Absent(t *testing.T) {
	// Per architecture spec: association presence is a graph-layer concern.
	// Absent required associations are valid at instance layer; validated at graph.Check()
	// via E_UNRESOLVED_REQUIRED.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), false, false). // NOT optional
		Done())

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
	edge, ok := valid.Edge("employer")
	assert.False(t, ok)
	assert.Nil(t, edge)
}

func TestValidateEdges_ShapeMismatch_ArrayForSingle(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false). // many=false
		Done())

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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), true, true). // many=true
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"tags": map[string]any{"_target_id": "10"}, // Object when expecting array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected array")
}

func TestValidateEdges_MissingFK(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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
	assert.Equal(t, 1, edge.TargetCount())

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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()). // id is string
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), true, true).
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": "1",
			"tags": []any{
				"not an object", // Invalid element
			},
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "expected object for edge target")
}

func TestValidateEdges_OptionalEdgeWithEmptyArray(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), true, true). // Optional, many
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"tags": []any{}, // Empty array - valid for optional
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// Edge should be present but empty
	edge, ok := valid.Edge("tags")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.True(t, edge.IsEmpty())
}

func TestValidateEdges_RequiredEdgeWithEmptyArray(t *testing.T) {
	// Per architecture spec: association empty-array validation is a graph-layer concern.
	// Empty arrays for required associations are valid at instance layer; validated at graph.Check()
	// via E_UNRESOLVED_REQUIRED with reason="empty".
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), false, true). // NOT optional, many
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"tags": []any{}, // Empty array - valid at instance layer
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	require.True(t, result.OK())
	require.NotNil(t, valid)

	// Edge should be present but empty
	edge, ok := valid.Edge("tags")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.True(t, edge.IsEmpty())
}

// --- Composite FK Tests ---

func TestValidateEdges_CompositeFK(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	edge, ok := valid.Edge("enrollment")
	require.True(t, ok)
	require.NotNil(t, edge)
	assert.Equal(t, 1, edge.TargetCount())
	// Key should have both components
	assert.Equal(t, 2, edge.Targets()[0].TargetKey().Len())
}

func TestValidateEdges_PartialCompositeFK(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

// TestValidateEdges_FKCaseSensitive verifies that FK field matching is always case-sensitive,
// regardless of the StrictPropertyNames setting.
// Per architecture spec: "_target_ID" does NOT match expected "_target_id".
func TestValidateEdges_FKCaseSensitive(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

	t.Run("wrong_case_fails_even_without_strict_mode", func(t *testing.T) {
		// Default: StrictPropertyNames = false, but FK fields are ALWAYS case-sensitive
		validator := instance.NewValidator(s)

		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Wrong case: _target_ID instead of _target_id
				"employer": map[string]any{"_target_ID": "42"},
			},
		}

		valid, result := validator.ValidateOne(t.Context(), "Person", raw)

		assert.Nil(t, valid, "Should fail: _target_ID != _target_id (case-sensitive)")
		require.False(t, result.OK())
		assert.Contains(t, result.String(), "missing FK field")
	})

	t.Run("wrong_case_fails_with_strict_mode", func(t *testing.T) {
		// StrictPropertyNames = true, FK fields still case-sensitive
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
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false). // optional
		Done())

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

	// Verify error code is E_EDGE_SHAPE_MISMATCH
	issues := result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrEdgeShapeMismatch, issues[0].Code())

	// Verify expected/got details
	details := issues[0].Details()
	var hasExpected, hasGot bool
	for _, d := range details {
		if d.Key == diag.DetailKeyExpected {
			hasExpected = true
			assert.Equal(t, "object", d.Value)
		}
		if d.Key == diag.DetailKeyGot {
			hasGot = true
			assert.Equal(t, "null", d.Value)
		}
	}
	assert.True(t, hasExpected, "should have 'expected' detail")
	assert.True(t, hasGot, "should have 'got' detail")
}

func TestValidateEdges_ExplicitNull_Required(t *testing.T) {
	// Per architecture spec: null is always a shape error.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), false, false). // NOT optional
		Done())

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

	issues := result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrEdgeShapeMismatch, issues[0].Code())
}

func TestValidateEdges_ExplicitNull_Many(t *testing.T) {
	// Per architecture spec: null is always a shape error (expects array).
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Tag").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Item").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("tags", schema.NewTypeRef("", "Tag", location.Span{}), true, true). // optional, many
		Done())

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":   "1",
			"tags": nil, // Explicit null - expects array
		},
	}

	valid, result := validator.ValidateOne(t.Context(), "Item", raw)

	assert.Nil(t, valid)
	require.False(t, result.OK())
	assert.Contains(t, result.String(), "null is not a valid edge value")

	issues := result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrEdgeShapeMismatch, issues[0].Code())

	// Verify expected shape is "array" for many relation
	details := issues[0].Details()
	for _, d := range details {
		if d.Key == diag.DetailKeyExpected {
			assert.Equal(t, "array", d.Value)
		}
	}
}

// --- FK Diagnostic Detail Tests ---

func TestValidateEdges_FKDiagnosticDetails_MissingAll(t *testing.T) {
	// Verify E_MISSING_FK_TARGET includes 'relation' and 'expected' details.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)

	// Find the E_MISSING_FK_TARGET issue
	var fkIssue diag.Issue
	for _, iss := range issues {
		if iss.Code() == instance.ErrMissingFKTarget {
			fkIssue = iss
			break
		}
	}
	require.False(t, fkIssue.IsZero(), "should have E_MISSING_FK_TARGET issue")

	// Verify required details
	details := fkIssue.Details()
	var hasRelation, hasExpected bool
	for _, d := range details {
		if d.Key == diag.DetailKeyRelationName {
			hasRelation = true
			assert.Equal(t, "employer", d.Value)
		}
		if d.Key == diag.DetailKeyExpected {
			hasExpected = true
			assert.Equal(t, "_target_id", d.Value)
		}
	}
	assert.True(t, hasRelation, "should have 'relation' detail")
	assert.True(t, hasExpected, "should have 'expected' detail")
}

func TestValidateEdges_FKDiagnosticDetails_Partial(t *testing.T) {
	// Verify E_PARTIAL_COMPOSITE_FK includes 'relation', 'expected', and 'got' details.
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)

	// Find the E_PARTIAL_COMPOSITE_FK issue
	var fkIssue diag.Issue
	for _, iss := range issues {
		if iss.Code() == instance.ErrPartialCompositeFK {
			fkIssue = iss
			break
		}
	}
	require.False(t, fkIssue.IsZero(), "should have E_PARTIAL_COMPOSITE_FK issue")

	// Verify required details
	details := fkIssue.Details()
	var hasRelation, hasExpected, hasGot bool
	for _, d := range details {
		if d.Key == diag.DetailKeyRelationName {
			hasRelation = true
			assert.Equal(t, "enrollment", d.Value)
		}
		if d.Key == diag.DetailKeyExpected {
			hasExpected = true
			assert.Equal(t, "_target_region, _target_studentId", d.Value)
		}
		if d.Key == diag.DetailKeyGot {
			hasGot = true
			assert.Equal(t, "_target_region", d.Value)
		}
	}
	assert.True(t, hasRelation, "should have 'relation' detail")
	assert.True(t, hasExpected, "should have 'expected' detail")
	assert.True(t, hasGot, "should have 'got' detail")
}

func TestValidateEdges_SingleFK_NullValue(t *testing.T) {
	// Single-PK + _target_id: null
	// Spec: present=1 -> E_TYPE_MISMATCH for the null field (not E_MISSING_FK_TARGET)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("employer", schema.NewTypeRef("", "Company", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	require.Len(t, issues, 1, "should have exactly one issue")
	assert.Equal(t, instance.ErrTypeMismatch, issues[0].Code(),
		"should be E_TYPE_MISMATCH, not E_MISSING_FK_TARGET")
	assert.Contains(t, issues[0].Message(), "null")

	// Verify expected/got details
	details := issues[0].Details()
	var hasExpected, hasGot bool
	for _, d := range details {
		if d.Key == diag.DetailKeyExpected {
			hasExpected = true
			assert.Equal(t, "string", d.Value)
		}
		if d.Key == diag.DetailKeyGot {
			hasGot = true
			assert.Equal(t, "null", d.Value)
		}
	}
	assert.True(t, hasExpected, "should have 'expected' detail")
	assert.True(t, hasGot, "should have 'got' detail")
}

func TestValidateEdges_CompositeFK_OneNullOneValid(t *testing.T) {
	// Composite PK: all present, one null
	// Spec: present==expected -> E_TYPE_MISMATCH for null field only (not E_PARTIAL_COMPOSITE_FK)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	// Should have exactly one E_TYPE_MISMATCH for the null field
	// Should NOT have E_PARTIAL_COMPOSITE_FK (both keys present)
	require.Len(t, issues, 1, "should have exactly one issue")
	assert.Equal(t, instance.ErrTypeMismatch, issues[0].Code(),
		"should be E_TYPE_MISMATCH, not E_PARTIAL_COMPOSITE_FK")
	assert.Contains(t, issues[0].Message(), "_target_region")
}

func TestValidateEdges_CompositeFK_OneNullOneMissing(t *testing.T) {
	// Composite PK: one present (null), one missing
	// Spec: present=1 -> E_PARTIAL_COMPOSITE_FK + E_TYPE_MISMATCH for null
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	// Should have:
	// 1. E_TYPE_MISMATCH for the null field
	// 2. E_PARTIAL_COMPOSITE_FK (present=1, expected=2)
	require.Len(t, issues, 2, "should have two issues")

	var hasPartialFK, hasTypeMismatch bool
	for _, iss := range issues {
		if iss.Code() == instance.ErrPartialCompositeFK {
			hasPartialFK = true
			// Verify 'got' includes _target_region (the present key)
			for _, d := range iss.Details() {
				if d.Key == diag.DetailKeyGot {
					assert.Equal(t, "_target_region", d.Value)
				}
			}
		}
		if iss.Code() == instance.ErrTypeMismatch {
			hasTypeMismatch = true
			assert.Contains(t, iss.Message(), "_target_region")
		}
	}
	assert.True(t, hasPartialFK, "should have E_PARTIAL_COMPOSITE_FK")
	assert.True(t, hasTypeMismatch, "should have E_TYPE_MISMATCH for null")
}

func TestValidateEdges_CompositeFK_OneNullOneInvalidType(t *testing.T) {
	// Composite PK: both present, both invalid (one null, one wrong type)
	// Spec: present==expected -> E_TYPE_MISMATCH for each invalid field (not E_PARTIAL_COMPOSITE_FK)
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	// Should have two E_TYPE_MISMATCH issues, one for each invalid field
	// Should NOT have E_PARTIAL_COMPOSITE_FK (both keys present)
	require.Len(t, issues, 2, "should have two type mismatch issues")

	for _, iss := range issues {
		assert.Equal(t, instance.ErrTypeMismatch, iss.Code(),
			"all issues should be E_TYPE_MISMATCH")
	}
}

func TestValidateEdges_CompositeFK_OneInvalidOneMissing(t *testing.T) {
	// Composite PK: one present (invalid type), one missing
	// Spec: present=1 -> E_PARTIAL_COMPOSITE_FK + E_TYPE_MISMATCH
	// This tests that 'got' includes present keys even if invalid
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		AddType("Enrollment").
		WithPrimaryKey("region", schema.NewStringConstraint()).
		WithPrimaryKey("studentId", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithRelation("enrollment", schema.NewTypeRef("", "Enrollment", location.Span{}), true, false).
		Done())

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

	issues := result.IssuesSlice()
	require.Len(t, issues, 2, "should have two issues")

	var hasPartialFK, hasTypeMismatch bool
	for _, iss := range issues {
		if iss.Code() == instance.ErrPartialCompositeFK {
			hasPartialFK = true
			// Verify 'got' includes _target_region (present even though invalid)
			for _, d := range iss.Details() {
				if d.Key == diag.DetailKeyGot {
					assert.Equal(t, "_target_region", d.Value,
						"'got' should list present keys even if invalid")
				}
			}
		}
		if iss.Code() == instance.ErrTypeMismatch {
			hasTypeMismatch = true
		}
	}
	assert.True(t, hasPartialFK, "should have E_PARTIAL_COMPOSITE_FK")
	assert.True(t, hasTypeMismatch, "should have E_TYPE_MISMATCH")
}

// --- Multiplicity Matrix Test ---
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
			s := mustBuild(t, schema.NewBuilder().
				WithName("test").
				AddType("Item").
				WithPrimaryKey("id", schema.NewStringConstraint()).
				Done().
				AddType("Source").
				WithPrimaryKey("id", schema.NewStringConstraint()).
				WithRelation("items", schema.NewTypeRef("", "Item", location.Span{}), tc.optional, tc.many).
				Done())

			validator := instance.NewValidator(s)

			// Build raw instance
			props := map[string]any{"id": "1"}
			if tc.input != nil {
				props["items"] = tc.input
			}
			// Note: absent means not in props at all, handled by not adding to props

			raw := instance.RawInstance{
				Properties: props,
			}

			valid, result := validator.ValidateOne(t.Context(), "Source", raw)

			if tc.expectSuccess {
				require.True(t, result.OK(), "%s: should succeed but got failure", tc.description)
				require.NotNil(t, valid, "%s: should have valid instance", tc.description)

				edge, ok := valid.Edge("items")
				if tc.expectEdges > 0 {
					require.True(t, ok, "%s: should have edge", tc.description)
					assert.Equal(t, tc.expectEdges, edge.TargetCount(), "%s: edge count", tc.description)
				} else if ok {
					// Zero edges means either no edge data or empty
					assert.Equal(t, 0, edge.TargetCount(), "%s: should have 0 targets", tc.description)
				}
			} else {
				assert.Nil(t, valid, "%s: should fail but got valid", tc.description)
				require.False(t, result.OK(), "%s: should have failure", tc.description)
			}
		})
	}
}
