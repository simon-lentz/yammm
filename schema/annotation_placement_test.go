package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestAnnotationPlacement_TheZeroValueNamesNoPlacement holds the zero
// placement apart from both real ones: a spec or a completion context built
// without a placement renders as unknown and matches no registered spec.
func TestAnnotationPlacement_TheZeroValueNamesNoPlacement(t *testing.T) {
	t.Parallel()

	var zero schema.AnnotationPlacement
	if got := zero.String(); got != "@?" {
		t.Errorf("AnnotationPlacement(0).String() = %q, want @?", got)
	}
	for _, spec := range schema.AnnotationSpecs() {
		if spec.Placement() == zero {
			t.Errorf("the registered spec %s holds the zero placement", spec.Name())
		}
	}
}
