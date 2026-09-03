package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestRelation_SetTargetID(t *testing.T) {
	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		// Initially zero
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.True(t, r.TargetID().IsZero(), "initially should be zero")

	schema.TestSetRelationTargetID(r, targetID)

	assert.Equal(t, targetID, r.TargetID())
	assert.False(t, r.TargetID().IsZero())
}

func TestRelation_Equal_WithTargetID(t *testing.T) {
	targetID1 := schema.NewTypeID(location.NewSourceID("test://a"), "Target")
	targetID2 := schema.NewTypeID(location.NewSourceID("test://b"), "Target")

	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		targetID1,
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	r2 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		targetID2,
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.False(t, r1.Equal(r2), "relations with different targetID should not be equal")

	schema.TestSetRelationTargetID(r2, targetID1)
	assert.True(t, r1.Equal(r2), "relations with same targetID should be equal")
}

func TestRelation_Equal_ZeroTargetIDs(t *testing.T) {
	// Two relations with zero targetIDs are never equal, however alike their
	// other fields are
	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	r2 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	// An unresolved relation is equal to nothing: the load that left it
	// unresolved already carries that diagnostic.
	assert.False(t, r1.Equal(r2), "relations with zero targetIDs are never equal")
}

func TestRelation_Seal_PreventsSetTargetID(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	// SetTargetID should work before sealing
	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")
	schema.TestSetRelationTargetID(r, targetID)
	assert.Equal(t, targetID, r.TargetID())

	// Seal the relation
	schema.TestSealRelation(r)

	// SetTargetID should panic after sealing
	defer func() {
		if rec := recover(); rec == nil {
			t.Errorf("expected panic on SetTargetID after Seal, but no panic occurred")
		}
	}()

	newTargetID := schema.NewTypeID(location.NewSourceID("test://other"), "Other")
	schema.TestSetRelationTargetID(r, newTargetID)
}

func TestRelation_SetTargetID_WorksBeforeSeal(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")
	schema.TestSetRelationTargetID(r, targetID)
	assert.Equal(t, targetID, r.TargetID())

	// Can set multiple times before sealing
	newTargetID := schema.NewTypeID(location.NewSourceID("test://other"), "Other")
	schema.TestSetRelationTargetID(r, newTargetID)
	assert.Equal(t, newTargetID, r.TargetID())
}

func TestNewRelation(t *testing.T) {
	target := schema.NewTypeRef("users", "Person", location.Span{})
	targetID := schema.NewTypeID(location.MustNewSourceID("test://users"), "Person")
	span := location.Span{
		Source: location.MustNewSourceID("test://schema"),
		Start:  location.Position{Line: 10, Column: 5, Byte: 100},
		End:    location.Position{Line: 10, Column: 50, Byte: 150},
	}
	props := []*schema.Property{
		schema.TestNewProperty("since", location.Span{}, "", schema.NewTimestampConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"works_at",
		target,
		targetID,
		span,
		"Employment relationship",
		true,
		// optional
		false,
		// reverse many
		"Employee",
		props,
	)

	assert.NotNil(t, r)
	assert.Equal(t, schema.RelationAssociation, r.Kind())
	assert.Equal(t, "WORKS_AT", r.Name())
	assert.Equal(t, "works_at", r.FieldName())
	assert.Equal(t, target, r.Target())
	assert.Equal(t, targetID, r.TargetID())
	assert.Equal(t, span, r.Span())
	assert.Equal(t, "Employment relationship", r.Documentation())
	assert.True(t, r.IsOptional())
	assert.False(t, r.IsMany())
	assert.Equal(t, "Employee", r.Owner())
	assert.Len(t, r.PropertiesSlice(), 1)
}

func TestRelationKind_String(t *testing.T) {
	tests := []struct {
		name     string
		kind     schema.RelationKind
		expected string
	}{
		{"association", schema.RelationAssociation, "association"},
		{"composition", schema.RelationComposition, "composition"},
		{"unknown", schema.RelationKind(99), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.kind.String())
		})
	}
}

func TestRelation_Kind(t *testing.T) {
	assoc := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"PART",
		"part",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.Equal(t, schema.RelationAssociation, assoc.Kind())
	assert.Equal(t, schema.RelationComposition, comp.Kind())
}

func TestRelation_Name(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"BELONGS_TO",
		"belongs_to",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.Equal(t, "BELONGS_TO", r.Name())
}

func TestRelation_FieldName(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"works_at",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.Equal(t, "works_at", r.FieldName())
}

func TestRelation_Target(t *testing.T) {
	target := schema.NewTypeRef("users", "Person", location.Span{})

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		target,
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.Equal(t, target, r.Target())
	assert.Equal(t, "users", r.Target().Qualifier())
	assert.Equal(t, "Person", r.Target().Name())
}

