package graph_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func rebuildTestSchema(t *testing.T) *schema.Schema {
	t.Helper()
	s, result := schema.NewBuilder().
		WithName("rebuild_test").
		WithSourceID(location.MustNewSourceID("test://rebuild.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithRelation("EMPLOYER", schema.NewTypeRef("", "Company", location.Span{}), false, false).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("title", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		t.Fatalf("rebuildTestSchema: %s", result)
	}
	return s
}

func TestRebuildSnapshot_EmptyParts(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types:     []schema.TypeID{},
		Instances: []graph.InstanceParts{},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if len(snap.Types()) != 0 {
		t.Errorf("expected 0 types, got %d", len(snap.Types()))
	}
}

func TestRebuildSnapshot_WithInstances(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company"), mustTypeID(t, s, "Person")},
		Instances: []graph.InstanceParts{
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
		Unresolved: []graph.UnresolvedParts{{
			SourceType: mustTypeID(t, s, "Person"), SourceKey: immutable.WrapKey([]any{"p1"}),
			Relation: "EMPLOYER", Reason: "absent",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	if len(snap.Types()) != 2 {
		t.Errorf("expected 2 types, got %d", len(snap.Types()))
	}
	if len(snap.InstancesOf(mustTypeID(t, s, "Company"))) != 1 {
		t.Errorf("expected 1 Company, got %d", len(snap.InstancesOf(mustTypeID(t, s, "Company"))))
	}
	if len(snap.InstancesOf(mustTypeID(t, s, "Person"))) != 1 {
		t.Errorf("expected 1 Person, got %d", len(snap.InstancesOf(mustTypeID(t, s, "Person"))))
	}
}

func TestRebuildSnapshot_WithEdges(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company"), mustTypeID(t, s, "Person")},
		Instances: []graph.InstanceParts{
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
		Edges: []graph.EdgeParts{
			{
				Relation:   "EMPLOYER",
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				TargetKey:  immutable.WrapKey([]any{"c1"}),
				Properties: immutable.Properties{},
			},
		},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	edges := snap.Edges()
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge, got %d", len(edges))
	}

	e := edges[0]
	if e.Relation() != "EMPLOYER" {
		t.Errorf("edge relation: got %q, want %q", e.Relation(), "EMPLOYER")
	}

	// EdgesFrom should also work.
	persons := snap.InstancesOf(mustTypeID(t, s, "Person"))
	if len(persons) != 1 {
		t.Fatalf("expected 1 Person")
	}
	edgesFrom := snap.EdgesFrom(persons[0])
	if len(edgesFrom) != 1 {
		t.Errorf("EdgesFrom: expected 1, got %d", len(edgesFrom))
	}
}

func TestRebuildSnapshot_EdgeMissingSource(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company")},
		Instances: []graph.InstanceParts{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
		},
		Edges: []graph.EdgeParts{
			{
				Relation:   "EMPLOYER",
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				TargetKey:  immutable.WrapKey([]any{"c1"}),
			},
		},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	if !result.HasErrors() {
		t.Error("expected error for missing edge source")
	}

	found := false
	for issue := range result.Errors() {
		if issue.Code() == diag.E_INTERNAL {
			found = true
		}
	}
	if !found {
		t.Error("expected E_INTERNAL diagnostic")
	}
}

func TestRebuildSnapshot_WithDuplicates(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{mustTypeID(t, s, "Company")},
		Instances: []graph.InstanceParts{
			{
				TypeID:     mustTypeID(t, s, "Company"),
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
		},
		Duplicates: []graph.DuplicateParts{
			{
				Instance: graph.InstanceParts{
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme Corp"}),
				},
			},
		},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	dups := snap.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(dups))
	}

	dup := dups[0]
	if dup.Instance().TypeName() != "Company" {
		t.Errorf("duplicate type: got %q", dup.Instance().TypeName())
	}
	if !dup.Diagnostic().IsZero() {
		t.Error("loaded duplicate should not have diagnostic")
	}
	if dup.Conflict() == nil {
		t.Error("conflict should be resolved")
	}
}

func TestRebuildSnapshot_DuplicateConflictMissing(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types:     []schema.TypeID{mustTypeID(t, s, "Company")},
		Instances: []graph.InstanceParts{},
		Duplicates: []graph.DuplicateParts{
			{
				Instance: graph.InstanceParts{
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c_missing"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c_missing", "title": "Missing"}),
				},
			},
		},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	if !result.HasErrors() {
		t.Error("expected error for missing conflict")
	}
}

func TestRebuildSnapshot_WithUnresolved(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{mustTypeID(t, s, "Person")},
		Instances: []graph.InstanceParts{
			{
				TypeID:     mustTypeID(t, s, "Person"),
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
			},
		},
		Unresolved: []graph.UnresolvedParts{
			{
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				Relation:   "EMPLOYER",
				TargetKey:  immutable.WrapKey([]any{"c99"}),
				Reason:     "target_missing",
			},
		},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	unresolved := snap.Unresolved()
	if len(unresolved) != 1 {
		t.Fatalf("expected 1 unresolved, got %d", len(unresolved))
	}

	u := unresolved[0]
	if u.Relation() != "EMPLOYER" {
		t.Errorf("unresolved relation: got %q", u.Relation())
	}
	if u.Source() == nil {
		t.Error("source should be resolved")
	}
}

// TestRebuildSnapshot_OneSlotConflictResolvesThroughSlot pins the (one)
// cardinality relation, where the rejected child and the slot's occupant
// share no key: the conflict resolves by the slot address with an empty
// stated key, never by matching the rejected child's own key.
// TestWireProbe_DuplicateOneSlotConflict in the snapshot package drives the
// same relation from the wire side.
func TestRebuildSnapshot_OneSlotConflictResolvesThroughSlot(t *testing.T) {
	s := testSchemaWithOneComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	occupant := graph.InstanceParts{
		TypeID:     childID,
		PrimaryKey: immutable.WrapKey([]any{"c1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "first"}),
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: []graph.InstanceParts{
			{
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"CHILD": {occupant}},
			},
		},
		Duplicates: []graph.DuplicateParts{{
			Instance: graph.InstanceParts{
				TypeID:     childID,
				PrimaryKey: immutable.WrapKey([]any{"c2"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c2", "name": "second"}),
			},
			ParentType: parentID,
			ParentKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "CHILD",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	dups := snap.Duplicates()
	if len(dups) != 1 {
		t.Fatalf("expected 1 duplicate, got %d", len(dups))
	}
	conflict := dups[0].Conflict()
	if conflict == nil {
		t.Fatal("conflict not resolved")
	}
	if got, want := conflict.PrimaryKey().String(), graph.FormatKey("c1"); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
}

// TestRebuildSnapshot_ManyConflictIsTheSiblingAtItsKey pins sibling selection
// in a keyed (many) slot: the rejected child's own key picks the surviving
// sibling, as Graph.AddComposed records it.
func TestRebuildSnapshot_ManyConflictIsTheSiblingAtItsKey(t *testing.T) {
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	child := func(key, name string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     childID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": name}),
		}
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: []graph.InstanceParts{
			{
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"CHILDREN": {child("c1", "kept"), child("c2", "other")}},
			},
		},
		Duplicates: []graph.DuplicateParts{{
			Instance:   child("c1", "rejected"),
			ParentType: parentID,
			ParentKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "CHILDREN",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	conflict := snap.Duplicates()[0].Conflict()
	if conflict == nil {
		t.Fatal("conflict not resolved")
	}
	if got, want := conflict.PrimaryKey().String(), graph.FormatKey("c1"); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
	v, ok := conflict.Property("name")
	if !ok {
		t.Fatal("conflict has no name property")
	}
	if got, _ := v.String(); got != "kept" {
		t.Errorf("conflict resolved to the wrong sibling: name = %q, want %q", got, "kept")
	}
}

// A composed duplicate is an instance of its slot's declared target, as
// Graph.AddComposed refuses any other child before it records one.
func TestRebuildSnapshot_RefusesAComposedDuplicateOutsideItsSlotsTarget(t *testing.T) {
	s := testSchemaWithOneComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: []graph.InstanceParts{
			{
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed: map[string][]graph.InstanceParts{"CHILD": {{
					TypeID:     childID,
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "first"}),
				}}},
			},
		},
		Duplicates: []graph.DuplicateParts{{
			Instance: graph.InstanceParts{
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p2"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p2", "name": "stray"}),
			},
			ParentType: parentID,
			ParentKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "CHILD",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	requireFatalNaming(t, result, "the composition \"CHILD\" declares "+childID.String())
}

// A root duplicate collides with the root at its own type and key, as
// Graph.Add records it; a record whose own key holds no root has no conflict.
func TestRebuildSnapshot_RefusesARootDuplicateWithNoRootAtItsKey(t *testing.T) {
	s := rebuildTestSchema(t)
	companyID := mustTypeID(t, s, "Company")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{companyID},
		Instances: []graph.InstanceParts{
			{
				TypeID:     companyID,
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			},
		},
		Duplicates: []graph.DuplicateParts{{
			Instance: graph.InstanceParts{
				TypeID:     companyID,
				PrimaryKey: immutable.WrapKey([]any{"c9"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c9", "title": "Ghost"}),
			},
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	requireFatalNaming(t, result, "no root is at its own type and key")
}

// requireFatalNaming requires a Fatal E_INTERNAL whose message holds phrase.
func requireFatalNaming(t *testing.T, result diag.Result, phrase string) {
	t.Helper()
	for issue := range result.Issues() {
		if issue.Severity() == diag.Fatal && issue.Code() == diag.E_INTERNAL && strings.Contains(issue.Message(), phrase) {
			return
		}
	}
	t.Errorf("want Fatal E_INTERNAL naming %q, got: %s", phrase, result)
}

// A keyed (many) duplicate collides only with the sibling at its own key, so a
// record whose key no sibling holds has no conflict, however many siblings the
// slot holds.
func TestRebuildSnapshot_RefusesAManyDuplicateWithNoSiblingAtItsKey(t *testing.T) {
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	child := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     childID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: []graph.InstanceParts{
			{
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"CHILDREN": {child("c1"), child("c2")}},
			},
		},
		Duplicates: []graph.DuplicateParts{{
			Instance:   child("c3"),
			ParentType: parentID,
			ParentKey:  immutable.WrapKey([]any{"p1"}),
			Relation:   "CHILDREN",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	requireFatalNaming(t, result, `holds no child of the composition "CHILDREN" at its key`)
}

// TestRebuildSnapshot_ZeroIdentityPartsRejected pins identity totality at
// the boundary: a zero TypeID at any parts position draws Fatal E_INTERNAL
// stating a zero identity at the position, and no snapshot returns.
func TestRebuildSnapshot_ZeroIdentityPartsRejected(t *testing.T) {
	s := rebuildTestSchema(t)
	companyID := mustTypeID(t, s, "Company")
	company := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     companyID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "title": key}),
		}
	}
	zeroInst := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key}),
		}
	}
	key := immutable.WrapKey([]any{"k1"})

	cases := []struct {
		name  string
		want  string
		parts graph.SnapshotParts
	}{
		{
			name:  "types entry",
			want:  "zero type identity at types entry 0",
			parts: graph.SnapshotParts{Types: []schema.TypeID{{}}},
		},
		{
			name: "instance",
			want: "zero type identity at instance position",
			parts: graph.SnapshotParts{
				Instances: []graph.InstanceParts{
					zeroInst("c1"),
				},
			},
		},
		{
			name: "composed child",
			want: "zero type identity at composed child position",
			parts: graph.SnapshotParts{
				Instances: []graph.InstanceParts{
					{
						TypeID:     companyID,
						PrimaryKey: immutable.WrapKey([]any{"c1"}),
						Properties: immutable.WrapProperties(map[string]any{"id": "c1"}),
						Composed:   map[string][]graph.InstanceParts{"X": {zeroInst("x1")}},
					},
				},
			},
		},
		{
			name: "edge source",
			want: "zero type identity at edge source position",
			parts: graph.SnapshotParts{
				Edges: []graph.EdgeParts{{Relation: "EMPLOYER", SourceKey: key, TargetKey: key}},
			},
		},
		{
			name: "duplicate parent",
			want: "zero type identity at duplicate parent position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{
					Instance:  company("c9"),
					ParentKey: key, Relation: "children",
				}},
			},
		},
		{
			name: "duplicate instance",
			want: "zero type identity at duplicate instance position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{
					Instance: zeroInst("c9"),
				}},
			},
		},
		{
			name: "unresolved source",
			want: "zero type identity at unresolved source position",
			parts: graph.SnapshotParts{
				Unresolved: []graph.UnresolvedParts{{SourceKey: key, Relation: "EMPLOYER", TargetKey: key, Reason: "target_missing"}},
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			snap, result := graph.RebuildSnapshot(s, tc.parts)
			if snap != nil {
				t.Error("expected nil snapshot")
			}
			found := false
			for issue := range result.Errors() {
				if issue.Code() == diag.E_INTERNAL && issue.Severity() == diag.Fatal &&
					strings.Contains(issue.Message(), tc.want) {
					found = true
				}
			}
			if !found {
				t.Errorf("expected Fatal E_INTERNAL naming %q, got: %s", tc.want, result)
			}
		})
	}
}

