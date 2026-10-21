package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// BenchmarkSchemaBuilder_CallerCapture times the per-call cost the
// SchemaBuilder type's Performance section names: one Property call in a loop,
// each a runtime.Callers capture and the property-map write. The PC is
// symbolized only when an error is rendered, which this loop never does.
//
// No gate reads its number, which operators can compare with their own
// profile; scripts/test.sh runs it once. The full-cycle benchmark below
// measures the aggregate per-record cost of a realistic builder pattern.
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
// Build.
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
