package value_test

import (
	"encoding/json"
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

// Each reader of the value layer reads a value as the form Scalar reads it in:
// the getters, the strata and the order agree with Scalar on every carrier.
func TestReaders_ReadEveryCarrierAsItsForm(t *testing.T) {
	t.Parallel()
	number := json.Number("12")
	wide := json.Number("99999999999999999999")
	var heldText any = "x"
	var heldInt any = int64(5)
	var heldBool any = true
	var heldFloat any = 1.5
	var heldUint any = uint64(7)
	if s, ok := value.GetString(&number); ok {
		t.Errorf("GetString read a pointer to a json.Number as the string %q", s)
	}
	if n, ok := value.GetInt64(&number); !ok || n != 12 {
		t.Errorf("GetInt64(*json.Number 12) = %d, %v; want 12, true", n, ok)
	}
	if s, ok := value.GetString(&heldText); !ok || s != "x" {
		t.Errorf("GetString(*any x) = %q, %v; want x, true", s, ok)
	}
	if n, ok := value.GetInt64(&heldInt); !ok || n != 5 {
		t.Errorf("GetInt64(*any 5) = %d, %v; want 5, true", n, ok)
	}
	if b, ok := value.GetBool(&heldBool); !ok || !b {
		t.Errorf("GetBool(*any true) = %v, %v; want true, true", b, ok)
	}
	if f, ok := value.GetFloat64(&heldFloat); !ok || f != 1.5 {
		t.Errorf("GetFloat64(*any 1.5) = %v, %v; want 1.5, true", f, ok)
	}
	if u, ok := value.GetUint64(&heldUint); !ok || u != 7 {
		t.Errorf("GetUint64(*any 7) = %v, %v; want 7, true", u, ok)
	}
	for _, c := range []struct {
		name string
		v    any
		want int
	}{
		{"a pointer to a json.Number", &number, value.NumericStrata},
		{"an interface holding a string, behind a pointer", &heldText, value.StringStrata},
		{"an interface holding an int64, behind a pointer", &heldInt, value.NumericStrata},
		{"an interface holding a bool, behind a pointer", &heldBool, value.BoolStrata},
	} {
		if got := value.TypeStrata(c.v); got != c.want {
			t.Errorf("TypeStrata(%s) = %d, want %d", c.name, got, c.want)
		}
	}
	if got, err := value.Order(&heldText, "x"); err != nil || got != 0 {
		t.Errorf("Order(*any x, x) = %d, %v; want 0", got, err)
	}
	if got, err := value.Order(&number, int64(12)); err != nil || got != 0 {
		t.Errorf("Order(*json.Number 12, 12) = %d, %v; want 0", got, err)
	}
	// A wide integer is compared exactly whatever carries it: the float64
	// nearest it is 1e20, which it is not.
	if got, err := value.Order(&wide, 1e20); err != nil || got != -1 {
		t.Errorf("Order(*json.Number 99999999999999999999, 1e20) = %d, %v; want -1", got, err)
	}
}

// A json.Number that is no JSON number literal is no number to any reader of
// the value layer: no getter reads it, it has no strata, it orders against
// nothing and equals nothing, itself included.
func TestReaders_AJSONNumberThatIsNoLiteralIsNoNumber(t *testing.T) {
	t.Parallel()
	for _, text := range []string{"+5", "007", "1_000", "0x1p4", ".5", "5.", "NaN", "Inf", "", " 5"} {
		n := json.Number(text)
		for _, v := range []any{n, &n} {
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
}

// Every reader of the value layer reads a value through its pointers,
// interfaces and named type without boxing it, as a validated row's values are
// read once a property: none of them allocates.
func TestReaders_ReadACarrierWithoutAllocating(t *testing.T) {
	raceskip.Skip(t)
	type named string
	text, n, f, number := "x", int64(5), 1.5, json.Number("12")
	var heldBool any = true
	var heldText any = named("x")
	var a, b any = &text, &text
	at, id := time.Now(), uuid.New()
	var pn, pf, pnum, hb, ht, pt, pu any = &n, &f, &number, &heldBool, &heldText, &at, &id
	for name, read := range map[string]func(){
		"GetString of a pointer":                 func() { _, _ = value.GetString(a) },
		"GetString of a named string, held":      func() { _, _ = value.GetString(ht) },
		"GetInt64 of a pointer":                  func() { _, _ = value.GetInt64(pn) },
		"GetInt64 of a pointer to a json.Number": func() { _, _ = value.GetInt64(pnum) },
		"GetFloat64 of a pointer":                func() { _, _ = value.GetFloat64(pf) },
		"GetUint64 of a pointer":                 func() { _, _ = value.GetUint64(pn) },
		"GetBool of an interface, held":          func() { _, _ = value.GetBool(hb) },
		"TypeStrata of a pointer":                func() { _ = value.TypeStrata(pn) },
		"NumberLiteral of a pointer":             func() { _, _ = value.NumberLiteral(pnum) },
		"GetTime of a pointer":                   func() { _, _ = value.GetTime(pt) },
		"GetUUID of a pointer":                   func() { _, _ = value.GetUUID(pu) },
		"Order of two pointers":                  func() { _, _ = value.Order(pn, pn) },
		"Equal of two pointers":                  func() { _ = value.Equal(a, b) },
	} {
		if allocs := testing.AllocsPerRun(100, read); allocs != 0 {
			t.Errorf("%s allocates %v per call, want 0", name, allocs)
		}
	}
}

// An unsigned or a float32 value is read as its base whatever carries it: a
// uint64 above math.MaxInt64 is no int64 but orders above every int64, and a
// float32 orders as the float it is.
func TestReaders_AnUnsignedOrFloat32CarrierIsReadAsItsBase(t *testing.T) {
	t.Parallel()
	type namedUint uint64
	type namedF32 float32
	top := uint64(math.MaxUint64)
	f := float32(1.5)
	for name, v := range map[string]any{"a pointer to a uint64": &top, "a named uint64": namedUint(top)} {
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
	for name, v := range map[string]any{"a pointer to a float32": &f, "a named float32": namedF32(f)} {
		if got, ok := value.GetFloat64(v); !ok || got != 1.5 {
			t.Errorf("GetFloat64(%s) = %v, %v; want 1.5", name, got, ok)
		}
		if got, err := value.Order(v, int64(1)); err != nil || got != 1 {
			t.Errorf("Order(%s 1.5, 1) = %d, %v; want 1", name, got, err)
		}
	}
}

// carriers returns v bare, behind a pointer, and held by an interface behind a
// pointer, each under its name.
func carriers(name string, v any) map[string]any {
	held := v
	pointer := reflect.New(reflect.TypeOf(v))
	pointer.Elem().Set(reflect.ValueOf(v))
	return map[string]any{name: v, name + " behind a pointer": pointer.Interface(), name + " held behind a pointer": &held}
}

// A map, a channel, a function or an unsafe pointer that holds a value has no
// strata whatever carries it, and none equals a nil pointer.
func TestTypeStrata_AMapChannelFunctionOrUnsafePointerHasNone(t *testing.T) {
	t.Parallel()
	var nilInt *int
	n := 1
	for name, v := range map[string]any{
		"a map":             map[string]int{"a": 1},
		"a channel":         make(chan int),
		"a function":        func() {},
		"an unsafe pointer": unsafe.Pointer(&n),
	} {
		for carrier, c := range carriers(name, v) {
			if s := value.TypeStrata(c); s != value.InvalidStrata {
				t.Errorf("TypeStrata(%s) = %d, want InvalidStrata", carrier, s)
			}
			if value.Equal(c, nilInt) {
				t.Errorf("Equal(%s, a nil pointer) = true", carrier)
			}
		}
	}
}

// A nil value is nil whatever carries it, as Scalar reads it and every writer
// writes it: a nil pointer, map, slice, channel, function or unsafe pointer,
// bare or behind a pointer or an interface, a bare nil slice or string-keyed
// map included.
func TestTypeStrata_ANilValueIsNilThroughEveryCarrier(t *testing.T) {
	t.Parallel()
	var nilInt *int
	for name, v := range map[string]any{
		"a nil pointer":                nilInt,
		"a nil map":                    map[string]int(nil),
		"a nil slice":                  []any(nil),
		"a nil channel":                (chan int)(nil),
		"a nil function":               (func())(nil),
		"a nil unsafe pointer":         unsafe.Pointer(nil),
		"a nil map keyed by no string": map[int]int(nil),
	} {
		for carrier, c := range carriers(name, v) {
			if s := value.TypeStrata(c); s != value.NilStrata {
				t.Errorf("TypeStrata(%s) = %d, want NilStrata", carrier, s)
			}
			if !value.Absent(c) {
				t.Errorf("Absent(%s) = false", carrier)
			}
			if form, ok := value.Scalar(c); !ok || form != nil {
				t.Errorf("Scalar(%s) = %#v, %v; want nil, true", carrier, form, ok)
			}
			if !value.Equal(c, nil) || !value.Equal(nil, c) || !value.Equal(c, nilInt) {
				t.Errorf("%s is not equal to nil and to a nil pointer", carrier)
			}
		}
	}
}

// A bare non-nil slice is a list, as ListElems reads it, and a nil slice is
// none, so it is not the empty list; a slice behind a pointer or an interface
// is no list, as Scalar and ListElems read it, so it has no strata and equals
// nothing.
func TestTypeStrata_ASliceIsAListOnlyBare(t *testing.T) {
	t.Parallel()
	one := []any{int64(1)}
	for name, c := range map[string]struct{ v, want any }{
		"a slice":        {one, []any{int64(1)}},
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
	for carrier, c := range carriers("a slice", one) {
		if carrier == "a slice" {
			continue
		}
		if _, isList := value.ListElems(c); isList {
			t.Fatalf("ListElems(%s) reads a list", carrier)
		}
		if s := value.TypeStrata(c); s != value.InvalidStrata {
			t.Errorf("TypeStrata(%s) = %d, want InvalidStrata", carrier, s)
		}
		if value.Equal(c, one) || value.Equal(one, c) {
			t.Errorf("Equal(%s, the slice it holds) = true", carrier)
		}
		if _, err := value.Order(c, one); err == nil {
			t.Errorf("Order(%s, the slice it holds) did not refuse", carrier)
		}
	}
}

// UnreadNumber names a json.Number whose text is no literal and nothing else.
func TestUnreadNumber_NamesOnlyTextThatIsNoLiteral(t *testing.T) {
	t.Parallel()
	five, signed := json.Number("5"), json.Number("+5")
	for _, v := range []any{five, &five, "5", int64(5), nil} {
		if n, ok := value.UnreadNumber(v); ok {
			t.Errorf("UnreadNumber(%#v) = %q, true; want false", v, n)
		}
	}
	for _, v := range []any{signed, &signed} {
		if n, ok := value.UnreadNumber(v); !ok || n != signed {
			t.Errorf("UnreadNumber(%#v) = %q, %v; want +5, true", v, n, ok)
		}
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
