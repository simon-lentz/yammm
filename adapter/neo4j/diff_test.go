package neo4j

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffConstraints_AllMatch(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}, TypeExpr: ""},
		{Kind: ConstraintNotNull, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: ""},
	}

	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	assert.Len(t, result.Match, 2)
	assert.Empty(t, result.Drift)
	assert.Empty(t, result.Create)
	assert.Empty(t, result.Drop)
}

func TestDiffConstraints_CreateNew(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
		{Kind: ConstraintType, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: "STRING"},
	}

	// Database only has the UNIQUENESS constraint.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	assert.Len(t, result.Match, 1)
	assert.Len(t, result.Create, 1)
	assert.Equal(t, "STRING", result.Create[0].TypeExpr)
}

func TestDiffConstraints_DropOrphaned(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}

	// Database has an extra constraint not in schema.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"old_field"}},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	assert.Len(t, result.Match, 1)
	assert.Len(t, result.Drop, 1)
	assert.Equal(t, "old_field", result.Drop[0].Properties[0])
}

func TestDiffConstraints_TypeDrift(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintType, Label: "test__Entity", Properties: []string{"name"}, TypeExpr: "STRING"},
	}

	// Database has the constraint but with a different type.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}, PropertyType: "INTEGER"},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	assert.Empty(t, result.Match)
	assert.Len(t, result.Drift, 1)
	assert.Contains(t, result.Drift[0].Reason, "property type mismatch")
	assert.Contains(t, result.Drift[0].Reason, "schema STRING")
	assert.Contains(t, result.Drift[0].Reason, "database INTEGER")
}

func TestDiffConstraints_FiltersNonOwnedConstraints(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}},
	}

	// Database has constraints from another schema — should be ignored.
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "other", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"other_schema__Foo"}, Properties: []string{"id"}},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	assert.Len(t, result.Match, 1)
	assert.Empty(t, result.Drop) // other_schema constraints not reported as drops
}

func TestDiffConstraints_Empty(t *testing.T) {
	t.Parallel()
	a := New()
	result := a.DiffConstraints(nil, nil, testOwned())
	assert.Empty(t, result.Match)
	assert.Empty(t, result.Drift)
	assert.Empty(t, result.Create)
	assert.Empty(t, result.Drop)
}

func TestDiffConstraints_CompositeKeys(t *testing.T) {
	t.Parallel()
	a := New()

	desired := []Constraint{
		{Kind: ConstraintUnique, Label: "test__Record", Properties: []string{"schema_id", "record_id"}},
	}

	// Properties in different order — should still match (sorted comparison).
	actual := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Record"}, Properties: []string{"record_id", "schema_id"}},
	}

	result := a.DiffConstraints(desired, actual, testOwned())
	require.Len(t, result.Match, 1)
}
