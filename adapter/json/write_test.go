package json

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// Test Schema Builders

func testSchemaSimple(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("simple").
		WithSourceID(location.MustNewSourceID("test://simple.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithProperty("age", schema.IntegerConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build simple schema: %s", result.String())
	}
	return s
}

func testSchemaMultiType(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("multi").
		WithSourceID(location.MustNewSourceID("test://multi.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build multi schema: %s", result.String())
	}
	return s
}

func testSchemaWithAssociation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("association").
		WithSourceID(location.MustNewSourceID("test://association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("EMPLOYER", schema.LocalTypeRef("Company", location.Span{}), true, false). // optional one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build association schema: %s", result.String())
	}
	return s
}

func testSchemaWithManyAssociation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("many_association").
		WithSourceID(location.MustNewSourceID("test://many_association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("EMPLOYERS", schema.LocalTypeRef("Company", location.Span{}), true, true). // optional many
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build many association schema: %s", result.String())
	}
	return s
}

func testSchemaWithComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("composition").
		WithSourceID(location.MustNewSourceID("test://composition.yammm")).
		AddType("Item").
		AsPart().
		WithPrimaryKey("sku", schema.StringConstraint{}).
		WithProperty("qty", schema.IntegerConstraint{}).
		Done().
		AddType("Order").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("customer", schema.StringConstraint{}).
		WithComposition("ITEMS", schema.LocalTypeRef("Item", location.Span{}), true, true). // optional many
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build composition schema: %s", result.String())
	}
	return s
}

func testSchemaWithOneComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("one_composition").
		WithSourceID(location.MustNewSourceID("test://one_composition.yammm")).
		AddType("Engine").
		AsPart().
		WithPrimaryKey("serial", schema.StringConstraint{}).
		WithProperty("displacement", schema.IntegerConstraint{}).
		Done().
		AddType("Car").
		WithPrimaryKey("vin", schema.StringConstraint{}).
		WithProperty("model", schema.StringConstraint{}).
		WithComposition("ENGINE", schema.LocalTypeRef("Engine", location.Span{}), true, false). // optional one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build one composition schema: %s", result.String())
	}
	return s
}

// Instance creation helpers

func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()

	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}

	return instance.NewValidInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pk),
		immutable.WrapProperties(props),
		nil, nil, nil,
	)
}

func mustValidInstanceWithEdge(
	t *testing.T,
	s *schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
	targetKeys [][]any,
) *instance.ValidInstance {
	t.Helper()

	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}

	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, targetKey := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(
			immutable.WrapKey(targetKey),
			immutable.Properties{},
		)
	}

	edges := map[string]*instance.ValidEdgeData{
		relationName: instance.NewValidEdgeData(targets),
	}

	return instance.NewValidInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pk),
		immutable.WrapProperties(props),
		edges,
		nil,
		nil,
	)
}

func mustValidPartInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()

	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}

	return instance.NewValidInstance(
		typeName,
		typ.ID(),
		immutable.WrapKey(pk),
		immutable.WrapProperties(props),
		nil, nil, nil,
	)
}

// Tests

func TestMarshalObject_NilResult(t *testing.T) {
	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	_, err = adapter.MarshalObject(nil)
	require.Error(t, err)
	assert.Equal(t, ErrNilResult, err)
}

func TestWriteObject_NilResult(t *testing.T) {
	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	var buf bytes.Buffer
	_, err = adapter.WriteObject(&buf, nil)
	require.Error(t, err)
	assert.Equal(t, ErrNilResult, err)
}

func TestMarshalObject_EmptyGraph(t *testing.T) {
	s := testSchemaSimple(t)
	g := graph.New(s)
	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))
	assert.Empty(t, output)
}

func TestMarshalObject_SingleType(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, inst)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	// Verify structure
	persons, ok := output["Person"].([]any)
	require.True(t, ok, "Expected Person to be array")
	require.Len(t, persons, 1)

	person := persons[0].(map[string]any)
	assert.Equal(t, "p1", person["id"])
	assert.Equal(t, "Alice", person["name"])
	assert.Equal(t, float64(30), person["age"]) // JSON numbers are float64
}

