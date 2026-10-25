package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// An empty key and an absent key are one state. immutable.Key.Len() and
// immutable.Key.String() already collapse them, so every position that reads a
// key must branch on length. These tests pin the two boundaries where the
// distinction leaked: the reader's conflict key, and the string form
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
	if built[0].Reason() != "absent" {
		t.Fatalf("fixture is vacuous: reason %q, want %q", built[0].Reason(), "absent")
	}
	if built[0].TargetKey() != "" {
		t.Fatalf("precondition: built TargetKey = %q, want empty", built[0].TargetKey())
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
	if got[0].Reason() != "absent" {
		t.Errorf("loaded reason = %q, want %q", got[0].Reason(), "absent")
	}
	if got[0].TargetKey() != "" {
		t.Errorf("loaded TargetKey = %q, want empty — a keyless key rendered as a key", got[0].TargetKey())
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
		if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "CHILDREN", child); res.HasErrors() {
			t.Fatalf("add child %s: %v", key, res)
		}
	}
	collider := mustValidInstance(t, s, "Child", []any{"c1"}, map[string]any{"value": "second"})
	if res := g.AddComposed(ctx, mustTypeID(t, s, "Parent"), graph.FormatKey("p1"), "CHILDREN", collider); !res.HasErrors() {
		t.Fatal("a colliding child key must report a duplicate")
	}

	snap := g.Snapshot()
	if n := len(snap.Duplicates()); n != 1 {
		t.Fatalf("snapshot holds %d duplicates, want 1", n)
	}
	return snap
}

// spliceConflictBlock rewrites the conflict block of the document's single
// duplicate record. Marshal writes a null conflict key for a keyless conflict
// alone, so an empty key beside a keyed one exists only by splicing.
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

// TestInfo_ReportsAnUnresolvableInstancesEntry pins that Info reports a short
// count rather than presenting one as complete. Info resolves no schema, so it
// cannot run Load's validation — but an instances entry naming no table row is
// a structural defect Info can see, and skipping it silently produced a clean
// summary for a document Load and Verify refuse.
func TestInfo_ReportsAnUnresolvableInstancesEntry(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	data := v3Doc(t)

	rows := len(wireTypeTable(t, data))
	edited := bytes.Replace(data,
		fmt.Appendf(nil, `{"type":%d,"items":`, childRow(t, data, "Parent")),
		fmt.Appendf(nil, `{"type":%d,"items":`, rows+3), 1)
	if bytes.Equal(edited, data) {
		t.Fatalf("fixture shape changed; no Parent instances entry in:\n%s", data)
	}

	// The clean document is the control: Info counts it and reports nothing.
	clean, cleanRes := snapshot.Info(ctx, data)
	if cleanRes.HasErrors() {
		t.Fatalf("Info reported the unedited document: %v", cleanRes)
	}
	if clean.TotalInstances == 0 {
		t.Fatal("fixture is vacuous: Info counts no instances in the unedited document")
	}

	// Load refuses the edited document, which is what makes a clean Info a defect.
	if _, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithIntegrityCheck(false)); !loadRes.HasErrors() {
		t.Fatal("precondition: Load must refuse an out-of-range instances row")
	}

	info, infoRes := snapshot.Info(ctx, edited)
	if !hasCode(infoRes, diag.E_SNAPSHOT_MALFORMED) {
		t.Errorf("Info summarized an out-of-range instances row without %s: %v",
			diag.E_SNAPSHOT_MALFORMED, infoRes)
	}
	if info != nil && info.TotalInstances >= clean.TotalInstances {
		t.Errorf("Info counted %d instances for a document missing a group, want fewer than %d",
			info.TotalInstances, clean.TotalInstances)
	}
}

