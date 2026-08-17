package graph_test

import (
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
		Instances: map[schema.TypeID][]graph.InstanceParts{},
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
		Instances: map[schema.TypeID][]graph.InstanceParts{
			mustTypeID(t, s, "Company"): {
				{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
				},
			},
			mustTypeID(t, s, "Person"): {
				{
					TypeName:   "Person",
					TypeID:     mustTypeID(t, s, "Person"),
					PrimaryKey: immutable.WrapKey([]any{"p1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
				},
			},
		},
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
		Instances: map[schema.TypeID][]graph.InstanceParts{
			mustTypeID(t, s, "Company"): {
				{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
				},
			},
			mustTypeID(t, s, "Person"): {
				{
					TypeName:   "Person",
					TypeID:     mustTypeID(t, s, "Person"),
					PrimaryKey: immutable.WrapKey([]any{"p1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
				},
			},
		},
		Edges: []graph.EdgeParts{
			{
				Relation:   "EMPLOYER",
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				TargetType: mustTypeID(t, s, "Company"),
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
		Instances: map[schema.TypeID][]graph.InstanceParts{
			mustTypeID(t, s, "Company"): {
				{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
				},
			},
		},
		Edges: []graph.EdgeParts{
			{
				Relation:   "EMPLOYER",
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				TargetType: mustTypeID(t, s, "Company"),
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
		Instances: map[schema.TypeID][]graph.InstanceParts{
			mustTypeID(t, s, "Company"): {
				{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
				},
			},
		},
		Duplicates: []graph.DuplicateParts{
			{
				Type: mustTypeID(t, s, "Company"),
				Key:  immutable.WrapKey([]any{"c1"}),
				Instance: graph.InstanceParts{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme Corp"}),
				},
				ConflictType: mustTypeID(t, s, "Company"),
				ConflictKey:  immutable.WrapKey([]any{"c1"}),
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
	if dup.Instance.TypeName() != "Company" {
		t.Errorf("duplicate type: got %q", dup.Instance.TypeName())
	}
	if dup.HasDiagnostic() {
		t.Error("loaded duplicate should not have diagnostic")
	}
	if dup.Conflict == nil {
		t.Error("conflict should be resolved")
	}
}

func TestRebuildSnapshot_DuplicateConflictMissing(t *testing.T) {
	s := rebuildTestSchema(t)
	parts := graph.SnapshotParts{
		Types:     []schema.TypeID{mustTypeID(t, s, "Company")},
		Instances: map[schema.TypeID][]graph.InstanceParts{mustTypeID(t, s, "Company"): {}},
		Duplicates: []graph.DuplicateParts{
			{
				Type: mustTypeID(t, s, "Company"),
				Key:  immutable.WrapKey([]any{"c_missing"}),
				Instance: graph.InstanceParts{
					TypeName:   "Company",
					TypeID:     mustTypeID(t, s, "Company"),
					PrimaryKey: immutable.WrapKey([]any{"c_missing"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c_missing", "title": "Missing"}),
				},
				ConflictType: mustTypeID(t, s, "Company"),
				ConflictKey:  immutable.WrapKey([]any{"c_missing"}),
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
		Instances: map[schema.TypeID][]graph.InstanceParts{
			mustTypeID(t, s, "Person"): {
				{
					TypeName:   "Person",
					TypeID:     mustTypeID(t, s, "Person"),
					PrimaryKey: immutable.WrapKey([]any{"p1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "Alice"}),
				},
			},
		},
		Unresolved: []graph.UnresolvedParts{
			{
				SourceType: mustTypeID(t, s, "Person"),
				SourceKey:  immutable.WrapKey([]any{"p1"}),
				Relation:   "EMPLOYER",
				TargetType: mustTypeID(t, s, "Company"),
				TargetKey:  immutable.WrapKey([]any{"c99"}),
				Required:   true,
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
	if u.Relation != "EMPLOYER" {
		t.Errorf("unresolved relation: got %q", u.Relation)
	}
	if u.Source == nil {
		t.Error("source should be resolved")
	}
}

// TestRebuildSnapshot_OneSlotConflictResolvesThroughSlot pins the (one)
// cardinality relation, where the rejected child and the slot's occupant
// share no key: the conflict resolves by the slot address with an empty
// stated key, never by matching the rejected child's own key.
// [TestWireProbe_DuplicateOneSlotConflict] drives the same relation from the
// wire side.
func TestRebuildSnapshot_OneSlotConflictResolvesThroughSlot(t *testing.T) {
	s := testSchemaWithOneComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	occupant := graph.InstanceParts{
		TypeName:   "Child",
		TypeID:     childID,
		PrimaryKey: immutable.WrapKey([]any{"c1"}),
		Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "first"}),
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"child": {occupant}},
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type: childID,
			Key:  immutable.WrapKey([]any{"c2"}),
			Instance: graph.InstanceParts{
				TypeName:   "Child",
				TypeID:     childID,
				PrimaryKey: immutable.WrapKey([]any{"c2"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c2", "name": "second"}),
			},
			ConflictType: childID,
			ParentType:   parentID,
			ParentKey:    immutable.WrapKey([]any{"p1"}),
			Relation:     "child",
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
	conflict := dups[0].Conflict
	if conflict == nil {
		t.Fatal("conflict not resolved")
	}
	if got, want := conflict.PrimaryKey().String(), graph.FormatKey("c1"); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
}

// TestRebuildSnapshot_ManyConflictSelectsByStatedKey pins sibling selection
// in a (many) slot: the stated conflict key picks the surviving sibling.
func TestRebuildSnapshot_ManyConflictSelectsByStatedKey(t *testing.T) {
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	child := func(key, name string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "Child",
			TypeID:     childID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": name}),
		}
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"children": {child("c1", "kept"), child("c2", "other")}},
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type:         childID,
			Key:          immutable.WrapKey([]any{"c1"}),
			Instance:     child("c1", "rejected"),
			ConflictType: childID,
			ConflictKey:  immutable.WrapKey([]any{"c1"}),
			ParentType:   parentID,
			ParentKey:    immutable.WrapKey([]any{"p1"}),
			Relation:     "children",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}

	conflict := snap.Duplicates()[0].Conflict
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

// TestRebuildSnapshot_ConflictTypeMismatchIsReported pins the cross-check on
// the stated conflict type: a slot occupant of a different type never
// resolves as the conflict.
func TestRebuildSnapshot_ConflictTypeMismatchIsReported(t *testing.T) {
	s := testSchemaWithOneComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed: map[string][]graph.InstanceParts{"child": {{
					TypeName:   "Child",
					TypeID:     childID,
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "first"}),
				}}},
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type: childID,
			Key:  immutable.WrapKey([]any{"c2"}),
			Instance: graph.InstanceParts{
				TypeName:   "Child",
				TypeID:     childID,
				PrimaryKey: immutable.WrapKey([]any{"c2"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c2", "name": "second"}),
			},
			ConflictType: parentID,
			ParentType:   parentID,
			ParentKey:    immutable.WrapKey([]any{"p1"}),
			Relation:     "child",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	if !result.HasErrors() {
		t.Error("expected error for a conflict type mismatch")
	}
}

// TestRebuildSnapshot_KeyedConflictTypeMismatchIsReported pins the type
// cross-check on keyed sibling selection: a matching key never resolves a
// conflict whose stated type disagrees with the occupant's.
func TestRebuildSnapshot_KeyedConflictTypeMismatchIsReported(t *testing.T) {
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed: map[string][]graph.InstanceParts{"children": {{
					TypeName:   "Child",
					TypeID:     childID,
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "first"}),
				}}},
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type: childID,
			Key:  immutable.WrapKey([]any{"c1"}),
			Instance: graph.InstanceParts{
				TypeName:   "Child",
				TypeID:     childID,
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "name": "again"}),
			},
			ConflictType: parentID,
			ConflictKey:  immutable.WrapKey([]any{"c1"}),
			ParentType:   parentID,
			ParentKey:    immutable.WrapKey([]any{"p1"}),
			Relation:     "children",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	if !result.HasErrors() {
		t.Error("expected error for a keyed conflict type mismatch")
	}
}

// TestRebuildSnapshot_RootConflictFollowsStatedAddress pins that a root
// conflict resolves at the stated address, never at the duplicate's own
// coordinates.
func TestRebuildSnapshot_RootConflictFollowsStatedAddress(t *testing.T) {
	s := rebuildTestSchema(t)
	companyID := mustTypeID(t, s, "Company")

	parts := graph.SnapshotParts{
		Types: []schema.TypeID{companyID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			companyID: {{
				TypeName:   "Company",
				TypeID:     companyID,
				PrimaryKey: immutable.WrapKey([]any{"c1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c1", "title": "Acme"}),
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type: companyID,
			Key:  immutable.WrapKey([]any{"c9"}),
			Instance: graph.InstanceParts{
				TypeName:   "Company",
				TypeID:     companyID,
				PrimaryKey: immutable.WrapKey([]any{"c9"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "c9", "title": "Ghost"}),
			},
			ConflictType: companyID,
			ConflictKey:  immutable.WrapKey([]any{"c1"}),
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if result.HasErrors() {
		t.Fatalf("RebuildSnapshot: %s", result)
	}
	conflict := snap.Duplicates()[0].Conflict
	if conflict == nil {
		t.Fatal("conflict not resolved")
	}
	if got, want := conflict.PrimaryKey().String(), graph.FormatKey("c1"); got != want {
		t.Errorf("conflict key = %s, want %s", got, want)
	}
}

// TestRebuildSnapshot_EmptyConflictKeyNeedsSoleOccupant pins the strictness
// of slot-alone addressing: with two occupants and no stated key, the
// conflict is ambiguous and never guessed.
func TestRebuildSnapshot_EmptyConflictKeyNeedsSoleOccupant(t *testing.T) {
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")

	child := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "Child",
			TypeID:     childID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "name": key}),
		}
	}
	parts := graph.SnapshotParts{
		Types: []schema.TypeID{parentID},
		Instances: map[schema.TypeID][]graph.InstanceParts{
			parentID: {{
				TypeName:   "Parent",
				TypeID:     parentID,
				PrimaryKey: immutable.WrapKey([]any{"p1"}),
				Properties: immutable.WrapProperties(map[string]any{"id": "p1", "name": "root"}),
				Composed:   map[string][]graph.InstanceParts{"children": {child("c1"), child("c2")}},
			}},
		},
		Duplicates: []graph.DuplicateParts{{
			Type:         childID,
			Key:          immutable.WrapKey([]any{"c3"}),
			Instance:     child("c3"),
			ConflictType: childID,
			ParentType:   parentID,
			ParentKey:    immutable.WrapKey([]any{"p1"}),
			Relation:     "children",
		}},
	}

	snap, result := graph.RebuildSnapshot(s, parts)
	if snap != nil {
		t.Error("expected nil snapshot on error")
	}
	if !result.HasErrors() {
		t.Error("expected error for an ambiguous slot-alone conflict address")
	}
}

// TestRebuildSnapshot_ZeroIdentityPartsRejected pins identity totality at
// the boundary: a zero TypeID at any parts position draws Fatal E_INTERNAL
// naming the position, and no snapshot returns.
func TestRebuildSnapshot_ZeroIdentityPartsRejected(t *testing.T) {
	s := rebuildTestSchema(t)
	companyID := mustTypeID(t, s, "Company")
	company := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "Company",
			TypeID:     companyID,
			PrimaryKey: immutable.WrapKey([]any{key}),
			Properties: immutable.WrapProperties(map[string]any{"id": key, "title": key}),
		}
	}
	zeroInst := func(key string) graph.InstanceParts {
		return graph.InstanceParts{
			TypeName:   "Company",
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
			want:  "types entry 0",
			parts: graph.SnapshotParts{Types: []schema.TypeID{{}}},
		},
		{
			name: "instances group key",
			want: "instance group",
			parts: graph.SnapshotParts{
				Instances: map[schema.TypeID][]graph.InstanceParts{{}: {company("c1")}},
			},
		},
		{
			name: "instance",
			want: "at instance position",
			parts: graph.SnapshotParts{
				Instances: map[schema.TypeID][]graph.InstanceParts{companyID: {zeroInst("c1")}},
			},
		},
		{
			name: "composed child",
			want: "at composed child position",
			parts: graph.SnapshotParts{
				Instances: map[schema.TypeID][]graph.InstanceParts{companyID: {{
					TypeName:   "Company",
					TypeID:     companyID,
					PrimaryKey: immutable.WrapKey([]any{"c1"}),
					Properties: immutable.WrapProperties(map[string]any{"id": "c1"}),
					Composed:   map[string][]graph.InstanceParts{"X": {zeroInst("x1")}},
				}}},
			},
		},
		{
			name: "edge source",
			want: "at edge source position",
			parts: graph.SnapshotParts{
				Edges: []graph.EdgeParts{{Relation: "EMPLOYER", SourceKey: key, TargetType: companyID, TargetKey: key}},
			},
		},
		{
			name: "edge target",
			want: "at edge target position",
			parts: graph.SnapshotParts{
				Edges: []graph.EdgeParts{{Relation: "EMPLOYER", SourceType: companyID, SourceKey: key, TargetKey: key}},
			},
		},
		{
			name: "duplicate",
			want: "at duplicate position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{Key: key, Instance: company("c9"), ConflictType: companyID, ConflictKey: key}},
			},
		},
		{
			name: "duplicate conflict",
			want: "at duplicate conflict position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{Type: companyID, Key: key, Instance: company("c9")}},
			},
		},
		{
			name: "duplicate parent",
			want: "at duplicate parent position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{
					Type: companyID, Key: key, Instance: company("c9"),
					ConflictType: companyID, ConflictKey: key,
					ParentKey: key, Relation: "children",
				}},
			},
		},
		{
			name: "duplicate instance",
			want: "at duplicate instance position",
			parts: graph.SnapshotParts{
				Duplicates: []graph.DuplicateParts{{
					Type: companyID, Key: key, Instance: zeroInst("c9"),
					ConflictType: companyID, ConflictKey: key,
				}},
			},
		},
		{
			name: "unresolved source",
			want: "at unresolved source position",
			parts: graph.SnapshotParts{
				Unresolved: []graph.UnresolvedParts{{SourceKey: key, Relation: "EMPLOYER", TargetType: companyID, TargetKey: key, Reason: "target_missing"}},
			},
		},
		{
			name: "unresolved target",
			want: "at unresolved target position",
			parts: graph.SnapshotParts{
				Unresolved: []graph.UnresolvedParts{{SourceType: companyID, SourceKey: key, Relation: "EMPLOYER", TargetKey: key, Reason: "target_missing"}},
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
