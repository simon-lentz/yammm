package snapshottest_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
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

// buildSnapshot is a local alias for the exported helper under test.
func buildSnapshot(t *testing.T, s *schema.Schema, instances ...*instance.ValidInstance) *graph.Snapshot {
	t.Helper()
	return snapshottest.BuildSnapshot(t, s, instances...)
}

func typeID(t *testing.T, s *schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("%s type missing", name)
	}
	return typ.ID()
}

// compositionSchema declares a two-level composition (Holder -> Account ->
// Card) so projection depth is observable.
func compositionSchema(t *testing.T) *schema.Schema {
	t.Helper()
	const src = `schema "snaptest"

part type Card {
	last4 String primary
}

part type Account {
	number String primary
	*-> CARDS (_:many) Card
}

type Holder {
	id String primary
	*-> ACCOUNTS (_:many) Account
}
`
	s, res := schema.LoadString(context.Background(), src, "snaptest.yammm")
	if res.HasErrors() {
		t.Fatalf("compositionSchema: %v", res.Err())
	}
	return s
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
			instancetest.TypeID(typeID(t, s, "Person")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1", "name": "Alice"}),
			instancetest.Edges(edges),
		),
	)

	snapshottest.AssertRoundTrip(t, snap, s)
	snapshottest.AssertDeterministic(t, snap)
	snapshottest.DiffSnapshots(t, snap, snap)
}

// TestDiffSnapshots_DetectsDifference pins that the comparer actually
// fails on structurally different snapshots, via a probe testing.T.
func TestDiffSnapshots_DetectsDifference(t *testing.T) {
	s := testSchema(t)
	a := buildSnapshot(t, s, instancetest.VI(
		"Person",
		instancetest.TypeID(typeID(t, s, "Person")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1", "name": "Alice"}),
	))
	b := buildSnapshot(t, s, instancetest.VI(
		"Person",
		instancetest.TypeID(typeID(t, s, "Person")),
		instancetest.PK("p1"),
		instancetest.Props(map[string]any{"id": "p1", "name": "Bob"}),
	))

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, a, b)
	if !probe.Failed() {
		t.Error("DiffSnapshots did not fail on differing snapshots")
	}
}

// TestDiffSnapshots_ComparesNumbersExactly pins exact numeric comparison on
// both axes: two int64 values differing by 1 above 2^53 collapse to the same
// float64, so a float-coercing comparer would miss that corruption class; and
// an int64/float64 pair of equal value must FAIL the diff — schema-aware
// float emission keeps KindFloat values float64 across a round trip, so a
// dynamic-type mismatch is a real defect, not a wire artifact.
func TestDiffSnapshots_ComparesNumbersExactly(t *testing.T) {
	s := testSchema(t)
	person := func(code any) *graph.Snapshot {
		return buildSnapshot(t, s, instancetest.VI(
			"Person",
			instancetest.TypeID(typeID(t, s, "Person")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1", "code": code}),
		))
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, person(int64(1<<53+1)), person(int64(1<<53)))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect a ±1 corruption of an int64 property above 2^53")
	}

	probe = &testing.T{}
	snapshottest.DiffSnapshots(probe, person(int64(1)), person(float64(1)))
	if !probe.Failed() {
		t.Error("DiffSnapshots must reject an int64/float64 dynamic-type mismatch")
	}
}

// TestDiffSnapshots_DistinguishesProvenancePresence pins that an instance
// carrying provenance with an empty source name and an instance carrying no
// provenance do not compare equal: a round trip dropping provenance objects
// is data loss even when the source name is empty.
func TestDiffSnapshots_DistinguishesProvenancePresence(t *testing.T) {
	s := testSchema(t)
	person := func(opts ...instancetest.VIOption) *graph.Snapshot {
		base := []instancetest.VIOption{
			instancetest.TypeID(typeID(t, s, "Person")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1"}),
		}
		return buildSnapshot(t, s, instancetest.VI("Person", append(base, opts...)...))
	}

	with := person(instancetest.Provenance(location.NewProvenance("", path.Root(), location.Span{})))
	without := person()

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, with, without)
	if !probe.Failed() {
		t.Error("DiffSnapshots must distinguish empty-named provenance from no provenance")
	}
}

// TestDiffSnapshots_ComparesComposedTreesRecursively pins that composition
// comparison descends past one level: snapshots differing only in a composed
// child's own children, or only in a composed child's provenance, must not
// compare equal.
func TestDiffSnapshots_ComparesComposedTreesRecursively(t *testing.T) {
	s := compositionSchema(t)
	holder := func(withCard bool, accountProv *location.Provenance) *graph.Snapshot {
		var cards []*instance.ValidInstance
		if withCard {
			cards = append(cards, instancetest.VI(
				"Card",
				instancetest.TypeID(typeID(t, s, "Card")),
				instancetest.PK("4242"),
				instancetest.Props(map[string]any{"last4": "4242"}),
			))
		}
		account := instancetest.VI(
			"Account",
			instancetest.TypeID(typeID(t, s, "Account")),
			instancetest.PK("a1"),
			instancetest.Props(map[string]any{"number": "a1"}),
			instancetest.Composed(map[string]immutable.Value{"CARDS": immutable.Wrap(cards)}),
			instancetest.Provenance(accountProv),
		)
		return buildSnapshot(t, s, instancetest.VI(
			"Holder",
			instancetest.TypeID(typeID(t, s, "Holder")),
			instancetest.PK("h1"),
			instancetest.Props(map[string]any{"id": "h1"}),
			instancetest.Composed(map[string]immutable.Value{"ACCOUNTS": immutable.Wrap([]*instance.ValidInstance{account})}),
		))
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, holder(true, nil), holder(false, nil))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect loss of a composed child's own children")
	}

	probe = &testing.T{}
	prov := location.NewProvenance("feed", path.Root(), location.Span{})
	snapshottest.DiffSnapshots(probe, holder(false, prov), holder(false, nil))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect loss of a composed child's provenance")
	}
}