func TestMarshalObject_MultipleInstances(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	// Add two instances
	inst1 := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	inst2 := mustValidInstance(t, s, "Person", []any{"p2"}, map[string]any{
		"id":   "p2",
		"name": "Bob",
		"age":  int64(25),
	})

	_, err := g.Add(ctx, inst1)
	require.NoError(t, err)
	_, err = g.Add(ctx, inst2)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	persons := output["Person"].([]any)
	require.Len(t, persons, 2)
}

func TestMarshalObject_MultipleTypes(t *testing.T) {
	ctx := context.Background()
	s := testSchemaMultiType(t)
	g := graph.New(s)

	person := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
	})
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{
		"id":   "c1",
		"name": "Acme Inc",
	})

	_, err := g.Add(ctx, person)
	require.NoError(t, err)
	_, err = g.Add(ctx, company)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	assert.Contains(t, output, "Person")
	assert.Contains(t, output, "Company")

	persons := output["Person"].([]any)
	companies := output["Company"].([]any)
	assert.Len(t, persons, 1)
	assert.Len(t, companies, 1)
}

func TestMarshalObject_WithEdge(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	// Add company first
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{
		"id":   "c1",
		"name": "Acme Inc",
	})
	_, err := g.Add(ctx, company)
	require.NoError(t, err)

	// Add person with edge to company
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
		map[string]any{
			"id":   "p1",
			"name": "Alice",
		},
		"EMPLOYER",
		[][]any{{"c1"}},
	)
	_, err = g.Add(ctx, person)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	persons := output["Person"].([]any)
	require.Len(t, persons, 1)

	person1 := persons[0].(map[string]any)
	assert.Equal(t, "p1", person1["id"])
	assert.Equal(t, "Alice", person1["name"])

	// Check FK field - should be "employer" (lowercase relation name)
	employer, ok := person1["employer"]
	require.True(t, ok, "Expected employer FK field")

	// FK is an array of key components
	employerKey := employer.([]any)
	assert.Equal(t, []any{"c1"}, employerKey)
}

func TestMarshalObject_WithManyEdges(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithManyAssociation(t)
	g := graph.New(s)

	// Add companies
	c1 := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{
		"id":   "c1",
		"name": "Acme Inc",
	})
	c2 := mustValidInstance(t, s, "Company", []any{"c2"}, map[string]any{
		"id":   "c2",
		"name": "Beta Corp",
	})
	_, err := g.Add(ctx, c1)
	require.NoError(t, err)
	_, err = g.Add(ctx, c2)
	require.NoError(t, err)

	// Add person with multiple employers
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
		map[string]any{
			"id":   "p1",
			"name": "Alice",
		},
		"EMPLOYERS",
		[][]any{{"c1"}, {"c2"}},
	)
	_, err = g.Add(ctx, person)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	persons := output["Person"].([]any)
	person1 := persons[0].(map[string]any)

	// Multiple targets: array of FK arrays
	employers := person1["employers"].([]any)
	assert.Len(t, employers, 2)
}

func TestMarshalObject_WithComposition(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	// Create order with inline items
	order := mustValidInstance(t, s, "Order", []any{"o1"}, map[string]any{
		"id":       "o1",
		"customer": "Alice",
	})
	_, err := g.Add(ctx, order)
	require.NoError(t, err)

	// Add composed items
	item1 := mustValidPartInstance(t, s, "Item", []any{"SKU-A"}, map[string]any{
		"sku": "SKU-A",
		"qty": int64(2),
	})
	item2 := mustValidPartInstance(t, s, "Item", []any{"SKU-B"}, map[string]any{
		"sku": "SKU-B",
		"qty": int64(1),
	})

	_, err = g.AddComposed(ctx, "Order", `["o1"]`, "ITEMS", item1)
	require.NoError(t, err)
	_, err = g.AddComposed(ctx, "Order", `["o1"]`, "ITEMS", item2)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	orders := output["Order"].([]any)
	require.Len(t, orders, 1)

	order1 := orders[0].(map[string]any)
	assert.Equal(t, "o1", order1["id"])

	// Check composed children - should be "items" (lowercase relation name)
	items, ok := order1["items"].([]any)
	require.True(t, ok, "Expected items array")
	assert.Len(t, items, 2)
}

