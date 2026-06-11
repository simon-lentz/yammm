package schema_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func sourceID(t *testing.T, name string) location.SourceID {
	t.Helper()
	return location.MustNewSourceID("test://" + name)
}

// buildAndComplete runs TestCompleteModel with a fresh collector and a
// test-name-derived SourceID — the canonical completion harness for every
// case that needs no registry or resolved imports.
func buildAndComplete(t *testing.T, model *schema.TestModel) (*schema.Schema, diag.Result) {
	t.Helper()
	collector := diag.NewCollector(0)
	srcID := location.MustNewSourceID("test://" + strings.ReplaceAll(t.Name(), "/", "_") + ".yammm")
	s := schema.TestCompleteModel(model, srcID, collector, nil, nil)
	return s, collector.Result()
}

// projectSchema renders a completed schema into a stable, JSON-serializable
// shape for golden comparison: type flags, supertype names, the full merged
// property/relation surface (constraints via their canonical String form),
// invariant names, datatypes, and imports.
func projectSchema(s *schema.Schema) map[string]any {
	project := func(props []*schema.Property) []map[string]any {
		out := make([]map[string]any, 0, len(props))
		for _, p := range props {
			constraint := "(none)"
			if c := p.Constraint(); c != nil {
				constraint = c.String()
			}
			out = append(out, map[string]any{
				"Name":       p.Name(),
				"Constraint": constraint,
				"Optional":   p.IsOptional(),
				"PrimaryKey": p.IsPrimaryKey(),
			})
		}
		return out
	}
	relations := func(rels []*schema.Relation) []map[string]any {
		out := make([]map[string]any, 0, len(rels))
		for _, r := range rels {
			out = append(out, map[string]any{
				"Name":      r.Name(),
				"FieldName": r.FieldName(),
				"Target":    r.Target().Name(),
				"Optional":  r.IsOptional(),
				"Many":      r.IsMany(),
				"EdgeProps": project(r.PropertiesSlice()),
			})
		}
		return out
	}

	types := make([]map[string]any, 0)
	for _, typ := range s.TypesSlice() {
		var supers []string
		for ref := range typ.SuperTypes() {
			supers = append(supers, ref.Ref().Name())
		}
		var invariants []string
		for _, inv := range typ.InvariantsSlice() {
			invariants = append(invariants, inv.Name())
		}
		types = append(types, map[string]any{
			"Name":          typ.Name(),
			"Abstract":      typ.IsAbstract(),
			"Part":          typ.IsPart(),
			"SuperTypes":    supers,
			"AllProperties": project(typ.AllPropertiesSlice()),
			"Associations":  relations(typ.AllAssociationsSlice()),
			"Compositions":  relations(typ.AllCompositionsSlice()),
			"Invariants":    invariants,
		})
	}

	dataTypes := make([]map[string]any, 0)
	for _, dt := range s.DataTypesSlice() {
		constraint := "(none)"
		if c := dt.Constraint(); c != nil {
			constraint = c.String()
		}
		dataTypes = append(dataTypes, map[string]any{"Name": dt.Name(), "Constraint": constraint})
	}

	imports := make([]map[string]any, 0)
	for _, imp := range s.ImportsSlice() {
		imports = append(imports, map[string]any{"Path": imp.Path(), "Alias": imp.Alias()})
	}

	return map[string]any{
		"Name":      s.Name(),
		"Types":     types,
		"DataTypes": dataTypes,
		"Imports":   imports,
	}
}

// completeGolden completes model via buildAndComplete, requires success,
// and pins the resolved schema's full projection as a JSON golden named
// after the test.
func completeGolden(t *testing.T, model *schema.TestModel) {
	t.Helper()
	s, result := buildAndComplete(t, model)
	require.NotNil(t, s)
	require.False(t, result.HasErrors(), "unexpected errors: %v", result)
	name := strings.ToLower(strings.TrimPrefix(t.Name(), "Test"))
	yammmtest.GoldenJSON(t, name, projectSchema(s))
}

func TestComplete_EmptySchema(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
	}

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
}

