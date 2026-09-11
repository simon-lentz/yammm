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
