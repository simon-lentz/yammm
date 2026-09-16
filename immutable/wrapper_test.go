package immutable

import "testing"

// TestWrap_AdoptsAValuesContent holds Wrap to one rule for its own Value: the
// content is contributed, so no Value ever holds a Value and an accessor reads
// through a re-wrap as it reads the original.
func TestWrap_AdoptsAValuesContent(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		inner Value
		check func(t *testing.T, v Value)
	}{
		{"a string", Wrap("x"), func(t *testing.T, v Value) { t.Helper(); wantString(t, v, "x") }},
		{"an integer", Wrap(42), func(t *testing.T, v Value) { t.Helper(); wantInt(t, v, 42) }},
		{"a bool", Wrap(true), func(t *testing.T, v Value) { t.Helper(); wantBool(t, v, true) }},
		{"literal nil", Wrap(nil), func(t *testing.T, v Value) { t.Helper(); wantNil(t, v) }},
		{"a string-keyed map", Wrap(map[string]any{"a": 1}), func(t *testing.T, v Value) {
			t.Helper()
			m, ok := v.Map()
			if !ok {
				t.Fatalf("Map() = _, false; want the adopted map")
			}
			if m.Len() != 1 {
				t.Errorf("Len() = %d; want 1", m.Len())
			}
		}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			row.check(t, Wrap(row.inner))
		})
	}
}

// TestWrap_AdoptsAWrapper holds Wrap to storing one of this package's own
// wrappers as itself rather than as an opaque struct, so what reads a wrapped
// value reads its content: a key renders the data, not an empty object.
func TestWrap_AdoptsAWrapper(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		value any
		want  string
	}{
		{"Properties", WrapProperties(map[string]any{"a": 1}), `[{"a":1}]`},
		{"a string-keyed Map", WrapMap(map[string]any{"a": 1}), `[{"a":1}]`},
		{"an int-keyed Map", WrapMap(map[int]any{1: "a"}), `[{"1":"a"}]`},
		{"a Slice", WrapSlice([]any{"a", 1}), `[["a",1]]`},
		{"a Key", WrapKey([]any{"us", 1}), `[["us",1]]`},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := WrapKey([]any{row.value}).String(); got != row.want {
				t.Errorf("WrapKey([]any{%s}).String() = %s; want %s", row.name, got, row.want)
			}
		})
	}
}

// TestValue_IsNil_ThroughAWrapper holds IsNil to asking the wrapper itself. A
// wrapper is a struct, so a reflect kind cannot answer for it: without the
// wrapper's own report, a nil Properties or Key reads as a non-nil value.
func TestValue_IsNil_ThroughAWrapper(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		value any
		want  bool
	}{
		{"a nil Properties", WrapProperties(nil), true},
		{"a nil Key", WrapKey(nil), true},
		{"a nil string-keyed Map", WrapMap(map[string]any(nil)), true},
		{"a nil int-keyed Map", WrapMap(map[int]any(nil)), true},
		{"a nil Slice", WrapSlice(nil), true},
		{"a populated Properties", WrapProperties(map[string]any{"a": 1}), false},
		{"a populated Key", WrapKey([]any{"a"}), false},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := Wrap(row.value).IsNil(); got != row.want {
				t.Errorf("Wrap(%s).IsNil() = %v; want %v", row.name, got, row.want)
			}
		})
	}
}

// TestWrap_ArrayIsStoredAsIs pins what an array is: a scalar carrier's
// spelling, not a list. uuid.UUID is [16]byte, so wrapping an array as a Slice
// would turn one UUID into sixteen byte values.
func TestWrap_ArrayIsStoredAsIs(t *testing.T) {
	t.Parallel()

	var carrier [16]byte
	carrier[0], carrier[15] = 0xfe, 0xed

	v := Wrap(carrier)
	if _, ok := v.Slice(); ok {
		t.Error("Slice() = _, true; an array is stored as it is, not wrapped as a Slice")
	}
	got, ok := v.Unwrap().([16]byte)
	if !ok {
		t.Fatalf("Unwrap() = %T; want [16]byte", v.Unwrap())
	}
	if got != carrier {
		t.Errorf("Unwrap() = %v; want the array as given", got)
	}
}

