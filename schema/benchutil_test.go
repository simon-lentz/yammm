package schema_test

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// buildTinyBenchSchema returns a minimal schema: one type with a PK plus two
// scalar properties. Used as the representative-schema reference point in the
// benchmark suite so the hash-cost impact of the idempotence path can be
// observed at small scale before scaling up via buildLargeBenchSchema.
//
// The schema registers without errors against a fresh Registry.
func buildTinyBenchSchema(tb testing.TB) *schema.Schema {
	tb.Helper()
	s, result := schema.NewBuilder().
		WithName("tiny_bench").
		WithSourceID(location.MustNewSourceID("bench://tiny_bench.yammm")).
		AddType("Entity").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithProperty("label", schema.NewStringConstraint()).
		Done().
		Build()
	if result.HasErrors() {
		tb.Fatalf("buildTinyBenchSchema: %v", result.Messages())
	}
	if s == nil {
		tb.Fatal("buildTinyBenchSchema: nil schema")
	}
	return s
}

// buildLargeBenchSchema returns a synthetic wide schema sized to match the
// complexity-ceiling target called out in api_enhancements.md §7's benchmark
// subsection: 120 types with 5 properties each (600 properties total) plus
// 60 associations distributed across the first 60 types (one each, pointing
// to type (i+7)%120).
//
// The schema has a deterministic shape: types are named Type000..Type119,
// properties are named pN_M where N is the type index and M is the property
// index within the type. The +7 stride produces a non-trivial reference
// graph (not self-loops, not all forward) so StructuralHash walks a
// realistic mix of refs.
//
// Use this helper for any future benchmark or stress test that wants a
// wide-but-valid schema without re-authoring the shape.
func buildLargeBenchSchema(tb testing.TB) *schema.Schema {
	tb.Helper()

	const (
		typeCount         = 120
		propsPerType      = 5 // 1 PK + 4 scalars
		associationCount  = 60
		associationStride = 7 // coprime to typeCount so (i+7)%120 visits many targets
	)

	b := schema.NewBuilder().
		WithName("large_bench").
		WithSourceID(location.MustNewSourceID("bench://large_bench.yammm"))

	for i := range typeCount {
		typeName := fmt.Sprintf("Type%03d", i)
		tb2 := b.AddType(typeName).
			WithPrimaryKey(fmt.Sprintf("p%03d_0", i), schema.NewStringConstraint())
		for p := 1; p < propsPerType; p++ {
			tb2.WithProperty(fmt.Sprintf("p%03d_%d", i, p), schema.NewStringConstraint())
		}
		// Add one association to the first associationCount types so each
		// association lands on a distinct type (no duplicate-type errors at
		// Build time). The target is resolved via LocalTypeRef; the symbol
		// resolver walks the builder's type list and finds TypeJ regardless
		// of declaration order.
		if i < associationCount {
			dst := fmt.Sprintf("Type%03d", (i*associationStride)%typeCount)
			tb2.WithRelation(
				fmt.Sprintf("assoc%03d", i),
				schema.LocalTypeRef(dst, location.Span{}),
				true, // optional
				true, // many
			)
		}
		b = tb2.Done()
	}

	s, result := b.Build()
	if result.HasErrors() {
		tb.Fatalf("buildLargeBenchSchema: %v", result.Messages())
	}
	if s == nil {
		tb.Fatal("buildLargeBenchSchema: nil schema")
	}
	return s
}
