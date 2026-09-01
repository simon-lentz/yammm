package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// TestSnapshot_AttestationIsACopy pins the defensive-copy rule for the one
// accessor that returns a pointer. Snapshot is documented immutable and safe
// for concurrent reads, and Attestation's fields are exported — so handing out
// the stored pointer would let one caller rewrite another's claim.
func TestSnapshot_AttestationIsACopy(t *testing.T) {
	s, res := schema.NewBuilder().
		WithName("att").
		WithSourceID(location.MustNewSourceID("test://att.yammm")).
		AddType("Thing").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("schema: %s", res)
	}
	ty, _ := s.Type("Thing")

	g := graph.New(s)
	g.Add(t.Context(), instance.NewValidInstance("Thing", ty.ID(),
		immutable.WrapKey([]any{"t1"}),
		immutable.WrapProperties(map[string]any{"id": "t1"}),
		nil, nil, nil))
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
