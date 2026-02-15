package instance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// --- Composition Validation Tests ---

func TestValidateCompositions_Single(t *testing.T) {
	// Create a part type
	addressType := makeType("Address", false, true, // isPart = true
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				map[string]any{"id": int64(100), "street": "Main St"},
			},
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)

	composed, ok := valid.Composed("addresses")
	require.True(t, ok)
	require.True(t, !composed.IsNil())
}

func TestValidateCompositions_Multiple(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				map[string]any{"id": int64(100), "street": "Main St"},
				map[string]any{"id": int64(101), "street": "Oak Ave"},
				map[string]any{"id": int64(102), "street": "Pine Rd"},
			},
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)

	composed, ok := valid.Composed("addresses")
	require.True(t, ok)
	require.True(t, !composed.IsNil())
}

func TestValidateCompositions_Optional_Nil(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			// "addresses" is not present - optional composition
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)

	// No composition should be present
	composed, ok := valid.Composed("addresses")
	assert.False(t, ok)
	assert.True(t, composed.IsNil())
}

func TestValidateCompositions_Optional_Empty(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": []any{}, // Empty array - valid for optional
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)

	// Composition should be present but empty
	composed, ok := valid.Composed("addresses")
	require.True(t, ok)
	require.True(t, !composed.IsNil())
}

func TestValidateCompositions_Required_Missing(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRequiredComposition(addressType)

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			// "addresses" missing - required composition
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "missing required composition")
}

func TestValidateCompositions_Required_Empty(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRequiredComposition(addressType)

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": []any{}, // Empty - not valid for required
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "required composition cannot be empty")
}

func TestValidateCompositions_DuplicatePK(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				map[string]any{"id": int64(100)},
				map[string]any{"id": int64(100)}, // Duplicate PK
			},
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "duplicate primary key")
}

func TestValidateCompositions_ChildValidationFails(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false), // Required
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				map[string]any{"id": int64(100)}, // Missing required "street"
			},
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "missing required")
}

func TestValidateCompositions_InvalidChildShape(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				"not an object", // Invalid - should be object
			},
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "composition child must be an object")
}

func TestValidateCompositions_NotArray(t *testing.T) {
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": map[string]any{"id": int64(100)}, // Object instead of array
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "expected array")
}

// --- Test Helper ---

// makeTypeWithRequiredComposition creates a type with a required composition relation.
func makeTypeWithRequiredComposition(partType *schema.Type) *schema.Type {
	const typeName = "Person"
	const relationName = "addresses"
	t := schema.NewType(typeName, location.SourceID{}, location.Span{}, "", false, false)

	idProp := makeProp("id", schema.NewIntegerConstraint(), false, true)
	t.SetProperties([]*schema.Property{idProp})
	t.SetAllProperties([]*schema.Property{idProp})
	t.SetPrimaryKeys([]*schema.Property{idProp})

	// Create composition relation to part type (NOT optional)
	rel := schema.NewRelation(
		schema.RelationComposition,
		relationName,
		relationName, // fieldName
		schema.NewTypeRef("", partType.Name(), location.Span{}),
		partType.ID(),
		location.Span{},
		"",
		false,    // NOT optional
		true,     // many
		"",       // backref
		true,     // reverseOptional
		false,    // reverseMany
		typeName, // owner
		nil,      // no edge properties for composition
	)
	rel.Seal()

	t.SetCompositions([]*schema.Relation{rel})
	t.SetAllCompositions([]*schema.Relation{rel})
	t.Seal()
	return t
}

// --- P0 Null vs Absent Tests ---

