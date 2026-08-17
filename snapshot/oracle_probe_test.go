package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// Probes of the v3 wire's structural contract. Each probe builds a document —
// through the graph API, through [graph.RebuildSnapshot], or by splicing the
// bytes of a marshalled document — drives it through Marshal, Load and
// Verify, and renders what happened as a stable signature string checked
// against probeOutcomes. An entry naming a silent acceptance or a wrong
// binding documents behaviour the wire currently exhibits; one naming a
// diagnostic documents a guard that holds.

// probeOutcomes registers the signature the current tree produces for every
// probe. A production change that moves a signature must move its entry in
// the same commit; the suite is red in both directions until they agree.
var probeOutcomes = map[string]string{
	"duplicate_one_slot_conflict":                  "load[error:E_SNAPSHOT_DANGLING_REFERENCE] verify[error:E_SNAPSHOT_DANGLING_REFERENCE]",
	"duplicate_type_differs_from_conflict":         "load[fatal:E_INTERNAL] verify[ok]",
	"duplicate_relation_without_parent":            "load[error:E_SNAPSHOT_DANGLING_REFERENCE] verify[error:E_SNAPSHOT_DANGLING_REFERENCE]",
	"duplicate_at_depth_two":                       "load[fatal:E_INTERNAL] verify[ok]",
	"types_table_wider_than_instances":             "fixpoint broken: types 2 -> 1",
	"table_row_stale_schema_path":                  "load[ok] verify[ok]; child bound to Part@entry.yammm",
	"table_row_tag_contradicts_identity":           `load[ok] verify[ok]; TypeName="Imposter" TypeID=Anchor@entry.yammm`,
	"root_type_index_contradicts_section":          "load[ok] verify[ok]; bound to section row Anchor@entry.yammm",
	"root_group_key_contradicts_parts_identity":    "load[ok] verify[ok]; went in as Site@entry.yammm, returned as Anchor@entry.yammm",
	"absent_edge_target_type":                      "load[ok] verify[ok]",
	"absent_duplicate_type":                        "load[ok] verify[ok]; 1 duplicates survive",
	"absent_unresolved_source_type":                "load[ok] verify[ok]; 1 unresolved survive",
	"absent_unresolved_target_type":                "load[ok] verify[ok]; target bound to Basin@base.yammm",
	"composed_child_row_ignores_relation_target":   "load[ok] verify[ok]; leaf bound to Root@nested.yammm",
	"duplicate_identity_rows":                      "load[ok] verify[ok]",
	"edge_target_row_out_of_range":                 "load[error:E_SNAPSHOT_DANGLING_REFERENCE error:E_SNAPSHOT_MALFORMED] verify[error:E_SNAPSHOT_DANGLING_REFERENCE]",
	"unresolved_source_row_out_of_range":           "load[error:E_SNAPSHOT_MALFORMED]; snapshot=non-nil with 0 unresolved",
	"composed_child_without_type_diagnostic_count": "load[error:E_SNAPSHOT_MALFORMED x2] verify[error:E_SNAPSHOT_MALFORMED]",
	"zero_identity_instance_marshal":               "rebuild[fatal:E_INTERNAL]",
	"float32_property_round_trip":                  "went in float32, returned float64",
	"duplicate_conflict_row_out_of_range":          "load[error:E_SNAPSHOT_DANGLING_REFERENCE] verify[error:E_SNAPSHOT_DANGLING_REFERENCE]",
	"duplicate_with_edges_and_composed":            "load[error:E_SNAPSHOT_COMPOSED_ON_DUPLICATE error:E_SNAPSHOT_EDGES_ON_DUPLICATE] verify[error:E_SNAPSHOT_COMPOSED_ON_DUPLICATE error:E_SNAPSHOT_EDGES_ON_DUPLICATE]",
	"composed_child_with_edges":                    "load[error:E_SNAPSHOT_INVALID_COMPOSED] verify[error:E_SNAPSHOT_INVALID_COMPOSED]",
	"dangling_edge_target_key":                     "load[error:E_SNAPSHOT_DANGLING_REFERENCE] verify[error:E_SNAPSHOT_DANGLING_REFERENCE]",
	"unparseable_provenance_path":                  "load[warning:E_SNAPSHOT_PATH_FALLBACK] verify[warning:E_SNAPSHOT_PATH_FALLBACK]",
	"composed_nesting_beyond_limit":                "load[error:E_SNAPSHOT_DEPTH_EXCEEDED] verify[error:E_SNAPSHOT_DEPTH_EXCEEDED]",
	"composed_duplicate_second_generation":         `gen2 carries conflict Child@test_comp.yammm[["c1"]] under Parent@test_comp.yammm[["p1"]].CHILDREN`,
}

