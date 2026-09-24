package snapshottest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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

// employerEdge builds the single-target EMPLOYER edge data for a Person.
func employerEdge(target string) map[string]*instance.ValidEdgeData {
	return map[string]*instance.ValidEdgeData{
		"EMPLOYER": instance.NewValidEdgeData([]instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{target}), immutable.WrapProperties(nil)),
		}),
	}
}

// company builds a Company instance with the given primary key.
func company(t *testing.T, s *schema.Schema, id string) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		"Company",
		instancetest.TypeID(typeID(t, s, "Company")),
		instancetest.PK(id),
		instancetest.Props(map[string]any{"id": id}),
	)
}

// person builds a Person instance whose EMPLOYER edge points at target.
func person(t *testing.T, s *schema.Schema, id, name, target string) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		"Person",
		instancetest.TypeID(typeID(t, s, "Person")),
		instancetest.PK(id),
		instancetest.Props(map[string]any{"id": id, "name": name}),
		instancetest.Edges(employerEdge(target)),
	)
}

// TestRoundTripHelpers exercises the live helper surface end-to-end on a
// snapshot with an instance, a resolved edge, and an unresolved edge.
//
// The resolved edge needs its target instance in the same snapshot: an edge
// whose target is absent lands in Unresolved and never reaches the
// projection's edge arm.
func TestRoundTripHelpers(t *testing.T) {
	s := testSchema(t)
	snap := buildSnapshot(
		t, s,
		company(t, s, "c1"),
		person(t, s, "p1", "Alice", "c1"),
		person(t, s, "p2", "Bob", "c-missing"),
	)

	snapshottest.AssertRoundTrip(t, snap, s)
	snapshottest.AssertDeterministic(t, snap)
	snapshottest.DiffSnapshots(t, snap, snap)
}

// TestDiffSnapshots_DetectsEdgeDifference pins the projection's edge arm.
// Two snapshots that differ only in an edge's target must not compare equal.
//
// Without it the arm is unasserted: deleting the projection's edge loop drops
// the edges from BOTH sides of every comparison, so every round-trip test
// stays green while the oracle stops comparing edges at all.
func TestDiffSnapshots_DetectsEdgeDifference(t *testing.T) {
	s := testSchema(t)
	a := buildSnapshot(
		t, s,
		company(t, s, "c1"), company(t, s, "c2"),
		person(t, s, "p1", "Alice", "c1"),
	)
	b := buildSnapshot(
		t, s,
		company(t, s, "c1"), company(t, s, "c2"),
		person(t, s, "p1", "Alice", "c2"),
	)

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, a, b)
	if !probe.Failed() {
		t.Error("DiffSnapshots did not fail on snapshots whose edges differ")
	}
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
			instancetest.Props(map[string]any{"id": "p1", "name": code}),
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

// TestDiffSnapshots_SeparatesUnresolvedBySource pins the source key on an
// unresolved record. Both snapshots hold the same two instances and one
// unresolved edge; only the instance the edge hangs off differs.
func TestDiffSnapshots_SeparatesUnresolvedBySource(t *testing.T) {
	s := testSchema(t)
	personParts := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     typeID(t, s, "Person"),
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key}),
		}
	}
	withSource := func(sourceKey string) *graph.Snapshot {
		built, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
			Types: []schema.TypeID{typeID(t, s, "Person")},
			Instances: []graph.InstanceParts{
				personParts("p1"),
				personParts("p2"),
			},
			Unresolved: []graph.UnresolvedParts{{
				SourceType: typeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{sourceKey}),
				Relation:   "EMPLOYER",
				TargetType: typeID(t, s, "Company"),
				TargetKey:  immutable.WrapKey([]any{"c99"}),
				Reason:     "target_missing",
			}},
		})
		if res.HasErrors() {
			t.Fatalf("rebuild: %s", res)
		}
		return built
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, withSource("p1"), withSource("p2"))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect an unresolved edge that moved to a different source instance")
	}
}

// TestDiffSnapshots_ComparesProvenancePaths pins the path itself, not only
// whether provenance is present. RawPath is populated only on a parse failure,
// so a projection reading it alone reports every well-formed path as empty.
func TestDiffSnapshots_ComparesProvenancePaths(t *testing.T) {
	s := testSchema(t)
	person := func(p path.Builder) *graph.Snapshot {
		return buildSnapshot(t, s, instancetest.VI(
			"Person",
			instancetest.TypeID(typeID(t, s, "Person")),
			instancetest.PK("p1"),
			instancetest.Props(map[string]any{"id": "p1"}),
			instancetest.Provenance(location.NewProvenance("feed", p, location.Span{})),
		))
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, person(path.Root().Key("first")), person(path.Root().Key("second")))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect two instances whose provenance paths differ")
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

// recordingTB records Fatalf instead of stopping the test, so a helper's
// failure can be asserted on.
type recordingTB struct {
	testing.TB
	fatals []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.fatals = append(r.fatals, fmt.Sprintf(format, args...))
}

// TestBuildSnapshot_FailsOnARefusalButADuplicate pins that a fixture Graph.Add
// refuses is a failed fixture, not a smaller snapshot: the refused instance
// would be missing from what the test then asserts on. A duplicate primary key
// is recorded in the snapshot, so a fixture may build one.
func TestBuildSnapshot_FailsOnARefusalButADuplicate(t *testing.T) {
	t.Parallel()
	s := testSchema(t)
	person := func(props map[string]any) *instance.ValidInstance {
		return instancetest.VI("Person", instancetest.TypeID(typeID(t, s, "Person")), instancetest.PK("p1"), instancetest.Props(props))
	}

	refused := &recordingTB{TB: t}
	snapshottest.BuildSnapshot(refused, s, person(map[string]any{"name": "no key property"}))
	if len(refused.fatals) != 1 || !strings.Contains(refused.fatals[0], "instance 0 was refused") {
		t.Errorf("a refused fixture: fatals = %q, want one naming instance 0", refused.fatals)
	}

	dup := &recordingTB{TB: t}
	snap := snapshottest.BuildSnapshot(dup, s, person(map[string]any{"id": "p1"}), person(map[string]any{"id": "p1"}))
	if len(dup.fatals) != 0 || len(snap.Duplicates()) != 1 {
		t.Errorf("a duplicate fixture: fatals = %q, %d duplicates; want none and one", dup.fatals, len(snap.Duplicates()))
	}
}
