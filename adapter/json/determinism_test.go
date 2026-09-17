package json

import (
	"bytes"
	"context"
	"testing"
)

// The writer picks three orderings that reach the bytes: the edges within an
// association, the children within a composition, and the instances within a
// type. Relation order and type order do not — both are object keys, which
// encoding/json sorts itself.
//
// The run count is the instrument. A range over a two-element Go map departs
// from insertion order about an eighth of the time, so six marshals miss a
// nondeterministic writer in nearly half of all runs; fifty miss in under a
// thousandth, and cost a hundredth of a second.
func TestMarshalObject_IsByteIdenticalAcrossRuns(t *testing.T) {
	t.Parallel()
	s, _ := p2Source(t)
	snap := buildP2Snapshot(t, s, p2Raws())
	a := New()

	first, err := a.MarshalObject(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := range 49 {
		got, err := a.MarshalObject(context.Background(), snap)
		if err != nil {
			t.Fatalf("marshal run %d: %v", i, err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("run %d differs from the first\nfirst: %s\ngot:   %s", i, first, got)
		}
	}
}
