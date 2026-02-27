package spec_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Associations — basic
// =============================================================================

// TestRelationships_AssociationBasic verifies that a basic association compiles.
// Source: SPEC.md, "Associations" — associations represent references between
// independent entities using --> syntax.
func TestRelationships_AssociationBasic(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/relationships/association_basic.yammm")

	// Verify the association exists on Person
	personType, ok := s.Type("Person")
	require.True(t, ok, "schema should contain Person type")

	rel, ok := personType.Relation("WORKS_AT")
	require.True(t, ok, "Person should have WORKS_AT relation")
	assert.True(t, rel.IsAssociation(), "WORKS_AT should be an association")
	assert.False(t, rel.IsComposition(), "WORKS_AT should not be a composition")
	assert.Equal(t, "Company", rel.Target().Name())
}

// =============================================================================
// Associations — with properties
// =============================================================================

// TestRelationships_AssociationWithProperties verifies that an association with
// edge properties compiles and the properties are accessible on the relation.
// Source: SPEC.md, "Associations" — associations may have edge properties.
func TestRelationships_AssociationWithProperties(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/relationships/association_with_properties.yammm")

	personType, ok := s.Type("Person")
	require.True(t, ok, "schema should contain Person type")

	rel, ok := personType.Relation("WORKS_AT")
	require.True(t, ok, "Person should have WORKS_AT relation")
	assert.True(t, rel.HasProperties(), "WORKS_AT should have edge properties")

	roleProp, ok := rel.Property("role")
	require.True(t, ok, "WORKS_AT should have a 'role' property")
	assert.True(t, roleProp.IsRequired(), "role should be required")

	sinceProp, ok := rel.Property("since")
	require.True(t, ok, "WORKS_AT should have a 'since' property")
	assert.False(t, sinceProp.IsRequired(), "since should be optional")
}

// =============================================================================
// Compositions — basic
// =============================================================================

// TestRelationships_CompositionBasic verifies that a basic composition targeting
// a part type compiles cleanly.
// Source: SPEC.md, "Compositions" — ownership where child entities are embedded.
func TestRelationships_CompositionBasic(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/relationships/composition_basic.yammm")

	orderType, ok := s.Type("Order")
	require.True(t, ok, "schema should contain Order type")

	rel, ok := orderType.Relation("ITEMS")
	require.True(t, ok, "Order should have ITEMS relation")
	assert.True(t, rel.IsComposition(), "ITEMS should be a composition")
	assert.False(t, rel.IsAssociation(), "ITEMS should not be an association")
	assert.Equal(t, "LineItem", rel.Target().Name())
}

// =============================================================================
// Compositions — requires part type target
// =============================================================================

// TestRelationships_CompositionRequiresPart verifies that a composition targeting
// a non-part type fails compilation with E_INVALID_COMPOSITION_TARGET.
// Source: SPEC.md, "Compositions" — the target must be a concrete part type.
func TestRelationships_CompositionRequiresPart(t *testing.T) {
	t.Parallel()
	result := loadSchemaExpectError(t, "testdata/relationships/composition_non_part.yammm")
	assertDiagHasCode(t, result, diag.E_INVALID_COMPOSITION_TARGET)
}

// =============================================================================
// Multiplicity — all 8 forms
// =============================================================================

// TestRelationships_MultiplicityAllForms verifies that all 8 multiplicity forms
// parse and compile successfully. Each form is checked for the expected
// optional/many flags on the resulting relation.
// Source: SPEC.md, "Multiplicity" — table of all 8 syntax forms.
func TestRelationships_MultiplicityAllForms(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/relationships/multiplicity_all_forms.yammm")

	sourceType, ok := s.Type("Source")
	require.True(t, ok, "schema should contain Source type")

	// The multiplicity table from the SPEC maps to the parser's handleMultiplicity
	// function. All 8 forms are tested against the spec-defined behavior.
	tests := []struct {
		name         string
		wantOptional bool
		wantMany     bool
	}{
		// (omitted) -> optional, one
		{"REL_OMITTED", true, false},
		// (_) -> optional, one
		{"REL_OPTIONAL", true, false},
		// (_:one) -> optional, one
		{"REL_OPTIONAL_ONE", true, false},
		// (_:many) -> optional, many
		{"REL_OPTIONAL_MANY", true, true},
		// (one) -> required, one
		{"REL_REQUIRED", false, false},
		// (one:one) -> required, one
		{"REL_REQUIRED_ONE", false, false},
		// (one:many) -> required, many
		{"REL_REQUIRED_MANY", false, true},
		// (many) -> optional, many
		{"REL_MANY", true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rel, ok := sourceType.Relation(tt.name)
			require.True(t, ok, "Source should have %s relation", tt.name)
			assert.Equal(t, tt.wantOptional, rel.IsOptional(),
				"%s: IsOptional mismatch", tt.name)
			assert.Equal(t, tt.wantMany, rel.IsMany(),
				"%s: IsMany mismatch", tt.name)
		})
	}
}

