package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func TestAnnotation_Builder_ValidAnnotationsExposed(t *testing.T) {
	t.Parallel()
	s, res := schema.NewBuilder().
		WithName("main").
		AddType("Trade").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("state", schema.NewStringConstraint()).
		WithPropertyAnnotation("state", "index").
		WithTypeAnnotation("index", "id", "state").
		Done().
		Build()
	if res.HasErrors() {
		t.Fatalf("build should succeed, got: %v", res)
	}
	if s == nil {
		t.Fatal("nil schema")
	}
	ty := schemaType(t, s, "Trade")
	if _, ok := typeProperty(t, ty, "state").Annotation("index"); !ok {
		t.Error("state should carry @index built via WithPropertyAnnotation")
	}
	if got := typeAnnNames(ty); len(got) != 1 || got[0] != "index" {
		t.Errorf("type annotations: got %v, want [index]", got)
	}
}

// Build is a pure function of builder state: pending property annotations are
// attached to a per-build copy, never appended to the retained property decls.
// Appending in place made the second Build see each annotation twice and fail
// with a duplicate-annotation error on input the first Build accepted.
func TestAnnotation_Builder_BuildIsRepeatable(t *testing.T) {
	t.Parallel()
	b := schema.NewBuilder().WithName("main")
	b.AddType("Trade").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("origin", schema.NewStringConstraint()).
		WithPropertyAnnotation("origin", "writeOnce")

	for _, label := range []string{"first", "second"} {
		s, res := b.Build()
		if res.HasErrors() {
			t.Fatalf("%s build should succeed, got: %v", label, res)
		}
		if s == nil {
			t.Fatalf("%s build returned a nil schema", label)
		}
		if got := propAnnNames(typeProperty(t, schemaType(t, s, "Trade"), "origin")); len(got) != 1 || got[0] != "writeOnce" {
			t.Errorf("%s build: origin annotations = %v, want [writeOnce]", label, got)
		}
	}
}

func TestAnnotation_Builder_UnknownPropertyTarget(t *testing.T) {
	t.Parallel()
	_, res := schema.NewBuilder().
		WithName("main").
		AddType("Trade").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithPropertyAnnotation("ghost", "index").
		Done().
		Build()
	if !res.HasCode(diag.E_UNKNOWN_ANNOTATION_TARGET) {
		t.Errorf("want E_UNKNOWN_ANNOTATION_TARGET for annotation on unknown property, got: %v", res)
	}
}

func TestAnnotation_Builder_InvalidAnnotationSameCodeAsParse(t *testing.T) {
	t.Parallel()
	// @vector on a non-Vector property draws E_INVALID_ANNOTATION_TARGET through
	// the Builder front door, exactly as through the parse front door — both
	// flow through the shared completion phase, no Builder-only carve-out.
	_, res := schema.NewBuilder().
		WithName("main").
		AddType("Trade").
		WithPrimaryKey("id", schema.NewStringConstraint()).
		WithProperty("x", schema.NewStringConstraint()).
		WithPropertyAnnotation("x", "vector", "cosine").
		Done().
		Build()
	if !res.HasCode(diag.E_INVALID_ANNOTATION_TARGET) {
		t.Errorf("want E_INVALID_ANNOTATION_TARGET, got: %v", res)
	}
}
