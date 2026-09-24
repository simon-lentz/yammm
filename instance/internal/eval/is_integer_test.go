package eval_test

import (
	"encoding/json"
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
)

// The Integer type checker accepts an integer and refuses every float, whole or
// not: `5.0 =~ Integer` is false. The Float checker widens an integer.
func TestIsInteger_RefusesEveryFloat(t *testing.T) {
	t.Parallel()
	isInteger, isFloat := eval.IsInteger(), eval.IsFloat()
	for _, v := range []any{int64(3), 3, uint8(3), json.Number("3")} {
		if ok, msg := isInteger(v); !ok {
			t.Errorf("IsInteger(%T %v) = false (%s), want true", v, v, msg)
		}
		if ok, msg := isFloat(v); !ok {
			t.Errorf("IsFloat(%T %v) = false (%s), want true: an integer widens", v, v, msg)
		}
	}
	for _, v := range []any{3.0, float32(3), 1e19, 3.5, json.Number("3.0"), json.Number("3e0")} {
		if ok, _ := isInteger(v); ok {
			t.Errorf("IsInteger(%T %v) = true, want false: a float is never an Integer", v, v)
		}
	}
}
