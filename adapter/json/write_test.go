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
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Test Schema Builders

// mustBuild finishes a schema build, failing the test on any build error.
func mustBuild(t *testing.T, s *schema.Schema, result interface{ HasErrors() bool }) *schema.Schema {
	t.Helper()
	if result.HasErrors() {
		t.Fatalf("failed to build test schema")
	}
	return s
}

func testSchemaSimple(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("simple").
		WithSourceID(location.MustNewSourceID("test://simple.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithProperty("age", schema.IntegerConstraint{}).
		Done().
		Build()
	return mustBuild(t, s, result)
}

func testSchemaMultiType(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

func testSchemaWithAssociation(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

func testSchemaWithManyAssociation(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

func testSchemaWithComposition(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

func testSchemaWithOneComposition(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

// testSchemaWithCamelCaseRelation creates a schema with CamelCase relation
// names to test lower_snake field-name normalization.
func testSchemaWithCamelCaseRelation(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
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
	return mustBuild(t, s, result)
}

// Instance creation helpers

// mustTypeID resolves a schema type's ID, failing the test if absent.
func mustTypeID(t *testing.T, s *schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	return typ.ID()
}

func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
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
	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, targetKey := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(
			immutable.WrapKey(targetKey),
			immutable.WrapProperties(nil),
		)
	}
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(map[string]*instance.ValidEdgeData{relationName: instance.NewValidEdgeData(targets)}),
	)
}

// mustAdd adds instances to the graph, failing fast on any add issue so a
// broken fixture cannot silently produce an empty graph.
func mustAdd(t *testing.T, g *graph.Graph, instances ...*instance.ValidInstance) {
	t.Helper()
	for _, inst := range instances {
		if err := g.Add(context.Background(), inst).Err(); err != nil {
			t.Fatalf("graph.Add(%s): %v", inst.TypeName(), err)
		}
	}
}

// mustAddComposed attaches a composed part instance, failing fast on issues.
func mustAddComposed(t *testing.T, g *graph.Graph, parentType, parentKey, relation string, part *instance.ValidInstance) {
	t.Helper()
	if err := g.AddComposed(context.Background(), parentType, parentKey, relation, part).Err(); err != nil {
		t.Fatalf("graph.AddComposed(%s.%s): %v", parentType, relation, err)
	}
}

// marshalToMap marshals the snapshot and decodes the bytes for structural
// assertions.
func marshalToMap(t *testing.T, a *Adapter, result *graph.Snapshot, opts ...WriteOption) map[string]any {
	t.Helper()
	data, err := a.MarshalObject(context.Background(), result, opts...)
	if err != nil {
		t.Fatalf("MarshalObject: %v", err)
	}
	var output map[string]any
	if err := json.Unmarshal(data, &output); err != nil {
		t.Fatalf("unmarshal output: %v", err)
	}
	return output
}

// Tests

func TestMarshalObject_NilResult(t *testing.T) {
	adapter := newAdapter(t)

	_, err := adapter.MarshalObject(context.Background(), nil)
	require.Error(t, err)
	assert.Equal(t, ErrNilResult, err)
}

func TestWriteObject_NilResult(t *testing.T) {
	adapter := newAdapter(t)

	var buf bytes.Buffer
	_, err := adapter.WriteObject(context.Background(), &buf, nil)
	require.Error(t, err)
	assert.Equal(t, ErrNilResult, err)
}

// TestMarshalObject_Golden pins the complete marshalled output per graph
// shape: key order, FK encoding (single = key array, many = array of key
// arrays — even with one target), composition inlining (one = object,
// many = array), lower_snake field naming, and the $diagnostics section.
func TestMarshalObject_Golden(t *testing.T) {
	cases := []struct {
		name   string
		schema func(*testing.T) *schema.Schema
		build  func(*testing.T, *schema.Schema, *graph.Graph)
		opts   []WriteOption
	}{
		{
			name:   "empty_graph",
			schema: testSchemaSimple,
			build:  func(*testing.T, *schema.Schema, *graph.Graph) {},
		},
		{
			name:   "single_type",
			schema: testSchemaSimple,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
					"id": "p1", "name": "Alice", "age": int64(30),
				}))
			},
		},
		{
			name:   "multiple_instances",
			schema: testSchemaSimple,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice", "age": int64(30)}),
					mustValidInstance(t, s, "Person", []any{"p2"}, map[string]any{"id": "p2", "name": "Bob", "age": int64(25)}),
				)
			},
		},
		{
			name:   "multiple_types",
			schema: testSchemaMultiType,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"id": "p1", "name": "Alice"}),
					mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme Inc"}),
				)
			},
		},
		{
			name:   "with_edge",
			schema: testSchemaWithAssociation,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme Inc"}),
					mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
						map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"c1"}}),
				)
			},
		},
		{
			name:   "with_many_edges",
			schema: testSchemaWithManyAssociation,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme Inc"}),
					mustValidInstance(t, s, "Company", []any{"c2"}, map[string]any{"id": "c2", "name": "Beta Corp"}),
					mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
						map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYERS", [][]any{{"c1"}, {"c2"}}),
				)
			},
		},
		{
			name:   "many_association_single_target",
			schema: testSchemaWithManyAssociation,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				// A many-relation with one target must still serialize as an
				// array (schema cardinality, not runtime count).
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme Inc"}),
					mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
						map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYERS", [][]any{{"c1"}}),
				)
			},
		},
		{
			name:   "with_composition",
			schema: testSchemaWithComposition,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, mustValidInstance(t, s, "Order", []any{"o1"}, map[string]any{
					"id": "o1", "customer": "Alice",
				}))
				mustAddComposed(t, g, "Order", `["o1"]`, "ITEMS",
					mustValidInstance(t, s, "Item", []any{"SKU-A"}, map[string]any{"sku": "SKU-A", "qty": int64(2)}))
				mustAddComposed(t, g, "Order", `["o1"]`, "ITEMS",
					mustValidInstance(t, s, "Item", []any{"SKU-B"}, map[string]any{"sku": "SKU-B", "qty": int64(1)}))
			},
		},
		{
			name:   "many_composition_single_child",
			schema: testSchemaWithComposition,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, mustValidInstance(t, s, "Order", []any{"o1"}, map[string]any{
					"id": "o1", "customer": "Alice",
				}))
				mustAddComposed(t, g, "Order", `["o1"]`, "ITEMS",
					mustValidInstance(t, s, "Item", []any{"SKU-A"}, map[string]any{"sku": "SKU-A", "qty": int64(2)}))
			},
		},
		{
			name:   "with_one_composition",
			schema: testSchemaWithOneComposition,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, mustValidInstance(t, s, "Car", []any{"VIN123"}, map[string]any{
					"vin": "VIN123", "model": "Sedan",
				}))
				mustAddComposed(t, g, "Car", `["VIN123"]`, "ENGINE",
					mustValidInstance(t, s, "Engine", []any{"ENG001"}, map[string]any{"serial": "ENG001", "displacement": int64(2000)}))
			},
		},
		{
			name:   "lower_snake_field_names",
			schema: testSchemaWithCamelCaseRelation,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				// HTTPProxy must serialize as http_proxy (lower_snake), never
				// httpproxy.
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Proxy", []any{"px1"}, map[string]any{"id": "px1", "url": "http://proxy.example.com"}),
					mustValidInstanceWithEdge(t, s, "Service", []any{"svc1"},
						map[string]any{"id": "svc1", "name": "API Gateway"}, "HTTPProxy", [][]any{{"px1"}}),
				)
			},
		},
		{
			name:   "diagnostics_unresolved",
			schema: testSchemaWithAssociation,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, mustValidInstanceWithEdge(t, s, "Person", []any{"p1"},
					map[string]any{"id": "p1", "name": "Alice"}, "EMPLOYER", [][]any{{"missing-company"}}))
			},
			opts: []WriteOption{WithDiagnostics(true)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.schema(t)
			g := graph.New(s)
			tc.build(t, s, g)

			adapter := newAdapter(t)
			data, err := adapter.MarshalObject(context.Background(), g.Snapshot(), append(tc.opts, WithIndent("  "))...)
			if err != nil {
				t.Fatalf("MarshalObject: %v", err)
			}
			yammmtest.Golden(t, "marshal_"+tc.name, append(data, '\n'))
		})
	}
}

