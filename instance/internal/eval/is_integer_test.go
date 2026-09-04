package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
)

// The Integer type checker accepts a whole float only when it fits int64.
func TestIsInteger_WholeFloatMustFitInt64(t *testing.T) {
	t.Parallel()
	check := eval.IsInteger()
	if ok, _ := check(3.0); !ok {
		t.Error("3.0 is an integer")
	}
	if ok, _ := check(1e19); ok {
		t.Error("1e19 is whole but does not fit int64")
	}
	if ok, _ := check(3.5); ok {
		t.Error("3.5 has a fractional part")
	}
}
