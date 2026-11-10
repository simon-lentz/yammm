package value_test

import (
	"encoding/json"
	"maps"
	"math"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
)

// pointers returns a pointer to v, and a pointer to an interface that holds v,
// each under its name.
func pointers(name string, v any) map[string]any {
	held := v
	pointer := reflect.New(reflect.TypeOf(v))
	pointer.Elem().Set(reflect.ValueOf(v))
	return map[string]any{"a pointer to " + name: pointer.Interface(), "a pointer to an interface holding " + name: &held}
}

// A pointer is a value of no form to every reader of the value layer, a nil one
// too: no getter reads it, it has no kind and no strata, it is not absent, it
// orders against nothing and it equals nothing, itself and nil included.
func TestReaders_APointerIsAValueOfNoForm(t *testing.T) {
	t.Parallel()
	type selfPointer *selfPointer
	var self selfPointer
	self = &self
	var nilText *string
	var nilTime *time.Time
	var nilAny *any
	values := map[string]any{
		"a nil pointer to a string":       nilText,
		"a nil pointer to a time":         nilTime,
		"a nil pointer to an interface":   nilAny,
		"a pointer that points to itself": self,
		"an unsafe pointer":               unsafe.Pointer(&nilText),
		"a nil unsafe pointer":            unsafe.Pointer(nil),
	}
	for name, v := range map[string]any{
		"a string":      "x",
		"an int64":      int64(5),
		"a uint64":      uint64(7),
		"a float64":     1.5,
		"a bool":        true,
		"a json.Number": json.Number("12"),
		"a time":        time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC),
		"a uuid":        uuid.MustParse("12345678-1234-1234-1234-123456789abc"),
		"a list":        []any{int64(1)},
		"a nil slice":   []any(nil),
		"a nil map":     map[string]any(nil),
		"a map":         map[string]any{"a": int64(1)},
	} {
		maps.Copy(values, pointers(name, v))
	}
	for name, p := range values {
		if got, ok := value.Scalar(p); ok {
			t.Errorf("Scalar(%s) = %#v, true", name, got)
		}
		if got, ok := value.GetString(p); ok {
			t.Errorf("GetString(%s) = %q, true", name, got)
		}
		if got, ok := value.GetInt64(p); ok {
			t.Errorf("GetInt64(%s) = %d, true", name, got)
		}
		if got, ok := value.GetUint64(p); ok {
			t.Errorf("GetUint64(%s) = %d, true", name, got)
		}
		if got, ok := value.GetFloat64(p); ok {
			t.Errorf("GetFloat64(%s) = %v, true", name, got)
		}
		if got, ok := value.GetBool(p); ok {
			t.Errorf("GetBool(%s) = %v, true", name, got)
		}
		if got, ok := value.NumberLiteral(p); ok {
			t.Errorf("NumberLiteral(%s) = %q, true", name, got)
		}
		if got, ok := value.UnreadNumber(p); ok {
			t.Errorf("UnreadNumber(%s) = %q, true", name, got)
		}
		if got, isList := value.ListElems(p); isList {
			t.Errorf("ListElems(%s) = %#v, true", name, got)
		}
		if kind, _ := value.Classify(p); kind != value.UnspecifiedKind {
			t.Errorf("Classify(%s) = %v, want UnspecifiedKind", name, kind)
		}
		if s := value.TypeStrata(p); s != value.InvalidStrata {
			t.Errorf("TypeStrata(%s) = %d, want InvalidStrata", name, s)
		}
		if value.Absent(p) {
			t.Errorf("Absent(%s) = true", name)
		}
		for otherName, other := range map[string]any{"itself": p, "nil": nil, "a string": "x", "an int64": int64(5)} {
			if value.Equal(p, other) || value.Equal(other, p) {
				t.Errorf("Equal(%s, %s) = true", name, otherName)
			}
			if _, err := value.Order(p, other); err == nil {
				t.Errorf("Order(%s, %s) did not refuse", name, otherName)
			}
			if _, err := value.Order(other, p); err == nil {
				t.Errorf("Order(%s, %s) did not refuse", otherName, name)
			}
		}
	}
}

