package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstraintDiff_AllMatch(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}, TypeExpr: ""},
		{Kind: cypher.ConstraintNotNull, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: ""},
	}

	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	assert.Len(t, result.Match, 2)
	assert.Empty(t, result.Drift)
	assert.Empty(t, result.Create)
	assert.Empty(t, result.Drop)
}

func TestConstraintDiff_CreateNew(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
		{Kind: cypher.ConstraintType, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: "STRING"},
	}

	// Database only has the UNIQUENESS constraint.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	assert.Len(t, result.Match, 1)
	assert.Len(t, result.Create, 1)
	assert.Equal(t, "STRING", result.Create[0].TypeExpr)
}

func TestConstraintDiff_DropOrphaned(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}

	// Database has an extra constraint not in schema.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"old_field"}},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	assert.Len(t, result.Match, 1)
	assert.Len(t, result.Drop, 1)
	assert.Equal(t, "old_field", result.Drop[0].Properties[0])
}

func TestConstraintDiff_TypeDrift(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintType, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: "STRING"},
	}

	// Database has the constraint but with a different type.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}, PropertyType: "INTEGER"},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	assert.Empty(t, result.Match)
	assert.Len(t, result.Drift, 1)
	assert.Contains(t, result.Drift[0].Reason, "property type mismatch")
	assert.Contains(t, result.Drift[0].Reason, "schema STRING")
	assert.Contains(t, result.Drift[0].Reason, "database INTEGER")
}

func TestConstraintDiff_FiltersNonOwnedConstraints(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}

	// Database has constraints from another schema — should be ignored.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "other", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"other_schema__Foo"}, Properties: []string{"id"}},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	assert.Len(t, result.Match, 1)
	assert.Empty(t, result.Drop) // other_schema constraints not reported as drops
}

func TestConstraintDiff_Empty(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}
	result := diff(a, nil, nil, nil, nil, testOwned()).Constraints
	assert.Empty(t, result.Match)
	assert.Empty(t, result.Drift)
	assert.Empty(t, result.Create)
	assert.Empty(t, result.Drop)
}

func TestConstraintDiff_CompositeKeys(t *testing.T) {
	t.Parallel()
	a := cypher.Config{}

	desired := []cypher.Constraint{
		{Kind: cypher.ConstraintUnique, Label: "test__Record", Properties: []string{"schema_id", "record_id"}},
	}

	// Properties in different order — should still match (sorted comparison).
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Record"}, Properties: []string{"record_id", "schema_id"}},
	}

	result := diff(a, desired, nil, actual, nil, testOwned()).Constraints
	require.Len(t, result.Match, 1)
}
