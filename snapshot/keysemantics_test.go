package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// An empty key and an absent key are one state. immutable.Key.Len() and
// immutable.Key.String() already collapse them, so every position that reads a
// key must branch on length. These tests pin the two boundaries where the
// distinction leaked: the reader's conflict-address arm, and the string form
// graph.UnresolvedEdge.TargetKey carries.

// TestRoundTrip_AbsentAssociationKeepsEmptyTargetKey pins the documented
// contract that UnresolvedEdge.TargetKey is empty for the "absent" and "empty"
// reasons. The wire writes no target key for those reasons, so the value has to
// survive a round trip as "" rather than as the "[]" a keyless key renders to.
func TestRoundTrip_AbsentAssociationKeepsEmptyTargetKey(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchema(t)

	// Person declares a required EMPLOYER association and this instance carries
	// no edge for it, so the snapshot holds one "absent" unresolved record.
	snap := buildSnapshot(t, s,
		mustValidInstance(t, s, "Person", []any{"p1"}, map[string]any{"name": "Alice"}))

	built := snap.Unresolved()
	if len(built) != 1 {
		t.Fatalf("fixture is vacuous: %d unresolved records, want 1", len(built))
	}
	if built[0].Reason != "absent" {
		t.Fatalf("fixture is vacuous: reason %q, want %q", built[0].Reason, "absent")
	}
	if built[0].TargetKey != "" {
		t.Fatalf("precondition: built TargetKey = %q, want empty", built[0].TargetKey)
	}

	data, res := snapshot.Marshal(ctx, snap)
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	loaded, res := snapshot.Load(ctx, data, s)
	if err := res.Err(); err != nil {
		t.Fatalf("load: %v", err)
	}

	got := loaded.Unresolved()
	if len(got) != 1 {
		t.Fatalf("loaded %d unresolved records, want 1", len(got))
	}
	if got[0].Reason != "absent" {
		t.Errorf("loaded reason = %q, want %q", got[0].Reason, "absent")
	}
	if got[0].TargetKey != "" {
		t.Errorf("loaded TargetKey = %q, want empty — a keyless key rendered as a key", got[0].TargetKey)
	}
}

// composedSiblingCollision builds a parent whose CHILDREN slot holds two
// children, then rejects a third colliding with the first. The conflict is one
// of two occupants, so only a stated key can address it.
func composedSiblingCollision(ctx context.Context, t *testing.T, s *schema.Schema) *graph.Snapshot {
	t.Helper()

	g := graph.New(s)
	if res := g.Add(ctx, mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Root"})); res.HasErrors() {
		t.Fatalf("add parent: %v", res)
	}
	for _, key := range []string{"c1", "c2"} {
		child := mustValidInstance(t, s, "Child", []any{key}, map[string]any{"value": key})
		if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", child); res.HasErrors() {
			t.Fatalf("add child %s: %v", key, res)
		}
	}
	collider := mustValidInstance(t, s, "Child", []any{"c1"}, map[string]any{"value": "second"})
	if res := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "CHILDREN", collider); !res.HasErrors() {
		t.Fatal("a colliding child key must report a duplicate")
	}

	snap := g.Snapshot()
	if n := len(snap.Duplicates()); n != 1 {
		t.Fatalf("snapshot holds %d duplicates, want 1", n)
	}
	return snap
}

// spliceConflictBlock rewrites the conflict block of the document's single
// duplicate record. Marshal never writes an empty conflict key — it omits the
// key for a keyless conflict — so these shapes exist only by splicing.
func spliceConflictBlock(t *testing.T, data []byte, replacement string) []byte {
	t.Helper()
	const opener = `"conflict":{`
	at := bytes.Index(data, []byte(opener))
	if at < 0 {
		t.Fatalf("fixture shape changed; no conflict block in:\n%s", data)
	}
	end := bytes.IndexByte(data[at:], '}')
	if end < 0 {
		t.Fatalf("fixture shape changed; unterminated conflict block in:\n%s", data)
	}

	edited := append([]byte{}, data[:at]...)
	edited = append(edited, []byte(opener+replacement)...)
	edited = append(edited, data[at+end:]...)
	if bytes.Equal(edited, data) {
		t.Fatal("splice was a no-op; the fixture already carries the replacement")
	}
	return edited
}

// childRow returns the types-table row index the given type name occupies.
func childRow(t *testing.T, data []byte, name string) int {
	t.Helper()
	for i, row := range wireTypeTable(t, data) {
		if row.Name == name {
			return i
		}
	}
	t.Fatalf("fixture carries no %s row:\n%s", name, data)
	return -1
}

func emptyConflictKeyDoc(t *testing.T) []byte {
	t.Helper()
	data := v3Doc(t)
	return spliceConflictBlock(t, data,
		fmt.Sprintf(`"type":%d,"key":[]`, childRow(t, data, "Child")))
}

