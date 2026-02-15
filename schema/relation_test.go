package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestRelation_SetTargetID(t *testing.T) {
	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")

	r := schema.NewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{}, // Initially zero
		location.Span{},
		"",
		false, false,
		"",
		false, false,
		"Owner",
		nil,
	)

	assert.True(t, r.TargetID().IsZero(), "initially should be zero")

	r.SetTargetID(targetID)

	assert.Equal(t, targetID, r.TargetID())
	assert.False(t, r.TargetID().IsZero())
}

func TestRelation_Equal_WithTargetID(t *testing.T) {
	targetID1 := schema.NewTypeID(location.NewSourceID("test://a"), "Target")
	targetID2 := schema.NewTypeID(location.NewSourceID("test://b"), "Target")

	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.NewTypeRef("", "Target", location.Span{}), targetID1,
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.NewTypeRef("", "Target", location.Span{}), targetID2,
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2), "relations with different targetID should not be equal")

	r2.SetTargetID(targetID1)
	assert.True(t, r1.Equal(r2), "relations with same targetID should be equal")
}

func TestRelation_Equal_ZeroTargetIDs(t *testing.T) {
	// Two relations with zero targetIDs are equal if all other fields match
	// This is the current (incorrect) behavior that CC3 aims to fix
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.NewTypeRef("", "Target", location.Span{}), schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.NewTypeRef("", "Target", location.Span{}), schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	// With zero targetIDs, they appear equal (this is the bug CC3 fixes)
	assert.True(t, r1.Equal(r2), "relations with zero targetIDs are equal")
}

func TestRelation_Seal_PreventsSetTargetID(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"",
		false, false,
		"Owner",
		nil,
	)

	// SetTargetID should work before sealing
	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")
	r.SetTargetID(targetID)
	assert.Equal(t, targetID, r.TargetID())

	// Seal the relation
	r.Seal()

	// SetTargetID should panic after sealing
	defer func() {
		if rec := recover(); rec == nil {
			t.Errorf("expected panic on SetTargetID after Seal, but no panic occurred")
		}
	}()

	newTargetID := schema.NewTypeID(location.NewSourceID("test://other"), "Other")
	r.SetTargetID(newTargetID)
}

func TestRelation_SetTargetID_WorksBeforeSeal(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation,
		"REL",
		"rel",
		schema.NewTypeRef("", "Target", location.Span{}),
		schema.TypeID{},
		location.Span{},
		"",
		false, false,
		"",
		false, false,
		"Owner",
		nil,
	)

	targetID := schema.NewTypeID(location.NewSourceID("test://schema"), "Target")
	r.SetTargetID(targetID)
	assert.Equal(t, targetID, r.TargetID())

	// Can set multiple times before sealing
	newTargetID := schema.NewTypeID(location.NewSourceID("test://other"), "Other")
	r.SetTargetID(newTargetID)
	assert.Equal(t, newTargetID, r.TargetID())
}

// --- Constructor and Accessor Tests ---

func TestNewRelation(t *testing.T) {
	target := schema.NewTypeRef("users", "Person", location.Span{})
	targetID := schema.NewTypeID(location.MustNewSourceID("test://users"), "Person")
	span := location.Span{
		Source: location.MustNewSourceID("test://schema"),
		Start:  location.Position{Line: 10, Column: 5, Byte: 100},
		End:    location.Position{Line: 10, Column: 50, Byte: 150},
	}
	props := []*schema.Property{
		schema.NewProperty("since", location.Span{}, "", schema.NewTimestampConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
	}

	r := schema.NewRelation(
		schema.RelationAssociation,
		"WORKS_AT",
		"works_at",
		target,
		targetID,
		span,
		"Employment relationship",
		true,  // optional
		false, // not many
		"EMPLOYEES",
		false, // reverse optional
		true,  // reverse many
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
	assert.Equal(t, "EMPLOYEES", r.Backref())
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
	assoc := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	comp := schema.NewRelation(
		schema.RelationComposition, "PART", "part",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.Equal(t, schema.RelationAssociation, assoc.Kind())
	assert.Equal(t, schema.RelationComposition, comp.Kind())
}

func TestRelation_Name(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "BELONGS_TO", "belongs_to",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.Equal(t, "BELONGS_TO", r.Name())
}

func TestRelation_FieldName(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "WORKS_AT", "works_at",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.Equal(t, "works_at", r.FieldName())
}

func TestRelation_Target(t *testing.T) {
	target := schema.NewTypeRef("users", "Person", location.Span{})

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		target, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
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

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		span, "", false, false, "", false, false, "Owner", nil,
	)

	result := r.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 15, result.Start.Line)
	assert.Equal(t, 150, result.Start.Byte)
}

func TestRelation_Documentation(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "Describes the relationship", false, false, "", false, false, "Owner", nil,
	)

	assert.Equal(t, "Describes the relationship", r.Documentation())
}

func TestRelation_IsOptional(t *testing.T) {
	optional := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", true, false, "", false, false, "Owner", nil,
	)
	required := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.True(t, optional.IsOptional())
	assert.False(t, required.IsOptional())
}