func TestMarshalObject_WithOneComposition(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithOneComposition(t)
	g := graph.New(s)

	car := mustValidInstance(t, s, "Car", []any{"VIN123"}, map[string]any{
		"vin":   "VIN123",
		"model": "Sedan",
	})
	_, err := g.Add(ctx, car)
	require.NoError(t, err)

	engine := mustValidPartInstance(t, s, "Engine", []any{"ENG001"}, map[string]any{
		"serial":       "ENG001",
		"displacement": int64(2000),
	})
	_, err = g.AddComposed(ctx, "Car", `["VIN123"]`, "ENGINE", engine)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	cars := output["Car"].([]any)
	car1 := cars[0].(map[string]any)

	// (one) cardinality: inline object, not array
	engine1, ok := car1["engine"].(map[string]any)
	require.True(t, ok, "Expected engine to be object (one cardinality)")
	assert.Equal(t, "ENG001", engine1["serial"])
}

func TestMarshalObject_WithIndent(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, inst)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	// Compact
	compact, err := adapter.MarshalObject(result)
	require.NoError(t, err)
	assert.NotContains(t, string(compact), "\n")

	// Pretty with tabs
	pretty, err := adapter.MarshalObject(result, WithIndent("\t"))
	require.NoError(t, err)
	assert.Contains(t, string(pretty), "\n")
	assert.Contains(t, string(pretty), "\t")

	// Pretty with spaces
	prettySpaces, err := adapter.MarshalObject(result, WithIndent("  "))
	require.NoError(t, err)
	assert.Contains(t, string(prettySpaces), "\n")
	assert.Contains(t, string(prettySpaces), "  ")
}

func TestMarshalObject_Deterministic(t *testing.T) {
	ctx := context.Background()
	s := testSchemaMultiType(t)
	g := graph.New(s)

	// Add instances in arbitrary order
	for i := range 10 {
		p := mustValidInstance(t, s, "Person", []any{string(rune('a' + i))}, map[string]any{
			"id":   string(rune('a' + i)),
			"name": "Person" + string(rune('A'+i)),
		})
		c := mustValidInstance(t, s, "Company", []any{string(rune('z' - i))}, map[string]any{
			"id":   string(rune('z' - i)),
			"name": "Company" + string(rune('Z'-i)),
		})
		_, err := g.Add(ctx, p)
		require.NoError(t, err)
		_, err = g.Add(ctx, c)
		require.NoError(t, err)
	}

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	// Run multiple times and verify identical output
	var outputs [][]byte
	for range 5 {
		data, err := adapter.MarshalObject(result)
		require.NoError(t, err)
		outputs = append(outputs, data)
	}

	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i], "Output should be deterministic")
	}
}

func TestWriteObject_WritesToBuffer(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, inst)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	var buf bytes.Buffer
	n, err := adapter.WriteObject(&buf, result)
	require.NoError(t, err)
	assert.Equal(t, int64(buf.Len()), n)
	assert.Greater(t, n, int64(0))

	// Verify JSON is valid
	var output map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &output))
}

func TestMarshalObject_WithDiagnostics_NoIssues(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, inst)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result, WithDiagnostics(true))
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	// No diagnostics section if there are no issues
	_, hasDiag := output["$diagnostics"]
	assert.False(t, hasDiag, "Should not have $diagnostics when no issues")
}

// TestMarshalObject_ManyAssociationSingleTarget verifies that a many-association
// with exactly one target still serializes as an array, not a scalar.
// This tests schema-based cardinality decision vs runtime count.
func TestMarshalObject_ManyAssociationSingleTarget(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithManyAssociation(t)
	g := graph.New(s)

	// Add single company
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{
		"id":   "c1",
		"name": "Acme Inc",
	})
	_, err := g.Add(ctx, company)
	require.NoError(t, err)

	// Add person with single employer (but relation is many)
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
		map[string]any{
			"id":   "p1",
			"name": "Alice",
		},
		"EMPLOYERS",     // This is a (many) relation
		[][]any{{"c1"}}, // Only one target
	)
	_, err = g.Add(ctx, person)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	persons := output["Person"].([]any)
	person1 := persons[0].(map[string]any)

	// Even with single target, many-relation should produce array
	employers, ok := person1["employers"].([]any)
	require.True(t, ok, "Expected employers to be array even with single target (many cardinality)")
	assert.Len(t, employers, 1)
}

