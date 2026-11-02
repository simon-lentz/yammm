package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

func TestAnnotation_ValidAnnotationsExposed(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "main"

type Trade {
	id String primary
	state String required @index
	@@index(id, state)
}
`, "main.yammm")
	if res.HasErrors() {
		t.Fatalf("load should succeed, got: %v", res)
	}
	ty := schemaType(t, s, "Trade")
	if _, ok := typeProperty(t, ty, "state").Annotation("index"); !ok {
		t.Error("state should carry @index")
	}
	if got := typeAnnNames(ty); len(got) != 1 || got[0] != "index" {
		t.Errorf("type annotations: got %v, want [index]", got)
	}
}

func TestAnnotation_VectorOnAStringIsAnInvalidTarget(t *testing.T) {
	t.Parallel()
	_, res := schema.LoadString(t.Context(), `schema "main"

type Trade {
	id String primary
	x String required @vector(cosine)
}
`, "main.yammm")
	if !res.HasCode(diag.E_INVALID_ANNOTATION_TARGET) {
		t.Errorf("want E_INVALID_ANNOTATION_TARGET, got: %v", res)
	}
}
