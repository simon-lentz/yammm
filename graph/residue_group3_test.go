package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// group3Schema declares one Run type whose key takes the named constraint, so
// a snapshot written under one spelling of the schema can be imported under
// another — the key migration replayed over a persisted snapshot.
func group3Schema(t *testing.T, keyKind string) *schema.Schema {
	t.Helper()
	src := `schema "group3"

type Run {
	at ` + keyKind + ` primary
}
`
	s, res := schema.LoadString(t.Context(), src, "group3.yammm")
	if res.HasErrors() {
		t.Fatalf("load %s: %s", keyKind, res)
	}
	return s
}

func group3Run(t *testing.T, s *schema.Schema, key, prop string) *instance.ValidInstance {
	t.Helper()
	return instance.NewValidInstance("Run", mustTypeID(t, s, "Run"),
		immutable.WrapKey([]any{key}),
		immutable.WrapProperties(map[string]any{"at": prop}),
		nil, nil, nil)
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
	before, after := group3Schema(t, "String"), group3Schema(t, "Timestamp")
	runID := mustTypeID(t, before, "Run")
	if runID != mustTypeID(t, after, "Run") {
		t.Fatal("the two schemas hold different type identities, so this is not one type migrated")
	}

	persisted := graph.New(before)
	if r := persisted.Add(t.Context(), group3Run(t, before, rawInstant, rawInstant)); !r.OK() {
		t.Fatalf("add under the String key: %s", r)
	}
	// The String constraint canonicalizes nothing, so the persisted address is
	// the raw text; that is the premise the rest of the test rests on.
	if got := persisted.Snapshot().InstancesOf(runID)[0].PrimaryKey().String(); got != graph.FormatKey(rawInstant) {
		t.Fatalf("the persisted address is %s, want the raw %s", got, graph.FormatKey(rawInstant))
	}

	g := graph.NewFromSnapshot(after, persisted.Snapshot())
	imported := g.Snapshot()
	want := graph.FormatKey(canonInstant)

	own := imported.InstancesOf(runID)[0].PrimaryKey().String()
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
	res := g.Add(t.Context(), group3Run(t, after, rawInstant, rawInstant))
	if !hasIssueCode(res, diag.E_DUPLICATE_PK) {
		t.Errorf("re-adding the imported instance did not draw E_DUPLICATE_PK: %s", res)
	}
	if n := len(g.Snapshot().InstancesOf(runID)); n != 1 {
		t.Errorf("instances of Run = %d, want 1", n)
	}
}

// TestNewFromSnapshot_ReKeyIsANoOpOnTheCommonPath pins that the re-key changes
// nothing when the schema has not moved: a same-schema import's addresses are
// already canonical, and canon.key is idempotent on them.
func TestNewFromSnapshot_ReKeyIsANoOpOnTheCommonPath(t *testing.T) {
	t.Parallel()
	s := group3Schema(t, "Timestamp")
	runID := mustTypeID(t, s, "Run")

	src := graph.New(s)
	if r := src.Add(t.Context(), group3Run(t, s, rawInstant, rawInstant)); !r.OK() {
		t.Fatalf("add: %s", r)
	}
	before := src.Snapshot().InstancesOf(runID)[0].PrimaryKey().String()

	after := graph.NewFromSnapshot(s, src.Snapshot()).Snapshot().InstancesOf(runID)[0].PrimaryKey().String()
	if after != before {
		t.Errorf("the same-schema import moved the address: %s, was %s", after, before)
	}
	if before != graph.FormatKey(canonInstant) {
		t.Errorf("the Add path did not canonicalize: %s", before)
	}
}