// TestMarshalObject_ManyCompositionSingleChild verifies that a many-composition
// with exactly one child still serializes as an array, not a scalar.
func TestMarshalObject_ManyCompositionSingleChild(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	g := graph.New(s)

	order := mustValidInstance(t, s, "Order", []any{"o1"}, map[string]any{
		"id":       "o1",
		"customer": "Alice",
	})
	_, err := g.Add(ctx, order)
	require.NoError(t, err)

	// Add only one item (but relation is many)
	item := mustValidPartInstance(t, s, "Item", []any{"SKU-A"}, map[string]any{
		"sku": "SKU-A",
		"qty": int64(2),
	})
	_, err = g.AddComposed(ctx, "Order", `["o1"]`, "ITEMS", item)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	orders := output["Order"].([]any)
	order1 := orders[0].(map[string]any)

	// Even with single child, many-composition should produce array
	items, ok := order1["items"].([]any)
	require.True(t, ok, "Expected items to be array even with single child (many cardinality)")
	assert.Len(t, items, 1)
}

// testSchemaWithCamelCaseRelation creates a schema with CamelCase relation names
// to test lower_snake normalization.
func testSchemaWithCamelCaseRelation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("camelcase").
		WithSourceID(location.MustNewSourceID("test://camelcase.yammm")).
		AddType("Proxy").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("url", schema.StringConstraint{}).
		Done().
		AddType("Service").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("HTTPProxy", schema.LocalTypeRef("Proxy", location.Span{}), true, false). // CamelCase
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build camelcase schema: %s", result.String())
	}
	return s
}

// TestMarshalObject_LowerSnakeFieldNames verifies that CamelCase relation names
// are properly converted to lower_snake field names using the schema's FieldName().
func TestMarshalObject_LowerSnakeFieldNames(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithCamelCaseRelation(t)
	g := graph.New(s)

	// Add proxy
	proxy := mustValidInstance(t, s, "Proxy", []any{"px1"}, map[string]any{
		"id":  "px1",
		"url": "http://proxy.example.com",
	})
	_, err := g.Add(ctx, proxy)
	require.NoError(t, err)

	// Add service with HTTPProxy relation
	service := mustValidInstanceWithEdge(t, s, "Service", []any{"svc1"},
		map[string]any{
			"id":   "svc1",
			"name": "API Gateway",
		},
		"HTTPProxy",
		[][]any{{"px1"}},
	)
	_, err = g.Add(ctx, service)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result)
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	services := output["Service"].([]any)
	service1 := services[0].(map[string]any)

	// Should be "http_proxy" (lower_snake), not "httpproxy" (just lowercase)
	_, hasHTTPProxy := service1["http_proxy"]
	assert.True(t, hasHTTPProxy, "Expected http_proxy field (lower_snake from HTTPProxy)")

	_, hasWrongCase := service1["httpproxy"]
	assert.False(t, hasWrongCase, "Should not have httpproxy (wrong normalization)")
}

// TestMarshalObject_Deterministic_MultipleSnapshots tests determinism across
// multiple graph snapshots with different construction orders.
func TestMarshalObject_Deterministic_MultipleSnapshots(t *testing.T) {
	ctx := context.Background()
	s := testSchemaMultiType(t)

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	// Build graph multiple times in different orders and take snapshots
	var outputs [][]byte
	for run := range 3 {
		g := graph.New(s)

		// Vary insertion order based on run
		for i := range 5 {
			idx := (i + run) % 5
			p := mustValidInstance(t, s, "Person", []any{string(rune('a' + idx))}, map[string]any{
				"id":   string(rune('a' + idx)),
				"name": "Person" + string(rune('A'+idx)),
			})
			c := mustValidInstance(t, s, "Company", []any{string(rune('z' - idx))}, map[string]any{
				"id":   string(rune('z' - idx)),
				"name": "Company" + string(rune('Z'-idx)),
			})
			_, err := g.Add(ctx, p)
			require.NoError(t, err)
			_, err = g.Add(ctx, c)
			require.NoError(t, err)
		}

		result := g.Snapshot()
		data, err := adapter.MarshalObject(result)
		require.NoError(t, err)
		outputs = append(outputs, data)
	}

	// All outputs should be identical
	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i],
			"Output should be deterministic across different construction orders")
	}
}

