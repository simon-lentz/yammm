package snapshottest_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

func testSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, res := schema.NewBuilder().
		WithName("snapshottest-self").
		WithSourceID(location.MustNewSourceID("test://snapshottest-self.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("testSchema: %v", res.Err())
	}
	return s
}

func buildSnapshot(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	g := graph.New(s)
	for _, inst := range instances {
		g.Add(context.Background(), inst)
	}
	return g.Snapshot()
}

func personType(t *testing.T, s *schema.Schema) schema.TypeID {
	t.Helper()
	typ, ok := s.Type("Person")
	if !ok {
		t.Fatal("Person type missing")
	}
	return typ.ID()
}

// TestRoundTripHelpers exercises the live helper surface end-to-end on a
// snapshot with an instance, a resolved edge, and an unresolved edge.
func TestRoundTripHelpers(t *testing.T) {
	s := testSchema(t)
	edges := map[string]*instance.ValidEdgeData{
		"EMPLOYER": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{"c-missing"}), immutable.WrapProperties(nil)),
		}),
	}
	snap := buildSnapshot(
		t, s,
		instancetest.VI(
			"Person",
			instancetest.TypeID(personType(t, s)),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1", "name": "Alice"}),
			instancetest.Edges(edges),
		),
	)

	snapshottest.AssertRoundTrip(t, snap, s)
	snapshottest.AssertDeterministic(t, snap, s)
	snapshottest.DiffSnapshots(t, snap, snap)
}

// TestDiffSnapshots_DetectsDifference pins that the comparer actually
// fails on structurally different snapshots, via a probe testing.T.
func TestDiffSnapshots_DetectsDifference(t *testing.T) {
	s := testSchema(t)
	a := buildSnapshot(t, s, instancetest.VI(
		"Person",
		instancetest.TypeID(personType(t, s)),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1", "name": "Alice"}),
	))
	b := buildSnapshot(t, s, instancetest.VI(
		"Person",
		instancetest.TypeID(personType(t, s)),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1", "name": "Bob"}),
	))

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, a, b)
	if !probe.Failed() {
		t.Error("DiffSnapshots did not fail on differing snapshots")
	}
}