// sigOf renders a result as sorted severity:code atoms with multiplicities.
func sigOf(res diag.Result) string {
	counts := map[string]int{}
	for issue := range res.Issues() {
		counts[strings.ToLower(issue.Severity().String())+":"+issue.Code().String()]++
	}
	if len(counts) == 0 {
		return "ok"
	}
	atoms := slices.Sorted(maps.Keys(counts))
	for i, a := range atoms {
		if counts[a] > 1 {
			atoms[i] = fmt.Sprintf("%s x%d", a, counts[a])
		}
	}
	return strings.Join(atoms, " ")
}

// checkProbe compares a probe's rendered outcome against its table entry.
func checkProbe(t *testing.T, name, got string) {
	t.Helper()
	want, ok := probeOutcomes[name]
	if !ok {
		t.Fatalf("probe %q has no probeOutcomes entry; the tree produces:\n  %s", name, got)
	}
	if got != want {
		t.Errorf("probe %q outcome moved\n  table: %s\n  tree:  %s", name, want, got)
	}
}

// spliceOnce replaces the sole occurrence of anchor, failing when the anchor
// is absent or ambiguous — a probe that edits the wrong bytes proves nothing.
func spliceOnce(t *testing.T, data []byte, anchor, replacement string) []byte {
	t.Helper()
	switch n := bytes.Count(data, []byte(anchor)); n {
	case 1:
		return bytes.Replace(data, []byte(anchor), []byte(replacement), 1)
	default:
		t.Fatalf("fixture shape changed; anchor %q occurs %d times in:\n%s", anchor, n, data)
		return nil
	}
}

// loadAndVerify renders both read surfaces over one document.
func loadAndVerify(ctx context.Context, t *testing.T, data []byte, s *schema.Schema) (loadSig, verifySig string, loaded *graph.Snapshot) {
	t.Helper()
	snap, loadRes := snapshot.Load(ctx, data, s, snapshot.WithSkipIntegrityCheck())
	verifyRes := snapshot.Verify(ctx, data, s, snapshot.WithSkipIntegrityCheck())
	return sigOf(loadRes), sigOf(verifyRes), snap
}

// marshalParts assembles parts on the identity fixture and marshals them.
func marshalParts(ctx context.Context, t *testing.T, s *schema.Schema, parts graph.SnapshotParts) []byte {
	t.Helper()
	built, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("assembling: %s", result)
	}
	data, result := snapshot.Marshal(ctx, built)
	if err := result.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return data
}

// ident renders an identity for a signature as name@source-basename, so the
// table's entries do not depend on where the repository is checked out.
func ident(id schema.TypeID) string {
	if id.IsZero() {
		return "<zero>"
	}
	return id.Name() + "@" + filepath.Base(id.SchemaPath().String())
}

// slotSchema declares a required-single composition, the one cardinality
// whose duplicate's conflict shares nothing with the rejected child.
func slotSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("slot").
		WithSourceID(location.MustNewSourceID("test://slot.yammm")).
		AddType("Holder").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithComposition("SLOT", schema.NewTypeRef("", "Filler", location.Span{}), true, false).
		Done().
		AddType("Filler").
		AsPart().
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("label", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("slotSchema: %s", result)
	}
	return s
}

