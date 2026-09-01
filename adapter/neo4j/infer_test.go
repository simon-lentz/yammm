package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/stretchr/testify/assert"
)

func TestParseLabel(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		label      string
		sep        string
		wantSchema string
		wantType   string
	}{
		{"default separator", "book_catalog__Publisher", "__", "book_catalog", "Publisher"},
		{"no separator present", "Person", "__", "", "Person"},
		{"custom separator", "test---Entity", "---", "test", "Entity"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sn, tn := parseLabel(tc.label, tc.sep)
			if sn != tc.wantSchema || tn != tc.wantType {
				t.Errorf("parseLabel(%q, %q) = (%q, %q); want (%q, %q)",
					tc.label, tc.sep, sn, tn, tc.wantSchema, tc.wantType)
			}
		})
	}
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

// TestInferSchema_Golden pins the complete emitted .yammm text per scenario —
// type/property/relationship rendering, primary-key markers, import lines,
// cross-schema annotations, TODO placeholders, and schema filtering (the
// filters_by_schema golden contains no `type Bar`). InferSchema sorts its
// output, so the text is deterministic.
func TestInferSchema_Golden(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name          string
		opts          []Option
		constraints   []RemoteConstraint
		relationships []RemoteRelationship
		schemaName    string
	}{
		{
			name: "basic_types",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
				{Name: "c2", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}},
				{Name: "c3", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"name"}, PropertyType: "STRING"},
				{Name: "c4", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"count"}, PropertyType: "INTEGER"},
				{Name: "c5", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"active"}, PropertyType: "BOOLEAN"},
				{Name: "c6", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"created_at"}, PropertyType: "DATE"},
			},
			schemaName: "test",
		},
		{
			name: "with_relationships",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Employee"}, Properties: []string{"id"}},
				{Name: "c2", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Company"}, Properties: []string{"id"}},
			},
			relationships: []RemoteRelationship{
				{RelationType: "WORKS_AT", SourceLabels: []string{"test__Employee"}, TargetLabels: []string{"test__Company"}},
			},
			schemaName: "test",
		},
		{
			name: "cross_schema_relationships",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"catalog__Publisher"}, Properties: []string{"id"}},
			},
			relationships: []RemoteRelationship{
				{RelationType: "IN_REGION", SourceLabels: []string{"catalog__Publisher"}, TargetLabels: []string{"geo__Region"}},
			},
			schemaName: "catalog",
		},
		{
			name:       "empty_input",
			schemaName: "",
		},
		{
			name: "missing_property_type",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Entity"}, Properties: []string{"id"}},
			},
			schemaName: "test",
		},
		{
			name: "filters_by_schema",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"alpha__Foo"}, Properties: []string{"id"}},
				{Name: "c2", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"beta__Bar"}, Properties: []string{"id"}},
			},
			schemaName: "alpha",
		},
		{
			name: "nodekey_composite",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "NODE_KEY", EntityType: "NODE", LabelsOrTypes: []string{"test__Record"}, Properties: []string{"schema_id", "record_id"}},
			},
			schemaName: "test",
		},
		{
			name: "part_type",
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__Order"}, Properties: []string{"order_id"}},
				{Name: "c2", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test__LineItem"}, Properties: []string{"_composed_key"}},
				{Name: "c3", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__LineItem"}, Properties: []string{"_composed_key"}},
				{Name: "c4", Type: "NODE_PROPERTY_EXISTENCE", EntityType: "NODE", LabelsOrTypes: []string{"test__LineItem"}, Properties: []string{"description"}},
				{Name: "c5", Type: "NODE_PROPERTY_TYPE", EntityType: "NODE", LabelsOrTypes: []string{"test__LineItem"}, Properties: []string{"quantity"}, PropertyType: "INTEGER"},
			},
			schemaName: "test",
		},
		{
			name: "custom_separator",
			opts: []Option{WithLabelSeparator("---")},
			constraints: []RemoteConstraint{
				{Name: "c1", Type: "UNIQUENESS", EntityType: "NODE", LabelsOrTypes: []string{"test---Entity"}, Properties: []string{"id"}},
			},
			schemaName: "test",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			output := New(tc.opts...).InferSchema(tc.constraints, tc.relationships, tc.schemaName)
			yammmtest.Golden(t, "infer_"+tc.name, []byte(output))
		})
	}
}
