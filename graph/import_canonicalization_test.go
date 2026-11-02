package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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
	s, res := schema.LoadString(t.Context(), src, "instant_key.yammm")
	if res.HasErrors() {
		t.Fatalf("load %s: %s", keyKind, res)
	}
	return s
}

// instantKeyInstance validates one Run over s. Over the String spelling of the
// schema its raw text reaches the graph as it stands.
func instantKeyInstance(t *testing.T, s schema.Schema, at string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, "Run", map[string]any{"at": at})
}

// TestNewFromSnapshot_ReKeysUnderTheImportingSchema pins that an imported
// instance is addressable, by every spelling, under the schema doing the
// importing. A snapshot persisted while the key was a String carries the raw
// text; replayed under a schema that made the key a Timestamp, the import
// installed that text as the address while every lookup canonicalized, so the
// instance was addressable by NOTHING — not even its own carried key — and a
// re-Add duplicated it silently. Removing the re-key turns this red.
func TestNewFromSnapshot_ReKeysUnderTheImportingSchema(t *testing.T) {
	t.Parallel()
	before, after := instantKeySchema(t, "String"), instantKeySchema(t, "Timestamp")
	runID := mustTypeID(t, before, "Run")
	if runID != mustTypeID(t, after, "Run") {
		t.Fatal("the two schemas hold different type identities, so this is not one type migrated")
	}

	persisted := graph.New(before)
	if r := persisted.Add(t.Context(), instantKeyInstance(t, before, rawInstant)); !r.OK() {
		t.Fatalf("add under the String key: %s", r)
	}
	// The String constraint canonicalizes nothing, so the persisted address is
	// the raw text; that is the premise the rest of the test rests on.
	if got := slices.Collect(persisted.Snapshot().InstancesOf(runID))[0].PrimaryKey().String(); got != graph.FormatKey(rawInstant) {
		t.Fatalf("the persisted address is %s, want the raw %s", got, graph.FormatKey(rawInstant))
	}

	g := mustImport(t, after, persisted.Snapshot())
	imported := g.Snapshot()
	want := graph.FormatKey(canonInstant)

	own := slices.Collect(imported.InstancesOf(runID))[0].PrimaryKey().String()
	if own != want {
		t.Errorf("the imported instance carries %s, want the canonical %s", own, want)
	}
	for _, sp := range []struct{ name, key string }{
		{"the instance's own key", own},
		{"FormatKey(canonical)", want},
		{"FormatKey(raw)", graph.FormatKey(rawInstant)},
	} {
		if _, ok := imported.InstanceByKey(runID, sp.key); !ok {
			t.Errorf("InstanceByKey by %s (%s) missed", sp.name, sp.key)
		}
	}

	// The second half of the defect: with the index holding one spelling and
	// Add canonicalizing another, the re-Add found no conflict.
	res := g.Add(t.Context(), instantKeyInstance(t, after, rawInstant))
	if !hasIssueCode(res, diag.E_DUPLICATE_PK) {
		t.Errorf("re-adding the imported instance did not draw E_DUPLICATE_PK: %s", res)
	}
	if n := len(slices.Collect(g.Snapshot().InstancesOf(runID))); n != 1 {
		t.Errorf("instances of Run = %d, want 1", n)
	}
}

// TestNewFromSnapshot_ReKeyIsANoOpOnTheCommonPath pins that the re-key changes
// nothing when the schema has not moved: a same-schema import's addresses are
// already canonical, and canon.key is idempotent on them.
func TestNewFromSnapshot_ReKeyIsANoOpOnTheCommonPath(t *testing.T) {
	t.Parallel()
	s := instantKeySchema(t, "Timestamp")
	runID := mustTypeID(t, s, "Run")

	src := graph.New(s)
	if r := src.Add(t.Context(), instantKeyInstance(t, s, rawInstant)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	before := slices.Collect(src.Snapshot().InstancesOf(runID))[0].PrimaryKey().String()

	after := slices.Collect(mustImport(t, s, src.Snapshot()).Snapshot().InstancesOf(runID))[0].PrimaryKey().String()
	if after != before {
		t.Errorf("the same-schema import moved the address: %s, was %s", after, before)
	}
	if before != graph.FormatKey(canonInstant) {
		t.Errorf("the Add path did not canonicalize: %s", before)
	}
}

// A key component and a key property that spell one value two ways store one
// spelling.
func TestRebuild_KeyComponentAgreesAcrossSpellings(t *testing.T) {
	t.Parallel()
	s := loadSchemaAt(t, `schema "key_and_property"

type Run {
	at Timestamp primary
	alt Timestamp
}
`, "key_and_property.yammm")
	runID := mustTypeID(t, s, "Run")

	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{runID},
		Instances: []snapparts.Instance{
			{
				TypeID:     runID,
				PrimaryKey: immutable.WrapKey([]any{rawInstant}),
				Properties: immutable.WrapProperties(map[string]any{"at": canonInstant}),
			},
		},
	})
	inst := slices.Collect(snap.InstancesOf(runID))[0]
	if got, want := inst.PrimaryKey().String(), graph.FormatKey(canonInstant); got != want {
		t.Errorf("key = %s, want %s", got, want)
	}
	if at, _ := inst.Property("at"); at.Unwrap() != canonInstant {
		t.Errorf("at = %v, want %s", at.Unwrap(), canonInstant)
	}
}

