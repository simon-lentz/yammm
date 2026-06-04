package neo4j

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLabel_Default(t *testing.T) {
	t.Parallel()
	sn, tn := parseLabel("book_catalog__Publisher", "__")
	assert.Equal(t, "book_catalog", sn)
	assert.Equal(t, "Publisher", tn)
}

func TestParseLabel_NoSeparator(t *testing.T) {
	t.Parallel()
	sn, tn := parseLabel("Person", "__")
	assert.Empty(t, sn)
	assert.Equal(t, "Person", tn)
}

func TestParseLabel_CustomSeparator(t *testing.T) {
	t.Parallel()
	sn, tn := parseLabel("test---Entity", "---")
	assert.Equal(t, "test", sn)
	assert.Equal(t, "Entity", tn)
}

func TestReverseNeo4jType_AllMappings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		input     string
		wantType  string
		wantExact bool
	}{
		{"STRING", "String", false},
		{"INTEGER", "Integer", true},
		{"FLOAT", "Float", true},
		{"BOOLEAN", "Boolean", true},
		{"DATE", "Date", true},
		{"ZONED DATETIME", "Timestamp", true},
		{"LIST<STRING NOT NULL>", "List<String>", false},
		{"LIST<INTEGER NOT NULL>", "List<Integer>", true},
		{"LIST<FLOAT NOT NULL>", "List<Float>", true},
		{"LIST<BOOLEAN NOT NULL>", "List<Boolean>", true},
		{"LIST<DATE NOT NULL>", "List<Date>", true},
		{"LIST<ZONED DATETIME NOT NULL>", "List<Timestamp>", true},
		{"UNKNOWN_TYPE", "String", false},
	}

	for _, tc := range tests {
		yammmType, exact := reverseNeo4jType(tc.input)
		assert.Equal(t, tc.wantType, yammmType, "input: %s", tc.input)
		assert.Equal(t, tc.wantExact, exact, "input: %s", tc.input)
	}
}

func TestInferSchema_BasicTypes(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
		{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}},
		{Name: "c3", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}, PropertyType: "STRING"},
		{Name: "c4", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"count"}, PropertyType: "INTEGER"},
		{Name: "c5", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"active"}, PropertyType: "BOOLEAN"},
		{Name: "c6", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"created_at"}, PropertyType: "DATE"},
	}

	output, err := a.InferSchema(constraints, nil, "test")
	require.NoError(t, err)

	assert.Contains(t, output, `schema "test"`)
	assert.Contains(t, output, "type Entity {")
	assert.Contains(t, output, "id String primary")
	assert.Contains(t, output, "name String required")
	assert.Contains(t, output, "count Integer")
	assert.Contains(t, output, "active Boolean")
	assert.Contains(t, output, "created_at Date")
}

func TestInferSchema_WithRelationships(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Employee"}, Properties: []string{"id"}},
		{Name: "c2", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Company"}, Properties: []string{"id"}},
	}

	relationships := []RemoteRelationship{
		{RelationType: "WORKS_AT", SourceLabels: []string{"test__Employee"}, TargetLabels: []string{"test__Company"}},
	}

	output, err := a.InferSchema(constraints, relationships, "test")
	require.NoError(t, err)

	assert.Contains(t, output, "--> WORKS_AT Company")
}

func TestInferSchema_CrossSchemaRelationships(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"catalog__Publisher"}, Properties: []string{"id"}},
	}

	relationships := []RemoteRelationship{
		{RelationType: "IN_REGION", SourceLabels: []string{"catalog__Publisher"}, TargetLabels: []string{"geo__Region"}},
	}

	output, err := a.InferSchema(constraints, relationships, "catalog")
	require.NoError(t, err)

	assert.Contains(t, output, `import "geo"`)
	assert.Contains(t, output, "--> IN_REGION geo.Region")
	assert.Contains(t, output, "cross-schema")
}

func TestInferSchema_EmptyInput(t *testing.T) {
	t.Parallel()
	a := New()
	output, err := a.InferSchema(nil, nil, "")
	require.NoError(t, err)
	assert.Contains(t, output, `schema "inferred"`)
}

func TestInferSchema_MissingPropertyType(t *testing.T) {
	t.Parallel()
	a := New()

	// Property with no type constraint — defaults to String with TODO.
	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
	}

	output, err := a.InferSchema(constraints, nil, "test")
	require.NoError(t, err)

	// id is primary key but has no explicit type — defaults to String.
	assert.Contains(t, output, "id String primary")
}

func TestInferSchema_FiltersBySchema(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"alpha__Foo"}, Properties: []string{"id"}},
		{Name: "c2", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"beta__Bar"}, Properties: []string{"id"}},
	}

	output, err := a.InferSchema(constraints, nil, "alpha")
	require.NoError(t, err)

	assert.Contains(t, output, "type Foo {")
	assert.NotContains(t, output, "type Bar {")
}

func TestInferSchema_NodeKeyComposite(t *testing.T) {
	t.Parallel()
	a := New()

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "NODE_KEY", EntityType: "NODE", LabelsOrTypes: []string{"test__Record"}, Properties: []string{"schema_id", "record_id"}},
	}

	output, err := a.InferSchema(constraints, nil, "test")
	require.NoError(t, err)

	// Both should be primary keys.
	lines := strings.Split(output, "\n")
	var pkLines []string
	for _, line := range lines {
		if strings.Contains(line, "primary") {
			pkLines = append(pkLines, strings.TrimSpace(line))
		}
	}
	assert.Len(t, pkLines, 2)
}

func TestInferSchema_CustomSeparator(t *testing.T) {
	t.Parallel()
	a := New(WithLabelSeparator("---"))

	constraints := []RemoteConstraint{
		{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test---Entity"}, Properties: []string{"id"}},
	}

	output, err := a.InferSchema(constraints, nil, "test")
	require.NoError(t, err)

	assert.Contains(t, output, `schema "test"`)
	assert.Contains(t, output, "type Entity {")
	assert.Contains(t, output, "id String primary")
}
