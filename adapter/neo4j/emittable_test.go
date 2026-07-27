package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// The label-identifier gate in emittableTypes is the sole such gate for
// constraints, indexes, AND shape, and it is live behavior rather than defence:
// the CLI exposes --separator, and a separator that is not a legal Neo4j
// identifier character makes every label invalid. Without the gate the emitters
// produce Cypher the server rejects at apply time, with no yammm diagnostic at
// all.
func TestEmittableTypes_InvalidLabelIsGated(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	// "-" is legal in the CLI flag but not in an unquoted Neo4j identifier.
	a := New(WithLabelSeparator("-"))
	ctx := context.Background()

	t.Run("indexes", func(t *testing.T) {
		t.Parallel()
		got, result := a.IndexesStructured(ctx, s)
		assertGated(t, got == nil, result.HasCode(E_NEO4J_INVALID_IDENTIFIER), result.Err())
	})
	t.Run("constraints", func(t *testing.T) {
		t.Parallel()
		got, result := a.ConstraintsStructured(ctx, s)
		assertGated(t, got == nil, result.HasCode(E_NEO4J_INVALID_IDENTIFIER), result.Err())
	})
	t.Run("shape", func(t *testing.T) {
		t.Parallel()
		got, result := a.ShapeForSchema(ctx, s)
		assertGated(t, got == nil, result.HasCode(E_NEO4J_INVALID_IDENTIFIER), result.Err())
	})
}

func assertGated(t *testing.T, gotNil, hasCode bool, err error) {
	t.Helper()
	if !hasCode {
		t.Errorf("want E_NEO4J_INVALID_IDENTIFIER for a label the separator made illegal, got: %v", err)
	}
	if !gotNil {
		t.Error("an invalid label must suppress emission, not be emitted into Cypher the server rejects")
	}
}

// A valid separator still emits, so the gate above turns on the label actually
// being illegal rather than on the separator being non-default.
func TestEmittableTypes_ValidSeparatorStillEmits(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	got, result := New(WithLabelSeparator("_")).IndexesStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("an underscore separator is a legal identifier character: %v", result.Err())
	}
	if len(got) == 0 {
		t.Fatal("expected indexes to be emitted")
	}
	// The default separator is "__", which also satisfies a "index_test_" prefix
	// test — so the assertion names the exact label. With the option dropped the
	// label would be index_test__Document and this fails.
	for _, idx := range got {
		if strings.Contains(idx.Label, "__") {
			t.Errorf("label %q still uses the default separator, not the configured %q", idx.Label, "_")
		}
		if !strings.HasPrefix(idx.Label, "index_test_") {
			t.Errorf("label %q does not use the configured separator", idx.Label)
		}
	}
}

// Property-level @index and @vector iterate the MERGED property set, so an
// annotation declared on an abstract ancestor emits for each concrete subtype's
// own label. This is the only fixture in the package that annotates an inherited
// property; every other one annotates a property the annotated type owns, and so
// says nothing about whether a consumer annotating a mixin gets DDL at all.
func TestIndexes_InheritedPropertyAnnotations(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_inherited.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if result.HasErrors() {
		t.Fatalf("IndexesStructured: %v", result.Err())
	}

	var names []string
	for _, idx := range indexes {
		names = append(names, idx.Name)
	}
	slices.Sort(names)

	want := []string{
		"inherit_test__Alpha_audited_at_idx",
		"inherit_test__Alpha_vec_vector_idx",
		"inherit_test__Beta_audited_at_idx",
		"inherit_test__Beta_vec_vector_idx",
	}
	if !slices.Equal(names, want) {
		t.Errorf("inherited property annotations emitted %v; want %v", names, want)
	}

	// The abstract declarer itself emits nothing — it has no label.
	for _, idx := range indexes {
		if strings.Contains(idx.Label, "Auditable") {
			t.Errorf("abstract declarer emitted an index: %s", idx.Statement)
		}
	}
}