// TestMarshalObject_DiagnosticsDuplicates needs a raw g.Add: adding the
// duplicate is the scenario, so the fail-fast helper does not apply.
func TestMarshalObject_DiagnosticsDuplicates(t *testing.T) {
	ctx := t.Context()
	s := testSchemaSimple(t)
	g := graph.New(s)

	mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Alice", "age": int64(30),
	}))
	// Duplicate PK — expected to be reported, not fatal.
	g.Add(ctx, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Bob", "age": int64(25),
	}))

	adapter := newAdapter(t)
	data, err := adapter.MarshalObject(context.Background(), g.Snapshot(), WithDiagnostics(true), WithIndent("  "))
	require.NoError(t, err)
	yammmtest.Golden(t, "marshal_diagnostics_duplicates", append(data, '\n'))
}

func TestMarshalObject_WithDiagnostics_NoIssues(t *testing.T) {
	s := testSchemaSimple(t)
	g := graph.New(s)
	mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Alice", "age": int64(30),
	}))

	output := marshalToMap(t, newAdapter(t), g.Snapshot(), WithDiagnostics(true))

	// No diagnostics section if there are no issues.
	_, hasDiag := output["$diagnostics"]
	assert.False(t, hasDiag, "Should not have $diagnostics when no issues")
}

