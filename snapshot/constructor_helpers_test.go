package snapshot_test

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/schema"
)

// importInto and seedInto are the two import constructors.
func importInto(s schema.Schema, snap graph.Snapshot) (*graph.Graph, diag.Result) {
	return graph.NewFromSnapshot(s, snap)
}

func seedInto(ctx context.Context, s schema.Schema, snap graph.Snapshot) (*graph.BatchAssembler, diag.Result) {
	return graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)
}