// The collision detector must walk types whose label is an invalid identifier,
// which is what labeledTypes was split out from emittableTypes to preserve:
// emittableTypes skips such a type after reporting it, so a detector built on
// that gate would go silent for exactly the labels most likely to collide.
//
// The detector's positive branch itself cannot be reached through either schema
// front door — SanitizeIdentifier is the identity function on the DSL name
// productions, and NewBuilder rejects any name it is not (E_INVALID_NAME), so
// two distinct declared names cannot sanitize together. What is testable, and
// what the split is actually about, is that the walk still SEES those types.
func TestLabeledTypes_IncludesInvalidLabels(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	// "-" makes every emitted label an invalid Neo4j identifier.
	a := New(WithLabelSeparator("-"))
	ctx := context.Background()

	var seen int
	for _, label := range a.labeledTypes(ctx, s) {
		seen++
		if err := ValidateIdentifier(label, "label"); err == nil {
			t.Errorf("label %q should be an invalid identifier under this separator", label)
		}
	}
	if seen == 0 {
		t.Fatal("labeledTypes skipped every type; the collision detector would see nothing")
	}

	// The emittable gate, by contrast, reports and skips them all.
	collector := diag.NewCollector(0)
	emitted := 0
	for range a.emittableTypes(ctx, s, collector) {
		emitted++
	}
	if emitted != 0 {
		t.Errorf("emittableTypes yielded %d types with invalid labels; it must skip them", emitted)
	}
	if !collector.Result().HasCode(E_NEO4J_INVALID_IDENTIFIER) {
		t.Error("emittableTypes should report each skipped invalid label")
	}
}

// Adapter.OwnedLabels is what every diff scopes itself by, and nothing exercised
// it: the diff tests hand-build sets through ownedSet. If it stopped agreeing
// with the labels the emitters actually produce, every diff would own nothing,
// report every declared object as a Create and every remote object as neither
// matched nor dropped, and the suite would stay green.
func TestOwnedLabels_MatchesEmittedLabels(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadSchema(t, "indexes.yammm")
	a := New()

	owned := a.OwnedLabels(ctx, s)

	// Every label the index emitter produced is owned.
	indexes, res := a.IndexesStructured(ctx, s)
	if res.HasErrors() {
		t.Fatalf("IndexesStructured: %v", res.Err())
	}
	if len(indexes) == 0 {
		t.Fatal("fixture emitted no indexes; the assertion would be vacuous")
	}
	for _, idx := range indexes {
		if !owned.Contains(idx.Label) {
			t.Errorf("emitted an index on %q, which OwnedLabels does not contain", idx.Label)
		}
	}

	// And every label the constraint emitter produced.
	constraints, cRes := a.ConstraintsStructured(ctx, s)
	if cRes.HasErrors() {
		t.Fatalf("ConstraintsStructured: %v", cRes.Err())
	}
	for _, c := range constraints {
		if !owned.Contains(c.Label) {
			t.Errorf("emitted a constraint on %q, which OwnedLabels does not contain", c.Label)
		}
	}

	// Abstract types emit nothing and are not owned.
	if owned.Contains("index_test__Tracked") {
		t.Error("an abstract type's label is owned; it can hold no objects")
	}
	// Neither is another schema's.
	if owned.Contains("other__Thing") {
		t.Error("a foreign label is owned")
	}
}

// The label set deliberately includes types whose label is not a valid Neo4j
// identifier — the emitters skip those, but a database may still hold objects on
// that label from an earlier configuration and they are the schema's to report.
func TestOwnedLabels_IncludesLabelsTheEmittersSkip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s := loadSchema(t, "indexes.yammm")
	a := New(WithLabelSeparator("-"))

	owned := a.OwnedLabels(ctx, s)
	if !owned.Contains("index_test-Document") {
		t.Error("a label the emitters skip as an invalid identifier is not owned")
	}
	if idx, res := a.IndexesStructured(ctx, s); !res.HasErrors() || idx != nil {
		t.Error("precondition: the emitters should reject this configuration entirely")
	}
}

// A nil *OwnedLabels owns nothing. This is a documented, deliberate zero value,
// pinned so it cannot become incidental.
func TestOwnedLabels_NilOwnsNothing(t *testing.T) {
	t.Parallel()
	var owned *OwnedLabels
	if owned.Contains("anything") {
		t.Error("a nil OwnedLabels claimed a label")
	}
	if _, ok := owned.LabelOf([]string{"a", "b"}); ok {
		t.Error("a nil OwnedLabels resolved an owned label")
	}
	got := New().DiffIndexes(
		[]Index{{Name: "i", Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}},
		[]RemoteIndex{{
			Name: "i", Type: "RANGE", EntityType: "NODE",
			LabelsOrTypes: []string{"app__T"}, Properties: []string{"x"},
		}}, nil,
	)
	// Nothing is owned, so nothing matches and nothing drops. Blocking is
	// collected before ownership, though, so the taken name still reports as
	// drift naming the holder — passing a nil by mistake surfaces the conflict
	// rather than yielding a confident plan of statements that would all no-op.
	wantCounts(t, got, 0, 1, 0, 0, 0)

	// With no remote holding the name there is nothing to block, and it is a Create.
	free := New().DiffIndexes(
		[]Index{{Name: "i", Kind: IndexRange, Label: "app__T", Properties: []string{"x"}}},
		nil, nil,
	)
	wantCounts(t, free, 0, 0, 1, 0, 0)
}