// A json.Number that is no JSON number literal is no number to any reader of
// the value layer: no getter reads it, it has no strata, it orders against
// nothing and equals nothing, itself included.
func TestReaders_AJSONNumberThatIsNoLiteralIsNoNumber(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"+5", "007", "1_000", "0x1p4", ".5", "5.", "NaN", "Inf", "", " 5"} {
		v := json.Number(text)
		if got, ok := value.GetInt64(v); ok {
			t.Errorf("GetInt64(%q) = %d", text, got)
		}
		if got, ok := value.GetFloat64(v); ok {
			t.Errorf("GetFloat64(%q) = %v", text, got)
		}
		if got, ok := value.GetUint64(v); ok {
			t.Errorf("GetUint64(%q) = %d", text, got)
		}
		if got, ok := value.GetString(v); ok {
			t.Errorf("GetString(%q) = %q", text, got)
		}
		if kind, _ := value.Classify(v); kind != value.UnspecifiedKind {
			t.Errorf("Classify(%q) = %v, want UnspecifiedKind", text, kind)
		}
		if s := value.TypeStrata(v); s != value.InvalidStrata {
			t.Errorf("TypeStrata(%q) = %d, want InvalidStrata", text, s)
		}
		if _, err := value.Order(v, int64(5)); err == nil {
			t.Errorf("Order(%q, 5) ordered a json.Number that is no literal", text)
		}
		if value.Equal(v, v) {
			t.Errorf("Equal(%q, itself) = true", text)
		}
	}
}

// Every reader of the value layer reads a value of a predeclared or a named
// type, and refuses a pointer, without boxing it, as a validated row's values
// are read once a property: none of them allocates.
func TestReaders_ReadAValueWithoutAllocating(t *testing.T) {
	raceskip.Skip(t)
	type named string
	type namedInt int32
	text, n := "x", int64(5)
	var str, namedStr, i, ni, f, u, b, number, pointer any = text, named("x"), n, namedInt(5), 1.5, uint64(7), true, json.Number("12"), &text
	for name, read := range map[string]func(){
		"GetString of a string":         func() { _, _ = value.GetString(str) },
		"GetString of a named string":   func() { _, _ = value.GetString(namedStr) },
		"GetString of a pointer":        func() { _, _ = value.GetString(pointer) },
		"GetInt64 of an int64":          func() { _, _ = value.GetInt64(i) },
		"GetInt64 of a named int32":     func() { _, _ = value.GetInt64(ni) },
		"GetInt64 of a json.Number":     func() { _, _ = value.GetInt64(number) },
		"GetInt64 of a pointer":         func() { _, _ = value.GetInt64(pointer) },
		"GetFloat64 of a float64":       func() { _, _ = value.GetFloat64(f) },
		"GetUint64 of a uint64":         func() { _, _ = value.GetUint64(u) },
		"GetBool of a bool":             func() { _, _ = value.GetBool(b) },
		"TypeStrata of a named int32":   func() { _ = value.TypeStrata(ni) },
		"TypeStrata of a pointer":       func() { _ = value.TypeStrata(pointer) },
		"NumberLiteral of a number":     func() { _, _ = value.NumberLiteral(number) },
		"Absent of a pointer":           func() { _ = value.Absent(pointer) },
		"Order of two named int32":      func() { _, _ = value.Order(ni, ni) },
		"Order of a number and a float": func() { _, _ = value.Order(number, f) },
		"Equal of two named strings":    func() { _ = value.Equal(namedStr, namedStr) },
		"Equal of two pointers":         func() { _ = value.Equal(pointer, pointer) },
	} {
		if allocs := testing.AllocsPerRun(100, read); allocs != 0 {
			t.Errorf("%s allocates %v per call, want 0", name, allocs)
		}
	}
}

// A named unsigned or float32 value is read as its base: a uint64 above
// math.MaxInt64 is no int64 but orders above every int64, and a float32 orders
// as the float it is.
func TestReaders_ANamedUnsignedOrFloat32IsReadAsItsBase(t *testing.T) {
	t.Parallel()
	type namedUint uint64
	type namedF32 float32
	top := uint64(math.MaxUint64)
	for name, v := range map[string]any{"a uint64": top, "a named uint64": namedUint(top)} {
		if n, ok := value.GetInt64(v); ok {
			t.Errorf("GetInt64(%s at MaxUint64) = %d, true; want no int64", name, n)
		}
		if u, ok := value.GetUint64(v); !ok || u != top {
			t.Errorf("GetUint64(%s) = %d, %v; want MaxUint64", name, u, ok)
		}
		if got, err := value.Order(v, int64(0)); err != nil || got != 1 {
			t.Errorf("Order(%s at MaxUint64, 0) = %d, %v; want 1", name, got, err)
		}
	}
	for name, v := range map[string]any{"a float32": float32(1.5), "a named float32": namedF32(1.5)} {
		if got, ok := value.GetFloat64(v); !ok || got != 1.5 {
			t.Errorf("GetFloat64(%s) = %v, %v; want 1.5", name, got, ok)
		}
		if got, err := value.Order(v, int64(1)); err != nil || got != 1 {
			t.Errorf("Order(%s 1.5, 1) = %d, %v; want 1", name, got, err)
		}
	}
}

