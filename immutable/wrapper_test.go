package immutable

import (
	"reflect"
	"testing"
)

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
		{"an integer", Wrap(int64(42)), func(t *testing.T, v Value) { t.Helper(); wantInt(t, v, 42) }},
		{"a bool", Wrap(true), func(t *testing.T, v Value) { t.Helper(); wantBool(t, v, true) }},
		{"literal nil", Wrap(nil), func(t *testing.T, v Value) { t.Helper(); wantNil(t, v) }},
		{"a string-keyed map", Wrap(map[string]any{"a": 1}), func(t *testing.T, v Value) {
			t.Helper()
			m, ok := v.Any().(Properties)
			if !ok {
				t.Fatalf("Any() = %T; want the adopted Properties", v.Any())
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
			if _, isValue := Wrap(row.inner).Any().(Value); isValue {
				t.Error("a Value holds a Value")
			}
		})
	}
}

// TestWrap_AdoptsAWrapper holds Wrap to storing one of this package's own
// containers as itself rather than as an opaque struct, so what reads a wrapped
// value reads its content: a key renders the data, not an empty object.
func TestWrap_AdoptsAWrapper(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		value any
		want  string
	}{
		{"Properties", WrapProperties(map[string]any{"a": 1}), `[{"a":1}]`},
		{"a Slice", WrapSlice([]any{"a", 1}), `[["a",1]]`},
		{"a Key", WrapKey([]any{"us", 1}), `[["us",1]]`},
		{"a Value", Wrap(map[int]any{1: "a"}), `[{"1":"a"}]`},
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

// TestIsNilContainer_ThroughAWrap holds IsNilContainer to asking the container
// itself, and Wrap to storing a nil map or slice as a nil container. A
// container is a struct, so a reflect kind cannot answer for it. An empty
// container is not nil.
func TestIsNilContainer_ThroughAWrap(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name  string
		value any
		want  bool
	}{
		{"a nil Properties", WrapProperties(nil), true},
		{"a nil Key", WrapKey(nil), true},
		{"a nil Slice", WrapSlice(nil), true},
		{"a nil string-keyed map", map[string]any(nil), true},
		{"a nil slice", []any(nil), true},
		{"a nil typed slice", []string(nil), true},
		{"an empty Properties", WrapProperties(map[string]any{}), false},
		{"an empty Key", WrapKey([]any{}), false},
		{"an empty Slice", WrapSlice([]any{}), false},
		{"an empty string-keyed map", map[string]any{}, false},
		{"a populated Properties", WrapProperties(map[string]any{"a": 1}), false},
		{"a populated Key", WrapKey([]any{"a"}), false},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := IsNilContainer(Wrap(row.value).Any()); got != row.want {
				t.Errorf("IsNilContainer(Wrap(%s).Any()) = %v; want %v", row.name, got, row.want)
			}
		})
	}
}

// A nil map with a non-string key is copied as that typed nil: no container,
// and not literal nil.
func TestWrap_NilNonStringKeyedMapKeepsItsType(t *testing.T) {
	t.Parallel()
	v := Wrap(map[int]any(nil))
	if u, ok := v.Any().(map[int]any); !ok || u != nil {
		t.Errorf("Any() = %#v, want the typed nil map[int]any", v.Any())
	}
	if IsNilContainer(v.Any()) {
		t.Error("IsNilContainer reports true for a Go map")
	}
}

// embedsKey is a named type of this package that embeds a container, so its
// package path is the container's.
type embedsKey struct {
	Key
	Name string
}

// noContainers are values IsNilContainer reads as no container: a Go nil, a nil
// Go map or slice, a pointer to a nil container and a struct that embeds one.
func noContainers() map[string]any {
	nilSlice := WrapSlice(nil)
	return map[string]any{
		"literal nil":                         nil,
		"a nil Go map":                        map[string]any(nil),
		"a nil Go slice":                      []any(nil),
		"a pointer to a nil Slice":            &nilSlice,
		"a nil pointer to a Properties":       (*Properties)(nil),
		"a Value that holds nothing":          Wrap(nil),
		"a Value that holds a nil Slice":      Wrap([]any(nil)),
		"a struct embedding a nil Slice":      struct{ Slice }{},
		"a struct embedding a nil Properties": struct{ Properties }{},
		"a struct embedding a nil Key":        struct{ Key }{},
		"a named struct embedding a nil Key":  embedsKey{},
	}
}

