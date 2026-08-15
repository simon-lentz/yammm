package snapshot_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Regions of the writer and the reader that a mutation run found no test
// executes. Each test here was written against a surviving mutation, and the
// mutation is named beside it: an assertion whose mutation was never scored is
// how these regions reached the tree covered on paper and dead in fact.

// nestedSchema composes to depth two, which no other fixture does.
const nestedSchema = `schema "nested"

type Root {
	id String primary
	*-> MID (many) Mid
}

part type Mid {
	id String primary
	*-> LEAF (many) Leaf
}

part type Leaf {
	id String primary
	label String
}
`

// crossSchemaEdgeSchema adds a cross-schema relation to the shape
// identity_test.go declares, which has types on both sides and no edge between.
const crossSchemaEdgeEntry = `schema "geo"

import "base.yammm" as base

type Anchor {
	id String primary
	depth Float
	--> DRAINS (_) base.Basin
}
`

const crossSchemaEdgeBase = `schema "base"

type Basin {
	id String primary
	area Float
}
`

func mustProvenance(t *testing.T, name, p string) *location.Provenance {
	t.Helper()
	parsed, err := path.Parse(p)
	if err != nil {
		t.Fatalf("parse provenance path %q: %v", p, err)
	}
	return location.NewProvenance(name, parsed, location.Span{})
}

// Provenance is emitted at every instance position and asserted at none: the
// marshalProvenance call could return nil with the suite green. Rebuilt from
// parts because that is the only entry point that sets provenance directly.
//
// Mutation: w.Provenance = nil in marshalInstanceV3.
func TestMarshal_ProvenanceSurvivesTheRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Provenance: mustProvenance(t, "parents.json", "$.rows[0]"),
				Composed: map[string][]graph.InstanceParts{
					"CHILDREN": {{
						TypeName:   "Child",
						TypeID:     childID,
						PrimaryKey: immutable.WrapKey([]any{"c1"}),
						Properties: immutable.WrapProperties(map[string]any{"id": "c1", "value": "v"}),
						Provenance: mustProvenance(t, "children.json", "$.rows[1]"),
					}},
				},
			}},
		},
	}

	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	data, result := snapshot.Marshal(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), "parents.json") || !strings.Contains(string(data), "children.json") {
		t.Fatalf("marshalled document carries no provenance:\n%s", data)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	root, ok := loaded.InstanceByKey(parentID, graph.FormatKey("p1"))
	if !ok {
		t.Fatal("parent missing after round trip")
	}
	rp := root.Provenance()
	if rp == nil {
		t.Fatal("root provenance is nil after the round trip")
	}
	if rp.SourceName() != "parents.json" {
		t.Errorf("root provenance source = %q, want %q", rp.SourceName(), "parents.json")
	}

	children := root.Composed("CHILDREN")
	if len(children) != 1 {
		t.Fatalf("CHILDREN = %d, want 1", len(children))
	}
	cp := children[0].Provenance()
	if cp == nil {
		t.Fatal("composed-child provenance is nil after the round trip")
	}
	if cp.SourceName() != "children.json" {
		t.Errorf("child provenance source = %q, want %q", cp.SourceName(), "children.json")
	}
}