// TestWireProbe_DuplicateOneSlotConflict drives the duplicate relation whose
// conflict is the slot's occupant: the rejected filler and the occupant share
// no key, so the round trip must carry the conflict without deriving it from
// the duplicate's own coordinates.
func TestWireProbe_DuplicateOneSlotConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := slotSchema(t)

	g := graph.New(s)
	if res := g.Add(ctx, mustValidInstance(t, s, "Holder", []any{"h1"}, map[string]any{"id": "h1"})); res.HasErrors() {
		t.Fatalf("Add holder: %v", res)
	}
	occupant := mustValidInstance(t, s, "Filler", []any{"f1"}, map[string]any{"id": "f1", "label": "first"})
	if res := g.AddComposed(ctx, "Holder", graph.FormatKey("h1"), "SLOT", occupant); res.HasErrors() {
		t.Fatalf("AddComposed occupant: %v", res)
	}
	rejected := mustValidInstance(t, s, "Filler", []any{"f2"}, map[string]any{"id": "f2", "label": "second"})
	if res := g.AddComposed(ctx, "Holder", graph.FormatKey("h1"), "SLOT", rejected); !res.HasErrors() {
		t.Fatal("a second child in a required-single slot must be rejected")
	}

	snap := g.Snapshot()
	if len(snap.Duplicates()) != 1 {
		t.Fatalf("snapshot holds %d duplicates, want 1", len(snap.Duplicates()))
	}

	data, res := snapshot.Marshal(ctx, snap)
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}
	loadSig, verifySig, _ := loadAndVerify(ctx, t, data, s)
	checkProbe(t, "duplicate_one_slot_conflict", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_DuplicateTypeDiffersFromConflict edits a duplicate record's
// type row: the record's one type field must serve as the duplicate's own
// identity and as its conflict's coordinate at once.
func TestWireProbe_DuplicateTypeDiffersFromConflict(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data, res := snapshot.Marshal(ctx, composedPKCollision(t))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	edited := spliceOnce(t, data, `"duplicates":[{"type":0,`, `"duplicates":[{"type":1,`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_type_differs_from_conflict", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_DuplicateRelationWithoutParent deletes a composed duplicate's
// parent coordinates while keeping its relation, the state the wire writes
// only both-or-neither.
func TestWireProbe_DuplicateRelationWithoutParent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data, res := snapshot.Marshal(ctx, composedPKCollision(t))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	edited := spliceOnce(t, data, `,"parent_type":1,"parent_key":["p1"]`, ``)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_relation_without_parent", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_DuplicateAtDepthTwo records a duplicate whose composing
// parent is itself a composed child. The read-side existence check accepts
// coordinates at any depth; what happens after it decides the outcome.
func TestWireProbe_DuplicateAtDepthTwo(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	data := nestedDoc(ctx, t, s)

	dup := `{"type":0,"key":["l1"],"instance":{"key":["l1"],"type":0,"properties":{},"provenance":null},"parent_type":1,"parent_key":["m1"],"relation":"LEAF"}`
	edited := spliceOnce(t, data, `"duplicates":[]`, `"duplicates":[`+dup+`]`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_at_depth_two", "load["+loadSig+"] verify["+verifySig+"]")
}

// nestedDoc marshals the depth-two composition Root r1 → MID m1 → LEAF l1.
func nestedDoc(ctx context.Context, t *testing.T, s *schema.Schema) []byte {
	t.Helper()
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
	return marshalParts(ctx, t, s, parts)
}

// TestWireProbe_TypesTableWiderThanInstances marshals a snapshot whose Types
// holds a type with no instances, then re-marshals what Load returns. The
// table is the document's denotation, so the row must survive the trip.
func TestWireProbe_TypesTableWiderThanInstances(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")
	basinID := mustTypeIDIn(t, s, "base", "Basin")

	first := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID, basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
		},
	})

	loaded, res := snapshot.Load(ctx, first, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}
	second, res := snapshot.Marshal(ctx, loaded)
	if err := res.Err(); err != nil {
		t.Fatalf("second marshal: %v", err)
	}

	fact := "fixpoint holds"
	if !bytes.Equal(first, second) {
		fact = fmt.Sprintf("fixpoint broken: types %d -> %d", 2, len(loaded.Types()))
	}
	checkProbe(t, "types_table_wider_than_instances", fact)
}

// TestWireProbe_TableRowStaleSchemaPath rewrites a table row's schema path to
// one the closure does not hold. The row's bare name is also declared by the
// entry schema, so what the reader binds says which rule it followed.
func TestWireProbe_TableRowStaleSchemaPath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	siteID := mustTypeIDIn(t, s, "", "Site")
	basePartID := mustTypeIDIn(t, s, "base", "Part")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{siteID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			siteID: {{
				TypeName:   tagForm(s, siteID),
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"s1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "s1"}),
				Composed: map[string][]graph.InstanceParts{
					"IMPORTED": {{
						TypeName:   tagForm(s, basePartID),
						TypeID:     basePartID,
						PrimaryKey: immutable.WrapKey([]any{"bp1"}),
						Properties: immutable.WrapProperties(map[string]any{"name": "bp1", "mass": float64(2)}),
					}},
				},
			}},
		},
	})

	edited := spliceOnce(t, data, `base.yammm","name":"Part"`, `ghost.yammm","name":"Part"`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		if site, ok := loaded.InstanceByKey(siteID, graph.FormatKey("s1")); ok {
			for _, child := range site.Composed("IMPORTED") {
				fact = "; child bound to " + ident(child.TypeID())
			}
		}
	}
	checkProbe(t, "table_row_stale_schema_path", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_TableRowTagContradictsIdentity rewrites a row's tag to a name
// that disagrees with the row's own schema path and name.
func TestWireProbe_TableRowTagContradictsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
		},
	})

	edited := spliceOnce(t, data, `"tag":"Anchor"`, `"tag":"Imposter"`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		if inst, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("a1")); ok {
			fact = fmt.Sprintf("; TypeName=%q TypeID=%s", inst.TypeName(), ident(inst.TypeID()))
		}
	}
	checkProbe(t, "table_row_tag_contradicts_identity", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_RootTypeIndexContradictsSection gives a root instance an
// explicit type index disagreeing with the section row it sits in.
func TestWireProbe_RootTypeIndexContradictsSection(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")
	basinID := mustTypeIDIn(t, s, "base", "Basin")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID, basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
			basinID: {{
				TypeName:   tagForm(s, basinID),
				TypeID:     basinID,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(7)}),
			}},
		},
	})

	edited := spliceOnce(t, data, `{"key":["a1"],"properties"`, `{"key":["a1"],"type":0,"properties"`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		if _, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("a1")); ok {
			fact = "; bound to section row " + ident(anchorID)
		}
		if _, ok := loaded.InstanceByKey(basinID, graph.FormatKey("a1")); ok {
			fact = "; bound to declared index " + ident(basinID)
		}
	}
	checkProbe(t, "root_type_index_contradicts_section", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_RootGroupKeyContradictsPartsIdentity assembles parts whose
// instance carries an identity different from the group it is filed under,
// and asks which one survives the round trip.
func TestWireProbe_RootGroupKeyContradictsPartsIdentity(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")
	siteID := mustTypeIDIn(t, s, "", "Site")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, siteID),
				TypeID:     siteID,
				PrimaryKey: immutable.WrapKey([]any{"x1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "x1"}),
			}},
		},
	})

	loadSig, verifySig, loaded := loadAndVerify(ctx, t, data, s)
	fact := ""
	if loaded != nil {
		if _, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("x1")); ok {
			fact = "; went in as " + ident(siteID) + ", returned as " + ident(anchorID)
		}
		if _, ok := loaded.InstanceByKey(siteID, graph.FormatKey("x1")); ok {
			fact = "; identity preserved as " + ident(siteID)
		}
	}
	checkProbe(t, "root_group_key_contradicts_parts_identity", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// edgeDoc marshals two Basins joined by a NEAR edge.
func edgeDoc(ctx context.Context, t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	basinID := mustTypeIDIn(t, s, "base", "Basin")
	basin := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   tagForm(s, basinID),
			TypeID:     basinID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "area": float64(1)}),
		}
	}
	return marshalParts(ctx, t, s, graph.SnapshotParts{
		Types:     []schema.TypeID{basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{basinID: {basin("b1"), basin("b2")}},
		Edges: []graph.EdgeParts{{
			Relation:   "NEAR",
			SourceType: basinID,
			SourceKey:  immutable.WrapKey([]any{"b1"}),
			TargetType: basinID,
			TargetKey:  immutable.WrapKey([]any{"b2"}),
			Properties: immutable.WrapProperties(map[string]any{"strength": float64(1)}),
		}},
	})
}