// TestMarshalObject_WithDiagnostics_Unresolved tests that unresolved edges
// are included in the $diagnostics section.
func TestMarshalObject_WithDiagnostics_Unresolved(t *testing.T) {
	ctx := context.Background()
	s := testSchemaWithAssociation(t)
	g := graph.New(s)

	// Add person with edge to non-existent company
	person := mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
		map[string]any{
			"id":   "p1",
			"name": "Alice",
		},
		"EMPLOYER",
		[][]any{{"missing-company"}}, // Target doesn't exist
	)
	_, err := g.Add(ctx, person)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result, WithDiagnostics(true))
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	// Should have $diagnostics section
	diag, ok := output["$diagnostics"].(map[string]any)
	require.True(t, ok, "Expected $diagnostics section")

	unresolved, ok := diag["unresolved"].([]any)
	require.True(t, ok, "Expected unresolved array")
	require.Len(t, unresolved, 1)

	unresolvedEdge := unresolved[0].(map[string]any)
	assert.Equal(t, "Person", unresolvedEdge["source_type"])
	assert.Equal(t, "employer", unresolvedEdge["relation"]) // lower_snake field name
	assert.Equal(t, "Company", unresolvedEdge["target_type"])
}

// TestMarshalObject_WithDiagnostics_Duplicates tests that duplicate records
// are included in the $diagnostics section.
func TestMarshalObject_WithDiagnostics_Duplicates(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	// Add first person
	person1 := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, person1)
	require.NoError(t, err)

	// Add duplicate with same PK
	person2 := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Bob", // Different data but same key
		"age":  int64(25),
	})
	_, err = g.Add(ctx, person2)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	data, err := adapter.MarshalObject(result, WithDiagnostics(true))
	require.NoError(t, err)

	var output map[string]any
	require.NoError(t, json.Unmarshal(data, &output))

	// Should have $diagnostics section
	diag, ok := output["$diagnostics"].(map[string]any)
	require.True(t, ok, "Expected $diagnostics section")

	duplicates, ok := diag["duplicates"].([]any)
	require.True(t, ok, "Expected duplicates array")
	require.Len(t, duplicates, 1)

	dup := duplicates[0].(map[string]any)
	assert.Equal(t, "Person", dup["type"])
}

// TestWriteObject_ShortWrite verifies that WriteObject returns io.ErrShortWrite
// when the writer accepts fewer bytes than provided.
func TestWriteObject_ShortWrite(t *testing.T) {
	ctx := context.Background()
	s := testSchemaSimple(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id":   "p1",
		"name": "Alice",
		"age":  int64(30),
	})
	_, err := g.Add(ctx, inst)
	require.NoError(t, err)

	result := g.Snapshot()

	adapter, err := NewAdapter(nil)
	require.NoError(t, err)

	// Use a limited writer that only accepts first 5 bytes
	lw := &limitedWriter{limit: 5}
	n, err := adapter.WriteObject(lw, result)
	require.Error(t, err)
	assert.ErrorIs(t, err, io.ErrShortWrite)
	assert.Equal(t, int64(5), n)
}

// limitedWriter is a test writer that only accepts up to `limit` bytes.
type limitedWriter struct {
	limit int
	n     int
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	remaining := w.limit - w.n
	if remaining <= 0 {
		return 0, nil // Short write without error (triggers ErrShortWrite)
	}
	if len(p) > remaining {
		w.n += remaining
		return remaining, nil // Short write
	}
	w.n += len(p)
	return len(p), nil
}

func TestParseKeyString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []any
	}{
		{"single string", `["abc"]`, []any{"abc"}},
		{"single int", `[123]`, []any{int64(123)}},
		{"composite with int", `["us",12345]`, []any{"us", int64(12345)}},
		{"empty", `[]`, []any{}},
		{"invalid json fallback", `not-json`, []any{"not-json"}},
		// Issue 9: Integer key preservation tests
		{"large int", `[9007199254740993]`, []any{int64(9007199254740993)}},
		{"float preserved", `[1.5]`, []any{float64(1.5)}},
		{"negative int", `[-42]`, []any{int64(-42)}},
		{"zero", `[0]`, []any{int64(0)}},
		{"mixed types", `["key",123,4.5]`, []any{"key", int64(123), float64(4.5)}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := parseKeyString(tc.input)
			assert.Equal(t, tc.expected, result)
		})
	}
}