// TestLoad_EmptyConflictKeyRefusesUnaddressableSlot pins both halves of the
// sole-occupant clamp. An empty key addresses the slot alone, which says
// nothing when the slot holds two children, and nothing when its one occupant
// is not the type the conflict block states. Either shape is a malformed
// address and draws a dangling-reference Error.
func TestLoad_EmptyConflictKeyRefusesUnaddressableSlot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	siblings := composedSiblingCollision(ctx, t, s)
	soleOccupant := v3Doc(t)

	t.Run("two occupants", func(t *testing.T) {
		t.Parallel()
		data, res := snapshot.Marshal(ctx, siblings)
		if err := res.Err(); err != nil {
			t.Fatalf("marshal: %v", err)
		}
		edited := spliceConflictBlock(t, data,
			fmt.Sprintf(`"type":%d,"key":[]`, childRow(t, data, "Child")))

		_, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
		if !hasCode(loadRes, diag.E_SNAPSHOT_DANGLING_REFERENCE) {
			t.Errorf("an empty conflict key addressing a two-child slot loaded without %s: %v",
				diag.E_SNAPSHOT_DANGLING_REFERENCE, loadRes)
		}
	})

	t.Run("sole occupant of another type", func(t *testing.T) {
		t.Parallel()
		data := soleOccupant
		edited := spliceConflictBlock(t, data,
			fmt.Sprintf(`"type":%d,"key":[]`, childRow(t, data, "Parent")))

		_, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
		if !hasCode(loadRes, diag.E_SNAPSHOT_DANGLING_REFERENCE) {
			t.Errorf("an empty conflict key naming the wrong type row loaded without %s: %v",
				diag.E_SNAPSHOT_DANGLING_REFERENCE, loadRes)
		}
	})
}

// TestLoadVerify_EmptyConflictKeyAgreeWithTheGraph pins the reader against
// graph.resolveDuplicateConflict on the one input where they used to disagree.
// An empty conflict key addresses the slot alone, which resolves when the slot
// has a sole occupant of the stated type. A reader that split on nil-ness
// instead took the keyed arm here, found no occupant whose key renders "[]",
// and refused a document the graph model accepts.
func TestLoadVerify_EmptyConflictKeyAgreeWithTheGraph(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	edited := emptyConflictKeyDoc(t)

	loaded, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithSkipIntegrityCheck())
	verifyRes := snapshot.Verify(ctx, edited, s, snapshot.WithSkipIntegrityCheck())

	if loadRes.HasErrors() {
		t.Errorf("Load refused an empty conflict key the graph model resolves: %v", loadRes)
	}
	if verifyRes.HasErrors() {
		t.Errorf("Verify refused an empty conflict key the graph model resolves: %v", verifyRes)
	}
	if loadRes.HasErrors() != verifyRes.HasErrors() {
		t.Errorf("Load and Verify disagree: load errors=%v, verify errors=%v",
			loadRes.HasErrors(), verifyRes.HasErrors())
	}
	if loaded == nil {
		t.Fatal("Load returned no snapshot")
	}
	if n := len(loaded.Duplicates()); n != 1 {
		t.Fatalf("loaded %d duplicates, want 1", n)
	}
	if conflict := loaded.Duplicates()[0].Conflict; conflict == nil {
		t.Error("the duplicate's conflict pointer is nil — the slot address did not resolve")
	}
}

// TestLoad_StatedConflictKeySelectsAmongSiblings drives the keyed arm where it
// discriminates: a slot holding two children, where the stated key picks one.
// A reader that answered from the slot's occupant count alone would refuse this
// document, and the single-child fixtures cannot tell the two arms apart.
func TestLoad_StatedConflictKeySelectsAmongSiblings(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	snap := composedSiblingCollision(ctx, t, s)

	data, res := snapshot.Marshal(ctx, snap)
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// The fixture is only discriminating while the slot holds more than one
	// child; a single occupant makes both arms agree.
	if n := bytes.Count(data, []byte(`"key":["c`)); n < 3 {
		t.Fatalf("fixture is vacuous: %d child keys, want the slot to hold two plus the duplicate:\n%s", n, data)
	}

	loaded, loadRes := snapshot.Load(ctx, data, s)
	if loadRes.HasErrors() {
		t.Fatalf("Load refused a stated conflict key that selects among siblings: %v", loadRes)
	}
	if verifyRes := snapshot.Verify(ctx, data, s); verifyRes.HasErrors() {
		t.Errorf("Verify refused a document Load accepted: %v", verifyRes)
	}
	if n := len(loaded.Duplicates()); n != 1 {
		t.Fatalf("loaded %d duplicates, want 1", n)
	}
	conflict := loaded.Duplicates()[0].Conflict
	if conflict == nil {
		t.Fatal("the duplicate's conflict pointer is nil — the stated key selected nothing")
	}
	if got := conflict.PrimaryKey().String(); got != `["c1"]` {
		t.Errorf("conflict resolved to %s, want [\"c1\"] — the stated key selected the wrong sibling", got)
	}
}
