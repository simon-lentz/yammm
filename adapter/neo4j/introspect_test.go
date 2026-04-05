package neo4j

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRemoteConstraints_Uniqueness(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"name":            "msrb_emma__Issuer_issuer_id_unique",
			"type":            "UNIQUENESS",
			"entityType":      "NODE",
			"labelsOrTypes":   []any{"msrb_emma__Issuer"},
			"properties":      []any{"issuer_id"},
			"propertyType":    nil,
			"createStatement": "CREATE CONSTRAINT msrb_emma__Issuer_issuer_id_unique IF NOT EXISTS FOR (n:msrb_emma__Issuer) REQUIRE n.issuer_id IS UNIQUE",
		},
	}

	constraints, err := ParseRemoteConstraints(records)
	require.NoError(t, err)
	require.Len(t, constraints, 1)

	c := constraints[0]
	assert.Equal(t, "msrb_emma__Issuer_issuer_id_unique", c.Name)
	assert.Equal(t, "UNIQUENESS", c.Type)
	assert.Equal(t, "NODE", c.EntityType)
	assert.Equal(t, []string{"msrb_emma__Issuer"}, c.LabelsOrTypes)
	assert.Equal(t, []string{"issuer_id"}, c.Properties)
	assert.Empty(t, c.PropertyType)
}

func TestParseRemoteConstraints_TypeConstraint(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"name":            "msrb_emma__Issuer_name_type",
			"type":            "NODE_PROPERTY_TYPE",
			"entityType":      "NODE",
			"labelsOrTypes":   []any{"msrb_emma__Issuer"},
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
	assert.Error(t, err)
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
			"name":          "idx_issuer_name",
			"type":          "RANGE",
			"entityType":    "NODE",
			"labelsOrTypes": []any{"msrb_emma__Issuer"},
			"properties":    []any{"name"},
		},
	}

	indexes, err := ParseRemoteIndexes(records)
	require.NoError(t, err)
	require.Len(t, indexes, 1)
	assert.Equal(t, "idx_issuer_name", indexes[0].Name)
	assert.Equal(t, "RANGE", indexes[0].Type)
}

func TestParseRemoteRelationships_Basic(t *testing.T) {
	t.Parallel()
	records := []map[string]any{
		{
			"relType":   "ISSUED_BY",
			"srcLabels": []any{"msrb_emma__Issue"},
			"tgtLabels": []any{"msrb_emma__Issuer"},
		},
		{
			"relType":   "IN_STATE",
			"srcLabels": []any{"msrb_emma__Issuer"},
			"tgtLabels": []any{"census_tiger__State"},
		},
	}

	rels, err := ParseRemoteRelationships(records)
	require.NoError(t, err)
	require.Len(t, rels, 2)
	assert.Equal(t, "ISSUED_BY", rels[0].RelationType)
	assert.Equal(t, []string{"msrb_emma__Issue"}, rels[0].SourceLabels)
	assert.Equal(t, []string{"msrb_emma__Issuer"}, rels[0].TargetLabels)
}

func TestIntrospectConstraintsQuery(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "SHOW CONSTRAINTS YIELD *", IntrospectConstraintsQuery())
}

func TestIntrospectRelationshipsQuery_WithPrefix(t *testing.T) {
	t.Parallel()
	query, params := IntrospectRelationshipsQuery("msrb_emma__")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "msrb_emma__", params["prefix"])
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
	query, params := a.IntrospectRelationshipsQueryFor("msrb_emma")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "msrb_emma__", params["prefix"])
}

func TestIntrospectRelationshipsQueryFor_WithPrefix(t *testing.T) {
	t.Parallel()
	a := New(WithLabelPrefix("app_"))
	query, params := a.IntrospectRelationshipsQueryFor("msrb_emma")
	assert.Contains(t, query, "STARTS WITH $prefix")
	assert.Equal(t, "app_msrb_emma__", params["prefix"])
}

func TestIntrospectRelationshipsQueryFor_EmptyFilter(t *testing.T) {
	t.Parallel()
	a := New()
	query, params := a.IntrospectRelationshipsQueryFor("")
	assert.NotContains(t, query, "WHERE")
	assert.Nil(t, params)
}
