package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

type selfPointer *selfPointer

// A pointer that points to itself holds no value of any kind, so every check
// and every coercion refuses it, the numeric kinds included, rather than hang.
func TestCheckValue_APointerThatPointsToItselfIsNoValueOfAnyKind(t *testing.T) {
	t.Parallel()
	var p selfPointer
	p = &p
	for _, c := range []schema.Constraint{
		schema.NewIntegerConstraint(), schema.NewFloatConstraint(), schema.NewStringConstraint(),
		schema.NewBooleanConstraint(), schema.NewTimestampConstraint(), schema.NewUUIDConstraint(),
	} {
		if err := eval.CheckValue(p, c); err == nil {
			t.Errorf("CheckValue accepted a pointer that points to itself at %s", c.Kind())
		}
		if _, err := eval.CoerceValue(p, c); err == nil {
			t.Errorf("CoerceValue stored a pointer that points to itself at %s", c.Kind())
		}
	}
}
