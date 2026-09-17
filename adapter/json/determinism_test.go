package json

import (
	"bytes"
	"context"
	"testing"
)

// The writer controls three orderings encoding/json does not: the edges within
// an association, the children within a composition, and the relation order on
// an instance. A map-keyed output hides the fourth, the type order, because
// encoding/json sorts object keys itself.
func TestMarshalObject_IsByteIdenticalAcrossRuns(t *testing.T) {
	t.Parallel()
	s, _ := p2Source(t)
	snap := buildP2Snapshot(t, s, p2Raws())
	a := New()

	first, err := a.MarshalObject(context.Background(), snap)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for i := range 5 {
		got, err := a.MarshalObject(context.Background(), snap)
		if err != nil {
			t.Fatalf("marshal run %d: %v", i, err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("run %d differs from the first\nfirst: %s\ngot:   %s", i, first, got)
		}
	}
}