func TestRelation_IsMany(t *testing.T) {
	many := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, true, "", false, false, "Owner", nil,
	)
	one := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.True(t, many.IsMany())
	assert.False(t, one.IsMany())
}

func TestRelation_Backref(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "MANAGES", "manages",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "MANAGED_BY", false, false, "Manager", nil,
	)

	assert.Equal(t, "MANAGED_BY", r.Backref())
}

func TestRelation_Backref_Empty(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.Equal(t, "", r.Backref())
}

func TestRelation_ReverseMultiplicity(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "REVERSE",
		true, // reverse optional
		true, // reverse many
		"Owner", nil,
	)

	opt, many := r.ReverseMultiplicity()
	assert.True(t, opt)
	assert.True(t, many)
}

func TestRelation_ReverseMultiplicity_OneToOne(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "REVERSE",
		false, // reverse not optional
		false, // reverse not many
		"Owner", nil,
	)

	opt, many := r.ReverseMultiplicity()
	assert.False(t, opt)
	assert.False(t, many)
}

func TestRelation_Owner(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "OWNS", "owns",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Company", nil,
	)

	assert.Equal(t, "Company", r.Owner())
}

func TestRelation_Properties_Iterator(t *testing.T) {
	props := []*schema.Property{
		schema.NewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
		schema.NewProperty("role", location.Span{}, "", nil, schema.DataTypeRef{}, true, false, schema.DeclaringScope{}),
	}

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", props,
	)

	count := 0
	for p := range r.Properties() {
		assert.NotNil(t, p)
		count++
	}
	assert.Equal(t, 2, count)
}

func TestRelation_Properties_Empty(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	count := 0
	for range r.Properties() {
		count++
	}
	assert.Equal(t, 0, count)
}

func TestRelation_PropertiesSlice(t *testing.T) {
	props := []*schema.Property{
		schema.NewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
	}

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", props,
	)

	result := r.PropertiesSlice()
	assert.Len(t, result, 1)
	assert.Equal(t, "since", result[0].Name())
}

func TestRelation_PropertiesSlice_DefensiveCopy(t *testing.T) {
	props := []*schema.Property{
		schema.NewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
	}

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", props,
	)

	slice1 := r.PropertiesSlice()
	slice2 := r.PropertiesSlice()

	// Modifying one slice should not affect the other
	slice1[0] = nil
	assert.NotNil(t, slice2[0])
}

func TestRelation_Property_Found(t *testing.T) {
	props := []*schema.Property{
		schema.NewProperty("since", location.Span{}, "", schema.NewTimestampConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
		schema.NewProperty("role", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, true, false, schema.DeclaringScope{}),
	}

	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", props,
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
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	p, ok := r.Property("nonexistent")
	assert.False(t, ok)
	assert.Nil(t, p)
}

func TestRelation_IsAssociation(t *testing.T) {
	assoc := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	comp := schema.NewRelation(
		schema.RelationComposition, "PART", "part",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.True(t, assoc.IsAssociation())
	assert.False(t, comp.IsAssociation())
}

func TestRelation_IsComposition(t *testing.T) {
	assoc := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	comp := schema.NewRelation(
		schema.RelationComposition, "PART", "part",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.False(t, assoc.IsComposition())
	assert.True(t, comp.IsComposition())
}

func TestRelation_HasProperties(t *testing.T) {
	withProps := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner",
		[]*schema.Property{
			schema.NewProperty("since", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
		},
	)
	withoutProps := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.True(t, withProps.HasProperties())
	assert.False(t, withoutProps.HasProperties())
}

func TestRelation_Equal_DifferentName(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL_A", "rel_a",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL_B", "rel_b",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentKind(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationComposition, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentMultiplicity(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", true, false, "", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, true, "", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentBackref(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "BACK_A", false, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "BACK_B", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentReverseMultiplicity(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "BACK", true, false, "Owner", nil,
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "BACK", false, true, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_DifferentPropertyCount(t *testing.T) {
	r1 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner",
		[]*schema.Property{
			schema.NewProperty("p1", location.Span{}, "", nil, schema.DataTypeRef{}, false, false, schema.DeclaringScope{}),
		},
	)
	r2 := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	assert.False(t, r1.Equal(r2))
}

func TestRelation_Equal_Nil(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)
	var nilRel *schema.Relation = nil

	assert.False(t, r.Equal(nilRel))
	assert.False(t, nilRel.Equal(r))
}

func TestRelation_Equal_BothNil(t *testing.T) {
	var r1 *schema.Relation = nil
	var r2 *schema.Relation = nil

	assert.True(t, r1.Equal(r2))
}

func TestRelation_IsSealed(t *testing.T) {
	r := schema.NewRelation(
		schema.RelationAssociation, "REL", "rel",
		schema.TypeRef{}, schema.TypeID{},
		location.Span{}, "", false, false, "", false, false, "Owner", nil,
	)

	// New relation should not be sealed
	assert.False(t, r.IsSealed(), "new relation should not be sealed")

	// After sealing, IsSealed should return true
	r.Seal()
	assert.True(t, r.IsSealed(), "sealed relation should report IsSealed() == true")
}