// TestAdd_KeyComponentAgreesAcrossSpellings pins that Graph.Add accepts an
// instance whose key component and key property spell one value two ways. The
// two were compared raw, so Add refused with E_GRAPH_INVALID_PK a record
// RebuildSnapshot accepts and stores consistently — the Add path contradicting
// the model the same range installed, that one value has one spelling.
// Comparing the two raw again turns this red.
func TestAdd_KeyComponentAgreesAcrossSpellings(t *testing.T) {
	t.Parallel()
	s := group3Schema(t, "Timestamp")
	runID := mustTypeID(t, s, "Run")

	g := graph.New(s)
	if res := g.Add(t.Context(), group3Run(t, s, rawInstant, canonInstant)); !res.OK() {
		t.Fatalf("two spellings of one instant were refused as a disagreement: %s", res)
	}
	if got := g.Snapshot().InstancesOf(runID)[0].PrimaryKey().String(); got != graph.FormatKey(canonInstant) {
		t.Errorf("the stored address is %s, want the canonical %s", got, graph.FormatKey(canonInstant))
	}

	// The rebuild path is the control: it accepted this record all along.
	if _, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{runID},
		Instances: map[schema.TypeID][]graph.InstanceParts{runID: {{
			TypeName: "Run", TypeID: runID,
			PrimaryKey: immutable.WrapKey([]any{rawInstant}),
			Properties: immutable.WrapProperties(map[string]any{"at": canonInstant}),
		}}},
	}); res.HasErrors() {
		t.Errorf("RebuildSnapshot refused the record it used to accept: %s", res)
	}
}

// TestAdd_KeyComponentStillRefusesADifferentValue is the counter-control: the
// canonical comparison must not accept a key that genuinely disagrees with its
// property, which is the whole reason the check exists.
func TestAdd_KeyComponentStillRefusesADifferentValue(t *testing.T) {
	t.Parallel()
	s := group3Schema(t, "Timestamp")
	const other = "2021-06-07T08:09:10Z"

	res := graph.New(s).Add(t.Context(), group3Run(t, s, canonInstant, other))
	if res.OK() {
		t.Fatal("a key naming a different instant than its property was accepted")
	}
	if !hasIssueCode(res, diag.E_GRAPH_INVALID_PK) {
		t.Errorf("want E_GRAPH_INVALID_PK; got %s", res)
	}
}

// TestAdd_EdgePropertiesAreCanonicalized pins that the Add path stores an
// association edge's own properties in the form the rebuild path stores them.
// The staged edge passed them through raw while it canonicalized the target
// key beside them, so one value reached the wire two ways depending on which
// path built the graph. It is driven through instance.NewValidInstance, the
// bypass that runs no validation: the Validator canonicalizes first and shows
// this clean.
func TestAdd_EdgePropertiesAreCanonicalized(t *testing.T) {
	t.Parallel()
	const src = `schema "group3_edge"

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
	s, res := schema.LoadString(t.Context(), src, "group3_edge.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	docID, noteID := mustTypeID(t, s, "Doc"), mustTypeID(t, s, "Note")

	note := instance.NewValidInstance("Note", noteID, immutable.WrapKey([]any{"n1"}),
		immutable.WrapProperties(map[string]any{"id": "n1"}), nil, nil, nil)
	doc := instance.NewValidInstance("Doc", docID, immutable.WrapKey([]any{"d1"}),
		immutable.WrapProperties(map[string]any{"id": "d1"}),
		edgeData("CITES", map[string]any{"seen_at": rawInstant}, []any{"n1"}), nil, nil)

	g := graph.New(s)
	if r := g.Add(t.Context(), note); !r.OK() {
		t.Fatalf("add note: %s", r)
	}
	if r := g.Add(t.Context(), doc); !r.OK() {
		t.Fatalf("add doc: %s", r)
	}

	edges := g.Snapshot().Edges()
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
// A-401 names the instance key and the pending target key; the edge properties
// beside them are the same class, and leaving them raw would keep import the
// one path that installs a value in a spelling no other path uses.
func TestNewFromSnapshot_EdgePropertiesAreCanonicalized(t *testing.T) {
	t.Parallel()
	const src = `schema "group3_import_edge"

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
	s, res := schema.LoadString(t.Context(), src, "group3_import_edge.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	docID, noteID := mustTypeID(t, s, "Doc"), mustTypeID(t, s, "Note")

	// Built through RebuildSnapshot with the canonicalizer INACTIVE, so the
	// persisted edge property keeps the raw spelling for the import to meet.
	inert, ires := schema.LoadString(t.Context(), `schema "group3_import_edge"

type Note {
	id String primary
}

type Doc {
	id String primary
	--> CITES (_) Note {
		seen_at String
	}
}
`, "group3_import_edge.yammm")
	if ires.HasErrors() {
		t.Fatalf("load the inert schema: %s", ires)
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{docID, noteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			docID:  {{TypeName: "Doc", TypeID: docID, PrimaryKey: immutable.WrapKey([]any{"d1"}), Properties: immutable.WrapProperties(map[string]any{"id": "d1"})}},
			noteID: {{TypeName: "Note", TypeID: noteID, PrimaryKey: immutable.WrapKey([]any{"n1"}), Properties: immutable.WrapProperties(map[string]any{"id": "n1"})}},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "CITES",
			SourceType: docID, SourceKey: immutable.WrapKey([]any{"d1"}),
			TargetType: noteID, TargetKey: immutable.WrapKey([]any{"n1"}),
			Properties: immutable.WrapProperties(map[string]any{"seen_at": rawInstant}),
		}},
	}
	persisted, pres := graph.RebuildSnapshot(inert, parts)
	if pres.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", pres)
	}
	if v, _ := persisted.Edges()[0].Properties().Get("seen_at"); v.Unwrap() != rawInstant {
		t.Fatalf("the persisted edge property is %v, want the raw %s", v.Unwrap(), rawInstant)
	}

	edges := graph.NewFromSnapshot(s, persisted).Snapshot().Edges()
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