// A map, a channel or a function that holds a value has no strata, and none
// equals nil.
func TestTypeStrata_AMapChannelOrFunctionHasNone(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"a map":      map[string]int{"a": 1},
		"a channel":  make(chan int),
		"a function": func() {},
	} {
		if s := value.TypeStrata(v); s != value.InvalidStrata {
			t.Errorf("TypeStrata(%s) = %d, want InvalidStrata", name, s)
		}
		if value.Equal(v, nil) || value.Equal(nil, v) {
			t.Errorf("Equal(%s, nil) = true", name)
		}
	}
}

// A value holds none when it is nil, a nil map or a nil slice, whatever the
// map's key or the slice's element. A nil channel or function holds none of
// these: it is a value of no form.
func TestAbsent_IsNilANilMapOrANilSlice(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"nil":                          nil,
		"a nil map":                    map[string]int(nil),
		"a nil slice":                  []any(nil),
		"a nil slice of strings":       []string(nil),
		"a nil map keyed by no string": map[int]int(nil),
	} {
		if s := value.TypeStrata(v); s != value.NilStrata {
			t.Errorf("TypeStrata(%s) = %d, want NilStrata", name, s)
		}
		if !value.Absent(v) {
			t.Errorf("Absent(%s) = false", name)
		}
		if form, ok := value.Scalar(v); !ok || form != nil {
			t.Errorf("Scalar(%s) = %#v, %v; want nil, true", name, form, ok)
		}
		if !value.Equal(v, nil) || !value.Equal(nil, v) || !value.Equal(v, []any(nil)) {
			t.Errorf("%s is not equal to nil and to a nil slice", name)
		}
		if cmp, err := value.Order(v, nil); err != nil || cmp != 0 {
			t.Errorf("Order(%s, nil) = %d, %v; want 0", name, cmp, err)
		}
	}
	for name, v := range map[string]any{
		"a nil channel":  (chan int)(nil),
		"a nil function": (func())(nil),
	} {
		if s := value.TypeStrata(v); s != value.InvalidStrata {
			t.Errorf("TypeStrata(%s) = %d, want InvalidStrata", name, s)
		}
		if value.Absent(v) {
			t.Errorf("Absent(%s) = true", name)
		}
		if form, ok := value.Scalar(v); ok {
			t.Errorf("Scalar(%s) = %#v, true", name, form)
		}
		if value.Equal(v, nil) || value.Equal(nil, v) {
			t.Errorf("Equal(%s, nil) = true", name)
		}
	}
}

// A non-nil slice is a list, as ListElems reads it, and a nil slice is none, so
// it is not the empty list.
func TestTypeStrata_ANilSliceIsNoEmptyList(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct{ v, want any }{
		"a slice":        {[]any{int64(1)}, []any{int64(1)}},
		"an empty slice": {[]any{}, []any{}},
	} {
		if s := value.TypeStrata(c.v); s != value.SliceStrata {
			t.Errorf("TypeStrata(%s) = %d, want SliceStrata", name, s)
		}
		if value.Absent(c.v) {
			t.Errorf("Absent(%s) = true", name)
		}
		if !value.Equal(c.v, c.want) {
			t.Errorf("Equal(%s, %#v) = false", name, c.want)
		}
		if cmp, err := value.Order(c.v, c.want); err != nil || cmp != 0 {
			t.Errorf("Order(%s, %#v) = %d, %v; want 0", name, c.want, cmp, err)
		}
	}
	for _, nilSlice := range []any{[]any(nil), []string(nil)} {
		if _, isList := value.ListElems(nilSlice); isList {
			t.Errorf("ListElems(%#v) reads a list", nilSlice)
		}
		if value.Equal(nilSlice, []any{}) || value.Equal([]any{}, nilSlice) {
			t.Errorf("Equal(%#v, the empty list) = true", nilSlice)
		}
		if cmp, err := value.Order(nilSlice, []any{}); err != nil || cmp >= 0 {
			t.Errorf("Order(%#v, the empty list) = %d, %v; want below it, as nil is", nilSlice, cmp, err)
		}
	}
}

// UnreadNumber names a json.Number whose text is no literal and nothing else.
func TestUnreadNumber_NamesOnlyTextThatIsNoLiteral(t *testing.T) {
	t.Parallel()
	five, signed := json.Number("5"), json.Number("+5")
	for _, v := range []any{five, &five, &signed, "5", "+5", int64(5), nil} {
		if n, ok := value.UnreadNumber(v); ok {
			t.Errorf("UnreadNumber(%#v) = %q, true; want false", v, n)
		}
	}
	if n, ok := value.UnreadNumber(signed); !ok || n != signed {
		t.Errorf("UnreadNumber(%#v) = %q, %v; want +5, true", signed, n, ok)
	}
}

