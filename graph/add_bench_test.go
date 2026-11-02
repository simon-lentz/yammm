package graph_test

import (
	"strconv"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
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
	s := benchFixture(b)
	ctx := b.Context()

	kids := make([]any, 8)
	for i := range kids {
		kids[i] = map[string]any{"id": string(rune('a' + i)), "name": "child"}
	}

	insts := make([]instance.ValidInstance, b.N)
	for i := range insts {
		insts[i] = instancetest.Valid(b, s, "Parent", map[string]any{"id": strconv.Itoa(i), "name": "parent", "children": kids})
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
	s := benchFixture(b)
	ctx := b.Context()

	insts := make([]instance.ValidInstance, b.N)
	for i := range insts {
		insts[i] = instancetest.Valid(b, s, "Parent", map[string]any{"id": strconv.Itoa(i), "name": "parent"})
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
func benchFixture(b *testing.B) schema.Schema {
	b.Helper()
	s, res := schema.LoadString(b.Context(), `schema "composition"

part type Child {
	id String primary
	name String required
}

type Parent {
	id String primary
	name String required
	*-> CHILDREN (many) Child
}
`, "bench_composition.yammm")
	if res.HasErrors() {
		b.Fatalf("build bench schema: %s", res.String())
	}
	return s
}
