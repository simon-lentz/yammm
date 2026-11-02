package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/schema"
)

// TestSnapshot_AttestationIsACopy pins the defensive-copy rule for the one
// accessor that returns a pointer. Snapshot is documented immutable and safe
// for concurrent reads, and Attestation's fields are exported — so handing out
// the stored pointer would let one caller rewrite another's claim.
func TestSnapshot_AttestationIsACopy(t *testing.T) {
	s, res := schema.LoadString(t.Context(), `schema "att"

type Thing {
	id String primary
}
`, "att.yammm")
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}

	g := graph.New(s)
	g.Add(t.Context(), instancetest.Valid(t, s, "Thing", map[string]any{"id": "t1"}))
	snap := g.Snapshot()

	first := snap.Attestation()
	if first == nil {
		t.Fatal("a built snapshot always makes a claim")
	}
	before := *first

	// Mutating what the accessor returned must not reach the snapshot.
	first.Values = !first.Values
	first.Associations = !first.Associations

	second := snap.Attestation()
	if *second != before {
		t.Errorf("mutating the returned attestation changed the snapshot: got %+v, want %+v", *second, before)
	}
}
