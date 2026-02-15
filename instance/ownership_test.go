package instance_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Ownership Isolation Tests
//
// These tests verify that ValidInstance outputs are isolated from mutations
// to the original RawInstance data:
//
// "After Validate() returns, callers MAY mutate their original RawInstance
// values without affecting any ValidInstance outputs."

// TestOwnership_NestedMapIsolation verifies that mutating deeply nested maps
// in the original data does not affect the ValidInstance properties.
func TestOwnership_NestedMapIsolation(t *testing.T) {
	// Create a part type for composition (compositions have nested map structure)
	addressType := makeType("Address", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("street", schema.NewStringConstraint(), false, false),
		makeProp("city", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	personType := makeTypeWithRelation("Person", addressType, "addresses")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, addressType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create nested structure - the address data is a nested map
	addressData := map[string]any{
		"id":     int64(100),
		"street": "Original Street",
		"city":   "Original City",
	}
	rawData := map[string]any{
		"id":        int64(1),
		"addresses": []any{addressData},
	}
	raw := instance.RawInstance{Properties: rawData}

	// Validate
	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Mutate the nested address data AFTER validation
	addressData["street"] = "Mutated Street"
	addressData["city"] = "Mutated City"

	// The ValidInstance's composed children should NOT be affected
	composed, ok := valid.Composed("addresses")
	require.True(t, ok)
	require.False(t, composed.IsNil())

	composedSlice, ok := composed.Slice()
	require.True(t, ok)
	require.Equal(t, 1, composedSlice.Len())

	child := composedSlice.Get(0).Unwrap().(*instance.ValidInstance)
	streetVal, ok := child.Property("street")
	require.True(t, ok)
	street, ok := streetVal.String()
	require.True(t, ok)
	assert.Equal(t, "Original Street", street, "nested map isolation failed: street was mutated")

	cityVal, ok := child.Property("city")
	require.True(t, ok)
	city, ok := cityVal.String()
	require.True(t, ok)
	assert.Equal(t, "Original City", city, "nested map isolation failed: city was mutated")
}

// TestOwnership_NestedSliceIsolation verifies that mutating nested slices
// (like adding/removing elements or modifying slice elements) in the original
// data does not affect the ValidInstance.
func TestOwnership_NestedSliceIsolation(t *testing.T) {
	// Create a part type
	noteType := makeType("Note", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("text", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	documentType := makeTypeWithRelation("Document", noteType, "notes")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{documentType, noteType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create data with slice of notes
	note1 := map[string]any{"id": int64(1), "text": "Original Note 1"}
	note2 := map[string]any{"id": int64(2), "text": "Original Note 2"}
	notesSlice := []any{note1, note2}
	rawData := map[string]any{
		"id":    int64(1),
		"notes": notesSlice,
	}
	raw := instance.RawInstance{Properties: rawData}

	// Validate
	valid, failure, err := validator.ValidateOne(context.Background(), "Document", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Mutate the slice: modify elements
	note1["text"] = "Mutated Note 1"
	note2["text"] = "Mutated Note 2"

	// Also try to mutate the slice structure (won't affect wrapped, but for completeness)
	rawData["notes"] = []any{map[string]any{"id": int64(999), "text": "New Note"}}

	// The ValidInstance should NOT be affected
	composed, ok := valid.Composed("notes")
	require.True(t, ok)
	require.False(t, composed.IsNil())

	composedSlice, ok := composed.Slice()
	require.True(t, ok)
	require.Equal(t, 2, composedSlice.Len(), "original slice length should be preserved")

	child0 := composedSlice.Get(0).Unwrap().(*instance.ValidInstance)
	text1Val, ok := child0.Property("text")
	require.True(t, ok)
	text1, ok := text1Val.String()
	require.True(t, ok)
	assert.Equal(t, "Original Note 1", text1, "nested slice isolation failed: note 1 text was mutated")

	child1 := composedSlice.Get(1).Unwrap().(*instance.ValidInstance)
	text2Val, ok := child1.Property("text")
	require.True(t, ok)
	text2, ok := text2Val.String()
	require.True(t, ok)
	assert.Equal(t, "Original Note 2", text2, "nested slice isolation failed: note 2 text was mutated")
}

// TestOwnership_CompositionIsolation verifies that mutating composition data
// in RawInstance does not affect recursively validated composed children.
func TestOwnership_CompositionIsolation(t *testing.T) {
	// Create a part type
	itemType := makeType("Item", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("name", schema.NewStringConstraint(), false, false),
	)

	// Create parent type with composition
	orderType := makeTypeWithRelation("Order", itemType, "items")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{orderType, itemType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create composition data
	itemData := map[string]any{"id": int64(100), "name": "Original Item"}
	compositionArray := []any{itemData}
	rawData := map[string]any{
		"id":    int64(1),
		"items": compositionArray,
	}
	raw := instance.RawInstance{Properties: rawData}

	// Validate
	valid, failure, err := validator.ValidateOne(context.Background(), "Order", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Mutate the composition data AFTER validation
	itemData["name"] = "Mutated Item"
	compositionArray[0] = map[string]any{"id": int64(999), "name": "Replaced Item"}
	rawData["items"] = nil // Try to null out the composition

	// The ValidInstance's composed children should NOT be affected
	composed, ok := valid.Composed("items")
	require.True(t, ok)
	require.False(t, composed.IsNil())

	composedSlice, ok := composed.Slice()
	require.True(t, ok)
	require.Equal(t, 1, composedSlice.Len())

	child := composedSlice.Get(0).Unwrap().(*instance.ValidInstance)
	nameVal, ok := child.Property("name")
	require.True(t, ok)
	name, ok := nameVal.String()
	require.True(t, ok)
	assert.Equal(t, "Original Item", name, "composition isolation failed")
}

// TestOwnership_DeeplyNestedCompositionIsolation verifies isolation with
// multiple levels of nesting (parent → child → child's nested properties).
func TestOwnership_DeeplyNestedCompositionIsolation(t *testing.T) {
	// Create a part type with multiple properties
	detailType := makeType("Detail", false, true,
		makeProp("id", schema.NewIntegerConstraint(), false, true),
		makeProp("value", schema.NewStringConstraint(), false, false),
		makeProp("count", schema.NewIntegerConstraint(), false, false),
	)

	// Create parent type
	containerType := makeTypeWithRelation("Container", detailType, "details")

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{containerType, detailType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create deeply nested structure
	detailData := map[string]any{
		"id":    int64(1),
		"value": "Original Value",
		"count": int64(42),
	}
	rawData := map[string]any{
		"id":      int64(1),
		"details": []any{detailData},
	}
	raw := instance.RawInstance{Properties: rawData}

	// Validate
	valid, failure, err := validator.ValidateOne(context.Background(), "Container", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Mutate all nested data
	detailData["value"] = "Mutated Value"
	detailData["count"] = int64(999)
	detailData["id"] = int64(888)

	// The ValidInstance should NOT be affected
	composed, ok := valid.Composed("details")
	require.True(t, ok)

	composedSlice, ok := composed.Slice()
	require.True(t, ok)
	require.Equal(t, 1, composedSlice.Len())

	child := composedSlice.Get(0).Unwrap().(*instance.ValidInstance)

	// Check all properties
	valueVal, ok := child.Property("value")
	require.True(t, ok)
	value, ok := valueVal.String()
	require.True(t, ok)
	assert.Equal(t, "Original Value", value, "deeply nested isolation failed: value was mutated")

	countVal, ok := child.Property("count")
	require.True(t, ok)
	count, ok := countVal.Int()
	require.True(t, ok)
	assert.Equal(t, int64(42), count, "deeply nested isolation failed: count was mutated")

	// PK should also be isolated
	assert.Equal(t, "[1]", child.PrimaryKey().String(), "deeply nested isolation failed: PK was mutated")
}

// TestOwnership_EdgePropertyIsolation verifies that mutating edge properties
// after validation does not affect ValidInstance edge data.
func TestOwnership_EdgePropertyIsolation(t *testing.T) {
	targetType := makeAssociationTarget("Company")

	// Create edge with properties
	edgeProps := []*schema.Property{
		makeProp("role", schema.NewStringConstraint(), false, false),
		makeProp("startDate", schema.NewStringConstraint(), true, false),
	}
	personType := makeTypeWithAssociation("Person", targetType, "employer", true, false, edgeProps)

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType, targetType})
	s.Seal()

	validator := instance.NewValidator(s)

	// Create edge with properties
	edgeData := map[string]any{
		"_target_id": int64(42),
		"role":       "Original Role",
		"startDate":  "2024-01-01",
	}
	rawData := map[string]any{
		"id":       int64(1),
		"employer": edgeData,
	}
	raw := instance.RawInstance{Properties: rawData}

	// Validate
	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	require.Nil(t, failure)
	require.NotNil(t, valid)

	// Mutate edge properties AFTER validation
	edgeData["role"] = "Mutated Role"
	edgeData["startDate"] = "2099-12-31"
	edgeData["_target_id"] = int64(999)

	// The ValidInstance's edge should NOT be affected
	edge, ok := valid.Edge("employer")
	require.True(t, ok)
	require.NotNil(t, edge)

	targets := edge.Targets()
	require.Len(t, targets, 1)

	// Check edge properties
	roleVal, ok := targets[0].Property("role")
	require.True(t, ok)
	role, ok := roleVal.String()
	require.True(t, ok)
	assert.Equal(t, "Original Role", role, "edge property isolation failed: role was mutated")

	startDateVal, ok := targets[0].Property("startDate")
	require.True(t, ok)
	startDate, ok := startDateVal.String()
	require.True(t, ok)
	assert.Equal(t, "2024-01-01", startDate, "edge property isolation failed: startDate was mutated")

	// Target key should also be isolated
	assert.Equal(t, "[42]", targets[0].TargetKey().String(), "edge property isolation failed: target key was mutated")
}
