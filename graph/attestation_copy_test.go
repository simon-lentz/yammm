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
	s, err := schema.LoadString(t.Context(), `schema "att"

type Thing {
	id String primary
}
`, "att.yammm")
	if err != nil {
		t.Fatalf("schema: %s", err)
	}

	g := graph.New(s)
	if err := g.Add(t.Context(), instancetest.Valid(t, s, "Thing", map[string]any{"id": "t1"})); err != nil {
		t.Errorf("Add: %v", err)
	}
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