// TestLoadVerify_RefuseAnEmptyConflictKeyUnderAKeyedManyComposition pins the
// conflict address of a keyed (many) duplicate: graph.Add records it against
// the child at its own key, and graph.RebuildSnapshot derives it so, so a
// record that states no conflict key states no conflict, whatever the slot
// holds. Each shape draws the one issue, and Load and Verify agree.
func TestLoadVerify_RefuseAnEmptyConflictKeyUnderAKeyedManyComposition(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := testSchemaWithComposition(t)
	sole := v3Doc(t)
	siblings, res := snapshot.Marshal(ctx, composedSiblingCollision(ctx, t, s))
	if err := res.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}

	for name, edited := range map[string][]byte{
		"a sole occupant, the key empty": emptyConflictKeyDoc(t),
		"a sole occupant, the key null": spliceConflictBlock(t, sole,
			fmt.Sprintf(`"type":%d,"key":null`, childRow(t, sole, "Child"))),
		"two occupants": spliceConflictBlock(t, siblings,
			fmt.Sprintf(`"type":%d,"key":[]`, childRow(t, siblings, "Child"))),
		"a sole occupant of another type than the block states": spliceConflictBlock(t, sole,
			fmt.Sprintf(`"type":%d,"key":[]`, childRow(t, sole, "Parent"))),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			loaded, loadRes := snapshot.Load(ctx, edited, s, snapshot.WithIntegrityCheck(false))
			verifyRes := snapshot.Verify(ctx, edited, s, snapshot.WithIntegrityCheck(false))
			if loaded != nil {
				t.Error("Load returned a snapshot")
			}
			for surface, res := range map[string]diag.Result{"Load": loadRes, "Verify": verifyRes} {
				if res.Len() != 1 || !hasCode(res, diag.E_SNAPSHOT_MALFORMED) ||
					!strings.Contains(res.String(), "its conflict is the child at its own key") {
					t.Errorf("%s reports %v, want one %s naming the child at its own key", surface, res, diag.E_SNAPSHOT_MALFORMED)
				}
			}
		})
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
	conflict := loaded.Duplicates()[0].Conflict()
	if conflict == nil {
		t.Fatal("the duplicate's conflict pointer is nil — the stated key selected nothing")
	}
	if got := conflict.PrimaryKey().String(); got != `["c1"]` {
		t.Errorf("conflict resolved to %s, want [\"c1\"] — the stated key selected the wrong sibling", got)
	}
}

// bigKeySchema keys on a String, because no schema may key on an Integer —
// a numeric key component reaches the writer only through caller-assembled
// parts.
const bigKeySchema = `schema "bigkey"

type Ref {
	id String primary
	--> POINTS (_) Ref { w Float }
}
`

// TestMarshal_KeepsAnIntegerTargetKeyBeyondExactFloat pins the unresolved
// record's target key against a float64 round trip. The key arrives as its
// canonical string form and is parsed back for the wire; parsing it without
// UseNumber rewrote any component past 2^53 to a neighbouring value, silently.
func TestMarshal_KeepsAnIntegerTargetKeyBeyondExactFloat(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.LoadString(t.Context(), bigKeySchema, "bigkey.yammm")
	if res.HasErrors() {
		t.Fatalf("load bigkey schema: %s", res)
	}
	typ, ok := s.Type("Ref")
	if !ok {
		t.Fatal("Ref missing")
	}
	id := typ.ID()

	const beyondExact = int64(1)<<53 + 1
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{id},
		Instances: []graph.InstanceParts{
			{
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"r1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "r1"}),
			},
		},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: id,
			SourceKey:  immutable.WrapKey([]any{"r1"}),
			Relation:   "POINTS",
			TargetKey:  immutable.WrapKey([]any{beyondExact}),
			Reason:     "target_missing",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}
	// The fixture is only meaningful while the value is outside float64's
	// exactly-representable integer range.
	if int64(float64(beyondExact)) == beyondExact {
		t.Fatal("fixture is vacuous: the value survives a float64 round trip")
	}

	data, mres := snapshot.Marshal(ctx, built)
	if err := mres.Err(); err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(data, []byte("9007199254740993")) {
		t.Errorf("the target key did not survive the writer:\n%s", data)
	}
}
