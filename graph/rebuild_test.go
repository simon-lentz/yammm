package graph_test

import (
	"slices"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

func rebuildTestSchema(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "rebuild_test"

type Person {
	id String primary
	name String required
	--> EMPLOYER (one) Company
}

type Company {
	id String primary
	title String required
}
`, "rebuild.yammm")
	if result.HasErrors() {
		t.Fatalf("rebuildTestSchema: %s", result)
	}
	return s
}

func TestRebuild_EmptyParts(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := snapparts.Parts{
		Types:     []schema.TypeID{},
		Instances: []snapparts.Instance{},
	}

	snap := rebuild(s, parts)
	if snap.IsZero() {
		t.Fatal("expected non-nil snapshot")
	}
	if len(snap.Types()) != 0 {
		t.Errorf("expected 0 types, got %d", len(snap.Types()))
	}
}

func TestRebuild_WithInstances(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := snapparts.Parts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company"), mustTypeID(t, s, "Person")},
		Instances: []snapparts.Instance{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
			{
				TypeID:     mustTypeID(t, s, "Person"),
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
			},
		},
		// Graph.Add records a required association the data does not name.
		Unresolved: []snapparts.Unresolved{{
			SourceType: mustTypeID(t, s, "Person"), SourceKey: immutable.WrapKey([]any{"p1"}),
			Relation: "EMPLOYER", Reason: "absent",
		}},
	}

	snap := rebuild(s, parts)

	if len(snap.Types()) != 2 {
		t.Errorf("expected 2 types, got %d", len(snap.Types()))
	}
	if len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Company")))) != 1 {
		t.Errorf("expected 1 Company, got %d", len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Company")))))
	}
	if len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))) != 1 {
		t.Errorf("expected 1 Person, got %d", len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))))
	}
}

func TestRebuild_WithEdges(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := snapparts.Parts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company"), mustTypeID(t, s, "Person")},
		Instances: []snapparts.Instance{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
			{
				TypeID:     mustTypeID(t, s, "Person"),
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
			},
		},
		Edges: []snapparts.Edge{
			{
				Relation:   "EMPLOYER",
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				TargetKey:  immutable.WrapKey([]any{"c1"}),
				Properties: immutable.Properties{},
			},
		},
	}

	snap := rebuild(s, parts)

	edges := slices.Collect(snap.Edges())
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}

	e := edges[0]
	if e.Relation() != "EMPLOYER" {
		t.Errorf("edge relation: got %q, want %q", e.Relation(), "EMPLOYER")
	}

	// EdgesFrom should also work.
	persons := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	if len(persons) != 1 {
		t.Fatalf("expected 1 Person")
	}
	edgesFrom := slices.Collect(snap.EdgesFrom(persons[0]))
	if len(edgesFrom) != 1 {
		t.Errorf("EdgesFrom: expected 1, got %d", len(edgesFrom))
	}
}

func TestRebuild_WithDuplicates(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := snapparts.Parts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company")},
		Instances: []snapparts.Instance{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
		},
		Duplicates: []snapparts.Instance{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme Corp"}),
			},
		},
	}

	snap := rebuild(s, parts)

	dups := slices.Collect(snap.Duplicates())
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(dups))
	}

	dup := dups[0]
	if dup.Instance().TypeName() != "Company" {
		t.Errorf("duplicate type: got %q", dup.Instance().TypeName())
	}
	if dup.Conflict().IsZero() {
		t.Error("conflict should be resolved")
	}
}

func TestRebuild_WithUnresolved(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := snapparts.Parts{
		Types: []schema.TypeID{mustTypeID(t, s, "Person")},
		Instances: []snapparts.Instance{
			{
				TypeID:     mustTypeID(t, s, "Person"),
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
			},
		},
		Unresolved: []snapparts.Unresolved{
			{
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				Relation:   "EMPLOYER",
				TargetKey:  immutable.WrapKey([]any{"c99"}),
				Reason:     "target_missing",
			},
		},
	}

	snap := rebuild(s, parts)

	unresolved := slices.Collect(snap.Unresolved())
	if len(unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(unresolved))
	}

	u := unresolved[0]
	if u.Relation() != "EMPLOYER" {
		t.Errorf("unresolved relation: got %q", u.Relation())
	}
	if u.Source().IsZero() {
		t.Error("source should be resolved")
	}
}

// TestRebuild_EstablishesTheDocumentedOrdering pins the symmetry between the two
// Snapshot constructors: a rebuilt Snapshot holds the ordering its accessors
// document, as a Graph-built one does.
func TestRebuild_EstablishesTheDocumentedOrdering(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")

	ip := func(key string) snapparts.Instance {
		return snapparts.Instance{
			TypeID:     parentID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}

	// Deliberately unsorted, and with a repeated identity.
	snap := rebuild(s, snapparts.Parts{
		Types: []schema.TypeID{parentID, parentID},
		Instances: []snapparts.Instance{
			ip("p3"),
			ip("p1"),
			ip("p2"),
		},
	})

	if got := snap.Types(); len(got) != 1 {
		t.Errorf("Types() = %d entries, want 1 — a repeated identity was not deduplicated", len(got))
	}
	var keys []string
	for inst := range snap.InstancesOf(parentID) {
		keys = append(keys, inst.PrimaryKey().String())
	}
	if !slices.IsSorted(keys) {
		t.Errorf("InstancesOf is not sorted by primary key: %v", keys)
	}
	// AllInstances walks Types x InstancesOf, so a duplicated identity would
	// yield every instance twice.
	n := 0
	for range snap.AllInstances() {
		n++
	}
	if n != 3 {
		t.Errorf("AllInstances yielded %d instances, want 3", n)
	}
}

// TestSnapshot_DuplicatesComeBackSorted pins the accessor, not the comparator.
// Only compareDuplicates was unit-tested, so deleting newSnapshot's sort left
// Duplicates() in append order — which under concurrent Add is the goroutine
// interleaving, and the marshalled document then varies run to run.
func TestSnapshot_DuplicatesComeBackSorted(t *testing.T) {
	t.Parallel()
	s := rebuildTestSchema(t)
	personID := mustTypeID(t, s, "Person")

	ip := func(key string) snapparts.Instance {
		return snapparts.Instance{
			TypeID:     personID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}
	// Graph.Add records a required association the data does not name.
	absent := func(key string) snapparts.Unresolved {
		return snapparts.Unresolved{SourceType: personID, SourceKey: immutable.WrapKey([]any{key}), Relation: "EMPLOYER", Reason: "absent"}
	}
	// A rejected duplicate of each root, as Graph.Add records it.
	dp := func(key string) snapparts.Instance {
		rejected := ip(key)
		rejected.Properties = immutable.WrapProperties(map[string]any{"id": key, "name": "rejected " + key})
		return rejected
	}

	// Handed in descending key order; the accessor must return ascending.
	snap := rebuild(s, snapparts.Parts{
		Types:      []schema.TypeID{personID},
		Instances:  []snapparts.Instance{ip("d1"), ip("d2"), ip("d3")},
		Duplicates: []snapparts.Instance{dp("d3"), dp("d1"), dp("d2")},
		Unresolved: []snapparts.Unresolved{absent("d1"), absent("d2"), absent("d3")},
	})

	var keys []string
	for d := range snap.Duplicates() {
		keys = append(keys, d.Instance().PrimaryKey().String())
	}
	if !slices.IsSorted(keys) {
		t.Errorf("Duplicates() is not in compareDuplicates order: %v", keys)
	}
}

// Each duplicate reads the root at its own address once, so the cost of the
// records grows with their number, never with its square.
func TestRebuild_DuplicatesCostLinearly(t *testing.T) {
	s := loadAs(t, "schema \"stamped\"\n\ntype Order {\n\tid Timestamp primary\n}\n")
	order := mustTypeID(t, s, "Order")
	allocations := func(k int) float64 {
		parts := snapparts.Parts{
			Instances:  make([]snapparts.Instance, k),
			Duplicates: make([]snapparts.Instance, k),
		}
		for i := range k {
			at := time.Unix(int64(i), 0).UTC().Format(time.RFC3339)
			record := snapparts.Instance{
				TypeID: order, PrimaryKey: immutable.WrapKey([]any{at}),
				Properties: immutable.WrapProperties(map[string]any{"id": at}),
			}
			parts.Instances[i], parts.Duplicates[i] = record, record
		}
		return testing.AllocsPerRun(1, func() { rebuild(s, parts) })
	}
	if small, large := allocations(100), allocations(400); large > 6*small {
		t.Errorf("%.0f allocations at 100 duplicates and %.0f at 400, want growth near 4 times", small, large)
	}
}
