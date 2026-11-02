package graph

import (
	"context"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
)

// MergeForTest reaches the add [BatchAssembler.AddValidOrMerge] runs, with the
// [diag.Result] an assembler wraps.
func (g *Graph) MergeForTest(ctx context.Context, inst instance.ValidInstance) (bool, diag.Result) {
	return g.add(ctx, inst, true)
}
