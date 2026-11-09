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

// Every column SHOW INDEXES reports reaches its field: the diff reads the
// entity type and the owning constraint, which keeps a backing index out of Drop.
func TestParseRemoteIndexes_ReadsEveryColumn(t *testing.T) {
	t.Parallel()
	indexes, err := ParseRemoteIndexes([]map[string]any{{
		"name":             "rel_since",
		"type":             "RANGE",
		"entityType":       "RELATIONSHIP",
		"labelsOrTypes":    []any{"EMPLOYS"},
		"properties":       []any{"since"},
		"options":          map[string]any{"indexProvider": "range-1.0"},
		"state":            "ONLINE",
		"owningConstraint": "c",
	}})
	require.NoError(t, err)
	yammmtest.Diff(t, []RemoteIndex{{
		Name:             "rel_since",
		Type:             "RANGE",
		EntityType:       "RELATIONSHIP",
		LabelsOrTypes:    []string{"EMPLOYS"},
		Properties:       []string{"since"},
		Options:          map[string]any{"indexProvider": "range-1.0"},
		State:            "ONLINE",
		OwningConstraint: "c",
	}}, indexes)
}

// A column of another shape than the server's reads as absent: a string column
// that holds no string is empty, a list keeps its strings alone, and a list
// column that is missing is nil.
func TestParseRemoteConstraints_ReadsAnUnexpectedShapeAsAbsent(t *testing.T) {
	t.Parallel()
	constraints, err := ParseRemoteConstraints([]map[string]any{
		{"name": "a", "type": 7, "labelsOrTypes": []any{"A", 3}},
		{"name": "b", "type": "UNIQUENESS"},
	})
	require.NoError(t, err)
	yammmtest.Diff(t, []RemoteConstraint{
		{Name: "a", LabelsOrTypes: []string{"A"}},
		{Name: "b", Type: "UNIQUENESS"},
	}, constraints)
	if constraints[1].LabelsOrTypes != nil || constraints[1].Properties != nil {
		t.Errorf("missing list columns read as %#v and %#v, want nil", constraints[1].LabelsOrTypes, constraints[1].Properties)
	}
}

func TestIntrospectConstraintsQuery(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "SHOW CONSTRAINTS YIELD *", IntrospectConstraintsQuery())
}
