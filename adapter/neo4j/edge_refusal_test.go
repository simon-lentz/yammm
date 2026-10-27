package neo4j

import "testing"

// An edge property's refusal names the relation and the property once each.
func TestBatchEdgeQueries_ARefusalNamesTheRelationOnce(t *testing.T) {
	t.Parallel()
	a, shape, snap := utf8Neo4jSnapshot(t, utf8Holder{when: "not a time"})
	_, err := a.BatchEdgeQueries(t.Context(), snap, shape)
	const want = `relation "LINK": property "when": coerce Timestamp: cannot parse "not a time" as an RFC3339 timestamp`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v; want %s", err, want)
	}
}
