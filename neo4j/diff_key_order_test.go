package neo4j

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema/cypher"
)

// A composite index declared out of name order matches a remote index over the
// same order.
func TestIndexDiff_ACompositeMatchesInItsDeclaredOrder(t *testing.T) {
	t.Parallel()

	desired := []cypher.Index{{Kind: cypher.IndexRange, Label: "test__Entity", Properties: []string{"b", "a"}}}
	actual := []RemoteIndex{{Name: "i1", Type: "RANGE", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"b", "a"}}}
	wantCounts(t, diff(cypher.Config{}, nil, desired, nil, actual, testOwned()).Indexes, 1, 0, 0, 0, 0)
}

// The exact pairing phase pairs the remote row in the declared order, where a
// row over the same properties in another order also exists.
func TestConstraintDiff_TheExactPhasePairsTheDeclaredOrder(t *testing.T) {
	t.Parallel()

	desired := []cypher.Constraint{{Name: "want", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"b", "a"}}}
	actual := []RemoteConstraint{rc("r1", "test__Entity", "a", "b"), rc("r2", "test__Entity", "b", "a")}
	got := diff(cypher.Config{}, desired, nil, actual, nil, testOwned()).Constraints
	if len(got.Match) != 1 || got.Match[0].Actual.Name != "r2" {
		t.Errorf("match = %+v; want the declared-order row r2", got.Match)
	}
}

// A drift over a name alone names the schema's kind and the database's.
func TestConstraintDiff_ANameDriftNamesBothKinds(t *testing.T) {
	t.Parallel()

	desired := []cypher.Constraint{{Name: "x", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"id"}}}
	actual := []RemoteConstraint{{Name: "x", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}}}
	got := diff(cypher.Config{}, desired, nil, actual, nil, testOwned()).Constraints
	if len(got.Drift) != 1 || !strings.Contains(got.Drift[0].Reason, "schema UNIQUENESS(id), database NODE_PROPERTY_EXISTENCE(id)") {
		t.Errorf("drift = %+v", got.Drift)
	}
}

// A kept constraint's backing index serves a declared index over its
// definition whatever case the server spells its type in.
func TestIndexDiff_ALowerCaseBackingIndexServes(t *testing.T) {
	t.Parallel()

	desired := []cypher.Index{{Name: "want_idx", Kind: cypher.IndexRange, Label: "test__Entity", Properties: []string{"a", "b"}}}
	keep := []cypher.Constraint{{Name: "k", Kind: cypher.ConstraintUnique, Label: "test__Entity", Properties: []string{"a", "b"}}}
	constraints := []RemoteConstraint{rc("k", "test__Entity", "a", "b")}
	indexes := []RemoteIndex{{Name: "k", Type: "range", EntityType: "NODE", State: "ONLINE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"a", "b"}, OwningConstraint: "k"}}
	wantCounts(t, diff(cypher.Config{}, keep, desired, constraints, indexes, testOwned()).Indexes, 1, 0, 0, 0, 0)
}
