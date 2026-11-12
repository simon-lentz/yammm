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

// A Timestamp's refusal names RFC 3339, for text a date alone too.
func TestCoerceValue_ATimestampRefusalNamesRFC3339(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"nope", "2026-08-19", "2026-08-19 12:00:00"} {
		_, err := coerceValue(schema.NewTimestampConstraint(), text)
		want := `coerce Timestamp: cannot parse "` + text + `" as an RFC3339 timestamp`
		if err == nil || err.Error() != want {
			t.Errorf("error = %v; want %s", err, want)
		}
	}
}
