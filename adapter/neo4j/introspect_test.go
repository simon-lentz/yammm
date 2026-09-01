package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteConstraints_Uniqueness(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"name":            "book_catalog__Publisher_publisher_id_unique",
			"type":            "UNIQUENESS",
			"entityType":      "NODE",
			"labelsOrTypes":   []any{"book_catalog__Publisher"},
			"properties":      []any{"publisher_id"},
			"propertyType":    nil,
			"createStatement": "CREATE CONSTRAINT book_catalog__Publisher_publisher_id_unique IF NOT EXISTS FOR (n:book_catalog__Publisher) REQUIRE n.publisher_id IS UNIQUE",
		},
	}

	constraints, err := ParseRemoteConstraints(records)
	require.NoError(t, err)

	want := []RemoteConstraint{{
		Name:            "book_catalog__Publisher_publisher_id_unique",
		Type:            "UNIQUENESS",
		EntityType:      "NODE",
		LabelsOrTypes:   []string{"book_catalog__Publisher"},
		Properties:      []string{"publisher_id"},
		CreateStatement: "CREATE CONSTRAINT book_catalog__Publisher_publisher_id_unique IF NOT EXISTS FOR (n:book_catalog__Publisher) REQUIRE n.publisher_id IS UNIQUE",
	}}
	yammmtest.Diff(t, want, constraints)
}

func TestParseRemoteConstraints_TypeConstraint(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"name":            "book_catalog__Publisher_name_type",
			"type":            "NODE_PROPERTY_TYPE",
			"entityType":      "NODE",
			"labelsOrTypes":   []any{"book_catalog__Publisher"},
			"properties":      []any{"name"},
			"propertyType":    "STRING",
			"createStatement": "CREATE CONSTRAINT ...",
		},
	}

	constraints, err := ParseRemoteConstraints(records)
	require.NoError(t, err)
	assert.Equal(t, "STRING", constraints[0].PropertyType)
}

func TestParseRemoteConstraints_MissingPropertyType(t *testing.T) {
	t.Parallel()
	// Simulates Neo4j < 5.9 where propertyType column is absent.
	records := []map[string]any{
		{
			"name":          "some_constraint",
			"type":          "NODE_PROPERTY_TYPE",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"SomeLabel"},
			"properties":    []any{"prop"},
			// propertyType key absent entirely
		},
	}

	constraints, err := ParseRemoteConstraints(records)
	require.NoError(t, err)
	assert.Empty(t, constraints[0].PropertyType)
}

func TestParseRemoteConstraints_MissingName(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{"type": "UNIQUENESS"},
	}

	_, err := ParseRemoteConstraints(records)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing constraint name")
}

func TestParseRemoteConstraints_MultipleConstraints(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{"name": "c1", "type": "UNIQUENESS", "entityType": "NODE", "labelsOrTypes": []any{"A"}, "properties": []any{"id"}},
		{"name": "c2", "type": "NODE_PROPERTY_EXISTENCE", "entityType": "NODE", "labelsOrTypes": []any{"A"}, "properties": []any{"name"}},
		{"name": "c3", "type": "NODE_PROPERTY_TYPE", "entityType": "NODE", "labelsOrTypes": []any{"A"}, "properties": []any{"name"}, "propertyType": "STRING"},
	}

	constraints, err := ParseRemoteConstraints(records)
	require.NoError(t, err)
	assert.Len(t, constraints, 3)
}

func TestParseRemoteConstraints_Empty(t *testing.T) {
	t.Parallel()
	constraints, err := ParseRemoteConstraints(nil)
	require.NoError(t, err)
	assert.Empty(t, constraints)
}

func TestParseRemoteIndexes_Basic(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"name":          "idx_publisher_name",
			"type":          "RANGE",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"book_catalog__Publisher"},
			"properties":    []any{"name"},
		},
	}

	indexes, err := ParseRemoteIndexes(records)
	require.NoError(t, err)
	require.Len(t, indexes, 1)
	assert.Equal(t, "idx_publisher_name", indexes[0].Name)
	assert.Equal(t, "RANGE", indexes[0].Type)
}

func TestParseRemoteRelationships_Basic(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"relType":   "ISSUED_BY",
			"srcLabels": []any{"book_catalog__Book"},
			"tgtLabels": []any{"book_catalog__Publisher"},
		},
		{
			"relType":   "IN_REGION",
			"srcLabels": []any{"book_catalog__Publisher"},
			"tgtLabels": []any{"geo_regions__Region"},
		},
	}

	rels, err := ParseRemoteRelationships(records)
	require.NoError(t, err)
	require.Len(t, rels, 2)
	assert.Equal(t, "ISSUED_BY", rels[0].RelationType)
	assert.Equal(t, []string{"book_catalog__Book"}, rels[0].SourceLabels)
	assert.Equal(t, []string{"book_catalog__Publisher"}, rels[0].TargetLabels)
}

func TestIntrospectConstraintsQuery(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "SHOW CONSTRAINTS YIELD *", IntrospectConstraintsQuery())
}

func TestIntrospectRelationshipsQuery_WithPrefix(t *testing.T) {
	t.Parallel()
	query, params := IntrospectRelationshipsQuery("book_catalog__")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "book_catalog__", params["prefix"])
}

func TestIntrospectRelationshipsQuery_NoPrefix(t *testing.T) {
	t.Parallel()
	query, params := IntrospectRelationshipsQuery("")
	assert.NotContains(t, query, "WHERE")
	assert.Nil(t, params)
}

func TestIntrospectRelationshipsQueryFor_WithSchema(t *testing.T) {
	t.Parallel()
	a := New()
	query, params := a.IntrospectRelationshipsQueryFor("book_catalog")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "book_catalog__", params["prefix"])
}

func TestIntrospectRelationshipsQueryFor_WithPrefix(t *testing.T) {
	t.Parallel()
	a := New(WithLabelPrefix("app_"))
	query, params := a.IntrospectRelationshipsQueryFor("book_catalog")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "app_book_catalog__", params["prefix"])
}

func TestIntrospectRelationshipsQueryFor_EmptyFilter(t *testing.T) {
	t.Parallel()
	a := New()
	query, params := a.IntrospectRelationshipsQueryFor("")
	assert.NotContains(t, query, "WHERE")
	assert.Nil(t, params)
}

// An all-space schema filter discovers everything, the same as an empty one.
//
// Adapter.Label trims a schema name before composing a label, so no written
// label carries a prefix built from untrimmed space. Testing the raw value took
// the scoped path and built a prefix nothing matched, which reports a populated
// database as empty.
//
// Mutation: testing schemaFilter instead of its trimmed form turns this red.
func TestIntrospectRelationshipsQueryFor_AllSpaceFilterIsUnscoped(t *testing.T) {
	t.Parallel()
	a := New()

	unscoped, unscopedParams := a.IntrospectRelationshipsQueryFor("")
	for _, filter := range []string{" ", "   ", "\t"} {
		got, params := a.IntrospectRelationshipsQueryFor(filter)
		if got != unscoped {
			t.Errorf("filter %q built a scoped query:\n got: %s\nwant: %s", filter, got, unscoped)
		}
		if len(params) != len(unscopedParams) {
			t.Errorf("filter %q produced params %v; want %v", filter, params, unscopedParams)
		}
	}

	// A real filter still scopes.
	scoped, params := a.IntrospectRelationshipsQueryFor("app")
	if scoped == unscoped || params["prefix"] == nil {
		t.Errorf("a non-empty filter did not scope: %s %v", scoped, params)
	}
}
