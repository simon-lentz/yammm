package cypher

import (
	"slices"
	"strings"
	"testing"
)

// A separator the schema name keeps the rule under is used as configured.
// "_" would be refused for schema "index_test", which holds it.
func TestIndexes_AConfiguredSeparatorIsUsed(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	got, result := Config{LabelSeparator: "Z"}.Indexes(s)
	if result.HasErrors() {
		t.Fatalf("separator Z keeps the label rule for schema index_test: %v", result.Err())
	}
	if len(got) == 0 {
		t.Fatal("expected indexes to be emitted")
	}
	for _, idx := range got {
		if !strings.HasPrefix(idx.Label, "index_testZ") {
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
	indexes, result := Config{}.Indexes(s)
	if result.HasErrors() {
		t.Fatalf("Indexes: %v", result.Err())
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

// Config.OwnedLabels is what every diff scopes itself by, and the neo4j diff
// tests hand-build their sets. If it stopped agreeing
// with the labels the emitters actually produce, every diff would own nothing,
// report every declared object as a Create and every remote object as neither
// matched nor dropped, and the suite would stay green.
func TestOwnedLabels_MatchesEmittedLabels(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	a := Config{}

	owned := a.OwnedLabels(s)

	// Every label the index emitter produced is owned.
	indexes, res := a.Indexes(s)
	if res.HasErrors() {
		t.Fatalf("Indexes: %v", res.Err())
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
	constraints, cRes := a.Constraints(s)
	if cRes.HasErrors() {
		t.Fatalf("Constraints: %v", cRes.Err())
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
	s := loadSchema(t, "indexes.yammm")
	a := Config{LabelSeparator: "-"}

	owned := a.OwnedLabels(s)
	if !owned.Contains("index_test-Document") {
		t.Error("a label the emitters skip as an invalid identifier is not owned")
	}
	if idx, res := a.Indexes(s); !res.HasErrors() || idx != nil {
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
}

// LabelOf answers with the first label it owns, in the object's own order.
func TestOwnedLabels_LabelOfAnswersTheFirstOwnedLabel(t *testing.T) {
	t.Parallel()

	owned := Config{}.OwnedLabels(loadSchema(t, "multiple_types.yammm"))
	for _, labels := range [][]string{
		{"multi_test__Gadget", "multi_test__Widget"},
		{"unowned", "multi_test__Gadget", "multi_test__Widget"},
	} {
		if got, ok := owned.LabelOf(labels); !ok || got != "multi_test__Gadget" {
			t.Errorf("LabelOf(%q) = %q, %v; want multi_test__Gadget, true", labels, got, ok)
		}
	}
}
