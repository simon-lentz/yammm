package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
)

// BenchmarkAdd_WithComposedChildren measures the per-Add cost on a shape with
// composed children, which is what the structural guard walks.
func BenchmarkAdd_WithComposedChildren(b *testing.B) {
	t := &testing.T{}
	s := testSchemaWithComposition(t) // Parent -> (many) Child (Child has a PK)
	parentID := mustTypeID(t, s, "Parent")
	childID := mustTypeID(t, s, "Child")
	ctx := b.Context()

	kids := make([]any, 8)
	for i := range kids {
		kids[i] = instancetest.VI(
			"Child",
			instancetest.TypeID(childID),
			instancetest.PK(string(rune('a'+i))),
			instancetest.Props(map[string]any{"id": string(rune('a' + i)), "name": "child"}),
		)
	}
	composed := map[string]immutable.Value{"children": immutable.Wrap(kids)}

	insts := make([]*instance.ValidInstance, b.N)
	for i := range insts {
		insts[i] = instancetest.VI(
			"Parent",
			instancetest.TypeID(parentID),
			instancetest.PK(i),
			instancetest.Props(map[string]any{"id": i, "name": "parent"}),
			instancetest.Composed(composed),
		)
	}

	g := graph.New(s)
	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		if res := g.Add(ctx, insts[i]); res.HasFatal() {
			b.Fatal(res.String())
		}
	}
}

// BenchmarkAdd_PlainInstance measures the same on an instance with no edges
// and no compositions — the shape the guard can do nothing but skip.
func BenchmarkAdd_PlainInstance(b *testing.B) {
	t := &testing.T{}
	s := testSchemaWithComposition(t)
	parentID := mustTypeID(t, s, "Parent")
	ctx := b.Context()

	insts := make([]*instance.ValidInstance, b.N)
	for i := range insts {
		insts[i] = instancetest.VI(
			"Parent",
			instancetest.TypeID(parentID),
			instancetest.PK(i),
			instancetest.Props(map[string]any{"id": i, "name": "parent"}),
		)
	}

	g := graph.New(s)
	b.ResetTimer()
	b.ReportAllocs()
	for i := range b.N {
		if res := g.Add(ctx, insts[i]); res.HasFatal() {
			b.Fatal(res.String())
		}
	}
}
