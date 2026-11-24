package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// An edge property's refusal names the relation and the property once each.
func TestEdgeQueries_ARefusalNamesTheRelationOnce(t *testing.T) {
	t.Parallel()
	a, snap := cypher.Config{}, holderSnapshot(t, holderValues{when: "not a time"})
	_, err := EdgeQueries(t.Context(), a, snap)
	const want = `relation "LINK": property "when": coerce Timestamp: cannot parse "not a time" as an RFC3339 timestamp`
	if err == nil || err.Error() != want {
		t.Errorf("error = %v; want %s", err, want)
	}
}

// A Timestamp's refusal names RFC 3339, for text a date alone too.
func TestCoerceValue_ATimestampRefusalNamesRFC3339(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"nope", "2026-08-19", "2026-08-19 12:00:00"} {
		_, err := coerceValue(schematest.Constraint(t, "Timestamp"), text)
		want := `coerce Timestamp: cannot parse "` + text + `" as an RFC3339 timestamp`
		if err == nil || err.Error() != want {
			t.Errorf("error = %v; want %s", err, want)
		}
	}
}

// A relationship type is written unquoted, so a relation the loader admits that
// is no Neo4j identifier is refused before any query is built: a Cypher
// reserved keyword, which RelationName's UPPER_SNAKE form does not exclude.
func TestEdgeQueries_RefusesARelationshipTypeThatIsNoIdentifier(t *testing.T) {
	t.Parallel()
	for rel, want := range map[string]string{
		"ON":    `relationship type "ON" is not a Neo4j identifier: the name is a Cypher reserved keyword`,
		"ORDER": `relationship type "ORDER" is not a Neo4j identifier: the name is a Cypher reserved keyword`,
		"LINK":  "",
	} {
		src := "schema \"p\"\n\ntype Target {\n\tkey String primary\n}\n\ntype Holder {\n\tid String primary\n\t--> " + rel + " (_:many) Target\n}\n"
		s, loadErr := schema.LoadString(t.Context(), src, "p.yammm")
		if loadErr != nil {
			t.Fatalf("%s: load: %s", rel, loadErr)
		}
		targetT, _ := s.Type("Target")
		holderT, _ := s.Type("Holder")
		snap := snappartstest.Loaded(t, s, snapparts.Parts{
			Types: []schema.TypeID{targetT.ID(), holderT.ID()},
			Instances: []snapparts.Instance{
				{TypeID: targetT.ID(), PrimaryKey: immutable.WrapKey([]any{"t1"}), Properties: immutable.WrapProperties(map[string]any{"key": "t1"})},
				{TypeID: holderT.ID(), PrimaryKey: immutable.WrapKey([]any{"h1"}), Properties: immutable.WrapProperties(map[string]any{"id": "h1"})},
			},
			Edges: []snapparts.Edge{{Relation: rel, SourceType: holderT.ID(), SourceKey: immutable.WrapKey([]any{"h1"}), TargetKey: immutable.WrapKey([]any{"t1"})}},
		})
		queries, err := EdgeQueries(t.Context(), cypher.Config{}, snap)
		if want == "" {
			if err != nil || len(queries) != 1 {
				t.Errorf("%s: %d queries, error %v; want one query", rel, len(queries), err)
			}
			continue
		}
		if err == nil || err.Error() != want {
			t.Errorf("%s: error = %v; want %s", rel, err, want)
		}
		if queries != nil {
			t.Errorf("%s: queries = %v; want none", rel, queries)
		}
	}
}