func TestValidateCompositions_ExplicitNull_Optional(t *testing.T) {
	// Per architecture spec: null is always a shape error, even for optional compositions.
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses") // optional by default

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": nil, // Explicit null - always a shape error
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "null is not a valid composition value")

	// Verify error code is E_EDGE_SHAPE_MISMATCH
	issues := failure.Result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrEdgeShapeMismatch, issues[0].Code())

	// Verify expected/got details
	details := issues[0].Details()
	var hasExpected, hasGot bool
	for _, d := range details {
		if d.Key == diag.DetailKeyExpected {
			hasExpected = true
			assert.Equal(t, "array", d.Value)
		}
		if d.Key == diag.DetailKeyGot {
			hasGot = true
			assert.Equal(t, "null", d.Value)
		}
	}
	assert.True(t, hasExpected, "should have 'expected' detail")
	assert.True(t, hasGot, "should have 'got' detail")
}

func TestValidateCompositions_ExplicitNull_Required(t *testing.T) {
	// Per architecture spec: null is always a shape error.
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRequiredComposition(addressType) // NOT optional

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": nil, // Explicit null - always a shape error
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)
	assert.Contains(t, failure.Error(), "null is not a valid composition value")

	issues := failure.Result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrEdgeShapeMismatch, issues[0].Code())
}

// --- Composition Reason Detail Tests ---

func TestValidateCompositions_ReasonDetail_Absent(t *testing.T) {
	// Verify E_UNRESOLVED_REQUIRED_COMPOSITION includes reason="absent" for missing field.
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRequiredComposition(addressType)

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			// "addresses" absent
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)

	issues := failure.Result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrUnresolvedRequiredComposition, issues[0].Code())

	// Verify required details including reason="absent"
	details := issues[0].Details()
	var hasReason, hasRelation, hasJsonField bool
	for _, d := range details {
		if d.Key == diag.DetailKeyReason {
			hasReason = true
			assert.Equal(t, "absent", d.Value)
		}
		if d.Key == diag.DetailKeyRelationName {
			hasRelation = true
			assert.Equal(t, "addresses", d.Value)
		}
		if d.Key == diag.DetailKeyJsonField {
			hasJsonField = true
			assert.Equal(t, "addresses", d.Value)
		}
	}
	assert.True(t, hasReason, "should have 'reason' detail")
	assert.True(t, hasRelation, "should have 'relation' detail")
	assert.True(t, hasJsonField, "should have 'json_field' detail")
}

func TestValidateCompositions_ReasonDetail_Empty(t *testing.T) {
	// Verify E_UNRESOLVED_REQUIRED_COMPOSITION includes reason="empty" for empty array.
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRequiredComposition(addressType)

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id":        int64(1),
			"addresses": []any{}, // Empty array
		},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	assert.Nil(t, valid)
	require.NotNil(t, failure)

	issues := failure.Result.IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, instance.ErrUnresolvedRequiredComposition, issues[0].Code())

	// Verify required details including reason="empty"
	details := issues[0].Details()
	var hasReason, hasRelation, hasJsonField bool
	for _, d := range details {
		if d.Key == diag.DetailKeyReason {
			hasReason = true
			assert.Equal(t, "empty", d.Value)
		}
		if d.Key == diag.DetailKeyRelationName {
			hasRelation = true
			assert.Equal(t, "addresses", d.Value)
		}
		if d.Key == diag.DetailKeyJsonField {
			hasJsonField = true
			assert.Equal(t, "addresses", d.Value)
		}
	}
	assert.True(t, hasReason, "should have 'reason' detail")
	assert.True(t, hasRelation, "should have 'relation' detail")
	assert.True(t, hasJsonField, "should have 'json_field' detail")
}

func TestValidateCompositions_DuplicatePK_PathFormat(t *testing.T) {
	// Verify that duplicate PK errors use PK-based path format, not array index.
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
	)
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"addresses": []any{
				map[string]any{"id": int64(100)},
				map[string]any{"id": int64(100)}, // Duplicate PK
			},
		},
	}

	_, failure, err := validator.ValidateOne(context.Background(), "Person", raw)

	require.NoError(t, err)
	require.NotNil(t, failure)

	// Find the duplicate PK error and check its path
	var foundPath string
	for issue := range failure.Result.Issues() {
		if issue.Code() == instance.ErrDuplicateComposedPK {
			foundPath = issue.Path()
			break
		}
	}

	// Path should use PK format: $.addresses[id=100], not $.addresses[1]
	assert.Contains(t, foundPath, "[id=100]", "path should use PK-based format")
	assert.NotContains(t, foundPath, "[1]", "path should not use array index")
}

