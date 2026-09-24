package snapshot_test

import (
	"context"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// importInto and seedInto are the two import constructors.
func importInto(s *schema.Schema, snap *graph.Snapshot) (*graph.Graph, diag.Result) {
	return graph.NewFromSnapshot(s, snap)
}

func seedInto(ctx context.Context, s *schema.Schema, snap *graph.Snapshot) (*graph.BatchAssembler, diag.Result) {
	return graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)
}

// partsOf restates snap as the parts RebuildSnapshot takes.
func partsOf(t *testing.T, snap *graph.Snapshot) graph.SnapshotParts {
	t.Helper()
	parts := graph.SnapshotParts{Types: snap.Types()}
	for inst := range snap.AllInstances() {
		parts.Instances = append(parts.Instances, instanceParts(inst))
	}
	for _, e := range snap.Edges() {
		parts.Edges = append(parts.Edges, graph.EdgeParts{
			Relation: e.Relation(), SourceType: e.Source().TypeID(), SourceKey: e.Source().PrimaryKey(),
			TargetKey: e.Target().PrimaryKey(), Properties: e.Properties(),
		})
	}
	for _, d := range snap.Duplicates() {
		dp := graph.DuplicateParts{
			Instance: instanceParts(d.Instance()),
			Relation: d.Relation(),
		}
		if d.Parent() != nil {
			dp.ParentType, dp.ParentKey = d.Parent().TypeID(), d.Parent().PrimaryKey()
		}
		parts.Duplicates = append(parts.Duplicates, dp)
	}
	for _, u := range snap.Unresolved() {
		var key immutable.Key
		if u.TargetKey() != "" {
			vals, err := graph.ParseKey(u.TargetKey())
			if err != nil {
				t.Fatalf("parse unresolved key %s: %v", u.TargetKey(), err)
			}
			key = immutable.WrapKey(vals)
		}
		parts.Unresolved = append(parts.Unresolved, graph.UnresolvedParts{
			SourceType: u.Source().TypeID(), SourceKey: u.Source().PrimaryKey(), Relation: u.Relation(),
			TargetKey: key, Reason: u.Reason(), Properties: u.Properties(),
		})
	}
	return parts
}

func instanceParts(inst *graph.Instance) graph.InstanceParts {
	ip := graph.InstanceParts{TypeID: inst.TypeID(), PrimaryKey: inst.PrimaryKey(), Properties: inst.Properties()}
	for _, rel := range inst.ComposedRelations() {
		if ip.Composed == nil {
			ip.Composed = map[string][]graph.InstanceParts{}
		}
		for _, child := range inst.Composed(rel) {
			ip.Composed[rel] = append(ip.Composed[rel], instanceParts(child))
		}
	}
	return ip
}