// TestIsNilContainer_OnlyAContainerIsOne holds IsNilContainer to this package's
// containers.
func TestIsNilContainer_OnlyAContainerIsOne(t *testing.T) {
	t.Parallel()
	for name, v := range noContainers() {
		if IsNilContainer(v) {
			t.Errorf("IsNilContainer(%s) = true; want false", name)
		}
	}
	for name, v := range map[string]any{
		"a zero Properties": Properties{},
		"a zero Slice":      Slice{},
		"a zero Key":        Key{},
	} {
		if !IsNilContainer(v) {
			t.Errorf("IsNilContainer(%s) = false; want true", name)
		}
	}
}

// TestIsNilContainer_AllocatesNothing holds the question every value reader
// asks of every value to no allocation, whichever way it is answered.
func TestIsNilContainer_AllocatesNothing(t *testing.T) {
	values := noContainers()
	values["a nil Slice"] = WrapSlice(nil)
	values["a Properties"] = WrapProperties(map[string]any{"a": "b"})
	for name, v := range values {
		if n := testing.AllocsPerRun(10, func() { _ = IsNilContainer(v) }); n != 0 {
			t.Errorf("IsNilContainer(%s) allocates %v times a call, want 0", name, n)
		}
	}
}

// wrapperSink keeps asWrapper's answer live, so the compiler cannot drop a
// conversion an allocation test means to see.
var wrapperSink wrapper

// TestAsWrapper_AllocatesNothing holds the container check every Wrap and Clone
// makes of every value to no allocation: a Clone of each List value pays it.
func TestAsWrapper_AllocatesNothing(t *testing.T) {
	values := noContainers()
	values["a Slice"] = WrapSlice([]any{"a"})
	values["a Properties"] = WrapProperties(map[string]any{"a": "b"})
	values["a Key"] = WrapKey([]any{"a"})
	for name, v := range values {
		if n := testing.AllocsPerRun(10, func() { wrapperSink, _ = asWrapper(v) }); n != 0 {
			t.Errorf("asWrapper(%s) allocates %v times a call, want 0", name, n)
		}
	}
}

// TestWrap_AStructEmbeddingAContainerIsStoredAsItself holds every reader that
// asks whether a value is a container to the value's type. A struct that embeds
// one has its methods by promotion, and is stored, read and cloned as a struct.
func TestWrap_AStructEmbeddingAContainerIsStoredAsItself(t *testing.T) {
	t.Parallel()
	for name, given := range map[string]any{
		"a Slice":      struct{ Slice }{WrapSlice([]any{int64(1)})},
		"a nil Slice":  struct{ Slice }{},
		"a Properties": struct{ Properties }{WrapProperties(map[string]any{"a": int64(1)})},
		"a nil Key":    struct{ Key }{},
	} {
		v := Wrap(given)
		if got := v.Any(); !reflect.DeepEqual(got, given) {
			t.Errorf("%s: Any returns %#v, want the struct", name, got)
		}
		if got := v.Clone(); !reflect.DeepEqual(got, given) {
			t.Errorf("%s: Clone returns %#v, want the struct", name, got)
		}
		if got := Wrap(map[int]any{1: given}).Clone(); !reflect.DeepEqual(got, map[int]any{1: given}) {
			t.Errorf("%s: in a map with an int key it clones to %#v, want the struct", name, got)
		}
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
	if _, ok := v.AsSlice(); ok {
		t.Error("AsSlice() = _, true; an array is stored as it is, not wrapped as a Slice")
	}
	got, ok := v.Any().([16]byte)
	if !ok {
		t.Fatalf("Any() = %T; want [16]byte", v.Any())
	}
	if got != carrier {
		t.Errorf("Any() = %v; want the array as given", got)
	}
}

// TestWrap_PointerToAContainerIsStoredAsAPointer holds the one rule that keeps
// a pointer out of the container dispatch. A pointer's method set holds its
// type's value methods, so taking one as a container would call a value method
// through the pointer, which panics when it is nil. Every reader that asks
// whether a value is a container goes through asWrapper, so each is driven here.
func TestWrap_PointerToAContainerIsStoredAsAPointer(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		nilp any
	}{
		{"a nil *Slice", (*Slice)(nil)},
		{"a nil *Properties", (*Properties)(nil)},
		{"a nil *Key", (*Key)(nil)},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			v := Wrap(row.nilp)
			if got := v.Clone(); got != row.nilp {
				t.Errorf("Clone returns %#v for %s, want the pointer itself", got, row.name)
			}
			if got := v.Any(); got != row.nilp {
				t.Errorf("Any returns %#v for %s, want the pointer itself", got, row.name)
			}
			if IsNilContainer(v.Any()) {
				t.Errorf("IsNilContainer reports true for %s", row.name)
			}
		})
	}

	t.Run("a non-nil *Properties is not the container", func(t *testing.T) {
		t.Parallel()
		p := WrapProperties(map[string]any{"a": 1})
		v := Wrap(&p)
		if _, isPointer := v.Any().(*Properties); !isPointer {
			t.Errorf("Any returns %T for a *Properties, want the pointer", v.Any())
		}
	})

	t.Run("a nil *Key renders as null in a canonical string", func(t *testing.T) {
		t.Parallel()
		if got := WrapKey([]any{(*Key)(nil)}).String(); got != "[null]" {
			t.Errorf("WrapKey over a nil *Key renders %q, want %q", got, "[null]")
		}
	})
}