// group3ComposedSchema declares a (one) composition beside a (_:many) one, so
// the cardinality guard has both the shape it refuses and the shape it must
// leave alone.
func group3ComposedSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "group3_composed"

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
	s, res := schema.LoadString(t.Context(), src, "group3_composed.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %s", res)
	}
	return s
}

func group3Part(t *testing.T, s *schema.Schema, typeName, prop, key string) graph.InstanceParts {
	t.Helper()
	return graph.InstanceParts{
		TypeName: typeName, TypeID: mustTypeID(t, s, typeName),
		PrimaryKey: immutable.WrapKey([]any{key}),
		Properties: immutable.WrapProperties(map[string]any{prop: key}),
	}
}

// TestRebuildSnapshot_RefusesASecondOccupantInAOneSlot pins the premise the
// (one) composed key rests on. The composed key segment carries no
// discriminating element for a (one) hop, on the ground that such a slot holds
// exactly one child — and no layer enforced it, so this PUBLIC entry point
// accepted two occupants with no diagnostic and the adapter then minted one
// byte-identical _composed_key for both. Removing the guard turns this red.
func TestRebuildSnapshot_RefusesASecondOccupantInAOneSlot(t *testing.T) {
	t.Parallel()
	s := group3ComposedSchema(t)
	orderID := mustTypeID(t, s, "Order")

	parts := func(relation string, children ...graph.InstanceParts) graph.SnapshotParts {
		root := group3Part(t, s, "Order", "id", "o1")
		root.Composed = map[string][]graph.InstanceParts{relation: children}
		return graph.SnapshotParts{
			Types:     []schema.TypeID{orderID},
			Instances: map[schema.TypeID][]graph.InstanceParts{orderID: {root}},
		}
	}

	_, res := graph.RebuildSnapshot(s, parts("ADDRESS",
		group3Part(t, s, "Address", "street", "first"),
		group3Part(t, s, "Address", "street", "second")))
	if !res.HasErrors() {
		t.Fatal("two occupants of a (one) composition were accepted")
	}
	if !hasIssueCode(res, diag.E_DUPLICATE_COMPOSED_PK) {
		t.Errorf("want E_DUPLICATE_COMPOSED_PK; got %s", res)
	}

	// The controls: one occupant is the shape the slot is for, and a (many)
	// slot carries as many as it likes.
	if _, res := graph.RebuildSnapshot(s, parts("ADDRESS",
		group3Part(t, s, "Address", "street", "first"))); res.HasErrors() {
		t.Errorf("a sole occupant of a (one) composition was refused: %s", res)
	}
	if _, res := graph.RebuildSnapshot(s, parts("LINES",
		group3Part(t, s, "Line", "sku", "a"),
		group3Part(t, s, "Line", "sku", "b"))); res.HasErrors() {
		t.Errorf("two occupants of a (many) composition were refused: %s", res)
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
