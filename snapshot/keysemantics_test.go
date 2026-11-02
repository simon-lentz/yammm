package snapshot_test

import (
	"bytes"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// An empty key and an absent key are one state. immutable.Key.Len() and
// immutable.Key.String() already collapse them, so every position that reads a
// key must branch on length. These tests pin the two boundaries where the
// distinction leaked: the string form graph.UnresolvedEdge.TargetKey carries.

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

	built := slices.Collect(snap.Unresolved())
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

	got := slices.Collect(loaded.Unresolved())
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

// bigKeySchema keys on a String, because no schema may key on an Integer —
// a numeric key component reaches the writer only through a document.
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
	built := snappartstest.Loaded(t, s, snapparts.Parts{
		Types: []schema.TypeID{id},
		Instances: []snapparts.Instance{
			{
				TypeID:     id,
				PrimaryKey: immutable.WrapKey([]any{"r1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "r1"}),
			},
		},
		Unresolved: []snapparts.Unresolved{{
			SourceType: id,
			SourceKey:  immutable.WrapKey([]any{"r1"}),
			Relation:   "POINTS",
			TargetKey:  immutable.WrapKey([]any{beyondExact}),
			Reason:     "target_missing",
		}},
	})
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
