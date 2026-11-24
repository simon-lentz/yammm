package immutable

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"
)

// copyInput is a value with every container kind a constructor walks, so a
// mutation of each after the wrap can show whether the wrap shares it.
func copyInput() map[string]any {
	return map[string]any{
		"name":   "a",
		"tags":   []any{"x", "y"},
		"nested": map[string]any{"k": "v"},
		"byID":   map[int]any{1: "one", 2: []any{"deep"}},
		"typed":  []string{"s"},
	}
}

// mutateCopyInput changes every map and slice copyInput holds, in place.
func mutateCopyInput(m map[string]any) {
	m["name"] = "changed"
	m["added"] = true
	m["tags"].([]any)[0] = "changed"
	m["nested"].(map[string]any)["k"] = "changed"
	byID := m["byID"].(map[int]any)
	byID[1] = "changed"
	byID[2].([]any)[0] = "changed"
	byID[3] = "added"
	m["typed"].([]string)[0] = "changed"
}

// TestWrap_CopiesItsInput holds every constructor to copying what it is given:
// a change the caller makes afterwards, at any depth and under any map key
// type, reaches nothing the constructor returned.
func TestWrap_CopiesItsInput(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		build func(in map[string]any) func() any
		want  func() any
	}{
		{"Wrap", func(in map[string]any) func() any {
			return Wrap(in).Clone
		}, func() any { return Wrap(copyInput()).Clone() }},
		{"WrapProperties", func(in map[string]any) func() any {
			p := WrapProperties(in)
			return func() any { return p.Clone() }
		}, func() any { return WrapProperties(copyInput()).Clone() }},
		{"WrapSlice", func(in map[string]any) func() any {
			s := WrapSlice([]any{in, in["byID"]})
			return func() any { return s.Clone() }
		}, func() any {
			fresh := copyInput()
			return WrapSlice([]any{fresh, fresh["byID"]}).Clone()
		}},
		{"WrapKey", func(in map[string]any) func() any {
			k := WrapKey([]any{in["byID"], in["tags"]})
			return func() any { return []any{k.Clone(), k.String()} }
		}, func() any {
			fresh := copyInput()
			k := WrapKey([]any{fresh["byID"], fresh["tags"]})
			return []any{k.Clone(), k.String()}
		}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			in := copyInput()
			read := row.build(in)
			mutateCopyInput(in)
			if got, want := read(), row.want(); !reflect.DeepEqual(got, want) {
				t.Errorf("after the caller changed its input, the wrapped value reads\n%#v\nwant\n%#v", got, want)
			}
		})
	}
}

// TestWrap_KeepsAnImmutableElementOfATypedMap holds the copy of a map with a
// non-string key to its element type: a Value or a container the element type
// cannot hold as Go data is kept as itself, at the wrap and at a Clone.
func TestWrap_KeepsAnImmutableElementOfATypedMap(t *testing.T) {
	t.Parallel()

	rows := map[string]any{
		"map[int]Value":        map[int]Value{1: Wrap("x")},
		"map[int]Properties":   map[int]Properties{1: WrapProperties(map[string]any{"a": "x"})},
		"map[int][]Value":      map[int][]Value{1: {Wrap(int64(2))}},
		"map[int]fmt.Stringer": map[int]fmt.Stringer{1: WrapKey([]any{"k"})},
	}
	for name, in := range rows {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, got := range []any{
				Wrap(in).Clone(),
				WrapProperties(map[string]any{"m": in}).Clone()["m"],
				WrapKey([]any{in}).Clone()[0],
			} {
				if !reflect.DeepEqual(got, in) {
					t.Errorf("Clone = %#v, want %#v", got, in)
				}
			}
		})
	}
}

// TestValue_Any_CopiesAMapWithANonStringKey holds Any to the copy rule for the
// one container stored as a Go map: a change to what Any returns reaches
// nothing the Value or a Key holds.
func TestValue_Any_CopiesAMapWithANonStringKey(t *testing.T) {
	t.Parallel()

	v := Wrap(map[int]any{1: "a", 2: []any{"b"}})
	got := v.Any().(map[int]any)
	got[1] = "changed"
	got[2].([]any)[0] = "changed"
	if want := map[int]any{1: "a", 2: []any{"b"}}; !reflect.DeepEqual(v.Clone(), want) {
		t.Errorf("after a change to Any's result, Clone = %#v, want %#v", v.Clone(), want)
	}

	k := WrapKey([]any{map[int]any{1: "a"}})
	k.At(0).Any().(map[int]any)[1] = "changed"
	data, err := json.Marshal(k.Clone())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != k.String() {
		t.Errorf("after a change to At(0).Any(), Clone marshals %s and String is %s", data, k.String())
	}
}

// TestWrap_AKeptElementCostsNoClone holds a kept element to its own cost: the
// copy decides by type before it clones, so the cost does not grow with what
// the element holds.
func TestWrap_AKeptElementCostsNoClone(t *testing.T) {
	allocsAt := func(n int) float64 {
		src := make(map[string]any, n)
		for i := range n {
			src[strconv.Itoa(i)] = []any{int64(i)}
		}
		in := map[int]Properties{1: WrapProperties(src)}
		return testing.AllocsPerRun(10, func() { _ = Wrap(in).Any() })
	}
	if small, large := allocsAt(10), allocsAt(400); small != large {
		t.Errorf("Wrap(...).Any() of a kept Properties allocates %v at 10 entries and %v at 400, want equal", small, large)
	}
}

// TestWrapKey_AKeptElementRendersAsAnEmptyObject holds the canonical string to
// the documented exception: an element a map with a non-string key keeps as
// itself renders as encoding/json renders it.
func TestWrapKey_AKeptElementRendersAsAnEmptyObject(t *testing.T) {
	k := WrapKey([]any{map[int]Value{1: Wrap(int64(5))}})
	if got := k.String(); got != `[{"1":{}}]` {
		t.Errorf("String = %s, want [{\"1\":{}}]", got)
	}
}

// TestWrapKey_NilIsTheZeroKey holds the two spellings of the empty key to one
// value, so nothing that compares fields can tell them apart.
func TestWrapKey_NilIsTheZeroKey(t *testing.T) {
	if got := WrapKey(nil); !reflect.DeepEqual(got, Key{}) {
		t.Errorf("WrapKey(nil) = %#v, want Key{}", got)
	}
}
