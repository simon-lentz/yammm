package json

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"math"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/snapparts"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// Test Schema Builders

// mustBuild finishes a schema build, failing the test on any build error.
func mustBuild(t *testing.T, s schema.Schema, result interface{ HasErrors() bool }) schema.Schema {
	t.Helper()
	if result.HasErrors() {
		t.Fatalf("failed to build test schema")
	}
	return s
}

func testSchemaSimple(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "simple"

type Person {
	id String primary
	name String required
	age Integer required
}
`, "simple.yammm")
	return mustBuild(t, s, result)
}

func testSchemaMultiType(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "multi"

type Person {
	id String primary
	name String required
}

type Company {
	id String primary
	name String required
}
`, "multi.yammm")
	return mustBuild(t, s, result)
}

func testSchemaWithAssociation(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "association"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (_:one) Company
}
`, "association.yammm")
	return mustBuild(t, s, result)
}

func testSchemaWithManyAssociation(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "many_association"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYERS (many) Company
}
`, "many_association.yammm")
	return mustBuild(t, s, result)
}

func testSchemaWithComposition(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "composition"

part type Item {
	sku String primary
	qty Integer required
}

type Order {
	id String primary
	customer String required
	*-> ITEMS (many) Item
}
`, "composition.yammm")
	return mustBuild(t, s, result)
}

func testSchemaWithOneComposition(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "one_composition"

part type Engine {
	serial String primary
	displacement Integer required
}

type Car {
	vin String primary
	model String required
	*-> ENGINE (_:one) Engine
}
`, "one_composition.yammm")
	return mustBuild(t, s, result)
}

// testSchemaWithCamelCaseRelation creates a schema whose relation name lowers
// to a multi-word field name, to pin that the writer keys on the field name.
func testSchemaWithCamelCaseRelation(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "camelcase"

type Proxy {
	id String primary
	url String required
}

type Service {
	id String primary
	name String required
	--> HTTP_PROXY (_:one) Proxy
}
`, "camelcase.yammm")
	return mustBuild(t, s, result)
}

// testSchemaFloats declares a Float as a property, under an alias, as a List's
// elements and as a Vector's, beside two Integers: one holding an integer,
// which takes no indicator, and one holding a float, which keeps it because the
// indicator follows the value held.
func testSchemaFloats(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "floats"

type Ratio = Float

type Reading {
	id String primary
	ratio Float
	scaled Ratio
	ratios List<Float>
	at Vector[2]
	narrow Float
	count Integer
	held Integer
}
`, "floats.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	return s
}

