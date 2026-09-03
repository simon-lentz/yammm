package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// BenchmarkAdd_WithComposedChildren measures the per-Add cost on a shape with
// composed children, which is what the builder walks.
//
// Every instance carries a distinct key, so one graph grows across b.N and the
// duplicate-PK map lookup it reports is the cost at increasing size, not a
// fixed one. Read it as a relative measure between tree shapes, which is what
// it was added for, and not as an absolute per-Add cost.
func BenchmarkAdd_WithComposedChildren(b *testing.B) {
	s, parentID, childID := benchFixture(b)
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
	composed := map[string]immutable.Value{"CHILDREN": immutable.Wrap(kids)}

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
		if res := g.Add(ctx, insts[i]); !res.OK() {
			b.Fatal(res.String())
		}
	}
}

// BenchmarkAdd_PlainInstance measures the same on an instance with no edges
// and no compositions — the shape the guard can do nothing but skip.
func BenchmarkAdd_PlainInstance(b *testing.B) {
	s, parentID, _ := benchFixture(b)
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
		if res := g.Add(ctx, insts[i]); !res.OK() {
			b.Fatal(res.String())
		}
	}
}

// benchFixture builds the benchmark schema without a zero-value *testing.T,
// whose Fatalf would panic on a nil runner instead of failing the benchmark.
func benchFixture(b *testing.B) (*schema.Schema, schema.TypeID, schema.TypeID) {
	b.Helper()
	s, res := schema.NewBuilder().
		WithName("composition").
		WithSourceID(location.MustNewSourceID("test://bench_composition.yammm")).
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("CHILDREN", schema.LocalTypeRef("Child", location.Span{}), true, true).
		Done().
		Build()
	if res.HasErrors() {
		b.Fatalf("build bench schema: %s", res.String())
	}
	parent, ok := s.Type("Parent")
	if !ok {
		b.Fatal("Parent not in the bench schema")
	}
	child, ok := s.Type("Child")
	if !ok {
		b.Fatal("Child not in the bench schema")
	}
	return s, parent.ID(), child.ID()
}