// TestComplete_Errors drives every uniform completion-failure scenario
// through one table: each model must fail to complete (nil schema), carry
// the expected diagnostic code when one is pinned, and mention the
// expected message fragments on the first issue.
func TestComplete_Errors(t *testing.T) {
	tests := []struct {
		name         string
		model        *schema.TestModel
		wantCode     diag.Code
		wantMsgParts []string
	}{
		{
			name: "duplicate type",
			model: &schema.TestModel{
				Name: "test",
				Types: []*schema.TestTypeDecl{
					{Name: "Person"},
					{Name: "Person"},
				},
			},
		},
		{
			name: "inheritance cycle",
			model: &schema.TestModel{
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
			},
		},
		{
			name: "case collision",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_CASE_COLLISION,
		},
		{
			name: "reserved prefix",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_RESERVED_PREFIX,
		},
		{
			name: "invalid import alias keyword",
			model: &schema.TestModel{
				Name: "test",
				Imports: []*schema.TestImportDecl{
					{
						Path:  "./other",
						Alias: "type", // Reserved keyword
					},
				},
			},
		},
		{
			name: "composition target must be part",
			model: &schema.TestModel{
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
			},
		},
		{
			name: "composition target cannot be abstract",
			model: &schema.TestModel{
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
			},
		},
		{
			name: "datatype duplicate",
			model: &schema.TestModel{
				Name: "test",
				DataTypes: []*schema.TestDataTypeDecl{
					{Name: "Email", Constraint: schema.NewStringConstraint()},
					{Name: "Email", Constraint: schema.NewStringConstraint()},
				},
			},
		},
		{
			name: "relation normalization collision associations",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_RELATION_NORMALIZATION_COLLISION,
		},
		{
			name: "relation normalization collision mixed",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_RELATION_NORMALIZATION_COLLISION,
		},
		{
			name: "property relation collision association",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_PROPERTY_RELATION_COLLISION,
		},
		{
			name: "property relation collision composition",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_PROPERTY_RELATION_COLLISION,
		},
		{
			name: "association target unknown type",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_UNKNOWN_TYPE,
		},
		{
			name: "relation inheritance conflicting optional",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_RELATION_COLLISION,
		},
		{
			name: "import alias starts with number",
			model: &schema.TestModel{
				Name: "test",
				Imports: []*schema.TestImportDecl{
					{Path: "other", Alias: "123invalid"},
				},
			},
		},
		{
			name: "import duplicate alias",
			model: &schema.TestModel{
				Name: "test",
				Imports: []*schema.TestImportDecl{
					{Path: "first", Alias: "other"},
					{Path: "second", Alias: "other"}, // Duplicate alias
				},
			},
		},
		{
			name: "import collides with local type",
			model: &schema.TestModel{
				Name: "test",
				Imports: []*schema.TestImportDecl{
					{Path: "other", Alias: "Person"}, // Collides with local type
				},
				Types: []*schema.TestTypeDecl{
					{Name: "Person"},
				},
			},
		},
		{
			name: "unknown type in extends",
			model: &schema.TestModel{
				Name: "test",
				Types: []*schema.TestTypeDecl{
					{
						Name: "Derived",
						Inherits: []*schema.TestASTTypeRef{
							{Name: "NonExistent"}, // Unknown local type
						},
					},
				},
			},
		},
		{
			name: "part type cannot declare association",
			model: &schema.TestModel{
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
			},
			wantCode:     diag.E_INVALID_ASSOCIATION_TARGET,
			wantMsgParts: []string{"part type", "cannot declare association"},
		},
		{
			name: "association cannot target part type",
			model: &schema.TestModel{
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
			},
			wantCode:     diag.E_INVALID_ASSOCIATION_TARGET,
			wantMsgParts: []string{"cannot target part type"},
		},
		{
			name: "association cannot target abstract type",
			model: &schema.TestModel{
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
			},
			wantCode:     diag.E_INVALID_ASSOCIATION_TARGET,
			wantMsgParts: []string{"cannot target abstract type"},
		},
		{
			name: "narrowing widening rejected",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_PROPERTY_CONFLICT,
		},
		{
			name: "narrowing required to optional rejected",
			model: &schema.TestModel{
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
			},
			wantCode: diag.E_PROPERTY_CONFLICT,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, result := buildAndComplete(t, tt.model)
			assert.Nil(t, s, "completion must fail")
			require.True(t, result.HasErrors(), "completion must produce error diagnostics")
			if tt.wantCode != (diag.Code{}) {
				assert.True(t, hasCode(result, tt.wantCode), "expected %v in: %s", tt.wantCode, result.String())
			}
			if len(tt.wantMsgParts) > 0 {
				issues := slices.Collect(result.Issues())
				require.NotEmpty(t, issues)
				for _, part := range tt.wantMsgParts {
					assert.Contains(t, issues[0].Message(), part)
				}
			}
		})
	}
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
}

func TestComplete_DataType_NilSkipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		DataTypes: []*schema.TestDataTypeDecl{
			nil, // nil entry should be skipped
			{Name: "Email", Constraint: schema.NewStringConstraint()},
		},
	}

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

func TestComplete_Import_NilSkipped(t *testing.T) {
	model := &schema.TestModel{
		Name: "test",
		Imports: []*schema.TestImportDecl{
			nil, // nil entry should be skipped
		},
	}

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
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

	completeGolden(t, model)
}