// unresolvedDoc marshals one Basin carrying an unresolved NEAR edge.
func unresolvedDoc(ctx context.Context, t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	basinID := mustTypeIDIn(t, s, "base", "Basin")
	return marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{basinID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			basinID: {{
				TypeName:   tagForm(s, basinID),
				TypeID:     basinID,
				PrimaryKey: immutable.WrapKey([]any{"b1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "b1", "area": float64(1)}),
			}},
		},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: basinID,
			SourceKey:  immutable.WrapKey([]any{"b1"}),
			Relation:   "NEAR",
			TargetType: basinID,
			TargetKey:  immutable.WrapKey([]any{"gone"}),
			Reason:     "target_missing",
			Properties: immutable.WrapProperties(map[string]any{"strength": float64(1)}),
		}},
	})
}

// rootDupDoc marshals an Anchor beside a duplicate record naming it.
func rootDupDoc(ctx context.Context, t *testing.T, s *schema.Schema) []byte {
	t.Helper()
	anchorID := mustTypeIDIn(t, s, "", "Anchor")
	inst := graph.InstanceParts{
		TypeName:   tagForm(s, anchorID),
		TypeID:     anchorID,
		PrimaryKey: immutable.WrapKey([]any{"a9"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "a9", "depth": float64(4)}),
	}
	return marshalParts(ctx, t, s, graph.SnapshotParts{
		Types:     []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{anchorID: {inst}},
		Duplicates: []graph.DuplicateParts{{
			Type:         anchorID,
			Key:          immutable.WrapKey([]any{"a9"}),
			Instance:     inst,
			ConflictType: anchorID,
			ConflictKey:  immutable.WrapKey([]any{"a9"}),
		}},
	})
}