// An immutable container that holds nil, as immutable stores a nil slice or
// map, holds no value to every reader here, as the nil it stores does.
func TestReaders_AnImmutableContainerHoldingNilHoldsNone(t *testing.T) {
	t.Parallel()
	for name, v := range map[string]any{
		"a wrapped nil slice": immutable.Wrap([]any(nil)).Unwrap(),
		"a wrapped nil map":   immutable.Wrap(map[string]any(nil)).Unwrap(),
		"a nil Properties":    immutable.WrapProperties(nil),
		"a nil Key":           immutable.WrapKey(nil),
		"a nil int-keyed Map": immutable.WrapMap(map[int]any(nil)),
	} {
		if !value.Absent(v) {
			t.Errorf("Absent(%s) = false", name)
		}
		if got, ok := value.Scalar(v); !ok || got != nil {
			t.Errorf("Scalar(%s) = %v, %v; want nil, true", name, got, ok)
		}
		if s := value.TypeStrata(v); s != value.NilStrata {
			t.Errorf("TypeStrata(%s) = %d, want NilStrata", name, s)
		}
		if _, isList := value.ListElems(v); isList {
			t.Errorf("ListElems(%s) reads a list", name)
		}
		if !value.Equal(v, nil) || !value.Equal(nil, v) {
			t.Errorf("Equal(%s, nil) = false", name)
		}
	}
	for name, pair := range map[string][2]any{
		"a wrapped nil slice and a wrapped empty one": {immutable.Wrap([]any(nil)).Unwrap(), immutable.Wrap([]any{}).Unwrap()},
		"a wrapped nil map and a wrapped empty one":   {immutable.Wrap(map[string]any(nil)).Unwrap(), immutable.Wrap(map[string]any{}).Unwrap()},
		"a nil Properties and an empty map":           {immutable.WrapProperties(nil), map[string]any{}},
	} {
		if value.Equal(pair[0], pair[1]) || value.Equal(pair[1], pair[0]) {
			t.Errorf("Equal(%s) = true", name)
		}
	}
}

// Map is a generic type of the immutable container's name in another package.
// It embeds the container, so reflect names its instances as it names the
// container's, Map[string].
type Map[K comparable] struct {
	immutable.Map[K]
	Name string
}

// A struct that embeds an immutable container holding nil is a struct, read as
// every other struct is: it holds a value, and no reader reads it as a list.
func TestReaders_AStructEmbeddingANilContainerHoldsAValue(t *testing.T) {
	t.Parallel()
	structStrata := value.TypeStrata(struct{ A int }{})
	for name, v := range map[string]any{
		"a struct embedding a nil Slice":      struct{ immutable.Slice }{},
		"a struct embedding a nil Map[int]":   struct{ immutable.Map[int] }{},
		"a struct embedding a nil Properties": struct{ immutable.Properties }{},
		"another package's Map":               Map[string]{},
	} {
		if value.Absent(v) {
			t.Errorf("Absent(%s) = true", name)
		}
		if got, ok := value.Scalar(v); ok {
			t.Errorf("Scalar(%s) = %v, true; want no scalar", name, got)
		}
		if s := value.TypeStrata(v); s != structStrata {
			t.Errorf("TypeStrata(%s) = %d, want %d, as any struct", name, s, structStrata)
		}
		if _, isList := value.ListElems(v); isList {
			t.Errorf("ListElems(%s) reads a list", name)
		}
		if value.Equal(v, nil) || value.Equal(nil, v) {
			t.Errorf("Equal(%s, nil) = true", name)
		}
	}
}

// Equal holds a value that holds none apart from every value that holds one,
// so it stays transitive: a nil map equals nil, and neither equals an empty map.
func TestEqual_ANilMapIsNoEmptyMap(t *testing.T) {
	t.Parallel()
	for name, nilMap := range map[string]any{
		"a nil map[string]any": map[string]any(nil),
		"a nil map[string]int": map[string]int(nil),
	} {
		if !value.Equal(nilMap, nil) || !value.Equal(nil, nilMap) {
			t.Errorf("Equal(%s, nil) = false", name)
		}
		for _, empty := range []any{map[string]any{}, immutable.Wrap(map[string]any{}).Unwrap()} {
			if value.Equal(nilMap, empty) || value.Equal(empty, nilMap) {
				t.Errorf("Equal(%s, %#v) = true", name, empty)
			}
		}
	}
}