// No test marshals a composed child that has its own composed children, so the
// recursion is exercised to depth 1 and dropped grandchildren stay green.
//
// Mutation: cw.Composed = nil after the recursive marshalInstanceV3 call.
func TestMarshal_NestedCompositionSurvivesToDepthTwo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	rootID := mustTypeID(t, s, "Root")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{rootID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			rootID: {{
				TypeName:   "Root",
				TypeID:     rootID,
				PrimaryKey: immutable.WrapKey([]any{"r1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "r1"}),
				Composed: map[string][]graph.InstanceParts{
					"MID": {{
						TypeName:   "Mid",
						TypeID:     mustTypeID(t, s, "Mid"),
						PrimaryKey: immutable.WrapKey([]any{"m1"}),
						Properties: immutable.WrapProperties(map[string]any{"id": "m1"}),
						Composed: map[string][]graph.InstanceParts{
							"LEAF": {{
								TypeName:   "Leaf",
								TypeID:     mustTypeID(t, s, "Leaf"),
								PrimaryKey: immutable.WrapKey([]any{"l1"}),
								Properties: immutable.WrapProperties(map[string]any{"id": "l1", "label": "deep"}),
							}},
						},
					}},
				},
			}},
		},
	}

	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	data, result := snapshot.Marshal(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"deep"`) {
		t.Fatalf("the depth-2 child never reached the wire:\n%s", data)
	}

	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	root, ok := loaded.InstanceByKey(rootID, graph.FormatKey("r1"))
	if !ok {
		t.Fatal("root missing after round trip")
	}
	mids := root.Composed("MID")
	if len(mids) != 1 {
		t.Fatalf("MID = %d, want 1", len(mids))
	}
	leaves := mids[0].Composed("LEAF")
	if len(leaves) != 1 {
		t.Fatalf("LEAF = %d, want 1 — the grandchild was dropped between marshal and load", len(leaves))
	}
	if got, ok := leaves[0].Properties().Get("label"); !ok || got.Unwrap() != "deep" {
		t.Errorf("grandchild label = %v, want %q", got.Unwrap(), "deep")
	}
}

// Marshal's instances-section error branch is never driven and marshalFatalErr
// is 0% covered. buildTypeTable deletes the zero TypeID from the table, so a
// snapshot whose Types carries one reaches the table miss the branch reports.
//
// Mutation: delete the `if err != nil` block after marshalInstancesV3.
func TestMarshal_InstancesSectionErrorIsReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{{}},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			{}: {{
				TypeName:   "Ghost",
				PrimaryKey: immutable.WrapKey([]any{"g1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "g1"}),
			}},
		},
	}
	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Skipf("RebuildSnapshot refuses a zero-identity instance, so this branch is unreachable from here: %s", result)
	}

	data, result := snapshot.Marshal(ctx, built)
	if data != nil {
		t.Errorf("Marshal returned %d bytes for a snapshot it cannot write", len(data))
	}
	if !result.HasErrors() {
		t.Fatal("Marshal accepted a snapshot whose type is absent from the table")
	}
	if !hasCode(result, diag.E_INTERNAL) {
		t.Errorf("result = %s, want an E_INTERNAL naming the failed section", result)
	}
}

// No test round-trips a resolved edge whose target is an instance of an
// imported type, so the edge target's identity is unexercised across a schema
// boundary — the position most likely to lose an alias.
//
// Mutation: targetID := writerTypeID(inst, s) in marshalInstanceV3's edge loop.
func TestRoundTrip_ResolvedEdgeToImportedTypeTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadSourcesWithEntry(t.Context(), map[string][]byte{
		"entry.yammm": []byte(crossSchemaEdgeEntry),
		"base.yammm":  []byte(crossSchemaEdgeBase),
	}, "entry.yammm", ".", schema.WithSourcesOnly())
	if result.HasErrors() {
		t.Fatalf("load cross-schema-with-edge fixture: %s", result)
	}

	anchorID := mustTypeIDIn(t, s, "", "Anchor")
	basinID := mustTypeIDIn(t, s, "base", "Basin")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{anchorID, basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   "Anchor",
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
			basinID: {{
				TypeName:   "base.Basin",
				TypeID:     basinID,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
			}},
		},
		Edges: []graph.EdgeParts{{
			Relation:   "DRAINS",
			SourceType: anchorID,
			SourceKey:  immutable.WrapKey([]any{"a1"}),
			TargetType: basinID,
			TargetKey:  immutable.WrapKey([]any{"b1"}),
			Properties: immutable.WrapProperties(nil),
		}},
	}

	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	data, result := snapshot.Marshal(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, result := snapshot.Load(ctx, data, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}

	anchor, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("a1"))
	if !ok {
		t.Fatal("anchor missing after round trip")
	}
	edges := loaded.EdgesFrom(anchor)
	if len(edges) != 1 {
		t.Fatalf("edges from anchor = %d, want 1", len(edges))
	}
	target := edges[0].Target()
	if target.TypeID() != basinID {
		t.Errorf("edge target identity = %s, want %s — the cross-schema target lost its identity",
			target.TypeID(), basinID)
	}
	if target.PrimaryKey().String() != graph.FormatKey("b1") {
		t.Errorf("edge target key = %s, want %s", target.PrimaryKey().String(), graph.FormatKey("b1"))
	}
}

// UpdateMetadata preserves a legacy int-shaped body while Marshal(Load(x))
// heals it. The wire contract documents that divergence and nothing drove it.
//
// Mutation: have UpdateMetadata rebuild the body rather than reuse it.
func TestUpdateMetadata_PreservesALegacyIntShapedBodyThatReMarshalHeals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})

	// MarshalLegacyV2 shares the emitter and cannot write the int-shaped form a
	// pre-v0.12 writer produced, so it is put back by hand and re-sealed.
	written, result := snapshot.MarshalLegacyV2(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("MarshalLegacyV2: %v", err)
	}
	if !bytes.Contains(written, []byte(`"reading":2.0`)) {
		t.Fatalf("fixture does not carry the healed form to un-heal:\n%s", written)
	}
	legacy, result := snapshot.UpdateMetadata(ctx,
		bytes.Replace(written, []byte(`"reading":2.0`), []byte(`"reading":2`), 1), nil)
	if err := result.Err(); err != nil {
		t.Fatalf("re-sealing the legacy fixture: %v", err)
	}
	if !bytes.Contains(legacy, []byte(`"reading":2,`)) {
		t.Fatalf("legacy fixture is not int-shaped:\n%s", legacy)
	}

	updated, result := snapshot.UpdateMetadata(ctx, legacy, map[string]string{"stage": "done"})
	if err := result.Err(); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	loaded, result := snapshot.Load(ctx, legacy, s)
	if err := result.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	healed, result := snapshot.Marshal(ctx, loaded)
	if err := result.Err(); err != nil {
		t.Fatalf("re-marshal: %v", err)
	}

	// The divergence itself: one path keeps the body it was given, the other
	// re-encodes it under the schema and gains the float indicator.
	if !bytes.Contains(updated, []byte(`"reading":2,`)) {
		t.Errorf("UpdateMetadata healed the body; it must reuse the input's bytes verbatim:\n%s", updated)
	}
	if !bytes.Contains(healed, []byte(`"reading":2.0`)) {
		t.Errorf("Marshal(Load(x)) did not heal the int-shaped float:\n%s", healed)
	}
}

// The v2 decoder validates composed children through a recursion no test
// drives, so its depth limit and its edges-on-a-composed-child invariant are
// both dead. v3's equivalent is covered; v2's is not, and v2 is a read path
// this package keeps forever.
//
// Mutation: drop the sd.processInstance recursion in decoder.go.
func TestLoad_LegacyComposedChildWithEdgesIsReported(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})

	written, result := snapshot.MarshalLegacyV2(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("MarshalLegacyV2: %v", err)
	}

	// A composed child must not carry edges. RebuildSnapshot will not build one
	// that does, so the shape is injected and the document re-sealed.
	child := []byte(`{"key":["p1"],"properties":{"name":"p1","weight":3.0},"provenance":null}`)
	withEdges := []byte(`{"key":["p1"],"properties":{"name":"p1","weight":3.0},` +
		`"edges":{"FEED":[{"target_type":"Station","target_key":["st1"],"properties":{}}]},` +
		`"provenance":null}`)
	if !bytes.Contains(written, child) {
		t.Fatalf("legacy composed child is not in the shape this test injects into:\n%s", written)
	}
	tampered, result := snapshot.UpdateMetadata(ctx, bytes.Replace(written, child, withEdges, 1), nil)
	if err := result.Err(); err != nil {
		t.Fatalf("re-sealing the tampered fixture: %v", err)
	}

	_, result = snapshot.Load(ctx, tampered, s)
	if !hasCode(result, diag.E_SNAPSHOT_INVALID_COMPOSED) {
		t.Errorf("result = %s, want E_SNAPSHOT_INVALID_COMPOSED — the composed-child "+
			"edge invariant is never reached on the v2 path", result)
	}
}

// wireEdgeProps' contract is that a resolved and an unresolved edge on one
// relation encode their properties identically. They agree because both derive
// the relation from the source instance's own identity, not because they share
// the helper — so the property is worth holding rather than assuming.
// buildWireTestSnapshot carries both shapes of a FEED edge with gain = 2.
//
// Mutation: derive the unresolved site's rel from u.TargetType.
func TestMarshal_ResolvedAndUnresolvedEdgePropsEncodeAlike(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})

	data, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// gain is Float-constrained, so both positions emit the indicator or one of
	// them encoded under a relation that does not declare the property.
	if n := strings.Count(string(data), `"gain":2.0`); n != 2 {
		t.Errorf(`"gain":2.0 appears %d times, want 2 — the resolved and unresolved `+
			"edges encoded one value two ways:\n%s", n, data)
	}
}
