package graph_test

import (
	"context"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
)

var rangeCount int

// A writer ranges these once per record, so a range that moved its variables to the
// heap would cost each record an allocation.
func TestRecordIterators_ARangeAllocatesNothing(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)
	company := mustValidInstance(t, s, "Company", []any{"c1"}, map[string]any{"id": "c1", "name": "Acme"})
	if res := g.Add(context.Background(), company); !res.OK() {
		t.Fatal(res)
	}
	snap := g.Snapshot()
	id := mustTypeID(t, s, "Company")
	inst := slices.Collect(snap.InstancesOf(id))[0]
	var targets instance.ValidEdgeData
	held := company

	for name, ranged := range map[string]func(){
		"Snapshot.InstancesOf": func() {
			n := 0
			for range snap.InstancesOf(id) {
				n++
			}
			rangeCount = n
		},
		"Snapshot.Edges": func() {
			n := 0
			for range snap.Edges() {
				n++
			}
			rangeCount = n
		},
		"Snapshot.EdgesFrom": func() {
			n := 0
			for range snap.EdgesFrom(inst) {
				n++
			}
			rangeCount = n
		},
		"Snapshot.Duplicates": func() {
			n := 0
			for range snap.Duplicates() {
				n++
			}
			rangeCount = n
		},
		"Snapshot.Unresolved": func() {
			n := 0
			for range snap.Unresolved() {
				n++
			}
			rangeCount = n
		},
		"Instance.Composed": func() {
			n := 0
			for range inst.Composed("NONE") {
				n++
			}
			rangeCount = n
		},
		"ValidEdgeData.Targets": func() {
			n := 0
			for range targets.Targets() {
				n++
			}
			rangeCount = n
		},
		"ValidInstance.Edges": func() {
			n := 0
			for range held.Edges() {
				n++
			}
			rangeCount = n
		},
		"ValidInstance.Compositions": func() {
			n := 0
			for range held.Compositions() {
				n++
			}
			rangeCount = n
		},
	} {
		if allocs := testing.AllocsPerRun(10, ranged); allocs != 0 {
			t.Errorf("a range over %s allocated %v times, want 0", name, allocs)
		}
	}
}
