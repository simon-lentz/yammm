package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// BenchmarkSchemaBuilder_CallerCapture pins the per-builder-method call cost
// so the Godoc's "~400–800 ns per builder-method call on M2-class hardware"
// claim (schema_builder.go's SchemaBuilder type, Performance section) is
// quantified rather than asserted. Runs a single Property call in a tight
// b.N loop; each iteration exercises runtime.Callers + runtime.CallersFrames
// plus the property-map write.
//
// Not a CI gate — reports a number operators can cross-check against the
// Godoc claim and their own profile. This complements the full-cycle
// benchmark below (which measures aggregate per-record cost on a realistic
// builder pattern).
func BenchmarkSchemaBuilder_CallerCapture(b *testing.B) {
	s := benchSchema(b)
	builder, err := instance.BuilderFor(s, "Person")
	if err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for b.Loop() {
		builder.Property("name", "Alice")
	}
}

// BenchmarkSchemaBuilder_FullCycle measures the per-record aggregate cost of
// a typical builder usage: construction, ~3 property calls, ~1 edge call,
// Build. Useful for cross-checking the Godoc's "aggregate per-batch overhead
// ~1–10 ms for low-thousands-record batches" framing (schema_builder.go's
// SchemaBuilder type, Performance section).
func BenchmarkSchemaBuilder_FullCycle(b *testing.B) {
	s := benchSchema(b)
	b.ResetTimer()
	for b.Loop() {
		builder, err := instance.BuilderFor(s, "Person")
		if err != nil {
			b.Fatal(err)
		}
		_, err = builder.
			Property("id", "p1").
			Property("name", "Alice").
			Property("nickname", "Al").
			EdgeTo("works_at", "c1").
			Build()
		if err != nil {
			b.Fatal(err)
		}
	}
}

func benchSchema(b *testing.B) *schema.Schema {
	b.Helper()
	s, result := schema.NewBuilder().
		WithName("bench").
		AddType("Company").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("name", schema.NewStringConstraint()).
		WithOptionalProperty("nickname", schema.NewStringConstraint()).
		WithRelation("WORKS_AT", schema.LocalTypeRef("Company", location.Span{}), true, false).
		Done().
		Build()
	if result.HasErrors() {
		b.Fatalf("benchmark schema build: %s", result)
	}
	return s
}