func TestRelation_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 15, Column: 3, Byte: 150},
		End:    location.Position{Line: 15, Column: 40, Byte: 187},
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		span,
		"",
		false,
		false,
		"Owner",
		nil,
	)

	result := r.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 15, result.Start.Line)
	assert.Equal(t, 150, result.Start.Byte)
}

func TestRelation_Documentation(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"Describes the relationship",
		false,
		false,
		"Owner",
		nil,
	)

	assert.Equal(t, "Describes the relationship", r.Documentation())
}

func TestRelation_IsOptional(t *testing.T) {
	optional := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		true,
		false,
		"Owner",
		nil,
	)
	required := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.True(t, optional.IsOptional())
	assert.False(t, required.IsOptional())
}

func TestRelation_IsMany(t *testing.T) {
	many := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		true,
		"Owner",
		nil,
	)
	one := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.True(t, many.IsMany())
	assert.False(t, one.IsMany())
}

func TestRelation_Owner(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"OWNS",
		"owns",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Company",
		nil,
	)

	assert.Equal(t, "Company", r.Owner())
}

func TestRelation_Properties_Iterator(t *testing.T) {
	props := []*schema.Property{
		schema.TestNewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
		schema.TestNewProperty("role", location.Span{}, "", nil, schema.DataTypeRef{}, true, false, schema.DeclaringScope{}, nil),
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		props,
	)

	count := 0
	for p := range r.Properties() {
		assert.NotNil(t, p)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestRelation_Properties_Empty(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	count := 0
	for range r.Properties() {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestRelation_PropertiesSlice(t *testing.T) {
	props := []*schema.Property{
		schema.TestNewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		props,
	)

	result := r.PropertiesSlice()
	assert.Len(t, result, 1)
	assert.Equal(t, "since", result[0].Name())
}

func TestRelation_PropertiesSlice_DefensiveCopy(t *testing.T) {
	props := []*schema.Property{
		schema.TestNewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		props,
	)

	slice1 := r.PropertiesSlice()
	slice2 := r.PropertiesSlice()

	// Modifying one slice should not affect the other
	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestRelation_Property_Found(t *testing.T) {
	props := []*schema.Property{
		schema.TestNewProperty("since", location.Span{}, "", schema.NewTimestampConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
		schema.TestNewProperty("role", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, true, false, schema.DeclaringScope{}, nil),
	}

	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		props,
	)

	p, ok := r.Property("since")
	assert.True(t, ok)
	assert.NotNil(t, p)
	assert.Equal(t, "since", p.Name())

	p, ok = r.Property("role")
	assert.True(t, ok)
	assert.Equal(t, "role", p.Name())
}

func TestRelation_Property_NotFound(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	p, ok := r.Property("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, p)
}

func TestRelation_IsAssociation(t *testing.T) {
	assoc := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"PART",
		"part",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.True(t, assoc.IsAssociation())
	assert.False(t, comp.IsAssociation())
}

func TestRelation_IsComposition(t *testing.T) {
	assoc := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	comp := schema.TestNewRelation(
		schema.RelationComposition,
		"PART",
		"part",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.False(t, assoc.IsComposition())
	assert.True(t, comp.IsComposition())
}

func TestRelation_HasProperties(t *testing.T) {
	withProps := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		[]*schema.Property{
			schema.TestNewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
		},
	)
	withoutProps := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.True(t, withProps.HasProperties())
	assert.False(t, withoutProps.HasProperties())
}

func TestRelation_Equal_DifferentName(t *testing.T) {
	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL_A",
		"rel_a",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	r2 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL_B",
		"rel_b",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentKind(t *testing.T) {
	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	r2 := schema.TestNewRelation(
		schema.RelationComposition,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentMultiplicity(t *testing.T) {
	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		true,
		false,
		"Owner",
		nil,
	)
	r2 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		true,
		"Owner",
		nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentPropertyCount(t *testing.T) {
	r1 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		[]*schema.Property{
			schema.TestNewProperty("p1", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}, nil),
		},
	)
	r2 := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_Nil(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)
	var nilRel *schema.Relation

	assert.False(t, r.Equal(nilRel))
	assert.False(t, nilRel.Equal(r))
}

func TestRelation_Equal_BothNil(t *testing.T) {
	var r1 *schema.Relation
	var r2 *schema.Relation

	assert.True(t, r1.Equal(r2))
}

func TestRelation_IsSealed(t *testing.T) {
	r := schema.TestNewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.TypeRef{},
		schema.TypeID{},
		location.Span{},
		"",
		false,
		false,
		"Owner",
		nil,
	)

	// New relation should not be sealed
	assert.False(t, schema.TestIsSealedRelation(r), "new relation should not be sealed")

	// After sealing, IsSealed should return true
	schema.TestSealRelation(r)
	assert.True(t, schema.TestIsSealedRelation(r), "sealed relation should report IsSealed() == true")
}