// TestRebuildSnapshot_EstablishesTheDocumentedOrdering pins the symmetry
// between the two Snapshot constructors. RebuildSnapshot sorted only edges and
// types, so a caller-assembled Snapshot violated the ordering its own
// accessors document while a Graph-built one honoured it.
func TestRebuildSnapshot_EstablishesTheDocumentedOrdering(t *testing.T) {
	t.Parallel()
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")

	ip := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     parentID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}

	// Deliberately unsorted, and with a repeated identity.
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types: []schema.TypeID{parentID, parentID},
		Instances: []graph.InstanceParts{
			ip("p3"),
			ip("p1"),
			ip("p2"),
		},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res.String())
	}

	if got := snap.Types(); len(got) != 1 {
		t.Errorf("Types() = %d entries, want 1 — a repeated identity was not deduplicated", len(got))
	}
	var keys []string
	for _, inst := range snap.InstancesOf(parentID) {
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

	ip := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeID:     personID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}
	// Graph.Add records a required association the data does not name.
	absent := func(key string) graph.UnresolvedParts {
		return graph.UnresolvedParts{SourceType: personID, SourceKey: immutable.WrapKey([]any{key}), Relation: "EMPLOYER", Reason: "absent"}
	}
	// A rejected duplicate of each root, as Graph.Add records it.
	dp := func(key string) graph.DuplicateParts {
		rejected := ip(key)
		rejected.Properties = immutable.WrapProperties(map[string]any{"id": key, "name": "rejected " + key})
		return graph.DuplicateParts{Instance: rejected}
	}

	// Handed in descending key order; the accessor must return ascending.
	snap, res := graph.RebuildSnapshot(s, graph.SnapshotParts{
		Types:      []schema.TypeID{personID},
		Instances:  []graph.InstanceParts{ip("d1"), ip("d2"), ip("d3")},
		Duplicates: []graph.DuplicateParts{dp("d3"), dp("d1"), dp("d2")},
		Unresolved: []graph.UnresolvedParts{absent("d1"), absent("d2"), absent("d3")},
	})
	if res.HasErrors() {
		t.Fatalf("rebuild: %s", res.String())
	}

	var keys []string
	for _, d := range snap.Duplicates() {
		keys = append(keys, d.Instance().PrimaryKey().String())
	}
	if !slices.IsSorted(keys) {
		t.Errorf("Duplicates() is not in compareDuplicates order: %v", keys)
	}
}
