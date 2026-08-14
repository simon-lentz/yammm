package graph_test

import (
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