func TestMarshalObject_WithIndent(t *testing.T) {
	s := testSchemaSimple(t)
	g := graph.New(s)
	mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Alice", "age": int64(30),
	}))
	result := g.Snapshot()

	adapter := newAdapter(t)

	// Compact by default.
	compact, err := adapter.MarshalObject(context.Background(), result)
	require.NoError(t, err)
	assert.NotContains(t, string(compact), "\n")

	// Pretty with tabs.
	pretty, err := adapter.MarshalObject(context.Background(), result, WithIndent("\t"))
	require.NoError(t, err)
	assert.Contains(t, string(pretty), "\n")
	assert.Contains(t, string(pretty), "\t")

	// Pretty with spaces.
	prettySpaces, err := adapter.MarshalObject(context.Background(), result, WithIndent("  "))
	require.NoError(t, err)
	assert.Contains(t, string(prettySpaces), "\n")
	assert.Contains(t, string(prettySpaces), "  ")
}

func TestMarshalObject_Deterministic(t *testing.T) {
	s := testSchemaMultiType(t)
	g := graph.New(s)

	// Add instances in arbitrary order.
	for i := range 10 {
		mustAdd(
			t, g,
			mustValidInstance(t, s, "Person", []any{string(rune('a' + i))}, map[string]any{
				"id": string(rune('a' + i)), "name": "Person" + string(rune('A'+i)),
			}),
			mustValidInstance(t, s, "Company", []any{string(rune('z' - i))}, map[string]any{
				"id": string(rune('z' - i)), "name": "Company" + string(rune('Z'-i)),
			}),
		)
	}
	result := g.Snapshot()

	adapter := newAdapter(t)

	var outputs [][]byte
	for range 5 {
		data, err := adapter.MarshalObject(context.Background(), result)
		require.NoError(t, err)
		outputs = append(outputs, data)
	}
	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i], "Output should be deterministic")
	}
}

// TestMarshalObject_Deterministic_MultipleSnapshots tests determinism across
// graphs built in different insertion orders.
func TestMarshalObject_Deterministic_MultipleSnapshots(t *testing.T) {
	s := testSchemaMultiType(t)
	adapter := newAdapter(t)

	var outputs [][]byte
	for run := range 3 {
		g := graph.New(s)
		for i := range 5 {
			idx := (i + run) % 5
			mustAdd(
				t, g,
				mustValidInstance(t, s, "Person", []any{string(rune('a' + idx))}, map[string]any{
					"id": string(rune('a' + idx)), "name": "Person" + string(rune('A'+idx)),
				}),
				mustValidInstance(t, s, "Company", []any{string(rune('z' - idx))}, map[string]any{
					"id": string(rune('z' - idx)), "name": "Company" + string(rune('Z'-idx)),
				}),
			)
		}

		data, err := adapter.MarshalObject(context.Background(), g.Snapshot())
		require.NoError(t, err)
		outputs = append(outputs, data)
	}

	for i := 1; i < len(outputs); i++ {
		assert.Equal(t, outputs[0], outputs[i],
			"Output should be deterministic across different construction orders")
	}
}

func TestWriteObject_WritesToBuffer(t *testing.T) {
	s := testSchemaSimple(t)
	g := graph.New(s)
	mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Alice", "age": int64(30),
	}))

	adapter := newAdapter(t)

	var buf bytes.Buffer
	n, err := adapter.WriteObject(context.Background(), &buf, g.Snapshot())
	require.NoError(t, err)
	assert.Equal(t, int64(buf.Len()), n)
	assert.Positive(t, n)

	var output map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &output))
}