// TestWrap_ReadsANestedContainersContent holds a map with a non-string key to
// the rule a string-keyed one keeps: a container or a Value inside it
// contributes its content, when it is copied at the wrap and again at a Clone.
func TestWrap_ReadsANestedContainersContent(t *testing.T) {
	t.Parallel()

	want := map[int]any{
		1: map[string]any{"a": int64(1)},
		2: []any{"x"},
		3: map[string]any{"b": "c"},
	}
	v := Wrap(map[int]any{
		1: WrapProperties(map[string]any{"a": int64(1)}),
		2: WrapSlice([]any{"x"}),
		3: Wrap(map[string]any{"b": "c"}),
	})
	if got := v.Any(); !reflect.DeepEqual(got, want) {
		t.Errorf("Any() = %#v, want %#v", got, want)
	}
	if got := v.Clone(); !reflect.DeepEqual(got, want) {
		t.Errorf("Clone() = %#v, want %#v", got, want)
	}
	if got := WrapKey([]any{map[int]any{1: WrapProperties(map[string]any{"a": int64(1)})}}).String(); got != `[{"1":{"a":1}}]` {
		t.Errorf("a key over a nested container renders %s, want [{\"1\":{\"a\":1}}]", got)
	}
}

// TestClone_NilContainerClonesToATypedNil keeps a nil container apart from an
// empty one through a clone. The type survives, so a reader that ranges over
// the result sees no entries either way and a reader that compares them by
// their rendered form still tells them apart. Both depths are driven: the
// walk's root and a container inside a map with an int key.
func TestClone_NilContainerClonesToATypedNil(t *testing.T) {
	t.Parallel()

	t.Run("at the root", func(t *testing.T) {
		t.Parallel()
		got := Wrap(WrapProperties(nil)).Clone()
		m, ok := got.(map[string]any)
		if !ok {
			t.Fatalf("a nil Properties clones to %#v, want a typed nil map[string]any", got)
		}
		if m != nil {
			t.Errorf("a nil Properties clones to %#v, want nil", m)
		}
	})

	t.Run("nested in a map with an int key", func(t *testing.T) {
		t.Parallel()
		outer, ok := Wrap(map[int]any{1: WrapProperties(nil)}).Clone().(map[int]any)
		if !ok {
			t.Fatal("the outer map does not clone to a map[int]any")
		}
		m, ok := outer[1].(map[string]any)
		if !ok {
			t.Fatalf("a nested nil Properties clones to %#v, want a typed nil map[string]any", outer[1])
		}
		if m != nil {
			t.Errorf("a nested nil Properties clones to %#v, want nil", m)
		}
	})
}
