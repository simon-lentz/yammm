package snapshot_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Round-trip and divergence pins for writer and reader regions no other test
// file drives. Each test names the mutation that turns it red.

// nestedSchema composes to depth two.
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

// Provenance must survive the round trip at every instance position. Rebuilt
// from parts because that is the only entry point that sets provenance
// directly.
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

// A composed child's own composed children must survive the round trip.
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

// A resolved edge whose target is an instance of an imported type must keep
// the target's identity across the schema boundary — the position most likely
// to lose an alias.
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

// UpdateMetadata preserves an int-shaped body while Marshal(Load(x)) heals
// it — the divergence the wire contract documents.
//
// Mutation: have UpdateMetadata rebuild the body rather than reuse it.
func TestUpdateMetadata_PreservesAnIntShapedBodyThatReMarshalHeals(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadWireTestSchema(t)
	snap := buildWireTestSnapshot(t, s, float64(2), []any{float64(1)})

	// Marshal always writes the healed form, so the int shape is put back by
	// hand and the document re-sealed.
	written, result := snapshot.Marshal(ctx, snap)
	if err := result.Err(); err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if !bytes.Contains(written, []byte(`"reading":2.0`)) {
		t.Fatalf("fixture does not carry the healed form to un-heal:\n%s", written)
	}
	intShaped, result := snapshot.UpdateMetadata(ctx,
		bytes.Replace(written, []byte(`"reading":2.0`), []byte(`"reading":2`), 1), nil)
	if err := result.Err(); err != nil {
		t.Fatalf("re-sealing the int-shaped fixture: %v", err)
	}
	if !bytes.Contains(intShaped, []byte(`"reading":2,`)) {
		t.Fatalf("fixture is not int-shaped:\n%s", intShaped)
	}

	updated, result := snapshot.UpdateMetadata(ctx, intShaped, map[string]string{"stage": "done"})
	if err := result.Err(); err != nil {
		t.Fatalf("UpdateMetadata: %v", err)
	}

	loaded, result := snapshot.Load(ctx, intShaped, s)
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