// TestWriteObject_ShortWrite verifies that WriteObject returns io.ErrShortWrite
// when the writer accepts fewer bytes than provided.
func TestWriteObject_ShortWrite(t *testing.T) {
	s := testSchemaSimple(t)
	g := graph.New(s)
	mustAdd(t, g, mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{
		"id": "p1", "name": "Alice", "age": int64(30),
	}))

	adapter := newAdapter(t)

	lw := &limitedWriter{limit: 5}
	n, err := adapter.WriteObject(context.Background(), lw, g.Snapshot())
	require.Error(t, err)
	require.ErrorIs(t, err, io.ErrShortWrite)
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

func TestMarshalInstance_NilInstance(t *testing.T) {
	t.Parallel()
	a := newAdapter(t)

	_, err := a.MarshalInstance(context.Background(), nil, nil)
	assert.ErrorIs(t, err, ErrNilInstance)
}

func TestMarshalInstance_Properties(t *testing.T) {
	t.Parallel()
	s := testSchemaSimple(t)
	a := newAdapter(t)

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Person", instance.RawInstance{
		Properties: map[string]any{"id": "p1", "name": "Alice", "age": int64(30)},
	})
	require.True(t, result.OK(), result.String())

	st, _ := s.Type("Person")
	data, err := a.MarshalInstance(context.Background(), valid, st)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(data, &obj))
	assert.Equal(t, "p1", obj["id"])
	assert.Equal(t, "Alice", obj["name"])
	assert.Equal(t, float64(30), obj["age"]) // JSON numbers are float64
}

func TestMarshalInstance_WithEdge(t *testing.T) {
	t.Parallel()
	s := testSchemaWithAssociation(t)
	a := newAdapter(t)

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Person", instance.RawInstance{
		Properties: map[string]any{
			"id":       "p1",
			"name":     "Alice",
			"employer": map[string]any{"_target_id": "c1"},
		},
	})
	require.True(t, result.OK(), result.String())

	st, _ := s.Type("Person")
	data, err := a.MarshalInstance(context.Background(), valid, st)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(data, &obj))
	assert.Equal(t, "p1", obj["id"])
	// FK is serialized as the key array (single cardinality).
	assert.Equal(t, []any{"c1"}, obj["employer"])
}

func TestMarshalInstance_WithManyEdges(t *testing.T) {
	t.Parallel()
	s := testSchemaWithManyAssociation(t)
	a := newAdapter(t)

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Person", instance.RawInstance{
		Properties: map[string]any{
			"id":   "p1",
			"name": "Alice",
			"employers": []any{
				map[string]any{"_target_id": "c1"},
				map[string]any{"_target_id": "c2"},
			},
		},
	})
	require.True(t, result.OK(), result.String())

	st, _ := s.Type("Person")
	data, err := a.MarshalInstance(context.Background(), valid, st)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(data, &obj))
	// Many cardinality: array of key arrays.
	fks, ok := obj["employers"].([]any)
	require.True(t, ok, "employers should be array, got %T", obj["employers"])
	assert.Len(t, fks, 2)
}

func TestMarshalInstance_WithIndent(t *testing.T) {
	t.Parallel()
	s := testSchemaSimple(t)
	a := newAdapter(t)

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Person", instance.RawInstance{
		Properties: map[string]any{"id": "p1", "name": "Bob", "age": int64(25)},
	})
	require.True(t, result.OK(), result.String())

	st, _ := s.Type("Person")
	data, err := a.MarshalInstance(context.Background(), valid, st, WithIndent("  "))
	require.NoError(t, err)

	assert.Contains(t, string(data), "\n")
	assert.Contains(t, string(data), "  ")
}

func TestMarshalInstance_NilSchemaType(t *testing.T) {
	t.Parallel()
	s := testSchemaSimple(t)
	a := newAdapter(t)

	v := instance.NewValidator(s)
	valid, result := v.ValidateOne(context.Background(), "Person", instance.RawInstance{
		Properties: map[string]any{"id": "p1", "name": "Carol", "age": int64(40)},
	})
	require.True(t, result.OK(), result.String())

	// Pass nil schemaType — should still serialize properties.
	data, err := a.MarshalInstance(context.Background(), valid, nil)
	require.NoError(t, err)

	var obj map[string]any
	require.NoError(t, json.Unmarshal(data, &obj))
	assert.Equal(t, "p1", obj["id"])
	assert.Equal(t, "Carol", obj["name"])
}