// testSchemaTemporal carries every kind that canonicalizes — Timestamp
// (default and declared layout), UUID and Date — plus a List<Date>, so the
// writer's rendering of each stored form has a fixture.
func testSchemaTemporal(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "temporal"

type Event {
	id String primary
	occurred_at Timestamp required
	logged_at Timestamp["2006-01-02 15:04:05"] required
	run_id UUID
	installed Date required
	service_days List<Date>
}
`, "temporal.yammm")
	return mustBuild(t, s, result)
}

// Instance creation helpers

// mustTypeID resolves a schema type's ID, failing the test if absent.
func mustTypeID(t *testing.T, s schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	return typ.ID()
}

// mustAdd adds instances to the graph, failing fast on any add issue so a
// broken fixture cannot silently produce an empty graph.
func mustAdd(t *testing.T, g *graph.Graph, instances ...instance.ValidInstance) {
	t.Helper()
	for _, inst := range instances {
		if err := g.Add(context.Background(), inst).Err(); err != nil {
			t.Fatalf("graph.Add(%s): %v", inst.TypeName(), err)
		}
	}
}

// Tests

func TestMarshalObject_NilResult(t *testing.T) {
	adapter := newAdapter(t)

	_, err := adapter.MarshalObject(context.Background(), graph.Snapshot{})
	require.Error(t, err)
	assert.Equal(t, ErrZeroSnapshot, err)
}

func TestWriteObject_NilResult(t *testing.T) {
	adapter := newAdapter(t)

	var buf bytes.Buffer
	_, err := adapter.WriteObject(context.Background(), &buf, graph.Snapshot{})
	require.Error(t, err)
	assert.Equal(t, ErrZeroSnapshot, err)
}

// TestMarshalObject_Golden pins the complete marshalled output per graph
// shape: key order, an association as a _target_-keyed object ((one) = one
// object, (many) = an array of them, even with one target), a composition as an
// array of child objects for every multiplicity, and field naming.
func TestMarshalObject_Golden(t *testing.T) {
	cases := []struct {
		name   string
		schema func(*testing.T) schema.Schema
		build  func(*testing.T, schema.Schema, *graph.Graph)
		snap   func(*testing.T, schema.Schema) graph.Snapshot
		opts   []WriteOption
	}{
		{
			name:   "empty_graph",
			schema: testSchemaSimple,
			build:  func(*testing.T, schema.Schema, *graph.Graph) {},
		},
		{
			name:   "single_type",
			schema: testSchemaSimple,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, instancetest.Valid(t, s, "Person", map[string]any{
					"id": "p1", "name": "Alice", "age": int64(30),
				}))
			},
		},
		{
			name:   "multiple_instances",
			schema: testSchemaSimple,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "Alice", "age": int64(30)}),
					instancetest.Valid(t, s, "Person", map[string]any{"id": "p2", "name": "Bob", "age": int64(25)}),
				)
			},
		},
		{
			name:   "multiple_types",
			schema: testSchemaMultiType,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "Alice"}),
					instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "Acme Inc"}),
				)
			},
		},
		{
			name:   "with_edge",
			schema: testSchemaWithAssociation,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "Acme Inc"}),
					instancetest.Valid(t, s, "Person", map[string]any{
						"id": "p1", "name": "Alice", "employer": map[string]any{"_target_id": "c1"},
					}),
				)
			},
		},
		{
			name:   "with_many_edges",
			schema: testSchemaWithManyAssociation,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "Acme Inc"}),
					instancetest.Valid(t, s, "Company", map[string]any{"id": "c2", "name": "Beta Corp"}),
					instancetest.Valid(t, s, "Person", map[string]any{
						"id": "p1", "name": "Alice",
						"employers": []any{map[string]any{"_target_id": "c1"}, map[string]any{"_target_id": "c2"}},
					}),
				)
			},
		},
		{
			name:   "many_association_single_target",
			schema: testSchemaWithManyAssociation,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				// A many-relation with one target must still serialize as an
				// array (schema cardinality, not runtime count).
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Company", map[string]any{"id": "c1", "name": "Acme Inc"}),
					instancetest.Valid(t, s, "Person", map[string]any{
						"id": "p1", "name": "Alice", "employers": []any{map[string]any{"_target_id": "c1"}},
					}),
				)
			},
		},
		{
			name:   "with_composition",
			schema: testSchemaWithComposition,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, instancetest.Valid(t, s, "Order", map[string]any{
					"id": "o1", "customer": "Alice",
					"items": []any{
						map[string]any{"sku": "SKU-A", "qty": int64(2)},
						map[string]any{"sku": "SKU-B", "qty": int64(1)},
					},
				}))
			},
		},
		{
			name:   "many_composition_single_child",
			schema: testSchemaWithComposition,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, instancetest.Valid(t, s, "Order", map[string]any{
					"id": "o1", "customer": "Alice",
					"items": []any{map[string]any{"sku": "SKU-A", "qty": int64(2)}},
				}))
			},
		},
		{
			name:   "with_one_composition",
			schema: testSchemaWithOneComposition,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				mustAdd(t, g, instancetest.Valid(t, s, "Car", map[string]any{
					"vin": "VIN123", "model": "Sedan",
					"engine": []any{map[string]any{"serial": "ENG001", "displacement": int64(2000)}},
				}))
			},
		},
		{
			name:   "lower_snake_field_names",
			schema: testSchemaWithCamelCaseRelation,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				// HTTP_PROXY must serialize under its field name, http_proxy.
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Proxy", map[string]any{"id": "px1", "url": "http://proxy.example.com"}),
					instancetest.Valid(t, s, "Service", map[string]any{
						"id": "svc1", "name": "API Gateway", "http_proxy": map[string]any{"_target_id": "px1"},
					}),
				)
			},
		},
		{
			name:   "temporal_kinds",
			schema: testSchemaTemporal,
			build: func(t *testing.T, s schema.Schema, g *graph.Graph) {
				t.Helper()
				// Native Go values, which reach the writer in their stored form:
				// the golden pins the text a native value exports as, end to end.
				plusTwo := time.FixedZone("", 2*60*60)
				mustAdd(
					t, g,
					instancetest.Valid(t, s, "Event", map[string]any{
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
					instancetest.Valid(t, s, "Event", map[string]any{
						"id":          "e2",
						"occurred_at": time.Date(2026, 8, 19, 14, 0, 0, 0, plusTwo),
						"logged_at":   time.Date(2026, 8, 19, 12, 0, 0, 0, plusTwo),
						"installed":   time.Date(2026, 8, 19, 0, 30, 0, 0, plusTwo),
					}),
				)
			},
		},
		{
			// A Float as a property, under an alias, in a List and in a Vector — an
			// edge property's is pinned by its own test — and every spelling that
			// decides what the indicator has to do: a whole value, a fraction, a negative
			// zero, and exponents encoding/json writes in both notations. A float held
			// under an Integer keeps its indicator. A float under an Integer is no
			// validated value, so a document holds these.
			name:   "float_kinds",
			schema: testSchemaFloats,
			snap: func(t *testing.T, s schema.Schema) graph.Snapshot {
				t.Helper()
				return snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{
					heldRoot(t, s, "Reading", "r1", map[string]any{
						"id":     "r1",
						"ratio":  float64(1),
						"scaled": float64(2), // whole, so the alias must resolve for the indicator to appear
						"ratios": []any{float64(3), float64(-0.25), math.Copysign(0, -1), float64(1e-7)},
						"at":     []any{float64(1e21), float64(5)},
						"narrow": 0.10000000149011612,
						"count":  int64(4),
						"held":   float64(6),
					}),
				}})
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := tc.schema(t)
			var snap graph.Snapshot
			if tc.snap != nil {
				snap = tc.snap(t, s)
			} else {
				g := graph.New(s)
				tc.build(t, s, g)
				snap = g.Snapshot()
			}

			adapter := newAdapter(t)
			data, err := adapter.MarshalObject(context.Background(), snap, append(tc.opts, WithIndent("  "))...)
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
	mustAdd(t, g, instancetest.Valid(t, s, "Person", map[string]any{
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
			instancetest.Valid(t, s, "Person", map[string]any{
				"id": string(rune('a' + i)), "name": "Person" + string(rune('A'+i)),
			}),
			instancetest.Valid(t, s, "Company", map[string]any{
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
				instancetest.Valid(t, s, "Person", map[string]any{
					"id": string(rune('a' + idx)), "name": "Person" + string(rune('A'+idx)),
				}),
				instancetest.Valid(t, s, "Company", map[string]any{
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
	mustAdd(t, g, instancetest.Valid(t, s, "Person", map[string]any{
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
	mustAdd(t, g, instancetest.Valid(t, s, "Person", map[string]any{
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
func testSchemaWithTimestamp(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "stamped"

type Event {
	id String primary
	observed_at Timestamp required
}
`, "stamped.yammm")
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
		instancetest.Valid(t, s, "Event", map[string]any{"id": "a", "observed_at": when}),
		instancetest.Valid(t, s, "Event", map[string]any{"id": "b", "observed_at": when.Format(time.RFC3339)}),
	)

	data, err := newAdapter(t).MarshalObject(context.Background(), g.Snapshot())
	require.NoError(t, err)

	got := string(data)
	assert.Contains(t, got, `"2020-01-02T03:04:05Z"`)
	assert.NotContains(t, got, "+0000 UTC",
		"the JSON writer must not render a time.Time through fmt.Sprint")
}
