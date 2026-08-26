package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The validator's per-instance cost has three parts a change to value
// normalization can move independently: property checking and coercion,
// invariant evaluation over scalars, and invariant evaluation over a
// collection — the last because a List property reaches the evaluator as an
// immutable.Slice and every consumer decides for itself what to do with it.
//
// The three benchmarks below separate those parts so a regression names its
// own cause. Without them a normalization change is argued rather than
// measured: nothing else in this repository times validation.
//
// Not a CI gate. Gate 7 proves each one still runs; the numbers are for
// cross-checking a profile.

// benchValidatorSchema loads a one-type schema carrying decls and invs. It
// takes source rather than the schema builder because an invariant has no
// builder form.
func benchValidatorSchema(b *testing.B, decls, invs string) *schema.Schema {
	b.Helper()
	src := "schema \"vbench\"\n\ntype T {\n\tid String primary\n" + decls + invs + "}\n"
	s, result := schema.LoadString(b.Context(), src, "vbench.yammm")
	if result.HasErrors() {
		b.Fatalf("benchmark schema did not load: %s", result)
	}
	return s
}

// benchValidate runs ValidateOne in a tight loop and fails on the first
// instance that does not validate — a benchmark measuring the error path
// would report a number for work the caller never does.
func benchValidate(b *testing.B, s *schema.Schema, props map[string]any) {
	b.Helper()
	v := instance.NewValidator(s)
	ctx := b.Context()
	b.ResetTimer()
	for b.Loop() {
		if _, res := v.ValidateOne(ctx, "T", instance.RawInstance{Properties: props}); !res.OK() {
			b.Fatalf("instance did not validate: %s", res)
		}
	}
}

const benchDecls = "\tname String required\n\tcount Integer\n\tratio Float\n\ttags List<String>\n"

// BenchmarkValidateOne_NoInvariants is the baseline: property checking and
// coercion with no expression evaluation at all. A normalization change that
// moves this number moved the checkers, not the evaluator.
func BenchmarkValidateOne_NoInvariants(b *testing.B) {
	s := benchValidatorSchema(b, benchDecls, "")
	benchValidate(b, s, benchValidatorProps())
}

// BenchmarkValidateOne_ScalarInvariants adds three scalar invariants. The
// delta from the baseline is scope construction plus scalar evaluation.
func BenchmarkValidateOne_ScalarInvariants(b *testing.B) {
	const invs = "\t! \"named\" name != \"\"\n" +
		"\t! \"counted\" count > 0\n" +
		"\t! \"bounded\" ratio < 1.0\n"
	s := benchValidatorSchema(b, benchDecls, invs)
	benchValidate(b, s, benchValidatorProps())
}

// BenchmarkValidateOne_ListInvariants evaluates over a List property, which
// is the path a value-normalization change touches hardest: the property
// leaves the scope as an immutable.Slice and each consumer converts it.
// Indexing and Len are used because both work today, so this benchmark
// measures the same construct before and after such a change.
func BenchmarkValidateOne_ListInvariants(b *testing.B) {
	const invs = "\t! \"first_tag\" tags[0] == \"alpha\"\n" +
		"\t! \"tag_count\" tags -> Len == 3\n" +
		"\t! \"tags_unique\" tags -> Unique -> Len == 3\n"
	s := benchValidatorSchema(b, benchDecls, invs)
	benchValidate(b, s, benchValidatorProps())
}

func benchValidatorProps() map[string]any {
	return map[string]any{
		"id":    "a",
		"name":  "Alice",
		"count": int64(7),
		"ratio": 0.5,
		"tags":  []any{"alpha", "beta", "gamma"},
	}
}

// The two benchmarks below share a schema, an instance and a workload; they
// differ only in whether the invariants NAME a relation. The delta is the cost
// unit 7 added — building the relation scope and evaluating over it — isolated
// from the edge and composition validation both pay.
//
// The three benchmarks above use a relation-free schema, where invariantScope
// returns the property map unchanged, so they cannot see this at all.

func relationBenchSchema(b *testing.B, invariants string) *schema.Schema {
	b.Helper()
	src := `schema "vbench"

type Company { id String primary }

part type Item {
    sku String primary
    qty Integer
}

type Order {
    id String primary
    --> WORKS_AT (_:one) Company
    *-> ITEMS (_:many) Item
` + invariants + `}
`
	s, result := schema.LoadString(b.Context(), src, "vbench.yammm")
	if result.HasErrors() {
		b.Fatalf("benchmark schema did not load: %s", result)
	}
	return s
}

func relationBenchProps() map[string]any {
	return map[string]any{
		"id":       "o1",
		"works_at": map[string]any{"_target_id": "c1"},
		"items": []any{
			map[string]any{"sku": "s1", "qty": int64(2)},
			map[string]any{"sku": "s2", "qty": int64(3)},
		},
	}
}

func benchOrder(b *testing.B, s *schema.Schema) {
	b.Helper()
	v := instance.NewValidator(s)
	ctx := b.Context()
	props := relationBenchProps()
	b.ResetTimer()
	for b.Loop() {
		if _, res := v.ValidateOne(ctx, "Order", instance.RawInstance{Properties: props}); !res.OK() {
			b.Fatalf("instance did not validate: %s", res)
		}
	}
}

// BenchmarkValidateOne_RelationsPropertyInvariants is the control: relations
// present and validated, but no invariant names one.
func BenchmarkValidateOne_RelationsPropertyInvariants(b *testing.B) {
	benchOrder(b, relationBenchSchema(b, "\n    ! \"has_id\" id != \"\"\n"))
}

// BenchmarkValidateOne_RelationInvariants names relations in three invariants,
// so the scope carries edge targets and composed children.
func BenchmarkValidateOne_RelationInvariants(b *testing.B) {
	benchOrder(b, relationBenchSchema(b, `
    ! "has_company" works_at != nil
    ! "has_items" ITEMS -> Len > 0
    ! "positive" ITEMS -> All |$i| { $i.qty > 0 }
`))
}
