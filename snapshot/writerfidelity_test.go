package snapshot_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot"
)

// The writer states what the snapshot holds and mints nothing of its own.
// These tests pin the three positions where it did otherwise.

// TestWriter_RefusesNonWhitespaceIndent pins that Marshal cannot report success
// while emitting bytes its own Load refuses. The indent is written between JSON
// tokens, so a non-whitespace one produces a document that is not JSON.
func TestWriter_RefusesNonWhitespaceIndent(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)
	snap, res := snapshot.Load(t.Context(), data, s)
	if res.HasErrors() {
		t.Fatalf("load fixture: %s", res)
	}

	out, mres := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent("xx"))
	if !mres.HasCode(diag.E_SNAPSHOT_MALFORMED) {
		t.Fatalf("Marshal accepted a non-whitespace indent and returned %d bytes: %s", len(out), mres)
	}
	if out != nil {
		t.Errorf("Marshal returned bytes alongside its refusal")
	}

	// The control: whitespace indents still marshal, and still parse.
	for _, ok := range []string{"", "  ", "\t"} {
		got, r := snapshot.Marshal(t.Context(), snap, snapshot.WithIndent(ok))
		if r.HasErrors() {
			t.Errorf("indent %q refused: %s", ok, r)
			continue
		}
		if !json.Valid(got) {
			t.Errorf("indent %q produced invalid JSON", ok)
		}
	}
}

// TestWriter_CanonicalizesTheKeyLikeItsProperty pins one spelling per value.
// The key is the address — edge targets, duplicate coordinates and an adapter's
// merge key read it — so a key in the caller's form beside a canonicalized
// property gave one entity two addresses.
func TestWriter_CanonicalizesTheKeyLikeItsProperty(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "wk"

type Doc {
	uid UUID primary
	other UUID
}
`, "wk.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	const upper = "3F2504E0-4F89-11D3-9A0C-0305E82C3301"
	const lower = "3f2504e0-4f89-11d3-9a0c-0305e82c3301"

	ty, _ := s.Type("Doc")
	g := graph.New(s)
	g.Add(t.Context(), instancetest.VI(
		"Doc",
		instancetest.TypeID(ty.ID()),
		instancetest.PK(upper),
		instancetest.Props(map[string]any{"uid": upper, "other": upper}),
	))
	data, mres := snapshot.Marshal(t.Context(), g.Snapshot())
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	doc := string(data)

	if strings.Contains(doc, upper) {
		t.Errorf("the document carries the un-canonicalized spelling %s:\n%s", upper, doc)
	}
	if !strings.Contains(doc, `"key":["`+lower+`"]`) {
		t.Errorf("the key is not canonicalized:\n%s", doc)
	}
	if !strings.Contains(doc, `"uid":"`+lower+`"`) {
		t.Errorf("the backing property is not canonicalized:\n%s", doc)
	}
}

// TestWriter_PreservesAnEmptyProvenancePath pins that a value the document
// carried is not replaced by one the writer mints. An empty path does not
// parse, so the reader falls back to the root and warns — and writing "$" back
// made the second read clean, losing both the value and the warning.
func TestWriter_PreservesAnEmptyProvenancePath(t *testing.T) {
	t.Parallel()
	s, data := attWireFixture(t)

	edited := strings.Replace(string(data), `"provenance":null`,
		`"provenance":{"source_name":"s","path":""}`, 1)
	if edited == string(data) {
		t.Skipf("fixture carries no null provenance: %s", data)
	}

	snap, res := snapshot.Load(t.Context(), []byte(edited), s, snapshot.WithIntegrityCheck(false))
	if res.HasErrors() {
		t.Fatalf("first load: %s", res)
	}
	if !res.HasCode(diag.E_SNAPSHOT_PATH_FALLBACK) {
		t.Errorf("an unparseable path must warn on the first read: %s", res)
	}

	again, mres := snapshot.Marshal(t.Context(), snap)
	if mres.HasErrors() {
		t.Fatalf("marshal: %s", mres)
	}
	if !strings.Contains(string(again), `"path":""`) {
		t.Errorf("the empty path was not written back:\n%s", again)
	}

	// The warning must survive too: a second read of a document still holding
	// the unparseable path reports it again.
	_, res2 := snapshot.Load(t.Context(), again, s)
	if !res2.HasCode(diag.E_SNAPSHOT_PATH_FALLBACK) {
		t.Errorf("the fallback warning vanished on the second read: %s", res2)
	}
}

// TestWriter_RefusesATargetKeyItCannotParse pins that a target key
// [graph.ParseKey] cannot read is an internal failure, not a Warning that
// drops the address. UnresolvedEdge.TargetKey is written from
// immutable.Key.String on every library path and ParseKey is its pinned
// inverse, so the only way to reach the arm is caller-assembled parts whose
// key holds a non-scalar component — the state Marshal's contract assigns to
// Fatal E_INTERNAL.
func TestWriter_RefusesATargetKeyItCannotParse(t *testing.T) {
	t.Parallel()

	s, res := schema.LoadString(t.Context(), bigKeySchema, "bigkey.yammm")
	if res.HasErrors() {
		t.Fatalf("load bigkey schema: %s", res)
	}
	typ, ok := s.Type("Ref")
	if !ok {
		t.Fatal("Ref missing")
	}
	id := typ.ID()

	nested := immutable.WrapKey([]any{[]any{"nested"}})
	if _, err := graph.ParseKey(nested.String()); err == nil {
		t.Fatalf("fixture is vacuous: ParseKey reads %s", nested)
	}
	built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{id},
		Instances: map[schema.TypeID][]graph.InstanceParts{id: {{
			TypeName:   "Ref",
			TypeID:     id,
			PrimaryKey: immutable.WrapKey([]any{"r1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "r1"}),
		}}},
		Unresolved: []graph.UnresolvedParts{{
			SourceType: id,
			SourceKey:  immutable.WrapKey([]any{"r1"}),
			Relation:   "POINTS",
			TargetType: id,
			TargetKey:  nested,
			Reason:     "target_missing",
		}},
	})
	if res.HasErrors() {
		t.Fatalf("assembling: %s", res)
	}

	data, mres := snapshot.Marshal(t.Context(), built)
	if data != nil {
		t.Errorf("Marshal wrote %d bytes over a key it cannot read back", len(data))
	}
	if !mres.HasFatal() || !mres.HasCode(diag.E_INTERNAL) {
		t.Fatalf("want Fatal E_INTERNAL, got: %s", mres)
	}
	if mres.HasCode(diag.W_SNAPSHOT_VALUE_DROPPED) {
		t.Errorf("the key was dropped under a Warning instead of refused: %s", mres)
	}
	named := false
	for issue := range mres.Issues() {
		if strings.Contains(issue.Message(), nested.String()) {
			named = true
		}
	}
	if !named {
		t.Errorf("the failure does not name the key %s: %s", nested, mres)
	}
}
