package json

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

// testSchemaTemporal carries every kind that canonicalizes — Timestamp
// (default and declared layout), UUID and Date — plus a List<Date>, so the
// writer's rendering of each stored form has a fixture.
func testSchemaTemporal(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("temporal").
		WithSourceID(location.MustNewSourceID("test://temporal.yammm")).
		AddType("Event").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("occurred_at", schema.NewTimestampConstraint()).
		WithProperty("logged_at", schema.NewTimestampConstraintFormatted("2006-01-02 15:04:05")).
		WithProperty("run_id", schema.NewUUIDConstraint()).
		WithProperty("installed", schema.NewDateConstraint()).
		WithProperty("service_days", schema.NewListConstraint(schema.NewDateConstraint())).
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
			name:   "temporal_kinds",
			schema: testSchemaTemporal,
			build: func(t *testing.T, s *schema.Schema, g *graph.Graph) {
				t.Helper()
				// Bypass-built from native Go values, so the golden pins the
				// writer's own rendering arm rather than the validator's.
				plusTwo := time.FixedZone("", 2*60*60)
				mustAdd(
					t, g,
					mustValidInstance(t, s, "Event", []any{"e1"}, map[string]any{
						"id":          "e1",
						"occurred_at": time.Date(2026, 8, 19, 12, 0, 0, 500_000_000, time.UTC),
						"logged_at":   time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC),
						"run_id":      uuid.MustParse("0A35EF0F-9D40-4B6B-A0A1-0D1A5A0E1F2B"),
						"installed":   time.Date(2026, 8, 19, 6, 0, 0, 0, time.UTC),
						"service_days": []any{
							time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC),
							"2026-08-20",
						},
					}),
					// A Timestamp keeps its offset; a Date is the calendar day in the
					// value's own location, so 00:30 +02:00 is the 19th, not the 18th.
					mustValidInstance(t, s, "Event", []any{"e2"}, map[string]any{
						"id":          "e2",
						"occurred_at": time.Date(2026, 8, 19, 14, 0, 0, 0, plusTwo),
						"logged_at":   time.Date(2026, 8, 19, 12, 0, 0, 0, plusTwo),
						"installed":   time.Date(2026, 8, 19, 0, 30, 0, 0, plusTwo),
					}),
				)
			},
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

// testSchemaWithTimestamp is the smallest schema carrying a Timestamp property.
func testSchemaWithTimestamp(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("stamped").
		WithSourceID(location.MustNewSourceID("test://stamped.yammm")).
		AddType("Event").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("observed_at", schema.TimestampConstraint{}).
		Done().
		Build()
	return mustBuild(t, s, result)
}

// unwrapValue hands the raw value to encoding/json, so a time.Time-valued
// Timestamp marshals through time.Time.MarshalJSON as strict RFC 3339 while a
// string-valued one marshals verbatim. The JSON writer therefore agrees with
// itself across the two representations where adapter/csv does not.
func TestMarshalObject_TimestampRendersPerRepresentation(t *testing.T) {
	s := testSchemaWithTimestamp(t)
	when := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)

	g := graph.New(s)
	mustAdd(
		t, g,
		mustValidInstance(t, s, "Event", []any{"a"},
			map[string]any{"id": "a", "observed_at": when}),
		mustValidInstance(t, s, "Event", []any{"b"},
			map[string]any{"id": "b", "observed_at": when.Format(time.RFC3339)}),
	)

	data, err := newAdapter(t).MarshalObject(context.Background(), g.Snapshot())
	require.NoError(t, err)

	got := string(data)
	assert.Contains(t, got, `"2020-01-02T03:04:05Z"`)
	assert.NotContains(t, got, "+0000 UTC",
		"the JSON writer must not render a time.Time through fmt.Sprint")
}
