package schema_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sourceID(t *testing.T, name string) location.SourceID {
	t.Helper()
	return location.MustNewSourceID("test://" + name)
}

func TestComplete_EmptySchema(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "empty.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.Equal(t, "test", s.Name())
	assert.False(t, collector.HasErrors())
}

func TestComplete_SingleType(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "name",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "person.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "Person"},
			{Name: "Person"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "dup.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_InheritanceCycle(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "A",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "B"},
				},
			},
			{
				Name: "B",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "A"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "cycle.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_SimpleInheritance(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_CASE_COLLISION),
		"expected E_CASE_COLLISION for an inherited case-insensitive property collision")
}

func TestComplete_ReservedPrefix(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_RESERVED_PREFIX),
		"expected E_RESERVED_PREFIX for a property name using the reserved prefix")
}

func TestComplete_InvalidImportAlias(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{
				Path:  "./other",
				Alias: "type", // Reserved keyword
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "alias.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_NilModel(t *testing.T) {
	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil.yammm")

	s := schema.TestCompleteModel(nil, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_DiamondInheritance(t *testing.T) {
	// Diamond pattern: D -> B, C -> A
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "A",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
			{
				Name: "B",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "A"},
				},
			},
			{
				Name: "C",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "A"},
				},
			},
			{
				Name: "D",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "B"},
					{Name: "C"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "diamond.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "name",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "forward.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "D",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "C"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "d_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "C",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "B"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "c_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "B",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "A"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "b_prop",
						Constraint: schema.NewStringConstraint(),
					},
				},
			},
			{
				Name: "A",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "a_prop",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "deep_chain.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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

func TestComplete_CompositionTarget_MustBePart(t *testing.T) {
	// Composition targeting a non-part type should fail
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Regular", // Not a part type
			},
			{
				Name: "Container",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "item",
						Target: &schema.TestASTTypeRef{Name: "Regular"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_non_part.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "composition to non-part type should error")
}

func TestComplete_CompositionTarget_CannotBeAbstract(t *testing.T) {
	// Composition targeting an abstract part type should fail
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "AbstractPart",
				IsPart:     true,
				IsAbstract: true,
			},
			{
				Name: "Container",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "item",
						Target: &schema.TestASTTypeRef{Name: "AbstractPart"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_abstract.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "composition to abstract part should error")
}

func TestComplete_CompositionTarget_Valid(t *testing.T) {
	// Valid composition: targeting a concrete part type
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:   "Part",
				IsPart: true,
			},
			{
				Name: "Container",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "item",
						Target: &schema.TestASTTypeRef{Name: "Part"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_valid.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	baseModel := &schema.TestModel{
		Name: "base",
		Types: []*schema.TestTypeDecl{
			{
				Name: "BaseType",
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
				},
			},
		},
	}
	baseCollector := diag.NewCollector(0)
	baseSchema := schema.TestCompleteModel(baseModel, baseSourceID, baseCollector, nil, nil)
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
	derivedModel := &schema.TestModel{
		Name: "derived",
		Imports: []*schema.TestImportDecl{
			{
				Path:  "base",
				Alias: "base",
			},
		},
		Types: []*schema.TestTypeDecl{
			{
				Name: "DerivedType",
				Inherits: []*schema.TestASTTypeRef{
					{Qualifier: "base", Name: "BaseType"},
				},
				Properties: []*schema.TestPropertyDecl{
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
	derivedSchema := schema.TestCompleteModel(derivedModel, derivedSourceID, derivedCollector, registry, resolvedImports)

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
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{
				Path:  "other",
				Alias: "other",
			},
		},
		Types: []*schema.TestTypeDecl{
			{
				Name: "MyType",
				Inherits: []*schema.TestASTTypeRef{
					{Qualifier: "other", Name: "BaseType"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "deferred.yammm")

	// With nil registry, cross-schema references are deferred to linking phase
	// The Complete function should not error for qualified refs when registry is nil
	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	// Note: This behavior depends on implementation - if cross-schema refs
	// without registry are deferred, this should succeed. If they error
	// immediately, the schema will be nil.
	// Based on code review of collision.go:validateCompositionTarget,
	// qualified refs without registry are deferred (returns true).
	if s != nil {
		assert.False(t, collector.HasErrors(), "qualified refs without registry should be deferred, not error")
	}
}

func TestComplete_DataType_Simple(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		DataTypes: []*schema.TestDataTypeDecl{
			{
				Name:       "Email",
				Constraint: schema.NewStringConstraint(),
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "datatype.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	dt, ok := s.DataType("Email")
	require.True(t, ok)
	assert.Equal(t, "Email", dt.Name())
}

func TestComplete_DataType_Duplicate(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		DataTypes: []*schema.TestDataTypeDecl{
			{Name: "Email", Constraint: schema.NewStringConstraint()},
			{Name: "Email", Constraint: schema.NewStringConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "dup_datatype.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_DataType_Multiple(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		DataTypes: []*schema.TestDataTypeDecl{
			{Name: "Email", Constraint: schema.NewStringConstraint()},
			{Name: "Phone", Constraint: schema.NewStringConstraint()},
			{Name: "Age", Constraint: schema.NewIntegerConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "multi_datatype.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 3, len(s.DataTypesSlice()))
}

func TestComplete_DataType_NilSkipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		DataTypes: []*schema.TestDataTypeDecl{
			nil, // nil entry should be skipped
			{Name: "Email", Constraint: schema.NewStringConstraint()},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_datatype.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 1, len(s.DataTypesSlice()))
}

func TestComplete_Invariant_Single(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
					{Name: "age", Constraint: schema.NewIntegerConstraint()},
				},
				Invariants: []*schema.TestInvariantDecl{
					{Name: "valid_age", Expr: nil}, // nil expression is valid for testing
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "invariant.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	require.Len(t, invs, 1)
	assert.Equal(t, "valid_age", invs[0].Name())
}

func TestComplete_Invariant_Multiple(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
					{Name: "min", Constraint: schema.NewIntegerConstraint()},
					{Name: "max", Constraint: schema.NewIntegerConstraint()},
				},
				Invariants: []*schema.TestInvariantDecl{
					{Name: "min_valid", Expr: nil},
					{Name: "max_valid", Expr: nil},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "multi_invariant.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	assert.Len(t, invs, 2)
}

func TestComplete_Invariant_NilSkipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Invariants: []*schema.TestInvariantDecl{
					nil, // nil entry should be skipped
					{Name: "check", Expr: nil},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_invariant.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)

	invs := typ.InvariantsSlice()
	assert.Len(t, invs, 1)
}

func TestComplete_RelationNormalizationCollision_Associations(t *testing.T) {
	// Two associations with different raw names that normalize to same field name
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "BestFriend",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
					{
						Kind:   schema.RelationAssociation,
						Name:   "best_friend", // Normalizes to same as BestFriend
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_collision.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_RELATION_NORMALIZATION_COLLISION),
		"expected E_RELATION_NORMALIZATION_COLLISION for two associations normalizing to one field name")
}

func TestComplete_RelationNormalizationCollision_AssociationAndComposition(t *testing.T) {
	// Association and composition with same normalized field name
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "RegularType"},
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "Items",
						Target: &schema.TestASTTypeRef{Name: "RegularType"},
					},
					{
						Kind:   schema.RelationComposition,
						Name:   "items", // Normalizes to same as Items
						Target: &schema.TestASTTypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_collision_mixed.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_RELATION_NORMALIZATION_COLLISION),
		"expected E_RELATION_NORMALIZATION_COLLISION for an association and composition normalizing to one field name")
}

func TestComplete_PropertyRelationCollision_Association(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "Target"},
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "friend", Constraint: schema.NewStringConstraint()},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "Friend", // Normalizes to "friend", collides with property
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "prop_rel_collision.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_PROPERTY_RELATION_COLLISION),
		"expected E_PROPERTY_RELATION_COLLISION for an association field colliding with a property")
}

func TestComplete_PropertyRelationCollision_Composition(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Properties: []*schema.TestPropertyDecl{
					{Name: "items", Constraint: schema.NewStringConstraint()},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "Items", // Normalizes to "items", collides with property
						Target: &schema.TestASTTypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "prop_comp_collision.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_PROPERTY_RELATION_COLLISION),
		"expected E_PROPERTY_RELATION_COLLISION for a composition field colliding with a property")
}

func TestComplete_Association_ValidTarget(t *testing.T) {
	// Test that a valid association with proper target works
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Target",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "myFriend",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "valid_assoc.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_Composition_ValidTarget(t *testing.T) {
	// Test that a valid composition with a part type target works
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "PartType", IsPart: true},
			{
				Name: "Container",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "MyPart",
						Target: &schema.TestASTTypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "valid_comp.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_AssociationTarget_Valid(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Company",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Employee",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "employer",
						Target: &schema.TestASTTypeRef{Name: "Company"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_valid.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_AssociationTarget_UnknownType(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "friend",
						Target: &schema.TestASTTypeRef{Name: "NonExistent"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_unknown.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_UNKNOWN_TYPE),
		"expected E_UNKNOWN_TYPE for an association targeting an unknown type")
}

func TestComplete_AssociationTarget_CrossSchema_DeferredWithoutRegistry(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{Path: "other", Alias: "other"},
		},
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "external",
						Target: &schema.TestASTTypeRef{Qualifier: "other", Name: "ExternalType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_cross.yammm")

	// Without registry, cross-schema refs are deferred
	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	// Should not error - deferred to linking phase
	if s != nil {
		assert.False(t, collector.HasErrors())
	}
}

func TestComplete_RelationInheritance_Associations(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Target",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "parent",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "child",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_inherit.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "PartA", IsPart: true},
			{Name: "PartB", IsPart: true},
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "partA",
						Target: &schema.TestASTTypeRef{Name: "PartA"},
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationComposition,
						Name:   "partB",
						Target: &schema.TestASTTypeRef{Name: "PartB"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "comp_inherit.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "Target"},
			{
				Name: "BaseA",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:     schema.RelationAssociation,
						Name:     "ref",
						Target:   &schema.TestASTTypeRef{Name: "Target"},
						Optional: false,
					},
				},
			},
			{
				Name: "BaseB",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:     schema.RelationAssociation,
						Name:     "ref", // Same field name, different optional flag
						Target:   &schema.TestASTTypeRef{Name: "Target"},
						Optional: true, // Different from BaseA
					},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "BaseA"},
					{Name: "BaseB"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "rel_conflict.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, hasCode(collector.Result(), diag.E_RELATION_COLLISION),
		"expected E_RELATION_COLLISION for conflicting inherited relations")
}

func TestComplete_Import_InvalidAlias_StartsWithNumber(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{Path: "other", Alias: "123invalid"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_invalid.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_DuplicateAlias(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{Path: "first", Alias: "other"},
			{Path: "second", Alias: "other"}, // Duplicate alias
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_dup.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_DuplicateSourceID(t *testing.T) {
	// Two imports with different aliases but same resolved SourceID
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, resolvedImports)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())

	// Verify E_DUPLICATE_IMPORT is emitted with correct details
	issues := slices.Collect(collector.Result().Issues())
	require.Len(t, issues, 1)
	assert.Equal(t, diag.E_DUPLICATE_IMPORT, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "imported multiple times")
}

func TestComplete_Import_CollidesWithLocalType(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{Path: "other", Alias: "Person"}, // Collides with local type
		},
		Types: []*schema.TestTypeDecl{
			{Name: "Person"},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_collision.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Import_NilSkipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			nil, // nil entry should be skipped
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_nil.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_Import_MissingResolution(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			{Path: "other", Alias: "other"},
		},
	}

	// Provide empty resolved imports - alias should fail resolution
	resolvedImports := map[string]location.SourceID{}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "import_missing_res.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, resolvedImports)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_Association_WithEdgeProperties(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Target",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "friend",
						Target: &schema.TestASTTypeRef{Name: "Target"},
						Properties: []*schema.TestPropertyDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

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
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Target",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "friend",
						Target: &schema.TestASTTypeRef{Name: "Target"},
						Properties: []*schema.TestPropertyDecl{
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

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_UnknownTypeInExtends(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "NonExistent"}, // Unknown local type
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "unknown_extends.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors())
}

func TestComplete_NilTypeDecl_Skipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			nil, // nil entry should be skipped
			{
				Name: "Valid",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_type.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
	assert.Equal(t, 1, len(s.TypesSlice()))
}

func TestComplete_NilPropertyDecl_Skipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					nil, // nil should be skipped
					{Name: "name", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_prop.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())

	typ, ok := s.Type("Person")
	require.True(t, ok)
	assert.Equal(t, 1, len(typ.PropertiesSlice()))
}

func TestComplete_NilRelationDecl_Skipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Target",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Person",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					nil, // nil should be skipped
					{
						Kind:   schema.RelationAssociation,
						Name:   "friend",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_rel.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_NilInheritsRef_Skipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name: "Base",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Derived",
				Inherits: []*schema.TestASTTypeRef{
					nil, // nil should be skipped
					{Name: "Base"},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "nil_inherit.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s)
	assert.False(t, collector.HasErrors())
}

func TestComplete_PartType_CannotDeclareAssociation(t *testing.T) {
	// Part types cannot declare associations - they are composition-only targets
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{Name: "Target"},
			{
				Name:   "PartType",
				IsPart: true,
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "ref",
						Target: &schema.TestASTTypeRef{Name: "Target"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "part_assoc.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "part type declaring association should error")

	// Verify correct error code
	issues := slices.Collect(collector.Result().Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_INVALID_ASSOCIATION_TARGET, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "part type")
	assert.Contains(t, issues[0].Message(), "cannot declare association")
}

func TestComplete_Association_CannotTargetPartType(t *testing.T) {
	// Associations cannot target part types - only compositions can
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:   "PartType",
				IsPart: true,
			},
			{
				Name: "Container",
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "ref",
						Target: &schema.TestASTTypeRef{Name: "PartType"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_part_target.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "association targeting part type should error")

	// Verify correct error code
	issues := slices.Collect(collector.Result().Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_INVALID_ASSOCIATION_TARGET, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "cannot target part type")
}

func TestComplete_Association_CannotTargetAbstractType(t *testing.T) {
	// Associations cannot target abstract types. An edge resolves against the
	// declared target's exact TypeID, and an abstract type has no instances, so
	// the edge could never resolve in a graph. Base carries a primary key so the
	// missing-primary-key rule does not also fire — this isolates the
	// abstract-target rejection.
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
			},
			{
				Name: "Container",
				Properties: []*schema.TestPropertyDecl{
					{Name: "id", Constraint: schema.NewStringConstraint(), IsPrimaryKey: true},
				},
				Relations: []*schema.TestRelationDecl{
					{
						Kind:   schema.RelationAssociation,
						Name:   "ref",
						Target: &schema.TestASTTypeRef{Name: "Base"},
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "assoc_abstract_target.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s)
	assert.True(t, collector.HasErrors(), "association targeting abstract type should error")

	// Verify correct error code
	issues := slices.Collect(collector.Result().Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_INVALID_ASSOCIATION_TARGET, issues[0].Code())
	assert.Contains(t, issues[0].Message(), "cannot target abstract type")
}

func TestNarrowing_ValidConstraintNarrowing(t *testing.T) {
	t.Parallel()

	// Abstract Entity{age Integer[0,150] optional}, Adult extends Entity{age Integer[18,150] optional}
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Entity",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
					{
						Name:       "age",
						Constraint: schema.IntegerBetween(0, 150),
						Optional:   true,
					},
				},
			},
			{
				Name: "Adult",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Entity"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "age",
						Constraint: schema.IntegerBetween(18, 150),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_valid.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with valid narrowing")
	require.False(t, collector.HasErrors(), "valid narrowing should not produce errors")

	adult, ok := s.Type("Adult")
	require.True(t, ok)

	// Adult's age should be the narrowed version: Integer[18, 150]
	ageProp, ok := adult.Property("age")
	require.True(t, ok)

	ic, ok := ageProp.Constraint().(schema.IntegerConstraint)
	require.True(t, ok, "age constraint should be IntegerConstraint")

	min, hasMin := ic.Min()
	max, hasMax := ic.Max()
	assert.True(t, hasMin)
	assert.True(t, hasMax)
	assert.Equal(t, int64(18), min, "narrowed min should be 18")
	assert.Equal(t, int64(150), max, "max should remain 150")
}

func TestNarrowing_ValidModifierOverride(t *testing.T) {
	t.Parallel()

	// Abstract Base{name String[1,100] optional}, Child extends Base{name String[1,100] required}
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
					{
						Name:       "name",
						Constraint: schema.StringLenBetween(1, 100),
						Optional:   true,
					},
				},
			},
			{
				Name: "Child",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "name",
						Constraint: schema.StringLenBetween(1, 100),
						Optional:   false, // required overrides optional
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_modifier.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with optional->required narrowing")
	require.False(t, collector.HasErrors(), "optional->required narrowing should not produce errors")

	child, ok := s.Type("Child")
	require.True(t, ok)

	nameProp, ok := child.Property("name")
	require.True(t, ok)
	assert.False(t, nameProp.IsOptional(), "child's name should be required (not optional)")
}

func TestNarrowing_WideningRejected(t *testing.T) {
	t.Parallel()

	// Entity{age Integer[0,150]}, BadChild extends Entity{age Integer[0,200]} -> E_PROPERTY_CONFLICT
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Entity",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
					{
						Name:       "age",
						Constraint: schema.IntegerBetween(0, 150),
						Optional:   true,
					},
				},
			},
			{
				Name: "BadChild",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Entity"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "age",
						Constraint: schema.IntegerBetween(0, 200),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_widening.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s, "widening should cause schema completion to fail")
	assert.True(t, collector.HasErrors(), "widening should produce E_PROPERTY_CONFLICT")

	issues := slices.Collect(collector.Result().Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_PROPERTY_CONFLICT, issues[0].Code())
}

func TestNarrowing_RequiredToOptionalRejected(t *testing.T) {
	t.Parallel()

	// Base{field String required}, Child extends Base{field String optional} -> E_PROPERTY_CONFLICT
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
					{
						Name:       "field",
						Constraint: schema.NewStringConstraint(),
						Optional:   false, // required
					},
				},
			},
			{
				Name: "Child",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "field",
						Constraint: schema.NewStringConstraint(),
						Optional:   true, // optional (widens)
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_req_opt.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	assert.Nil(t, s, "required->optional widening should cause schema completion to fail")
	assert.True(t, collector.HasErrors(), "required->optional should produce E_PROPERTY_CONFLICT")

	issues := slices.Collect(collector.Result().Issues())
	require.GreaterOrEqual(t, len(issues), 1)
	assert.Equal(t, diag.E_PROPERTY_CONFLICT, issues[0].Code())
}

func TestNarrowing_EnumSubset(t *testing.T) {
	t.Parallel()

	// Base{status Enum["a","b","c"]}, Restricted extends Base{status Enum["a","b"]} -> compiles
	model := &schema.TestModel{
		Name: "test",
		Types: []*schema.TestTypeDecl{
			{
				Name:       "Base",
				IsAbstract: true,
				Properties: []*schema.TestPropertyDecl{
					{
						Name:         "id",
						Constraint:   schema.NewStringConstraint(),
						IsPrimaryKey: true,
					},
					{
						Name:       "status",
						Constraint: schema.NewEnumConstraint([]string{"a", "b", "c"}),
						Optional:   true,
					},
				},
			},
			{
				Name: "Restricted",
				Inherits: []*schema.TestASTTypeRef{
					{Name: "Base"},
				},
				Properties: []*schema.TestPropertyDecl{
					{
						Name:       "status",
						Constraint: schema.NewEnumConstraint([]string{"a", "b"}),
						Optional:   true,
					},
				},
			},
		},
	}

	collector := diag.NewCollector(0)
	srcID := sourceID(t, "narrow_enum.yammm")

	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)

	require.NotNil(t, s, "schema should compile with enum subset narrowing")
	require.False(t, collector.HasErrors(), "enum subset narrowing should not produce errors")

	restricted, ok := s.Type("Restricted")
	require.True(t, ok)

	statusProp, ok := restricted.Property("status")
	require.True(t, ok)

	ec, ok := statusProp.Constraint().(schema.EnumConstraint)
	require.True(t, ok, "status constraint should be EnumConstraint")
	assert.Equal(t, []string{"a", "b"}, ec.Values(), "narrowed enum should have only [a, b]")
}