// Under an importing schema that makes the key a Timestamp, a key and a key
// property that spell one instant two ways agree. Comparing the two raw turns this
// red.
func TestNewFromSnapshot_AKeyAndItsPropertyAgreeAcrossSpellings(t *testing.T) {
	t.Parallel()
	before, after := instantKeySchema(t, "String"), instantKeySchema(t, "Timestamp")
	runID := mustTypeID(t, before, "Run")
	persisted := rebuild(before, snapparts.Parts{Instances: []snapparts.Instance{{
		TypeID:     runID,
		PrimaryKey: immutable.WrapKey([]any{rawInstant}),
		Properties: immutable.WrapProperties(map[string]any{"at": canonInstant}),
	}}})
	imported := mustImport(t, after, persisted).Snapshot()
	if got, want := slices.Collect(imported.InstancesOf(runID))[0].PrimaryKey().String(), graph.FormatKey(canonInstant); got != want {
		t.Errorf("imported key = %s, want %s", got, want)
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
	if r := g.Add(t.Context(), note); !r.OK() {
		t.Fatalf("add note: %s", r)
	}
	if r := g.Add(t.Context(), doc); !r.OK() {
		t.Fatalf("add doc: %s", r)
	}

	edges := slices.Collect(g.Snapshot().Edges())
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	v, ok := edges[0].Properties().Get("seen_at")
	if !ok {
		t.Fatal(`the edge carries no "seen_at" property`)
	}
	if got := v.Unwrap(); got != canonInstant {
		t.Errorf("the Add path stored edge property seen_at = %v, want the canonical %s", got, canonInstant)
	}
}

// TestNewFromSnapshot_EdgePropertiesAreCanonicalized pins that the import path
// stores an edge's own properties in the form every other path stores them.
// The instance key and the pending target key are canonicalized on import
// too. A raw edge property would make import the one path that installs a
// value in a spelling no other path uses.
func TestNewFromSnapshot_EdgePropertiesAreCanonicalized(t *testing.T) {
	t.Parallel()
	const src = `schema "import_edge_properties"

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
	s, res := schema.LoadString(t.Context(), src, "import_edge_properties.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	docID, noteID := mustTypeID(t, s, "Doc"), mustTypeID(t, s, "Note")

	// Rebuilt with the canonicalizer INACTIVE, so the persisted edge property keeps
	// the raw spelling for the import to meet.
	inert, ires := schema.LoadString(t.Context(), `schema "import_edge_properties"

type Note {
	id String primary
}

type Doc {
	id String primary
	--> CITES (_) Note {
		seen_at String
	}
}
`, "import_edge_properties.yammm")
	if ires.HasErrors() {
		t.Fatalf("load the inert schema: %s", ires)
	}
	parts := snapparts.Parts{
		Types: []schema.TypeID{docID, noteID},
		Instances: []snapparts.Instance{
			{TypeID: docID, PrimaryKey: immutable.WrapKey([]any{"d1"}), Properties: immutable.WrapProperties(map[string]any{"id": "d1"})},
			{TypeID: noteID, PrimaryKey: immutable.WrapKey([]any{"n1"}), Properties: immutable.WrapProperties(map[string]any{"id": "n1"})},
		},
		Edges: []snapparts.Edge{{
			Relation:   "CITES",
			SourceType: docID, SourceKey: immutable.WrapKey([]any{"d1"}),
			TargetKey:  immutable.WrapKey([]any{"n1"}),
			Properties: immutable.WrapProperties(map[string]any{"seen_at": rawInstant}),
		}},
	}
	persisted := rebuild(inert, parts)
	if v, _ := slices.Collect(persisted.Edges())[0].Properties().Get("seen_at"); v.Unwrap() != rawInstant {
		t.Fatalf("the persisted edge property is %v, want the raw %s", v.Unwrap(), rawInstant)
	}

	edges := slices.Collect(mustImport(t, s, persisted).Snapshot().Edges())
	if len(edges) != 1 {
		t.Fatalf("edges = %d, want 1", len(edges))
	}
	v, ok := edges[0].Properties().Get("seen_at")
	if !ok {
		t.Fatal(`the imported edge carries no "seen_at" property`)
	}
	if got := v.Unwrap(); got != canonInstant {
		t.Errorf("the import path stored edge property seen_at = %v, want the canonical %s", got, canonInstant)
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
	s, res := schema.LoadString(t.Context(), src, "composed_one_slot.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
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

func hasIssueCode(res diag.Result, code diag.Code) bool {
	for is := range res.Issues() {
		if is.Code() == code {
			return true
		}
	}
	return false
}