// =============================================================================
// Reverse Relationships
// =============================================================================

// TestRelationships_ReverseClause verifies that the reverse clause on an
// association is parsed and stored as metadata on the relation.
// Source: SPEC.md, "Reverse Relationships" — the optional reverse clause
// declares the inverse relationship name.
func TestRelationships_ReverseClause(t *testing.T) {
	t.Parallel()
	s, _ := loadSchemaRaw(t, "testdata/relationships/reverse.yammm")

	personType, ok := s.Type("Person")
	require.True(t, ok, "schema should contain Person type")

	rel, ok := personType.Relation("WORKS_AT")
	require.True(t, ok, "Person should have WORKS_AT relation")

	assert.Equal(t, "EMPLOYS", rel.Backref(),
		"reverse name should be EMPLOYS")
}

// =============================================================================
// Association Data — _target_id convention
// =============================================================================

// TestRelationships_AssociationData_SinglePK verifies that association edge data
// using the _target_id convention validates and produces graph edges.
// Source: SPEC.md, "Association Data in Instances" — single PK targets use
// _target_id, with edge properties alongside.
func TestRelationships_AssociationData_SinglePK(t *testing.T) {
	t.Parallel()

	data := "testdata/relationships/data.json"
	s, v := loadSchemaRaw(t, "testdata/relationships/association_data.yammm")

	// Validate Company target
	companies := loadTestData(t, data, "Company")
	companyInst := validateOne(t, v, "Company", companies[0])

	// Validate Person with edge data (works_at with _target_id)
	persons := loadTestData(t, data, "Person__with_edge")
	personInst := validateOne(t, v, "Person", persons[0])

	// Build graph and verify edge resolution
	snap := buildGraph(t, s, companyInst, personInst)

	edges := snap.Edges()
	require.Len(t, edges, 1, "should have exactly one edge")
	assert.Equal(t, "WORKS_AT", edges[0].Relation())
	assert.Equal(t, "Person", edges[0].Source().TypeName())
	assert.Equal(t, "Company", edges[0].Target().TypeName())

	// Verify edge property
	roleProp, ok := edges[0].Property("role")
	assert.True(t, ok, "edge should have role property")
	if ok {
		roleStr, isStr := roleProp.String()
		assert.True(t, isStr, "role property should be a string")
		assert.Equal(t, "Engineer", roleStr)
	}
}

// =============================================================================
// Multiplicity Required — Check
// =============================================================================

// TestRelationships_MultiplicityRequired_Check verifies that g.Check() reports
// errors for required associations when no edges are provided.
// Source: SPEC.md, "Multiplicity" — required relations must be resolved.
func TestRelationships_MultiplicityRequired_Check(t *testing.T) {
	t.Parallel()

	data := "testdata/relationships/data.json"
	s, v := loadSchemaRaw(t, "testdata/relationships/multiplicity_all_forms.yammm")
	ctx := context.Background()

	// Add target instance so it exists in the graph
	targets := loadTestData(t, data, "Target")
	targetInst := validateOne(t, v, "Target", targets[0])

	// Add source instance with NO edges — required associations should be flagged
	sources := loadTestData(t, data, "Source__no_edges")
	sourceInst := validateOne(t, v, "Source", sources[0])

	g := graph.New(s)
	result, err := g.Add(ctx, targetInst)
	require.NoError(t, err)
	require.True(t, result.OK(), "Add target: %v", result.Messages())

	result, err = g.Add(ctx, sourceInst)
	require.NoError(t, err)
	require.True(t, result.OK(), "Add source: %v", result.Messages())

	// Check should report unresolved required associations
	checkResult, err := g.Check(ctx)
	require.NoError(t, err)

	// (one), (one:one), and (one:many) are all required multiplicities.
	assert.False(t, checkResult.OK(),
		"Check should report errors for required associations")
	assertDiagHasCode(t, checkResult, diag.E_UNRESOLVED_REQUIRED)
}

// =============================================================================
// Association Data — person without edge is valid for optional associations
// =============================================================================

// TestRelationships_OptionalAssociation_NoEdge verifies that omitting an
// optional association field in instance data is valid.
// Source: SPEC.md, "Multiplicity" — omitted multiplicity means optional/one.
func TestRelationships_OptionalAssociation_NoEdge(t *testing.T) {
	t.Parallel()

	data := "testdata/relationships/data.json"
	v := loadSchema(t, "testdata/relationships/association_data.yammm")

	// Person with no works_at edge should validate (association is optional)
	persons := loadTestData(t, data, "Person__no_edge")
	assertValid(t, v, "Person", persons[0])
}