func TestValidateCompositions_CompositePK_PathFormat(t *testing.T) {
	// Verify that composite PKs are properly formatted in paths.
	enrollmentType := makeType("Enrollment", false, true,
		makeProp("region", schema.NewStringConstraint(), false, true),
		makeProp("studentId", schema.NewIntegerConstraint(), false, true),
	)
	schoolType := makeTypeWithRelation("School", enrollmentType, "enrollments")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{schoolType, enrollmentType})
	s.Seal()

	validator := instance.NewValidator(s)

	raw := instance.RawInstance{
		Properties: map[string]any{
			"id": int64(1),
			"enrollments": []any{
				map[string]any{"region": "us", "studentId": int64(123)},
				map[string]any{"region": "us", "studentId": int64(123)}, // Duplicate
			},
		},
	}

	_, failure, err := validator.ValidateOne(context.Background(), "School", raw)

	require.NoError(t, err)
	require.NotNil(t, failure)

	// Find the duplicate PK error and check its path
	var foundPath string
	for issue := range failure.Result.Issues() {
		if issue.Code() == instance.ErrDuplicateComposedPK {
			foundPath = issue.Path()
			break
		}
	}

	// Path should include composite PK: region="us",studentId=123
	assert.Contains(t, foundPath, `region="us"`, "path should include string PK field")
	assert.Contains(t, foundPath, "studentId=123", "path should include integer PK field")
}

// --- Ownership Isolation Tests ---

// TestOwnership_ValidateForCompositionIsolation verifies that mutating raw input
// after ValidateForComposition() does not affect the returned ValidInstance values.
//
// This tests the streaming path for composition validation, ensuring it has the
// same isolation guarantees as the inline validation path.
func TestOwnership_ValidateForCompositionIsolation(t *testing.T) {
	// Create a part type
	addressType := makeType("Address", false, true, // isPart = true
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create raw data for composition children
	addr1Data := map[string]any{"id": int64(100), "street": "Original Street 1"}
	addr2Data := map[string]any{"id": int64(101), "street": "Original Street 2"}

	raws := []instance.RawInstance{
		{Properties: addr1Data},
		{Properties: addr2Data},
	}

	// Validate using streaming path
	valid, failures, err := validator.ValidateForComposition(
		context.Background(), "Person", "addresses", raws,
	)

	require.NoError(t, err)
	assert.Len(t, failures, 0)
	require.Len(t, valid, 2)

	// Mutate original data AFTER validation
	addr1Data["street"] = "Mutated Street 1"
	addr1Data["id"] = int64(999)
	addr2Data["street"] = "Mutated Street 2"

	// Also try replacing the entire slice
	raws[0] = instance.RawInstance{Properties: map[string]any{"id": int64(888), "street": "Replaced"}}

	// The ValidInstance values should NOT be affected
	street1Val, ok := valid[0].Property("street")
	require.True(t, ok)
	street1, ok := street1Val.String()
	require.True(t, ok)
	assert.Equal(t, "Original Street 1", street1, "ValidateForComposition isolation failed: street1 was mutated")

	street2Val, ok := valid[1].Property("street")
	require.True(t, ok)
	street2, ok := street2Val.String()
	require.True(t, ok)
	assert.Equal(t, "Original Street 2", street2, "ValidateForComposition isolation failed: street2 was mutated")

	// Primary keys should also be isolated
	assert.Equal(t, "[100]", valid[0].PrimaryKey().String(), "ValidateForComposition isolation failed: PK was mutated")
	assert.Equal(t, "[101]", valid[1].PrimaryKey().String(), "ValidateForComposition isolation failed: PK was mutated")
}
