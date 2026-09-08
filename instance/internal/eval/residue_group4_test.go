package eval_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/value"
)

// TestArrayIsNotAList pins the rule at the readers that decide it.
// value.ListElems says a Go array is not a list — only a slice or an
// immutable.Slice is — and four readers went on accepting one, so Len answered
// on an array the rest of the evaluator refuses. Restoring a reflect.Array arm
// turns its row red.
func TestArrayIsNotAList(t *testing.T) {
	t.Parallel()
	// A [16]byte is the array the round found reachable: a uuid.UUID IS one.
	var arr [4]int64

	if _, ok := value.ListElems(arr); ok {
		t.Fatal("ListElems accepted an array, so this file pins the wrong rule")
	}

	_, err := eval.NewEvaluator().Evaluate(t.Context(),
		makeBuiltinCall(lit(arr), "len", nil, nil, nil), eval.EmptyScope())
	if err == nil {
		t.Fatal("Len answered on an array")
	}
	if !strings.Contains(err.Error(), "unsupported for type") {
		t.Errorf("want the unsupported-type message; got %v", err)
	}
}

// TestListMessagesNameAList pins the two messages that named a shape the code
// refuses. asSlice said "expects slice or array input" while its own reader
// accepts neither an array nor anything but a list, so the message described a
// rule the package had already retired.
func TestListMessagesNameAList(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, builtin string }{
		{"a numeric receiver into a list builtin", "sum"},
		{"a string receiver into a list builtin", "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := eval.NewEvaluator().Evaluate(t.Context(),
				makeBuiltinCall(lit(int64(42)), tc.builtin, nil, nil, nil), eval.EmptyScope())
			if err == nil {
				t.Fatalf("%s answered on a number", tc.builtin)
			}
			if strings.Contains(err.Error(), "slice or array") {
				t.Errorf("the message still names a shape the code refuses: %v", err)
			}
			if !strings.Contains(err.Error(), "expects a list") {
				t.Errorf("want a message naming a list; got %v", err)
			}
		})
	}
}
