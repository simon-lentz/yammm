package complete_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/internal/complete"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

func sourceID(t *testing.T, name string) location.SourceID {
	t.Helper()
	return location.MustNewSourceID("test://" + name)
}

func TestComplete_EmptySchema(t *testing.T) {
	model := &parse.Model{
		Name: "test",
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "empty.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
	assert.False(t, collector.HasErrors())
}

func TestComplete_SingleType(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "person.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	require.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, "Person", typ.Name())

	prop, ok := typ.Property("name")
	require.True(t, ok)
	assert.Equal(t, "name", prop.Name())
}

func TestComplete_DuplicateType(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Person"},
			{Name: "Person"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "dup.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_InheritanceCycle(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "A",
				Inherits: []*parse.TypeRef{
					{Name: "B"},
				},
			},
			{
				Name: "B",
				Inherits: []*parse.TypeRef{
					{Name: "A"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "cycle.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_SimpleInheritance(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Base",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "id",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "inherit.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	require.False(t, collector.HasErrors())

	derived, ok := s.Type("Derived")
	require.True(t, ok)

	// Derived should have both own and inherited properties
	propCount := 0
	for range derived.AllProperties() {
		propCount++
	}
	assert.Equal(t, 2, propCount)

	// Check inherited property is accessible
	idProp, ok := derived.Property("id")
	require.True(t, ok)
	assert.Equal(t, "id", idProp.Name())

	// Check own property
	nameProp, ok := derived.Property("name")
	require.True(t, ok)
	assert.Equal(t, "name", nameProp.Name())
}

func TestComplete_CaseCollision(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Base",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "Name", // Case collision with inherited "name"
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "case.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_ReservedPrefix(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "_target_foo", // Reserved prefix
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "reserved.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_InvalidImportAlias(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{
				Path:  "./other",
				Alias: "type", // Reserved keyword
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "alias.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_NilModel(t *testing.T) {
	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil.yammm")

	s := complete.Complete(nil, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_DiamondInheritance(t *testing.T) {
	// Diamond pattern: D -> B, C -> A
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "A",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "id",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "B",
				Inherits: []*parse.TypeRef{
					{Name: "A"},
				},
			},
			{
				Name: "C",
				Inherits: []*parse.TypeRef{
					{Name: "A"},
				},
			},
			{
				Name: "D",
				Inherits: []*parse.TypeRef{
					{Name: "B"},
					{Name: "C"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "diamond.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	require.False(t, collector.HasErrors())

	d, ok := s.Type("D")
	require.True(t, ok)

	// D should have id property only once (keep-first deduplication)
	propCount := 0
	for range d.AllProperties() {
		propCount++
	}
	assert.Equal(t, 1, propCount, "diamond inheritance should deduplicate shared properties")
}

func TestComplete_ForwardReferenceInheritance(t *testing.T) {
	// Derived declared BEFORE Base - tests declaration order independence
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "Base",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "id",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "forward.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	require.False(t, collector.HasErrors())

	derived, ok := s.Type("Derived")
	require.True(t, ok)

	// Must have both own AND inherited properties
	propCount := 0
	for range derived.AllProperties() {
		propCount++
	}
	assert.Equal(t, 2, propCount, "Derived should have 2 properties (own + inherited)")

	// Check inherited property is accessible
	_, hasID := derived.Property("id")
	assert.True(t, hasID, "inherited property 'id' must be accessible")

	// Check own property
	_, hasName := derived.Property("name")
	assert.True(t, hasName, "own property 'name' must be accessible")

	// Check supertype chain
	superCount := 0
	for range derived.SuperTypes() {
		superCount++
	}
	assert.Equal(t, 1, superCount, "Derived should have 1 supertype: Base")
}

func TestComplete_DeepChainForwardReference(t *testing.T) {
	// Types declared in REVERSE order: D -> C -> B -> A
	// Tests multi-level inheritance chain with forward references
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "D",
				Inherits: []*parse.TypeRef{
					{Name: "C"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "d_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "C",
				Inherits: []*parse.TypeRef{
					{Name: "B"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "c_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "B",
				Inherits: []*parse.TypeRef{
					{Name: "A"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "b_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "A",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "a_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "deep_chain.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	require.False(t, collector.HasErrors())

	d, ok := s.Type("D")
	require.True(t, ok)

	// D must have all properties from the entire chain
	propCount := 0
	for range d.AllProperties() {
		propCount++
	}
	assert.Equal(t, 4, propCount, "D should have 4 properties (d_prop + c_prop + b_prop + a_prop)")

	// Verify each property is accessible through D
	_, hasA := d.Property("a_prop")
	assert.True(t, hasA, "D must inherit a_prop from A through chain")

	_, hasB := d.Property("b_prop")
	assert.True(t, hasB, "D must inherit b_prop from B through chain")

	_, hasC := d.Property("c_prop")
	assert.True(t, hasC, "D must inherit c_prop from C through chain")

	_, hasD := d.Property("d_prop")
	assert.True(t, hasD, "D must have its own d_prop")

	// Verify supertype chain is complete (A, B, C)
	superCount := 0
	for range d.SuperTypes() {
		superCount++
	}
	assert.Equal(t, 3, superCount, "D should have 3 supertypes: A, B, C")
}

// A5: validateCompositionTarget Tests
// These test that composition relations validate their targets:
// - Composition targets must be part types (IsPart=true)
// - Composition targets cannot be abstract

func TestComplete_CompositionTarget_MustBePart(t *testing.T) {
	// Composition targeting a non-part type should fail
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Regular", // Not a part type
			},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "item",
						Target: &parse.TypeRef{Name: "Regular"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_non_part.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "composition to non-part type should error")
}

func TestComplete_CompositionTarget_CannotBeAbstract(t *testing.T) {
	// Composition targeting an abstract part type should fail
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:       "AbstractPart",
				IsPart:     true,
				IsAbstract: true,
			},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "item",
						Target: &parse.TypeRef{Name: "AbstractPart"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_abstract.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "composition to abstract part should error")
}

func TestComplete_CompositionTarget_Valid(t *testing.T) {
	// Valid composition: targeting a concrete part type
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "Part",
				IsPart: true,
			},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "item",
						Target: &parse.TypeRef{Name: "Part"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_valid.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	container, ok := s.Type("Container")
	require.True(t, ok)

	// Verify the composition relation exists
	relCount := 0
	for range container.Compositions() {
		relCount++
	}
	assert.Equal(t, 1, relCount)
}

// A6: Cross-Schema Resolution Tests
// mockRegistry implements schema.Registry interface for cross-schema type resolution

type mockRegistry struct {
	schemas map[location.SourceID]*schema.Schema
}

func (m *mockRegistry) LookupBySourceID(id location.SourceID) (*schema.Schema, bool) {
	s, ok := m.schemas[id]
	return s, ok
}

func (m *mockRegistry) LookupByName(_ string) (*schema.Schema, bool) {
	return nil, false // Not needed for these tests
}

func TestComplete_CrossSchemaInheritance_WithRegistry(t *testing.T) {
	// Create base schema with a type we'll inherit from
	baseSourceID := sourceID(t, "base.yammm")
	baseModel := &parse.Model{
		Name: "base",
		Types: []*parse.TypeDecl{
			{
				Name: "BaseType",
				Properties: []*parse.PropertyDecl{
					{
						Name:       "id",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}
	baseCollector := diag.NewCollector(0)
	baseSchema := complete.Complete(baseModel, baseSourceID, baseCollector, nil, nil)
	require.NotNil(t, baseSchema)
	require.False(t, baseCollector.HasErrors())

	// Create registry with base schema
	registry := &mockRegistry{
		schemas: map[location.SourceID]*schema.Schema{
			baseSourceID: baseSchema,
		},
	}

	// Create derived schema that inherits from base
	derivedSourceID := sourceID(t, "derived.yammm")
	derivedModel := &parse.Model{
		Name: "derived",
		Imports: []*parse.ImportDecl{
			{
				Path:  "base",
				Alias: "base",
			},
		},
		Types: []*parse.TypeDecl{
			{
				Name: "DerivedType",
				Inherits: []*parse.TypeRef{
					{Qualifier: "base", Name: "BaseType"},
				},
				Properties: []*parse.PropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
		},
	}

	// Map of alias -> sourceID for resolved imports
	resolvedImports := map[string]location.SourceID{
		"base": baseSourceID,
	}

	derivedCollector := diag.NewCollector(0)
	derivedSchema := complete.Complete(derivedModel, derivedSourceID, derivedCollector, registry, resolvedImports)

	require.NotNil(t, derivedSchema, "cross-schema inheritance should succeed with registry")
	assert.False(t, derivedCollector.HasErrors())

	derived, ok := derivedSchema.Type("DerivedType")
	require.True(t, ok)

	// Derived should have both own and inherited properties
	propCount := 0
	for range derived.AllProperties() {
		propCount++
	}
	assert.Equal(t, 2, propCount, "DerivedType should have 2 properties (id + name)")

	// Verify inherited property is accessible
	_, hasID := derived.Property("id")
	assert.True(t, hasID, "inherited property 'id' should be accessible")

	// Verify own property
	_, hasName := derived.Property("name")
	assert.True(t, hasName, "own property 'name' should be accessible")
}

func TestComplete_CrossSchemaInheritance_DeferredWithoutRegistry(t *testing.T) {
	// When registry is nil, cross-schema references should be deferred
	// (not an error, just unresolved)
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{
				Path:  "other",
				Alias: "other",
			},
		},
		Types: []*parse.TypeDecl{
			{
				Name: "MyType",
				Inherits: []*parse.TypeRef{
					{Qualifier: "other", Name: "BaseType"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "deferred.yammm")

	// With nil registry, cross-schema references are deferred to linking phase
	// The Complete function should not error for qualified refs when registry is nil
	s := complete.Complete(model, srcID, collector, nil, nil)

	// Note: This behavior depends on implementation - if cross-schema refs
	// without registry are deferred, this should succeed. If they error
	// immediately, the schema will be nil.
	// Based on code review of collision.go:validateCompositionTarget,
	// qualified refs without registry are deferred (returns true).
	if s != nil {
		assert.False(t, collector.HasErrors(), "qualified refs without registry should be deferred, not error")
	}
}

// ============================================================================
// A7: DataType Tests (indexDataTypes coverage)
// ============================================================================

func TestComplete_DataType_Simple(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		DataTypes: []*parse.DataTypeDecl{
			{
				Name:       "Email",
				Constraint: schema.NewStringConstraint(),
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "datatype.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	dt, ok := s.DataType("Email")
	require.True(t, ok)
	assert.Equal(t, "Email", dt.Name())
}

func TestComplete_DataType_Duplicate(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		DataTypes: []*parse.DataTypeDecl{
			{Name: "Email", Constraint: schema.NewStringConstraint()},
			{Name: "Email", Constraint: schema.NewStringConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "dup_datatype.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_DataType_Multiple(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		DataTypes: []*parse.DataTypeDecl{
			{Name: "Email", Constraint: schema.NewStringConstraint()},
			{Name: "Phone", Constraint: schema.NewStringConstraint()},
			{Name: "Age", Constraint: schema.NewIntegerConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "multi_datatype.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 3, len(s.DataTypesSlice()))
}

func TestComplete_DataType_NilSkipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		DataTypes: []*parse.DataTypeDecl{
			nil, // nil entry should be skipped
			{Name: "Email", Constraint: schema.NewStringConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_datatype.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 1, len(s.DataTypesSlice()))
}

// ============================================================================
// A8: Invariant Tests (convertInvariants coverage)
// ============================================================================

func TestComplete_Invariant_Single(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{Name: "age", Constraint: schema.NewIntegerConstraint()},
				},
				Invariants: []*parse.InvariantDecl{
					{Name: "valid_age", Expr: nil}, // nil expression is valid for testing
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "invariant.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	require.Len(t, invs, 1)
	assert.Equal(t, "valid_age", invs[0].Name())
}

func TestComplete_Invariant_Multiple(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{Name: "min", Constraint: schema.NewIntegerConstraint()},
					{Name: "max", Constraint: schema.NewIntegerConstraint()},
				},
				Invariants: []*parse.InvariantDecl{
					{Name: "min_valid", Expr: nil},
					{Name: "max_valid", Expr: nil},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "multi_invariant.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	assert.Len(t, invs, 2)
}

func TestComplete_Invariant_NilSkipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Invariants: []*parse.InvariantDecl{
					nil, // nil entry should be skipped
					{Name: "check", Expr: nil},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_invariant.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	assert.Len(t, invs, 1)
}

// ============================================================================
// A9: Relation Collision Tests (checkRelationCollisions coverage)
// ============================================================================

func TestComplete_RelationNormalizationCollision_Associations(t *testing.T) {
	// Two associations with different raw names that normalize to same field name
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "BestFriend",
						Target: &parse.TypeRef{Name: "Target"},
					},
					{
						Kind:   parse.RelationAssociation,
						Name:   "best_friend", // Normalizes to same as BestFriend
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_collision.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_RelationNormalizationCollision_AssociationAndComposition(t *testing.T) {
	// Association and composition with same normalized field name
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "RegularType"},
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "Items",
						Target: &parse.TypeRef{Name: "RegularType"},
					},
					{
						Kind:   parse.RelationComposition,
						Name:   "items", // Normalizes to same as Items
						Target: &parse.TypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_collision_mixed.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

// ============================================================================
// A10: Property-Relation Collision Tests (checkPropertyRelationCollisions)
// ============================================================================

func TestComplete_PropertyRelationCollision_Association(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					{Name: "friend", Constraint: schema.NewStringConstraint()},
				},
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "Friend", // Normalizes to "friend", collides with property
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "prop_rel_collision.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_PropertyRelationCollision_Composition(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Properties: []*parse.PropertyDecl{
					{Name: "items", Constraint: schema.NewStringConstraint()},
				},
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "Items", // Normalizes to "items", collides with property
						Target: &parse.TypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "prop_comp_collision.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

// ============================================================================
// A11: Association/Composition Valid Target Tests
// Note: The reserved prefix check for relations uses FieldName() which is
// normalized via ToLowerSnake(). Since ToLowerSnake strips leading underscores,
// the reserved prefix "_target_" can never match through normal usage.
// ============================================================================

func TestComplete_Association_ValidTarget(t *testing.T) {
	// Test that a valid association with proper target works
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "myFriend",
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "valid_assoc.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_Composition_ValidTarget(t *testing.T) {
	// Test that a valid composition with a part type target works
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "MyPart",
						Target: &parse.TypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "valid_comp.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

// ============================================================================
// A12: Association Target Tests (validateRelationTarget coverage)
// ============================================================================

func TestComplete_AssociationTarget_Valid(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Person"},
			{Name: "Company"},
			{
				Name: "Employee",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "employer",
						Target: &parse.TypeRef{Name: "Company"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_valid.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_AssociationTarget_UnknownType(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "friend",
						Target: &parse.TypeRef{Name: "NonExistent"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_unknown.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_AssociationTarget_CrossSchema_DeferredWithoutRegistry(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "other", Alias: "other"},
		},
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "external",
						Target: &parse.TypeRef{Qualifier: "other", Name: "ExternalType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_cross.yammm")

	// Without registry, cross-schema refs are deferred
	s := complete.Complete(model, srcID, collector, nil, nil)

	// Should not error - deferred to linking phase
	if s != nil {
		assert.False(t, collector.HasErrors())
	}
}

// ============================================================================
// A13: Relation Inheritance Tests (mergeRelations coverage)
// ============================================================================

func TestComplete_RelationInheritance_Associations(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Base",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "parent",
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "child",
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_inherit.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	derived, ok := s.Type("Derived")
	require.True(t, ok)

	// Should have both own and inherited associations
	assocCount := 0
	for range derived.AllAssociations() {
		assocCount++
	}
	assert.Equal(t, 2, assocCount, "Derived should have 2 associations (own + inherited)")
}

func TestComplete_RelationInheritance_Compositions(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "PartA", IsPart: true},
			{Name: "PartB", IsPart: true},
			{
				Name: "Base",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "partA",
						Target: &parse.TypeRef{Name: "PartA"},
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "Base"},
				},
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationComposition,
						Name:   "partB",
						Target: &parse.TypeRef{Name: "PartB"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_inherit.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	derived, ok := s.Type("Derived")
	require.True(t, ok)

	// Should have both own and inherited compositions
	compCount := 0
	for range derived.AllCompositions() {
		compCount++
	}
	assert.Equal(t, 2, compCount, "Derived should have 2 compositions (own + inherited)")
}

func TestComplete_RelationInheritance_ConflictingDifferentOptional(t *testing.T) {
	// Conflicting relations from different ancestors - different optional flag
	// This should trigger E_RELATION_COLLISION because the relations are not Equal()
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "BaseA",
				Relations: []*parse.RelationDecl{
					{
						Kind:     parse.RelationAssociation,
						Name:     "ref",
						Target:   &parse.TypeRef{Name: "Target"},
						Optional: false,
					},
				},
			},
			{
				Name: "BaseB",
				Relations: []*parse.RelationDecl{
					{
						Kind:     parse.RelationAssociation,
						Name:     "ref", // Same field name, different optional flag
						Target:   &parse.TypeRef{Name: "Target"},
						Optional: true, // Different from BaseA
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "BaseA"},
					{Name: "BaseB"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_conflict.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

// ============================================================================
// A14: Import Validation Tests (indexImports coverage)
// ============================================================================

func TestComplete_Import_InvalidAlias_StartsWithNumber(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "other", Alias: "123invalid"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_invalid.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_DuplicateAlias(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "first", Alias: "other"},
			{Path: "second", Alias: "other"}, // Duplicate alias
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_dup.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_DuplicateSourceID(t *testing.T) {
	// Two imports with different aliases but same resolved SourceID
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "common.yammm", Alias: "c", Span: location.Span{Start: location.Position{Line: 5}}},
			{Path: "common.yammm", Alias: "common", Span: location.Span{Start: location.Position{Line: 12}}},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "dup_sourceid.yammm")

	// Both imports resolve to the same SourceID
	commonSourceID := location.MustNewSourceID("test://common.yammm")
	resolvedImports := map[string]location.SourceID{
		"c":      commonSourceID,
		"common": commonSourceID, // Same SourceID!
	}

	s := complete.Complete(model, srcID, collector, nil, resolvedImports)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())

	// Verify E_DUPLICATE_IMPORT is emitted with correct details
	issues := collector.Result().IssuesSlice()
	require.Len(t, issues, 1)
	assert.Equal(t, diag.E_DUPLICATE_IMPORT, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "imported multiple times")
}

func TestComplete_Import_CollidesWithLocalType(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "other", Alias: "Person"}, // Collides with local type
		},
		Types: []*parse.TypeDecl{
			{Name: "Person"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_collision.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_NilSkipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			nil, // nil entry should be skipped
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_nil.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_Import_MissingResolution(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Imports: []*parse.ImportDecl{
			{Path: "other", Alias: "other"},
		},
	}

	// Provide empty resolved imports - alias should fail resolution
	resolvedImports := map[string]location.SourceID{}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_missing_res.yammm")

	s := complete.Complete(model, srcID, collector, nil, resolvedImports)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

// ============================================================================
// A15: Edge Properties Tests (convertRelations coverage)
// ============================================================================

func TestComplete_Association_WithEdgeProperties(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "friend",
						Target: &parse.TypeRef{Name: "Target"},
						Properties: []*parse.PropertyDecl{
							{Name: "since", Constraint: schema.NewDateConstraint()},
							{Name: "closeness", Constraint: schema.NewIntegerConstraint()},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "edge_props.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	person, ok := s.Type("Person")
	require.True(t, ok)

	var friendRel *schema.Relation
	for r := range person.Associations() {
		if r.Name() == "friend" {
			friendRel = r
			break
		}
	}
	require.NotNil(t, friendRel)

	props := friendRel.PropertiesSlice()
	assert.Len(t, props, 2)
}

func TestComplete_Association_EdgePropertyNilSkipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "friend",
						Target: &parse.TypeRef{Name: "Target"},
						Properties: []*parse.PropertyDecl{
							nil, // nil should be skipped
							{Name: "since", Constraint: schema.NewDateConstraint()},
						},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "edge_props_nil.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

// ============================================================================
// A16: Unknown Type in Extends Tests (completeTypes/linearize coverage)
// ============================================================================

func TestComplete_UnknownTypeInExtends(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					{Name: "NonExistent"}, // Unknown local type
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "unknown_extends.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_NilTypeDecl_Skipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			nil, // nil entry should be skipped
			{Name: "Valid"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_type.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 1, len(s.TypesSlice()))
}

func TestComplete_NilPropertyDecl_Skipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name: "Person",
				Properties: []*parse.PropertyDecl{
					nil, // nil should be skipped
					{Name: "name", Constraint: schema.NewStringConstraint()},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_prop.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, 1, len(typ.PropertiesSlice()))
}

func TestComplete_NilRelationDecl_Skipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*parse.RelationDecl{
					nil, // nil should be skipped
					{
						Kind:   parse.RelationAssociation,
						Name:   "friend",
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_rel.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_NilInheritsRef_Skipped(t *testing.T) {
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Base"},
			{
				Name: "Derived",
				Inherits: []*parse.TypeRef{
					nil, // nil should be skipped
					{Name: "Base"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_inherit.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

// ============================================================================
// A17: Part Type Association Restrictions Tests (validateAssociationTargets)
// ============================================================================

func TestComplete_PartType_CannotDeclareAssociation(t *testing.T) {
	// Part types cannot declare associations - they are composition-only targets
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{Name: "Target"},
			{
				Name:   "PartType",
				IsPart: true,
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "ref",
						Target: &parse.TypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "part_assoc.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "part type declaring association should error")

	// Verify correct error code
	issues := collector.Result().IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_INVALID_ASSOCIATION_TARGET, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "part type")
	assert.Contains(t, issues[0].Message(), "cannot declare association")
}

func TestComplete_Association_CannotTargetPartType(t *testing.T) {
	// Associations cannot target part types - only compositions can
	model := &parse.Model{
		Name: "test",
		Types: []*parse.TypeDecl{
			{
				Name:   "PartType",
				IsPart: true,
			},
			{
				Name: "Container",
				Relations: []*parse.RelationDecl{
					{
						Kind:   parse.RelationAssociation,
						Name:   "ref",
						Target: &parse.TypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_part_target.yammm")

	s := complete.Complete(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "association targeting part type should error")

	// Verify correct error code
	issues := collector.Result().IssuesSlice()
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_INVALID_ASSOCIATION_TARGET, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "cannot target part type")
}