// TestWrap_PointerToAContainerIsStoredAsAPointer holds the one rule that keeps
// a pointer out of the container dispatch. A pointer's method set holds its
// type's value methods, so *Map, *Slice, *Properties and *Key all satisfy
// wrapper; taking one as a container calls a value method through the pointer,
// which panics when it is nil. Every reader that asks whether a value is a
// container goes through asWrapper, so each is driven here.
func TestWrap_PointerToAContainerIsStoredAsAPointer(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		nilp any
	}{
		{"a nil *Map[string]", (*Map[string])(nil)},
		{"a nil *Slice", (*Slice)(nil)},
		{"a nil *Properties", (*Properties)(nil)},
		{"a nil *Key", (*Key)(nil)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			v := Wrap(row.nilp)
			if !v.IsNil() {
				t.Errorf("IsNil reports false for %s; a typed nil pointer is nil", row.name)
			}
			if got := v.Clone(); got != row.nilp {
				t.Errorf("Clone returns %#v for %s, want the pointer itself", got, row.name)
			}
			if got := v.Unwrap(); got != row.nilp {
				t.Errorf("Unwrap returns %#v for %s, want the pointer itself", got, row.name)
			}
		})
	}

	t.Run("a non-nil *Map[string] is not the container", func(t *testing.T) {
		t.Parallel()
		m := WrapMap(map[string]any{"a": 1})
		v := Wrap(&m)
		if _, ok := v.Map(); ok {
			t.Error("Map reports true for a *Map[string]; only a Map[string] is the container")
		}
		if _, isPointer := v.Unwrap().(*Map[string]); !isPointer {
			t.Errorf("Unwrap returns %T for a *Map[string], want the pointer", v.Unwrap())
		}
		if v.IsNil() {
			t.Error("IsNil reports true for a non-nil *Map[string]")
		}
	})

	t.Run("a nil *Key renders as null in a canonical string", func(t *testing.T) {
		t.Parallel()
		if got := WrapKey([]any{(*Key)(nil)}).String(); got != "[null]" {
			t.Errorf("WrapKey over a nil *Key renders %q, want %q", got, "[null]")
		}
	})
}

// TestClone_ReadsANestedContainersContent holds the clone walk to the rule its
// top level already keeps: a container contributes its content at every depth.
// A map with a non-string key is stored as given, so a container inside one is
// the only value the walk reaches without having wrapped it.
func TestClone_ReadsANestedContainersContent(t *testing.T) {
	t.Parallel()

	t.Run("a clone unwraps a nested container", func(t *testing.T) {
		t.Parallel()
		v := Wrap(map[int]any{1: WrapMap(map[string]any{"a": int64(1)})})
		outer, ok := v.Clone().(map[int]any)
		if !ok {
			t.Fatalf("Clone returns %T, want map[int]any", v.Clone())
		}
		inner, ok := outer[1].(map[string]any)
		if !ok {
			t.Fatalf("the nested container clones to %T, want map[string]any", outer[1])
		}
		if inner["a"] != int64(1) {
			t.Errorf("the nested content reads %#v, want 1", inner["a"])
		}
	})

	t.Run("a canonical string reads a nested container", func(t *testing.T) {
		t.Parallel()
		got := WrapKey([]any{map[int]any{1: WrapMap(map[string]any{"a": int64(1)})}}).String()
		if want := `[{"1":{"a":1}}]`; got != want {
			t.Errorf("a key over a nested container renders %s, want %s", got, want)
		}
	})
}

// TestClone_NilContainerClonesToATypedNil keeps a nil container apart from an
// empty one through a clone. The type survives, so a reader that ranges over
// the result sees no entries either way and a reader that compares them by
// their rendered form still tells them apart. Both depths are driven: the
// walk's root and a container reached through a map stored as given.
func TestClone_NilContainerClonesToATypedNil(t *testing.T) {
	t.Parallel()

	t.Run("at the root", func(t *testing.T) {
		t.Parallel()
		got := Wrap(WrapMap[string](nil)).Clone()
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("a nil Map[string] clones to %#v, want a typed nil map[string]any", got)
		}
		if m != nil {
			t.Errorf("a nil Map[string] clones to %#v, want nil", m)
		}
	})

	t.Run("nested in a map stored as given", func(t *testing.T) {
		t.Parallel()
		outer, ok := Wrap(map[int]any{1: WrapMap[string](nil)}).Clone().(map[int]any)
		if !ok {
			t.Fatal("the outer map does not clone to a map[int]any")
		}
		m, ok := outer[1].(map[string]any)
		if !ok {
			t.Fatalf("a nested nil Map[string] clones to %#v, want a typed nil map[string]any", outer[1])
		}
		if m != nil {
			t.Errorf("a nested nil Map[string] clones to %#v, want nil", m)
		}
	})
}
