package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// instantKeySchema declares one Run type whose key takes the named constraint, so
// a snapshot written under one spelling of the schema can be imported under
// another — the key migration replayed over a persisted snapshot.
func instantKeySchema(t *testing.T, keyKind string) schema.Schema {
	t.Helper()
	src := `schema "instant_key"

type Run {
	at ` + keyKind + ` primary
}
`
	s, err := schema.LoadString(t.Context(), src, "instant_key.yammm")
	if err != nil {
		t.Fatalf("load %s: %s", keyKind, err)
	}
	return s
}

// instantKeyInstance validates one Run over s. Over the String spelling of the
// schema its raw text reaches the graph as it stands.
func instantKeyInstance(t *testing.T, s schema.Schema, at string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, "Run", map[string]any{"at": at})
}

// An import keeps the address Add stored, in the form the key's kind stores.
func TestNewFromSnapshot_KeepsTheAddressAddStored(t *testing.T) {
	t.Parallel()
	s := instantKeySchema(t, "Timestamp")
	runID := mustTypeID(t, s, "Run")

	src := graph.New(s)
	if err := src.Add(t.Context(), instantKeyInstance(t, s, rawInstant)); err != nil {
		t.Fatalf("add: %s", err)
	}
	before := slices.Collect(src.Snapshot().InstancesOf(runID))[0].PrimaryKey().String()

	after := slices.Collect(graph.NewFromSnapshot(src.Snapshot()).Snapshot().InstancesOf(runID))[0].PrimaryKey().String()
	if after != before {
		t.Errorf("the import moved the address: %s, was %s", after, before)
	}
	if before != graph.FormatKey(canonInstant) {
		t.Errorf("the Add path did not canonicalize: %s", before)
	}
}

// TestAdd_EdgePropertiesAreCanonicalized pins that the Add path stores an
// association edge's own properties in the form the rebuild path stores them,
// so one value reaches the wire one way whichever path built the graph.
func TestAdd_EdgePropertiesAreCanonicalized(t *testing.T) {
	t.Parallel()
	const src = `schema "edge_properties"

type Note {
	id String primary
}

type Doc {
	id String primary
	--> CITES (_) Note {
		seen_at Timestamp
	}
}
`
	s := loadSchemaAt(t, src, "edge_properties.yammm")

	note := instancetest.Valid(t, s, "Note", map[string]any{"id": "n1"})
	doc := instancetest.Valid(t, s, "Doc", map[string]any{
		"id":    "d1",
		"cites": map[string]any{"_target_id": "n1", "seen_at": rawInstant},
	})

	g := graph.New(s)
	if err := g.Add(t.Context(), note); err != nil {
		t.Fatalf("add note: %s", err)
	}
	if err := g.Add(t.Context(), doc); err != nil {
		t.Fatalf("add doc: %s", err)
	}

	edges := slices.Collect(g.Snapshot().Edges())
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	v, ok := edges[0].Properties().Get("seen_at")
	if !ok {
		t.Fatal(`the edge carries no "seen_at" property`)
	}
	if got := v.Any(); got != canonInstant {
		t.Errorf("the Add path stored edge property seen_at = %v, want the canonical %s", got, canonInstant)
	}
}

// composedOneSlotSchema declares a (one) composition beside a (_:many) one.
func composedOneSlotSchema(t *testing.T) schema.Schema {
	t.Helper()
	const src = `schema "composed_one_slot"

type Order {
	id String primary
	*-> ADDRESS (one) Address
	*-> LINES (_:many) Line
}

part type Address {
	street String primary
}

part type Line {
	sku String primary
}
`
	s, err := schema.LoadString(t.Context(), src, "composed_one_slot.yammm")
	if err != nil {
		t.Fatalf("load: %s", err)
	}
	return s
}

func composedOneSlotPart(t *testing.T, s schema.Schema, typeName, prop, key string) snapparts.Instance {
	t.Helper()
	return snapparts.Instance{
		TypeID:     mustTypeID(t, s, typeName),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{prop: key}),
	}
}

// A rebuild holds the children each slot states: one occupant of a (one) slot, and
// as many as a (many) slot carries.
func TestRebuild_HoldsTheChildrenEachSlotStates(t *testing.T) {
	t.Parallel()
	s := composedOneSlotSchema(t)
	orderID := mustTypeID(t, s, "Order")

	parts := func(relation string, children ...snapparts.Instance) snapparts.Parts {
		root := composedOneSlotPart(t, s, "Order", "id", "o1")
		root.Composed = map[string][]snapparts.Instance{relation: children}
		return snapparts.Parts{
			Types: []schema.TypeID{orderID},
			Instances: []snapparts.Instance{
				root,
			},
		}
	}

	count := func(snap graph.Snapshot, relation string) int {
		return slices.Collect(snap.InstancesOf(orderID))[0].ComposedCount(relation)
	}
	if n := count(rebuild(s, parts("ADDRESS",
		composedOneSlotPart(t, s, "Address", "street", "first"))), "ADDRESS"); n != 1 {
		t.Errorf("a (one) slot holds %d children, want 1", n)
	}
	if n := count(rebuild(s, parts("LINES",
		composedOneSlotPart(t, s, "Line", "sku", "a"),
		composedOneSlotPart(t, s, "Line", "sku", "b"))), "LINES"); n != 2 {
		t.Errorf("a (many) slot holds %d children, want 2", n)
	}
}
