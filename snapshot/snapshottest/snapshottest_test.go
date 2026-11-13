package snapshottest_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/internal/snappartstest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/snapshot/snapshottest"
)

func testSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), `schema "snapshottest_self"

type Person {
	id String primary
	name String required
	--> EMPLOYER (one) Company
}

type Company {
	id String primary
}
`, "snapshottest_self.yammm")
	if res.HasErrors() {
		t.Fatalf("testSchema: %v", res.Err())
	}
	return s
}

// buildSnapshot is a local alias for the exported helper under test.
func buildSnapshot(t *testing.T, s schema.Schema, instances ...instance.ValidInstance) graph.Snapshot {
	t.Helper()
	return snapshottest.BuildSnapshot(t, s, instances...)
}

func typeID(t *testing.T, s schema.Schema, name string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(name)
	if !ok {
		t.Fatalf("%s type missing", name)
	}
	return typ.ID()
}

// compositionSchema declares a two-level composition (Holder -> Account ->
// Card) so projection depth is observable.
func compositionSchema(t *testing.T) schema.Schema {
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

// company builds a Company instance with the given primary key.
func company(t *testing.T, s schema.Schema, id string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, "Company", map[string]any{"id": id})
}

// person builds a Person instance whose EMPLOYER edge points at target.
func person(t *testing.T, s schema.Schema, id, name, target string) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, "Person", map[string]any{
		"id": id, "name": name, "employer": map[string]any{"_target_id": target},
	})
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
	a := buildSnapshot(t, s, instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "Alice"}))
	b := buildSnapshot(t, s, instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "Bob"}))

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
	personID := typeID(t, s, "Person")
	key := immutable.WrapKey([]any{"p1"})
	person := func(code any) graph.Snapshot {
		return snappartstest.Loaded(t, s, snapparts.Parts{
			Instances: []snapparts.Instance{{
				TypeID: personID, PrimaryKey: key, Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": code}),
			}},
			Unresolved: []snapparts.Unresolved{{SourceType: personID, SourceKey: key, Relation: "EMPLOYER", Reason: "absent"}},
		})
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
	person := func(prov location.Provenance) graph.Snapshot {
		return buildSnapshot(t, s, instancetest.ValidRaw(t, s, "Person", instance.RawInstance{
			Properties: map[string]any{"id": "p1", "name": "Alice"},
			Provenance: prov,
		}))
	}

	with := person(location.NewProvenance("", path.Root(), location.Span{}))
	without := person(location.Provenance{})

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
	personParts := func(key string) snapparts.Instance {
		return snapparts.Instance{
			TypeID:     typeID(t, s, "Person"),
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key}),
		}
	}
	withSource := func(sourceKey, other string) graph.Snapshot {
		return snappartstest.Loaded(t, s, snapparts.Parts{
			Types: []schema.TypeID{typeID(t, s, "Person")},
			Instances: []snapparts.Instance{
				personParts("p1"),
				personParts("p2"),
			},
			Unresolved: []snapparts.Unresolved{{
				SourceType: typeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{sourceKey}),
				Relation:   "EMPLOYER",
				TargetKey:  immutable.WrapKey([]any{"c99"}),
				Reason:     "target_missing",
			}, {
				// Graph.Add records the other person's required EMPLOYER as absent.
				SourceType: typeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{other}),
				Relation:   "EMPLOYER",
				Reason:     "absent",
			}},
		})
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, withSource("p1", "p2"), withSource("p2", "p1"))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect an unresolved edge that moved to a different source instance")
	}
}

// TestDiffSnapshots_ComparesProvenancePaths pins the path itself, not only
// whether provenance is present. RawPath is populated only on a parse failure,
// so a projection reading it alone reports every well-formed path as empty.
func TestDiffSnapshots_ComparesProvenancePaths(t *testing.T) {
	s := testSchema(t)
	person := func(p path.Builder) graph.Snapshot {
		return buildSnapshot(t, s, instancetest.ValidRaw(t, s, "Person", instance.RawInstance{
			Properties: map[string]any{"id": "p1", "name": "Alice"},
			Provenance: location.NewProvenance("feed", p, location.Span{}),
		}))
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
	// A validator gives a child its root's provenance, so a child that alone holds
	// one comes from a document.
	holder := func(withCard bool, accountProv location.Provenance) graph.Snapshot {
		account := snapparts.Instance{
			TypeID: typeID(t, s, "Account"), PrimaryKey: immutable.WrapKey([]any{"a1"}),
			Properties: immutable.WrapProperties(map[string]any{"number": "a1"}),
			Provenance: accountProv,
		}
		if withCard {
			account.Composed = map[string][]snapparts.Instance{"CARDS": {{
				TypeID: typeID(t, s, "Card"), PrimaryKey: immutable.WrapKey([]any{"4242"}),
				Properties: immutable.WrapProperties(map[string]any{"last4": "4242"}),
			}}}
		}
		return snappartstest.Loaded(t, s, snapparts.Parts{Instances: []snapparts.Instance{{
			TypeID: typeID(t, s, "Holder"), PrimaryKey: immutable.WrapKey([]any{"h1"}),
			Properties: immutable.WrapProperties(map[string]any{"id": "h1"}),
			Composed:   map[string][]snapparts.Instance{"ACCOUNTS": {account}},
		}}})
	}

	probe := &testing.T{}
	snapshottest.DiffSnapshots(probe, holder(true, location.Provenance{}), holder(false, location.Provenance{}))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect loss of a composed child's own children")
	}

	probe = &testing.T{}
	prov := location.NewProvenance("feed", path.Root(), location.Span{})
	snapshottest.DiffSnapshots(probe, holder(false, prov), holder(false, location.Provenance{}))
	if !probe.Failed() {
		t.Error("DiffSnapshots must detect loss of a composed child's provenance")
	}
}

// recordingTB records Fatalf and Errorf instead of failing the test, so a helper's
// failure can be asserted on.
type recordingTB struct {
	testing.TB
	fatals []string
	errors []string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

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
	// A validated child read out of its parent is the one record Add refuses for
	// its type.
	holder, res := schema.LoadString(t.Context(), `schema "holder"

part type P {
	id String primary
}

type H {
	id String primary
	*-> PS (many) P
}
`, "holder.yammm")
	if res.HasErrors() {
		t.Fatalf("load the holder: %s", res)
	}
	refused := &recordingTB{TB: t}
	var child instance.ValidInstance
	for _, composed := range instancetest.Valid(t, holder, "H", map[string]any{"id": "h", "ps": []any{map[string]any{"id": "p"}}}).Compositions() {
		children, _ := composed.Slice()
		child = children.Get(0).Unwrap().(instance.ValidInstance)
	}
	snapshottest.BuildSnapshot(refused, holder, child)
	if len(refused.fatals) != 1 || !strings.Contains(refused.fatals[0], "instance 0 was refused") {
		t.Errorf("a refused fixture: fatals = %q, want one naming instance 0", refused.fatals)
	}

	dup := &recordingTB{TB: t}
	first := instancetest.Valid(t, s, "Person", map[string]any{"id": "p1", "name": "Alice"})
	snap := snapshottest.BuildSnapshot(dup, s, first, first)
	if len(dup.fatals) != 0 || len(slices.Collect(snap.Duplicates())) != 1 {
		t.Errorf("a duplicate fixture: fatals = %q, %d duplicates; want none and one", dup.fatals, len(slices.Collect(snap.Duplicates())))
	}
}
