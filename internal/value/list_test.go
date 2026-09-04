package value_test

import (
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
)

// ListElems is the one reader of a list shape: a []any, a typed slice or
// array, and an immutable.Slice all read as their elements; anything else
// is not a list. A nil is not a list either — the nil-is-empty rule belongs
// to the builtins that hold it, not to the reader.
func TestListElems(t *testing.T) {
	t.Parallel()
	wrapped := immutable.Wrap([]any{int64(1), "a", []any{int64(2)}}).Unwrap()
	if _, ok := wrapped.(immutable.Slice); !ok {
		t.Fatalf("fixture: Wrap([]any).Unwrap() is %T, want immutable.Slice", wrapped)
	}
	id := uuid.MustParse("123e4567-e89b-12d3-a456-426614174000")
	for _, tc := range []struct {
		name string
		in   any
		want []any
		ok   bool
	}{
		{"[]any", []any{int64(1), "a"}, []any{int64(1), "a"}, true},
		{"typed slice", []string{"a", "b"}, []any{"a", "b"}, true},
		{"array", [3]int64{1, 2, 3}, []any{int64(1), int64(2), int64(3)}, true},
		{"immutable.Slice", wrapped, []any{int64(1), "a", immutable.Wrap([]any{int64(2)}).Unwrap()}, true},
		{"empty", []any{}, []any{}, true},
		{"nil", nil, nil, false},
		{"string", "abc", nil, false},
		{"map", map[string]any{"a": 1}, nil, false},
		{"scalar", int64(7), nil, false},
		{"uuid is an array of bytes", id, func() []any {
			out := make([]any, 0, len(id))
			for _, b := range id {
				out = append(out, b)
			}
			return out
		}(), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := value.ListElems(tc.in)
			if ok != tc.ok {
				t.Fatalf("ListElems(%T) ok = %v, want %v", tc.in, ok, tc.ok)
			}
			if tc.ok && !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ListElems(%T) = %#v, want %#v", tc.in, got, tc.want)
			}
		})
	}
}
