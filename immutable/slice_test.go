package immutable

import (
	"slices"
	"testing"
)

func TestSlice_WrapSlice(t *testing.T) {
	s := WrapSlice([]any{"a", "b", "c"})

	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}
	wantString(t, s.At(0), "a")
	wantString(t, s.At(2), "c")
}

// A nil slice gives the zero Slice, which is a nil container; an empty slice
// gives an empty one, which is not.
func TestSlice_NilAndEmpty(t *testing.T) {
	for name, tt := range map[string]struct {
		s       Slice
		wantNil bool
	}{
		"a nil slice":          {WrapSlice(nil), true},
		"the zero value":       {Slice{}, true},
		"an empty slice":       {WrapSlice([]any{}), false},
		"a nil slice, Wrap":    {Wrap([]string(nil)).Any().(Slice), true},
		"an empty slice, Wrap": {Wrap([]string{}).Any().(Slice), false},
	} {
		if tt.s.Len() != 0 {
			t.Errorf("%s: Len() = %d, want 0", name, tt.s.Len())
		}
		if got := IsNilContainer(tt.s); got != tt.wantNil {
			t.Errorf("%s: IsNilContainer = %v, want %v", name, got, tt.wantNil)
		}
		if got := tt.s.Clone() == nil; got != tt.wantNil {
			t.Errorf("%s: Clone() == nil is %v, want %v", name, got, tt.wantNil)
		}
		for range tt.s.Values() {
			t.Errorf("%s: Values yielded an element", name)
		}
	}
}

// At panics out of range, as a slice index does.
func TestSlice_At_PanicsOutOfRange(t *testing.T) {
	s := WrapSlice([]any{"a", "b"})

	for _, i := range []int{-1, 2, 5} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("At(%d) did not panic", i)
				}
			}()
			_ = s.At(i)
		}()
	}
}

func TestSlice_Values(t *testing.T) {
	s := WrapSlice([]any{int64(1), int64(2), int64(3)})

	for pass := range 2 {
		var values []int64
		for v := range s.Values() {
			n, ok := v.AsInt()
			if !ok {
				t.Fatalf("element %v is not an int64", v.Any())
			}
			values = append(values, n)
		}
		if !slices.Equal(values, []int64{1, 2, 3}) {
			t.Errorf("pass %d: Values() = %v, want [1 2 3]", pass, values)
		}
	}

	count := 0
	for range s.Values() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("Values: %d elements after a break at 2", count)
	}
}

func TestSlice_Clone(t *testing.T) {
	s := WrapSlice([]any{
		map[string]any{"key": "value"},
		"string",
		int64(42),
	})
	cloned := s.Clone()

	if len(cloned) != 3 {
		t.Fatalf("Clone() has %d elements, want 3", len(cloned))
	}
	nested, ok := cloned[0].(map[string]any)
	if !ok || nested["key"] != "value" {
		t.Fatalf("element 0 clones to %#v, want map[string]any{key: value}", cloned[0])
	}

	nested["key"] = "modified"
	cloned[1] = "changed"

	first, _ := s.At(0).Any().(Properties)
	if v, _ := first.Get("key"); v.Any() != "value" {
		t.Error("a change to a clone reached the Slice")
	}
	wantString(t, s.At(1), "string")
}

func TestSlice_NestedSlices(t *testing.T) {
	s := WrapSlice([]any{[]any{int64(1), int64(2)}, []any{int64(3), int64(4)}})

	first, ok := s.At(0).AsSlice()
	if !ok {
		t.Fatalf("element 0 holds %T, want Slice", s.At(0).Any())
	}
	if first.Len() != 2 {
		t.Errorf("nested Len() = %d, want 2", first.Len())
	}
	wantInt(t, first.At(0), 1)
}

func TestSlice_MixedTypes(t *testing.T) {
	s := WrapSlice([]any{
		"string",
		int64(42),
		3.14,
		true,
		nil,
		map[string]any{"key": "value"},
		[]any{1, 2, 3},
	})

	if s.Len() != 7 {
		t.Errorf("Len() = %d, want 7", s.Len())
	}
	wantString(t, s.At(0), "string")
	wantInt(t, s.At(1), 42)
	if f, ok := s.At(2).AsFloat(); !ok || f != 3.14 {
		t.Errorf("element 2 = %v, want 3.14", s.At(2).Any())
	}
	wantBool(t, s.At(3), true)
	wantNil(t, s.At(4))
	if _, ok := s.At(5).Any().(Properties); !ok {
		t.Errorf("element 5 holds %T, want Properties", s.At(5).Any())
	}
	if _, ok := s.At(6).AsSlice(); !ok {
		t.Errorf("element 6 holds %T, want Slice", s.At(6).Any())
	}
}
