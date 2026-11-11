package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// An edge property's refusal names the relation and the property once each.
func TestBatchEdgeQueries_ARefusalNamesTheRelationOnce(t *testing.T) {
	t.Parallel()
	a, shape, snap := holderSnapshot(t, holderValues{when: "not a time"})
	_, err := a.BatchEdgeQueries(t.Context(), snap, shape)
	const want = `relation "LINK": property "when": coerce Timestamp: cannot parse "not a time" as an RFC3339 timestamp`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v; want %s", err, want)
	}
}

// Under a declared format a Timestamp's refusal names that format.
func TestCoerceValue_ATimestampRefusalNamesTheDeclaredFormat(t *testing.T) {
	t.Parallel()
	_, err := coerceValue(schema.NewTimestampConstraintFormatted("2006-01-02"), "nope")
	const want = `coerce Timestamp: cannot parse "nope" against format "2006-01-02"`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v; want %s", err, want)
	}
}