// TestWireProbe_AbsentEdgeTargetType deletes an edge's target row. The field
// is a plain int, so its absence and an explicit row 0 are the same bytes to
// the decoder.
func TestWireProbe_AbsentEdgeTargetType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := edgeDoc(ctx, t, s)

	edited := spliceOnce(t, data, `{"target_type":0,`, `{`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "absent_edge_target_type", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_AbsentDuplicateType deletes a duplicate record's type row.
func TestWireProbe_AbsentDuplicateType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := rootDupDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"duplicates":[{"type":0,`, `"duplicates":[{`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		fact = fmt.Sprintf("; %d duplicates survive", len(loaded.Duplicates()))
	}
	checkProbe(t, "absent_duplicate_type", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_AbsentUnresolvedSourceType deletes an unresolved record's
// source row.
func TestWireProbe_AbsentUnresolvedSourceType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := unresolvedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `{"source_type":0,`, `{`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		fact = fmt.Sprintf("; %d unresolved survive", len(loaded.Unresolved()))
	}
	checkProbe(t, "absent_unresolved_source_type", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_AbsentUnresolvedTargetType deletes an unresolved record's
// target row.
func TestWireProbe_AbsentUnresolvedTargetType(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := unresolvedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `,"target_type":0,"target_key"`, `,"target_key"`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		for _, u := range loaded.Unresolved() {
			fact = "; target bound to " + ident(u.TargetType)
		}
	}
	checkProbe(t, "absent_unresolved_target_type", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_ComposedChildRowIgnoresRelationTarget rebinds a composed
// child's row to a type its relation does not target.
func TestWireProbe_ComposedChildRowIgnoresRelationTarget(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	data := nestedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"LEAF":[{"key":["l1"],"type":0,`, `"LEAF":[{"key":["l1"],"type":2,`)
	loadSig, verifySig, loaded := loadAndVerify(ctx, t, edited, s)
	fact := ""
	if loaded != nil {
		rootID := mustTypeID(t, s, "Root")
		if root, ok := loaded.InstanceByKey(rootID, graph.FormatKey("r1")); ok {
			for _, mid := range root.Composed("MID") {
				for _, leaf := range mid.Composed("LEAF") {
					fact = "; leaf bound to " + ident(leaf.TypeID())
				}
			}
		}
	}
	checkProbe(t, "composed_child_row_ignores_relation_target", "load["+loadSig+"] verify["+verifySig+"]"+fact)
}

// TestWireProbe_DuplicateIdentityRows appends a second table row carrying an
// identity the table already holds, with its parallel empty section.
func TestWireProbe_DuplicateIdentityRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
		},
	})

	start := bytes.Index(data, []byte(`"types":[`))
	end := bytes.Index(data, []byte(`],"instances"`))
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("fixture shape changed; types section not found in:\n%s", data)
	}
	row := string(data[start+len(`"types":[`) : end])
	if strings.Contains(row, "},{") {
		t.Fatalf("fixture shape changed; expected one table row, got:\n%s", row)
	}
	edited := spliceOnce(t, data, row+`]`, row+`,`+row+`]`)
	edited = spliceOnce(t, edited, `],"diagnostics":`, `,[]],"diagnostics":`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_identity_rows", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_EdgeTargetRowOutOfRange points an edge at a table row that
// does not exist, on both read surfaces.
func TestWireProbe_EdgeTargetRowOutOfRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := edgeDoc(ctx, t, s)

	edited := spliceOnce(t, data, `{"target_type":0,`, `{"target_type":9,`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "edge_target_row_out_of_range", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_UnresolvedSourceRowOutOfRange points an unresolved record's
// source at a row that does not exist, and reports whether Load's nil-on-error
// contract held.
func TestWireProbe_UnresolvedSourceRowOutOfRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := unresolvedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `{"source_type":0,`, `{"source_type":9,`)
	snap, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
	fact := "; snapshot=nil"
	if snap != nil {
		fact = fmt.Sprintf("; snapshot=non-nil with %d unresolved", len(snap.Unresolved()))
	}
	checkProbe(t, "unresolved_source_row_out_of_range", "load["+sigOf(loadRes)+"]"+fact)
}

// TestWireProbe_ComposedChildWithoutTypeDiagnosticCount counts what one
// missing child index draws from each surface.
func TestWireProbe_ComposedChildWithoutTypeDiagnosticCount(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	data := nestedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"LEAF":[{"key":["l1"],"type":0,`, `"LEAF":[{"key":["l1"],`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "composed_child_without_type_diagnostic_count", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_ZeroIdentityInstanceMarshal marshals a snapshot whose Types
// holds the zero identity.
func TestWireProbe_ZeroIdentityInstanceMarshal(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	built, result := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID, {}},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
		},
	})
	if result.HasErrors() {
		checkProbe(t, "zero_identity_instance_marshal", "rebuild["+sigOf(result)+"]")
		return
	}

	data, res := snapshot.Marshal(ctx, built)
	fact := ""
	if data == nil {
		fact = "; bytes=nil"
	}
	checkProbe(t, "zero_identity_instance_marshal", "marshal["+sigOf(res)+"]"+fact)
}

// TestWireProbe_Float32PropertyRoundTrip sends a float32 property through the
// round trip and reports the dynamic type on each side.
func TestWireProbe_Float32PropertyRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float32(1.5)}),
			}},
		},
	})

	loaded, res := snapshot.Load(ctx, data, s)
	if err := res.Err(); err != nil {
		checkProbe(t, "float32_property_round_trip", "load["+sigOf(res)+"]")
		return
	}
	fact := "depth absent"
	if inst, ok := loaded.InstanceByKey(anchorID, graph.FormatKey("a1")); ok {
		if v, ok := inst.Properties().Get("depth"); ok {
			fact = fmt.Sprintf("went in float32, returned %T", v.Unwrap())
		}
	}
	checkProbe(t, "float32_property_round_trip", fact)
}

// TestWireProbe_DuplicateConflictRowOutOfRange points a duplicate's type at a
// row that does not exist.
func TestWireProbe_DuplicateConflictRowOutOfRange(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := rootDupDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"duplicates":[{"type":0,`, `"duplicates":[{"type":9,`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_conflict_row_out_of_range", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_DuplicateWithEdgesAndComposed gives a duplicate record's
// instance both an edges map and a composed map.
func TestWireProbe_DuplicateWithEdgesAndComposed(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := rootDupDoc(ctx, t, s)

	extra := `"edges":{"E":[{"target_type":0,"target_key":["a9"],"properties":{}}]},"composed":{"C":[{"key":["x"],"type":0,"properties":{},"provenance":null}]},`
	edited := spliceOnce(t, data, `"instance":{"key":["a9"],`, `"instance":{"key":["a9"],`+extra)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "duplicate_with_edges_and_composed", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_ComposedChildWithEdges gives a composed child an edges map.
func TestWireProbe_ComposedChildWithEdges(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	data := nestedDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"LEAF":[{"key":["l1"],`, `"LEAF":[{"key":["l1"],"edges":{"E":[{"target_type":2,"target_key":["r1"],"properties":{}}]},`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "composed_child_with_edges", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_DanglingEdgeTargetKey points an edge at a key no instance
// holds.
func TestWireProbe_DanglingEdgeTargetKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	data := edgeDoc(ctx, t, s)

	edited := spliceOnce(t, data, `"target_key":["b2"]`, `"target_key":["ghost"]`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "dangling_edge_target_key", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_UnparseableProvenancePath replaces an instance's null
// provenance with one whose path cannot parse.
func TestWireProbe_UnparseableProvenancePath(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadIdentitySchema(t)
	anchorID := mustTypeIDIn(t, s, "", "Anchor")

	data := marshalParts(ctx, t, s, graph.SnapshotParts{
		Types: []schema.TypeID{anchorID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			anchorID: {{
				TypeName:   tagForm(s, anchorID),
				TypeID:     anchorID,
				PrimaryKey: immutable.WrapKey([]any{"a1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "a1", "depth": float64(3)}),
			}},
		},
	})

	edited := spliceOnce(t, data, `"provenance":null`, `"provenance":{"source_name":"probe","path":"not a path ["}`)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "unparseable_provenance_path", "load["+loadSig+"] verify["+verifySig+"]")
}

// TestWireProbe_ComposedDuplicateSecondGeneration re-marshals a loaded
// document carrying a composed-child duplicate: the parent coordinates and
// the conflict must survive into the second generation, not only the first.
func TestWireProbe_ComposedDuplicateSecondGeneration(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	first, res := snapshot.Marshal(ctx, composedPKCollision(t))
	if res.HasErrors() {
		t.Fatalf("marshal: %v", res)
	}

	gen1, res := snapshot.Load(ctx, first, s)
	if err := res.Err(); err != nil {
		checkProbe(t, "composed_duplicate_second_generation", "gen1 load["+sigOf(res)+"]")
		return
	}
	second, res := snapshot.Marshal(ctx, gen1)
	if err := res.Err(); err != nil {
		checkProbe(t, "composed_duplicate_second_generation", "gen2 marshal["+sigOf(res)+"]")
		return
	}
	gen2, res := snapshot.Load(ctx, second, s)
	if err := res.Err(); err != nil {
		checkProbe(t, "composed_duplicate_second_generation", "gen2 load["+sigOf(res)+"]")
		return
	}

	fact := "gen2 duplicate lost"
	if dups := gen2.Duplicates(); len(dups) == 1 {
		d := dups[0]
		switch {
		case d.Conflict == nil:
			fact = "gen2 conflict=nil"
		case d.Parent == nil || d.Relation == "":
			fact = fmt.Sprintf("gen2 parent=%v rel=%q", d.Parent != nil, d.Relation)
		default:
			fact = fmt.Sprintf("gen2 carries conflict %s[%s] under %s[%s].%s",
				ident(d.Conflict.TypeID()), d.Conflict.PrimaryKey(),
				ident(d.Parent.TypeID()), d.Parent.PrimaryKey(), d.Relation)
		}
	}
	if !bytes.Equal(first, second) {
		fact += "; bytes moved between generations"
	}
	checkProbe(t, "composed_duplicate_second_generation", fact)
}

// TestWireProbe_ComposedNestingBeyondLimit splices a composed chain deeper
// than the decoder's recursion bound.
func TestWireProbe_ComposedNestingBeyondLimit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, result := schema.LoadString(t.Context(), nestedSchema, "nested.yammm")
	if result.HasErrors() {
		t.Fatalf("load nested schema: %s", result)
	}
	data := nestedDoc(ctx, t, s)

	leaf := `{"key":["l1"],"type":0,"properties":{"id":"l1","label":"deep"},"provenance":null}`
	chain := leaf
	for i := range 35 {
		chain = fmt.Sprintf(`{"key":["d%d"],"type":0,"properties":{},"composed":{"LEAF":[%s]},"provenance":null}`, i, chain)
	}
	edited := spliceOnce(t, data, leaf, chain)
	loadSig, verifySig, _ := loadAndVerify(ctx, t, edited, s)
	checkProbe(t, "composed_nesting_beyond_limit", "load["+loadSig+"] verify["+verifySig+"]")
}
