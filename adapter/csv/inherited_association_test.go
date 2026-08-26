package csv

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
)

// inheritedSnapshot builds one Employee — a concrete subtype whose association
// is declared on its abstract parent — plus the Company it points at.
func inheritedSnapshot(t *testing.T) (*graph.Snapshot, *Adapter) {
	t.Helper()
	s := loadTestSchema(t, "inherited_relations.yammm")
	snap := buildSnapshot(t, s, map[string][]map[string]any{
		"Employee": {{
			"worker_id": "w1",
			"name":      "Alice",
			"grade":     "senior",
			"works_at":  map[string]any{"_target_company_id": "c1", "since": "2020"},
		}},
		"Company": {{"company_id": "c1", "name": "Acme"}},
	})
	return snap, New(WithSchema(s))
}

// An inherited association is written. buildColumnList iterated AllProperties
// for properties and own-body Associations for relations, so a subtype's row
// carried its inherited PROPERTIES while its inherited EDGES vanished — with no
// diagnostic, because an export has no diagnostic channel.
//
// Mutation: reverting buildColumnList to Associations() turns this red; the
// header loses both works_at columns.
func TestInheritedAssociation_IsWritten(t *testing.T) {
	snap, a := inheritedSnapshot(t)

	out, err := a.MarshalSnapshot(context.Background(), snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	got := string(out["Employee"])
	for _, want := range []string{
		"works_at._target_company_id",
		"works_at.since",
		"c1",
		"2020",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Employee CSV is missing %q — the inherited association was dropped:\n%s", want, got)
		}
	}
}

// The parse side refused the same columns, and it did so LOUDLY: a dotted
// column whose field was not in the own-body set drew E_CSV_COERCE. So a
// hand-written file carrying an inherited edge was rejected with a wrong
// diagnostic while the writer silently omitted it — the two sides had to move
// together or the round trip would fail noisily instead of quietly.
//
// Mutation: reverting recordToProps to Associations() turns this red with
// "does not match an association field".
func TestInheritedAssociation_IsParsed(t *testing.T) {
	snap, a := inheritedSnapshot(t)
	ctx := context.Background()

	out, err := a.MarshalSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	s := loadTestSchema(t, "inherited_relations.yammm")
	typ, _ := s.Type("Employee")
	parsed, res := a.ParseTyped(ctx, location.NewSourceID("Employee.csv"), "Employee",
		bytes.NewReader(out["Employee"]), typ)
	if res.HasErrors() {
		t.Fatalf("the parser rejected the writer's own output: %s\n%s", res, out["Employee"])
	}
	if len(parsed) != 1 {
		t.Fatalf("parsed %d rows, want 1", len(parsed))
	}
	if _, ok := parsed[0].Properties["works_at"]; !ok {
		t.Error("the parsed row carries no works_at edge")
	}
}

// The identity claim docs/VERSIONING.md makes for v0.15.0 — "a CSV round trip
// through the validator is now the identity for association-bearing types" —
// was false for a subtype until this fix. It holds now.
func TestInheritedAssociation_RoundTripIsTheIdentity(t *testing.T) {
	snap, a := inheritedSnapshot(t)
	ctx := context.Background()
	s := loadTestSchema(t, "inherited_relations.yammm")

	first, err := a.MarshalSnapshot(ctx, snap)
	if err != nil {
		t.Fatalf("MarshalSnapshot: %v", err)
	}

	v := instance.NewValidator(s)
	g := graph.New(s)
	for _, typeName := range []string{"Company", "Employee"} {
		typ, _ := s.Type(typeName)
		parsed, pres := a.ParseTyped(ctx, location.NewSourceID(typeName+".csv"), typeName,
			bytes.NewReader(first[typeName]), typ)
		if pres.HasErrors() {
			t.Fatalf("ParseTyped rejected %s: %s", typeName, pres)
		}
		for i, raw := range parsed {
			vi, vres := v.ValidateOne(ctx, typeName, raw)
			if vres.HasErrors() {
				t.Fatalf("validator rejected re-parsed %s[%d]: %s", typeName, i, vres)
			}
			if r := g.Add(ctx, vi); !r.OK() {
				t.Fatalf("re-add %s[%d]: %s", typeName, i, r)
			}
		}
	}
	if cres := g.Check(ctx); !cres.OK() {
		t.Fatalf("the re-built graph is incomplete — an inherited edge did not survive: %s", cres)
	}

	second, err := a.MarshalSnapshot(ctx, g.Snapshot())
	if err != nil {
		t.Fatalf("re-marshal: %v", err)
	}
	for typeName, want := range first {
		if !bytes.Equal(want, second[typeName]) {
			t.Errorf("%s round trip is not the identity\nfirst:\n%s\nsecond:\n%s", typeName, want, second[typeName])
		}
	}
}
