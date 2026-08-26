package value_test

import (
	"encoding/json"
	"math"
	"math/rand"
	"regexp"
	"slices"
	"testing"
	"testing/quick"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
)

func assertEqual[T comparable](t *testing.T, expected, got T) {
	t.Helper()
	if expected != got {
		t.Errorf("expected %v, got %v", expected, got)
	}
}

func TestTypeStrata(t *testing.T) {
	// Nil
	assertEqual(t, value.NilStrata, value.TypeStrata(nil))

	// Bool
	assertEqual(t, value.BoolStrata, value.TypeStrata(true))
	assertEqual(t, value.BoolStrata, value.TypeStrata(false))

	// Signed integers
	assertEqual(t, value.NumericStrata, value.TypeStrata(int(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(int8(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(int16(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(int32(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(int64(1)))

	// Unsigned integers (NEW in v2)
	assertEqual(t, value.NumericStrata, value.TypeStrata(uint(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(uint8(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(uint16(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(uint32(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(uint64(1)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(uintptr(1)))

	// Floats
	assertEqual(t, value.NumericStrata, value.TypeStrata(float32(3.14)))
	assertEqual(t, value.NumericStrata, value.TypeStrata(float64(3.14)))

	// Strings
	assertEqual(t, value.StringStrata, value.TypeStrata("hello"))

	// Regexp
	assertEqual(t, value.StringStrata, value.TypeStrata(regexp.MustCompile("x")))

	// Slices
	assertEqual(t, value.SliceStrata, value.TypeStrata([]string{"a"}))
	assertEqual(t, value.SliceStrata, value.TypeStrata([]int{1, 2}))
	assertEqual(t, value.SliceStrata, value.TypeStrata([]any{1, "a"}))

	// Invalid types
	assertEqual(t, value.InvalidStrata, value.TypeStrata(struct{}{}))
	assertEqual(t, value.InvalidStrata, value.TypeStrata(map[string]int{}))
	assertEqual(t, value.InvalidStrata, value.TypeStrata(make(chan int)))
}

// Named types over a predeclared base.
type (
	namedBool    bool
	namedInt     int
	namedInt64   int64
	namedUint    uint
	namedUint64  uint64
	namedFloat32 float32
	namedFloat64 float64
	namedString  string
)

// A named type takes the strata of its underlying kind. adapter/gogen emits
// `type <Name> <base>` for every DataType and inline enum, so rejecting them
// meant the generator produced values this package could not order.
//
// Mutation: deleting strataOfKind's call site in TypeStrata turns this red;
// widening it to accept reflect.Struct turns TestTypeStrata_Basic red on the
// struct{}{} case.
func TestTypeStrata_NamedTypesTakeTheirUnderlyingKind(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  int
	}{
		{"namedBool", namedBool(true), value.BoolStrata},
		{"namedInt", namedInt(42), value.NumericStrata},
		{"namedInt64", namedInt64(42), value.NumericStrata},
		{"namedUint", namedUint(42), value.NumericStrata},
		{"namedUint64", namedUint64(42), value.NumericStrata},
		{"namedFloat32", namedFloat32(3.14), value.NumericStrata},
		{"namedFloat64", namedFloat64(3.14), value.NumericStrata},
		{"namedString", namedString("hello"), value.StringStrata},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := value.TypeStrata(tt.input); got != tt.want {
				t.Errorf("TypeStrata(%T) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// A pointer takes the strata of what it points at, and a nil pointer is nil.
func TestTypeStrata_PointersFollowTheirTarget(t *testing.T) {
	i, f, s, b := int64(1), 1.5, "x", true
	var nilPtr *int64

	for _, tt := range []struct {
		name  string
		input any
		want  int
	}{
		{"*int64", &i, value.NumericStrata},
		{"*float64", &f, value.NumericStrata},
		{"*string", &s, value.StringStrata},
		{"*bool", &b, value.BoolStrata},
		{"nil *int64", nilPtr, value.NilStrata},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if got := value.TypeStrata(tt.input); got != tt.want {
				t.Errorf("TypeStrata(%T) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// An immutable.Slice is what Value.Unwrap returns for a List- or Vector-typed
// property, and it is a struct, so no reflect kind names it. Without the
// explicit case every invariant comparing a list was an evaluation error.
//
// Mutation: making TypeStrata's immutable.Slice case return InvalidStrata
// turns this red, and turns the elementwise test below red with the evaluation
// error this whole change removes.
func TestTypeStrata_ImmutableSliceIsASlice(t *testing.T) {
	wrapped := immutable.Wrap([]any{"a", "b"})
	slice, ok := wrapped.Slice()
	if !ok {
		t.Fatalf("Wrap([]any) did not produce a Slice, got %T", wrapped.Unwrap())
	}
	if got := value.TypeStrata(slice); got != value.SliceStrata {
		t.Errorf("TypeStrata(immutable.Slice) = %d, want SliceStrata (%d)", got, value.SliceStrata)
	}
}

// A json.Number is numeric everywhere in this package. ClassifyWithRegistry
// has always called it Int/Float; before TypeStrata agreed, a lexical number
// took the String strata and compared against int64 by strata RANK — answering
// "greater" with no error rather than comparing the two numbers.
//
// Mutation: deleting TypeStrata's json.Number case makes the Order case below
// return 1 instead of 0, silently; deleting the GetInt64 case makes it error.
func TestOrder_JSONNumberIsNumeric(t *testing.T) {
	if got := value.TypeStrata(json.Number("5")); got != value.NumericStrata {
		t.Errorf("TypeStrata(json.Number) = %d, want NumericStrata (%d)", got, value.NumericStrata)
	}
	for _, tt := range []struct {
		name  string
		left  any
		right any
		want  int
	}{
		{"equal to int64", json.Number("5"), int64(5), 0},
		{"less than int64", json.Number("4"), int64(5), -1},
		{"fractional against float64", json.Number("2.5"), 2.5, 0},
		{"int64 on the left", int64(9), json.Number("5"), 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := value.Order(tt.left, tt.right)
			if err != nil {
				t.Fatalf("Order: %v", err)
			}
			if got != tt.want {
				t.Errorf("Order = %d, want %d", got, tt.want)
			}
		})
	}
}

// Order compares a named type by its base value rather than erroring.
//
// Mutation: reverting GetInt64/GetFloat64/toStringComparable/toBool to a bare
// type switch turns this red while TypeStrata still reports a valid strata,
// which is the half-fixed state that would make Order's slice arm panic.
func TestOrder_NamedTypesCompareAsTheirBase(t *testing.T) {
	tests := []struct {
		name  string
		left  any
		right any
		want  int
	}{
		{"namedFloat greater than float64", namedFloat64(1.2), float64(1.0), 1},
		{"namedInt greater than int", namedInt(42), int(10), 1},
		{"namedString less than string", namedString("hello"), "world", -1},
		{"namedBool greater than bool", namedBool(true), false, 1},
		{"namedInt64 equal to int64", namedInt64(7), int64(7), 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := value.Order(tt.left, tt.right)
			if err != nil {
				t.Fatalf("Order(%T, %T): %v", tt.left, tt.right, err)
			}
			if got != tt.want {
				t.Errorf("Order(%v, %v) = %d, want %d", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// Order compares an immutable.Slice against a plain slice element by element,
// and against a Go slice on the other side.
//
// Mutation: reverting Order's SliceStrata arm to reflect.ValueOf(left).Len()
// makes this PANIC rather than fail, which is why the arm and the TypeStrata
// case must land together.
func TestOrder_ImmutableSliceComparesElementwise(t *testing.T) {
	sliceOf := func(elems ...any) any {
		s, ok := immutable.Wrap(elems).Slice()
		if !ok {
			t.Fatalf("Wrap did not produce a Slice")
		}
		return s
	}

	for _, tt := range []struct {
		name  string
		left  any
		right any
		want  int
	}{
		{"equal", sliceOf("a", "b"), sliceOf("a", "b"), 0},
		{"first element decides", sliceOf("a", "z"), sliceOf("b", "a"), -1},
		{"shorter is smaller", sliceOf("a"), sliceOf("a", "b"), -1},
		{"against a Go slice", sliceOf("a", "b"), []any{"a", "b"}, 0},
		{"Go slice on the left", []any{"a", "c"}, sliceOf("a", "b"), 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := value.Order(tt.left, tt.right)
			if err != nil {
				t.Fatalf("Order: %v", err)
			}
			if got != tt.want {
				t.Errorf("Order = %d, want %d", got, tt.want)
			}
		})
	}
}

// TestOrder_LargeUnsigned verifies that uint64/uintptr values > math.MaxInt64
// can be compared correctly. This was a bug where GetInt64 rejected overflow,
// causing Order to fail.
func TestOrder_LargeUnsigned(t *testing.T) {
	const (
		maxInt64      = int64(1<<63 - 1)  // 9223372036854775807
		maxInt64Plus1 = uint64(1 << 63)   // 9223372036854775808
		maxUint64     = uint64(1<<64 - 1) // 18446744073709551615
	)

	tests := []struct {
		name  string
		left  any
		right any
		want  int
	}{
		// Both large unsigned (> MaxInt64)
		{"large uint64 vs small uint64", maxInt64Plus1 + 1000, uint64(0), 1},
		{"small uint64 vs large uint64", uint64(0), maxInt64Plus1 + 1000, -1},
		{"max uint64 vs 0", maxUint64, uint64(0), 1},
		{"0 vs max uint64", uint64(0), maxUint64, -1},
		{"large uint64 vs large uint64 equal", maxInt64Plus1, maxInt64Plus1, 0},
		// NOTE: "large uintptr" test removed - uintptr(maxInt64Plus1) won't compile on 32-bit.
		// uintptr behavior is covered by uint64 tests (both use GetUint64 internally).

		// Mixed signed/unsigned with large unsigned values
		{"positive int64 vs large uint64", int64(42), maxInt64Plus1 + 1000, -1},
		{"large uint64 vs positive int64", maxInt64Plus1 + 1000, int64(42), 1},
		{"negative int64 vs large uint64", int64(-10), maxInt64Plus1, -1},
		{"large uint64 vs negative int64", maxInt64Plus1, int64(-10), 1},
		{"max int64 vs large uint64", maxInt64, maxInt64Plus1, -1},
		{"large uint64 vs max int64", maxInt64Plus1, maxInt64, 1},

		// Mixed signed/unsigned with small values (should still work)
		{"small int64 vs small uint64 equal", int64(42), uint64(42), 0},
		{"negative int64 vs small uint64", int64(-10), uint64(42), -1},
		{"small uint64 vs negative int64", uint64(42), int64(-10), 1},

		// Large unsigned vs float (precision caveat documented)
		{"large uint64 vs small float", maxInt64Plus1, float64(1.5), 1},
		{"small float vs large uint64", float64(1.5), maxInt64Plus1, -1},
		{"large uint64 vs large float", maxInt64Plus1, float64(1e19), -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := value.Order(tt.left, tt.right)
			if err != nil {
				t.Fatalf("Order(%v, %v) unexpected error: %v", tt.left, tt.right, err)
			}
			if got != tt.want {
				t.Errorf("Order(%v, %v) = %d, want %d", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// TestGetUint64 verifies the new GetUint64 function.
func TestGetUint64(t *testing.T) {
	tests := []struct {
		name   string
		input  any
		want   uint64
		wantOK bool
	}{
		// Unsigned types
		{"uint", uint(42), 42, true},
		{"uint8", uint8(255), 255, true},
		{"uint16", uint16(65535), 65535, true},
		{"uint32", uint32(4294967295), 4294967295, true},
		{"uint64 small", uint64(42), 42, true},
		{"uint64 large", uint64(1<<63 + 1000), 1<<63 + 1000, true},
		{"uint64 max", uint64(1<<64 - 1), 1<<64 - 1, true},
		{"uintptr", uintptr(12345), 12345, true},

		// Non-unsigned types return false
		{"int", int(42), 0, false},
		{"int64", int64(42), 0, false},
		{"float64", float64(42.5), 0, false},
		{"string", "42", 0, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := value.GetUint64(tt.input)
			if ok != tt.wantOK {
				t.Errorf("GetUint64(%v) ok = %v, want %v", tt.input, ok, tt.wantOK)
			}
			if got != tt.want {
				t.Errorf("GetUint64(%v) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

// TestUint64Compare verifies the new Uint64Compare function.
func TestUint64Compare(t *testing.T) {
	tests := []struct {
		left, right uint64
		want        int
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, 1, -1},
		{42, 42, 0},
		{1<<63 + 1000, 1 << 63, 1},
		{1 << 63, 1<<63 + 1000, -1},
		{1<<64 - 1, 0, 1}, // max uint64 vs 0
	}

	for _, tt := range tests {
		got := value.Uint64Compare(tt.left, tt.right)
		if got != tt.want {
			t.Errorf("Uint64Compare(%d, %d) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func TestTypeOrder(t *testing.T) {
	assertTypeOrder := func(t *testing.T, expected int, left, right any) {
		t.Helper()
		got, err := value.TypeOrder(left, right)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, expected, got)
	}

	// Nil comparisons
	assertTypeOrder(t, 0, nil, nil)
	assertTypeOrder(t, 1, false, nil)
	assertTypeOrder(t, -1, nil, true)

	// Bool comparisons (same strata)
	assertTypeOrder(t, 0, false, false)
	assertTypeOrder(t, 0, true, true)
	assertTypeOrder(t, 0, false, true)
	assertTypeOrder(t, 0, true, false)

	// Numeric comparisons (same strata across all numeric types)
	assertTypeOrder(t, 0, 1, 1)
	assertTypeOrder(t, 0, 1, 3.14)
	assertTypeOrder(t, 0, 3.14, 1)
	assertTypeOrder(t, 0, 1.12, 3.14)
	assertTypeOrder(t, 0, uint(42), int(42))
	assertTypeOrder(t, 0, uint64(1), float64(1.5))

	// Cross-strata comparisons
	assertTypeOrder(t, -1, false, 1)
	assertTypeOrder(t, -1, true, 1.2)
	assertTypeOrder(t, 1, 1, false)
	assertTypeOrder(t, 1, 1, true)

	// String comparisons
	assertTypeOrder(t, 0, "a", "b")
	assertTypeOrder(t, 1, "a", 1)
	assertTypeOrder(t, -1, 1, "a")

	// Slice comparisons
	assertTypeOrder(t, 0, []string{"a"}, []int{1})
	assertTypeOrder(t, 1, []int{1}, 1)
	assertTypeOrder(t, -1, 1, []int{1})

	t.Run("invalid types return error", func(t *testing.T) {
		_, err := value.TypeOrder(struct{}{}, struct{}{})
		if err == nil {
			t.Error("expected error for struct comparison")
		}
	})
}

func TestFloat64Compare_SpecialValues(t *testing.T) {
	nan := math.NaN()

	cases := []struct {
		name  string
		left  float64
		right float64
		want  int
	}{
		{"nan_equal_nan", nan, nan, 0},
		{"nan_greater_than_pos_inf", nan, math.Inf(1), 1},
		{"nan_greater_than_neg_inf", nan, math.Inf(-1), 1},
		{"nan_greater_than_finite", nan, 42, 1},
		{"pos_inf_less_than_nan", math.Inf(1), nan, -1},
		{"neg_inf_less_than_finite", math.Inf(-1), -100, -1},
		{"pos_inf_greater_than_finite", math.Inf(1), 9, 1},
		{"pos_inf_greater_than_neg_inf", math.Inf(1), math.Inf(-1), 1},
		{"neg_inf_equal_neg_inf", math.Inf(-1), math.Inf(-1), 0},
		{"pos_inf_equal_pos_inf", math.Inf(1), math.Inf(1), 0},
		{"finite_less_than_finite", 1.0, 2.0, -1},
		{"finite_greater_than_finite", 2.0, 1.0, 1},
		{"finite_equal_finite", 1.0, 1.0, 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assertEqual(t, tc.want, value.Float64Compare(tc.left, tc.right))
		})
	}
}

func TestIsFinite(t *testing.T) {
	tests := []struct {
		name   string
		input  float64
		expect bool
	}{
		{"zero", 0.0, true},
		{"positive", 1.5, true},
		{"negative", -1.5, true},
		{"max_float64", math.MaxFloat64, true},
		{"smallest_positive", math.SmallestNonzeroFloat64, true},
		{"nan", math.NaN(), false},
		{"pos_inf", math.Inf(1), false},
		{"neg_inf", math.Inf(-1), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := value.IsFinite(tt.input)
			if result != tt.expect {
				t.Errorf("IsFinite(%v) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestOrder(t *testing.T) {
	assertOrder := func(t *testing.T, expected int, left, right any) {
		t.Helper()
		got, err := value.Order(left, right)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, expected, got)
	}

	// Nil
	assertOrder(t, 0, nil, nil)
	assertOrder(t, 1, false, nil)
	assertOrder(t, -1, nil, true)

	// Bool
	assertOrder(t, 0, false, false)
	assertOrder(t, 0, true, true)
	assertOrder(t, -1, false, true)
	assertOrder(t, 1, true, false)

	// Signed integers
	assertOrder(t, 0, 1, 1)
	assertOrder(t, -1, 1, 2)
	assertOrder(t, 1, 2, 1)
	assertOrder(t, -1, int64(-5), int64(5))

	// Unsigned integers
	assertOrder(t, 0, uint(42), uint(42))
	assertOrder(t, -1, uint(1), uint(2))
	assertOrder(t, 1, uint(10), uint(5))
	assertOrder(t, 0, uint64(1000), uint64(1000))

	// Mixed signed/unsigned comparison
	assertOrder(t, 0, int(42), uint(42))
	assertOrder(t, -1, int(1), uint(2))
	assertOrder(t, 1, uint(10), int(5))

	// Float comparisons
	assertOrder(t, -1, 1, 3.14)
	assertOrder(t, 1, 3.14, 1)
	assertOrder(t, -1, 1.12, 3.14)

	// Cross-strata
	assertOrder(t, -1, false, 1)
	assertOrder(t, -1, true, 1.2)
	assertOrder(t, 1, 1, false)
	assertOrder(t, 1, 1, true)

	// String
	assertOrder(t, -1, "a", "b")
	assertOrder(t, 1, "a", 1)
	assertOrder(t, -1, 1, "a")

	// Slice
	assertOrder(t, 1, []string{"a"}, []int{1})
	assertOrder(t, 1, []int{1}, 1)
	assertOrder(t, -1, 1, []int{1})
	assertOrder(t, 1, []any{"a", "b", "c"}, []any{"a", "b"})
	assertOrder(t, 1, []any{"a", "b", 1}, []any{"a", "b"})
	assertOrder(t, -1, []any{"a", "b", 1}, []any{"a", "b", "c"})

	// Special float values
	nan := math.NaN()
	posInf := math.Inf(1)
	negInf := math.Inf(-1)

	assertOrder(t, 0, []any{nan}, []any{nan})
	assertOrder(t, 1, []any{nan}, []any{float64(0)})
	assertOrder(t, -1, []any{float64(0)}, []any{nan})

	assertOrder(t, 0, nan, nan)
	assertOrder(t, 1, nan, posInf)
	assertOrder(t, 1, nan, negInf)
	assertOrder(t, 1, nan, float64(42))
	assertOrder(t, -1, float64(0), nan)
	assertOrder(t, -1, negInf, float64(0))
	assertOrder(t, 1, posInf, float64(0))
	assertOrder(t, -1, negInf, posInf)
	assertOrder(t, 0, posInf, posInf)
	assertOrder(t, 0, negInf, negInf)

	t.Run("invalid types return error", func(t *testing.T) {
		_, err := value.Order(struct{}{}, struct{}{})
		if err == nil {
			t.Error("expected error for struct comparison")
		}
		_, err = value.Order(struct{}{}, 1)
		if err == nil {
			t.Error("expected error for struct vs int comparison")
		}
		_, err = value.Order([]any{struct{}{}}, []any{struct{}{}})
		if err == nil {
			t.Error("expected error for slice containing struct")
		}
	})
}

func TestOrder_Invariants(t *testing.T) {
	// Symmetry on equality
	nan := math.NaN()
	equalVals := []any{
		nil,
		false,
		true,
		int64(1),
		uint64(1),
		float64(1),
		math.Inf(1),
		math.Inf(-1),
		nan,
		"a",
		[]any{"a"},
		[]*regexp.Regexp{regexp.MustCompile("x")},
	}
	for _, v := range equalVals {
		lr, err := value.Order(v, v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rl, err := value.Order(v, v)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, 0, lr)
		assertEqual(t, 0, rl)
	}

	// Antisymmetry
	pairs := []struct {
		a any
		b any
	}{
		{nil, false},
		{false, true},
		{int64(0), int64(1)},
		{uint64(0), uint64(1)},
		{float64(1.0), float64(2.0)},
		{"a", "b"},
		{[]any{"a"}, []any{"a", "b"}},
		{math.Inf(-1), float64(0)},
		{float64(0), math.Inf(1)},
		{math.Inf(1), nan},
	}
	for _, p := range pairs {
		lr, err := value.Order(p.a, p.b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rl, err := value.Order(p.b, p.a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, sign(lr), -sign(rl))
	}

	// Transitivity on simple chains
	chain := func(vals ...any) {
		for i := range vals[:len(vals)-1] {
			res, err := value.Order(vals[i], vals[i+1])
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEqual(t, -1, sign(res))
		}
	}
	chain(int64(0), int64(1), int64(2))
	chain(uint64(0), uint64(1), uint64(2))
	chain("a", "b", "c")
	chain([]any{"a"}, []any{"a", "b"}, []any{"a", "b", "c"})
	chain(math.Inf(-1), float64(-1), float64(0), math.Inf(1), nan)
}

func sign(n int) int {
	switch {
	case n == 0:
		return 0
	case n > 0:
		return 1
	default:
		return -1
	}
}

func TestLess(t *testing.T) {
	t.Run("matches Order", func(t *testing.T) {
		inputs := []struct {
			a    any
			b    any
			want bool
		}{
			{nil, false, true},
			{false, nil, false},
			{int64(1), int64(2), true},
			{uint64(1), uint64(2), true},
			{float64(3), float64(3), false},
			{"a", "b", true},
			{[]any{"a"}, []any{"a", "b"}, true},
		}
		for _, tc := range inputs {
			got, err := value.Less(tc.a, tc.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEqual(t, tc.want, got)

			ord, err := value.Order(tc.a, tc.b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			assertEqual(t, ord < 0, got)
		}
	})

	t.Run("errors propagate", func(t *testing.T) {
		_, err := value.Less(struct{}{}, 1)
		if err == nil {
			t.Error("expected error for struct comparison")
		}
	})

	t.Run("usable with sort helpers", func(t *testing.T) {
		values := []any{"b", "a", "c"}
		slices.SortFunc(values, func(a, b any) int {
			lessAB, err := value.Less(a, b)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lessAB {
				return -1
			}
			lessBA, err := value.Less(b, a)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if lessBA {
				return 1
			}
			return 0
		})
		expected := []any{"a", "b", "c"}
		for i := range values {
			if values[i] != expected[i] {
				t.Errorf("at index %d: expected %v, got %v", i, expected[i], values[i])
			}
		}
	})
}

func TestGetInt64(t *testing.T) {
	t.Run("signed integers", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected int64
		}{
			{"int", int(42), 42},
			{"int8", int8(42), 42},
			{"int16", int16(42), 42},
			{"int32", int32(42), 42},
			{"int64", int64(42), 42},
			{"int negative", int(-10), -10},
			{"int64 max", int64(math.MaxInt64), math.MaxInt64},
			{"int64 min", int64(math.MinInt64), math.MinInt64},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := value.GetInt64(tt.input)
				if !ok {
					t.Errorf("expected ok=true for %s", tt.name)
				}
				if got != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, got)
				}
			})
		}
	})

	t.Run("unsigned integers within range", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected int64
		}{
			{"uint", uint(42), 42},
			{"uint8", uint8(42), 42},
			{"uint16", uint16(42), 42},
			{"uint32", uint32(42), 42},
			{"uint64", uint64(42), 42},
			{"uintptr", uintptr(42), 42},
			{"uint64 at MaxInt64", uint64(math.MaxInt64), math.MaxInt64},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := value.GetInt64(tt.input)
				if !ok {
					t.Errorf("expected ok=true for %s", tt.name)
				}
				if got != tt.expected {
					t.Errorf("expected %d, got %d", tt.expected, got)
				}
			})
		}
	})

	t.Run("unsigned integers overflow", func(t *testing.T) {
		tests := []struct {
			name  string
			input any
		}{
			{"uint64 over MaxInt64", uint64(math.MaxInt64) + 1},
			{"uint64 MaxUint64", uint64(math.MaxUint64)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := value.GetInt64(tt.input)
				if ok {
					t.Errorf("expected ok=false for %s, got value %d", tt.name, got)
				}
				if got != 0 {
					t.Errorf("expected 0 on overflow, got %d", got)
				}
			})
		}
	})

	t.Run("non-integer types", func(t *testing.T) {
		tests := []struct {
			name  string
			input any
		}{
			{"float64", float64(42.0)},
			{"string", "42"},
			{"bool", true},
			{"nil", nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, ok := value.GetInt64(tt.input)
				if ok {
					t.Errorf("expected ok=false for %s", tt.name)
				}
			})
		}
	})
}

func TestGetFloat64(t *testing.T) {
	t.Run("float types", func(t *testing.T) {
		tests := []struct {
			name     string
			input    any
			expected float64
		}{
			{"float32", float32(3.14), float64(float32(3.14))},
			{"float64", float64(3.14), 3.14},
			{"float64 negative", float64(-2.5), -2.5},
			{"float64 inf", math.Inf(1), math.Inf(1)},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				got, ok := value.GetFloat64(tt.input)
				if !ok {
					t.Errorf("expected ok=true for %s", tt.name)
				}
				if got != tt.expected && !(math.IsNaN(got) && math.IsNaN(tt.expected)) {
					t.Errorf("expected %v, got %v", tt.expected, got)
				}
			})
		}
	})

	t.Run("non-float types", func(t *testing.T) {
		tests := []struct {
			name  string
			input any
		}{
			{"int", int(42)},
			{"uint", uint(42)},
			{"string", "42.0"},
			{"bool", true},
			{"nil", nil},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				_, ok := value.GetFloat64(tt.input)
				if ok {
					t.Errorf("expected ok=false for %s", tt.name)
				}
			})
		}
	})
}

func TestInt64Compare(t *testing.T) {
	tests := []struct {
		left, right int64
		want        int
	}{
		{0, 0, 0},
		{1, 0, 1},
		{0, 1, -1},
		{-1, 1, -1},
		{1, -1, 1},
		{math.MaxInt64, math.MaxInt64, 0},
		{math.MinInt64, math.MinInt64, 0},
		{math.MinInt64, math.MaxInt64, -1},
	}
	for _, tt := range tests {
		got := value.Int64Compare(tt.left, tt.right)
		if got != tt.want {
			t.Errorf("Int64Compare(%d, %d) = %d, want %d", tt.left, tt.right, got, tt.want)
		}
	}
}

func FuzzOrder_Ints(f *testing.F) {
	f.Add(int64(0), int64(1))
	f.Add(int64(-5), int64(-5))
	f.Add(int64(math.MaxInt64), int64(math.MinInt64))
	f.Fuzz(func(t *testing.T, a int64, b int64) {
		cmp, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rcmp, err := value.Order(b, a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, sign(cmp), -sign(rcmp))
	})
}

func FuzzOrder_Strings(f *testing.F) {
	f.Add("a", "b")
	f.Add("same", "same")
	f.Add("", "")
	f.Fuzz(func(t *testing.T, a string, b string) {
		cmp, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rcmp, err := value.Order(b, a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, sign(cmp), -sign(rcmp))
	})
}

func TestOrder_RandomSupportedPairs(t *testing.T) {
	r := rand.New(rand.NewSource(123)) //nolint:gosec // deterministic pseudo-randomness is fine in tests
	vals := []any{
		nil, false, true,
		int64(0), int64(1), int64(-1),
		uint64(0), uint64(1), uint64(100),
		float64(1.5), float64(-0.5),
		"a", "b", "",
		[]any{"a"},
		[]any{int64(1), int64(2)},
	}

	for range 32 {
		a := vals[r.Intn(len(vals))]
		b := vals[r.Intn(len(vals))]
		cmp, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		rcmp, err := value.Order(b, a)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertEqual(t, sign(cmp), -sign(rcmp))
	}
}

// TestOrder_FloatIntTransitivity verifies that Order maintains transitivity
// when comparing floats with large integers (> 2^53). This is a regression test for
// the precision loss bug where float64(large_int) loses precision.
func TestOrder_FloatIntTransitivity(t *testing.T) {
	// The exact values from the review document that demonstrated the transitivity violation.
	// Prior to the fix: a==b (wrong), b==c, a>c → not transitive
	// After the fix: a>b, b==c, a>c → transitive
	t.Run("uint64 transitivity around 2^53", func(t *testing.T) {
		a := uint64(9007199254740993)  // 2^53 + 1
		b := float64(9007199254740992) // 2^53
		c := uint64(9007199254740992)  // 2^53

		ab, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("Order(a, b) error: %v", err)
		}
		bc, err := value.Order(b, c)
		if err != nil {
			t.Fatalf("Order(b, c) error: %v", err)
		}
		ac, err := value.Order(a, c)
		if err != nil {
			t.Fatalf("Order(a, c) error: %v", err)
		}

		// a should be greater than b (not equal!)
		if ab != 1 {
			t.Errorf("Order(uint64(2^53+1), float64(2^53)) = %d, want 1 (greater)", ab)
		}
		// b should equal c
		if bc != 0 {
			t.Errorf("Order(float64(2^53), uint64(2^53)) = %d, want 0 (equal)", bc)
		}
		// a should be greater than c (transitivity: a>b, b==c → a>c)
		if ac != 1 {
			t.Errorf("Order(uint64(2^53+1), uint64(2^53)) = %d, want 1 (greater)", ac)
		}
	})

	t.Run("int64 transitivity around 2^53", func(t *testing.T) {
		a := int64(9007199254740993)   // 2^53 + 1
		b := float64(9007199254740992) // 2^53
		c := int64(9007199254740992)   // 2^53

		ab, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("Order(a, b) error: %v", err)
		}
		bc, err := value.Order(b, c)
		if err != nil {
			t.Fatalf("Order(b, c) error: %v", err)
		}
		ac, err := value.Order(a, c)
		if err != nil {
			t.Fatalf("Order(a, c) error: %v", err)
		}

		if ab != 1 {
			t.Errorf("Order(int64(2^53+1), float64(2^53)) = %d, want 1", ab)
		}
		if bc != 0 {
			t.Errorf("Order(float64(2^53), int64(2^53)) = %d, want 0", bc)
		}
		if ac != 1 {
			t.Errorf("Order(int64(2^53+1), int64(2^53)) = %d, want 1", ac)
		}
	})

	t.Run("negative int64 transitivity", func(t *testing.T) {
		// Test negative values around -2^53
		// Note: float64(-9007199254740993) rounds to -9007199254740992.0
		a := int64(-9007199254740992)   // -2^53
		b := float64(-9007199254740992) // -2^53 (exact)
		c := int64(-9007199254740993)   // -(2^53 + 1)

		ab, err := value.Order(a, b)
		if err != nil {
			t.Fatalf("Order(a, b) error: %v", err)
		}
		bc, err := value.Order(b, c)
		if err != nil {
			t.Fatalf("Order(b, c) error: %v", err)
		}
		ac, err := value.Order(a, c)
		if err != nil {
			t.Fatalf("Order(a, c) error: %v", err)
		}

		// a == b (both are -2^53)
		if ab != 0 {
			t.Errorf("Order(-2^53, float64(-2^53)) = %d, want 0", ab)
		}
		// b > c (b is -2^53, c is -(2^53+1), so b > c)
		if bc != 1 {
			t.Errorf("Order(float64(-2^53), -(2^53+1)) = %d, want 1", bc)
		}
		// a > c (transitivity: a==b, b>c → a>c)
		if ac != 1 {
			t.Errorf("Order(-2^53, -(2^53+1)) = %d, want 1", ac)
		}
	})
}

// TestCompareInt64Float64 tests the exact int64 vs float64 comparison function.
func TestCompareInt64Float64(t *testing.T) {
	tests := []struct {
		name string
		i    int64
		f    float64
		want int
	}{
		// Basic cases
		{"equal zero", 0, 0.0, 0},
		{"equal positive", 42, 42.0, 0},
		{"equal negative", -42, -42.0, 0},
		{"int less than float", 41, 42.0, -1},
		{"int greater than float", 43, 42.0, 1},

		// Fractional floats
		{"int less than float with fraction", 42, 42.5, -1},
		{"int greater than float with fraction", 43, 42.5, 1},
		{"int equal to truncated negative fraction", -42, -42.5, 1}, // -42 > -42.5

		// Large integers (> 2^53)
		{"2^53 equal", 9007199254740992, 9007199254740992.0, 0},
		{"2^53+1 vs 2^53", 9007199254740993, 9007199254740992.0, 1},       // Critical case!
		{"2^53 vs 2^53+1 float", 9007199254740992, 9007199254740993.0, 0}, // 2^53+1 rounds to 2^53

		// Special float values
		{"int vs +Inf", 0, math.Inf(1), -1},
		{"int vs -Inf", 0, math.Inf(-1), 1},
		{"int vs NaN", 0, math.NaN(), -1},
		{"MaxInt64 vs +Inf", math.MaxInt64, math.Inf(1), -1},
		{"MinInt64 vs -Inf", math.MinInt64, math.Inf(-1), 1},

		// Boundary values
		{"MaxInt64 vs float just above", math.MaxInt64, float64(1 << 63), -1}, // 2^63 > MaxInt64
		{"MinInt64 vs float below", math.MinInt64, -float64(1<<63) * 1.5, 1},  // -1.5*2^63 < MinInt64
		{"MinInt64 vs float equal", math.MinInt64, -float64(1 << 63), 0},      // -2^63 == MinInt64
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := value.CompareInt64Float64(tt.i, tt.f)
			if got != tt.want {
				t.Errorf("CompareInt64Float64(%d, %v) = %d, want %d", tt.i, tt.f, got, tt.want)
			}
		})
	}
}

// TestCompareUint64Float64 tests the exact uint64 vs float64 comparison function.
func TestCompareUint64Float64(t *testing.T) {
	tests := []struct {
		name string
		u    uint64
		f    float64
		want int
	}{
		// Basic cases
		{"equal zero", 0, 0.0, 0},
		{"equal positive", 42, 42.0, 0},
		{"uint less than float", 41, 42.0, -1},
		{"uint greater than float", 43, 42.0, 1},

		// Negative floats (uint is always >= 0)
		{"uint vs negative float", 0, -1.0, 1},
		{"uint vs negative float 2", 100, -100.0, 1},

		// Fractional floats
		{"uint less than float with fraction", 42, 42.5, -1},
		{"uint greater than float with fraction", 43, 42.5, 1},

		// Large integers (> 2^53)
		{"2^53 equal", 9007199254740992, 9007199254740992.0, 0},
		{"2^53+1 vs 2^53", 9007199254740993, 9007199254740992.0, 1}, // Critical case!

		// Special float values
		{"uint vs +Inf", 0, math.Inf(1), -1},
		{"uint vs -Inf", 0, math.Inf(-1), 1},
		{"uint vs NaN", 0, math.NaN(), -1},
		{"MaxUint64 vs +Inf", math.MaxUint64, math.Inf(1), -1},

		// Boundary values
		{"MaxUint64 vs 2^64", math.MaxUint64, float64(1<<63) * 2, -1}, // 2^64 > MaxUint64
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := value.CompareUint64Float64(tt.u, tt.f)
			if got != tt.want {
				t.Errorf("CompareUint64Float64(%d, %v) = %d, want %d", tt.u, tt.f, got, tt.want)
			}
		})
	}
}

// TestOrder_StringComparableEdgeCases tests edge cases in toStringComparable
// through the Order function (toStringComparable is unexported).
func TestOrder_StringComparableEdgeCases(t *testing.T) {
	// Test nil regexp - should return error
	t.Run("nil regexp vs string returns error", func(t *testing.T) {
		var nilRegexp *regexp.Regexp = nil
		_, err := value.Order(nilRegexp, "test")
		if err == nil {
			t.Error("expected error for nil regexp comparison")
		}
	})

	t.Run("string vs nil regexp returns error", func(t *testing.T) {
		var nilRegexp *regexp.Regexp = nil
		_, err := value.Order("test", nilRegexp)
		if err == nil {
			t.Error("expected error for nil regexp comparison")
		}
	})

	t.Run("nil regexp vs nil regexp returns error", func(t *testing.T) {
		var nilRegexp1 *regexp.Regexp = nil
		var nilRegexp2 *regexp.Regexp = nil
		_, err := value.Order(nilRegexp1, nilRegexp2)
		if err == nil {
			t.Error("expected error for nil regexp comparison")
		}
	})

	// Test valid regexp comparisons
	t.Run("regexp vs regexp equal", func(t *testing.T) {
		r1 := regexp.MustCompile("abc")
		r2 := regexp.MustCompile("abc")
		cmp, err := value.Order(r1, r2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmp != 0 {
			t.Errorf("expected 0 (equal), got %d", cmp)
		}
	})

	t.Run("regexp vs regexp less", func(t *testing.T) {
		r1 := regexp.MustCompile("abc")
		r2 := regexp.MustCompile("def")
		cmp, err := value.Order(r1, r2)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmp != -1 {
			t.Errorf("expected -1 (less), got %d", cmp)
		}
	})

	t.Run("regexp vs string", func(t *testing.T) {
		r := regexp.MustCompile("abc")
		cmp, err := value.Order(r, "abc")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmp != 0 {
			t.Errorf("expected 0 (equal), got %d", cmp)
		}
	})

	t.Run("string vs regexp", func(t *testing.T) {
		r := regexp.MustCompile("def")
		cmp, err := value.Order("abc", r)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cmp != -1 {
			t.Errorf("expected -1 (less), got %d", cmp)
		}
	})
}

// TestGetInt64_UintptrOverflow tests uintptr overflow handling in GetInt64.
func TestGetInt64_UintptrOverflow(t *testing.T) {
	// On 64-bit systems, uintptr can hold values > MaxInt64
	// We can't directly test this with a literal since it would overflow on 32-bit
	// But we can test with a uint64 value that's converted to uintptr
	if uint64(^uintptr(0)) > uint64(math.MaxInt64) {
		// 64-bit system - test uintptr overflow
		largeUintptr := uintptr(uint64(math.MaxInt64) + 1)
		got, ok := value.GetInt64(largeUintptr)
		if ok {
			t.Errorf("expected ok=false for uintptr overflow, got value %d", got)
		}
		if got != 0 {
			t.Errorf("expected 0 on overflow, got %d", got)
		}
	} else {
		t.Skip("32-bit system - uintptr cannot overflow int64")
	}
}

// TestGetInt64_UintOverflow tests uint overflow handling in GetInt64.
func TestGetInt64_UintOverflow(t *testing.T) {
	// On 64-bit systems, uint can hold values > MaxInt64
	if uint64(^uint(0)) > uint64(math.MaxInt64) {
		// 64-bit system - test uint overflow
		largeUint := uint(uint64(math.MaxInt64) + 1)
		got, ok := value.GetInt64(largeUint)
		if ok {
			t.Errorf("expected ok=false for uint overflow, got value %d", got)
		}
		if got != 0 {
			t.Errorf("expected 0 on overflow, got %d", got)
		}
	} else {
		t.Skip("32-bit system - uint cannot overflow int64")
	}
}

// TestCompareInt64Float64_AdditionalEdgeCases tests more edge cases for int64/float64 comparison.
func TestCompareInt64Float64_AdditionalEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		i    int64
		f    float64
		want int
	}{
		// Float is exactly a whole number at the boundary
		{"MaxInt64 exact", math.MaxInt64, float64(math.MaxInt64), -1}, // float64(MaxInt64) rounds up to 2^63
		{"MinInt64 exact", math.MinInt64, -float64(1 << 63), 0},       // -2^63 == MinInt64

		// Negative fractional floats
		{"negative int vs negative fractional float", -43, -42.5, -1}, // -43 < -42.5
		{"negative int equal truncated", -42, -42.0, 0},

		// Float that truncates to int64 boundary
		{"int less than float trunc", 42, 43.9, -1},
		{"int greater than float trunc", 44, 43.9, 1},

		// Very large negative float
		{"MinInt64 vs very negative float", math.MinInt64, -float64(1<<63) * 2, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := value.CompareInt64Float64(tt.i, tt.f)
			if got != tt.want {
				t.Errorf("CompareInt64Float64(%d, %v) = %d, want %d", tt.i, tt.f, got, tt.want)
			}
		})
	}
}

// TestCompareUint64Float64_AdditionalEdgeCases tests more edge cases for uint64/float64 comparison.
func TestCompareUint64Float64_AdditionalEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		u    uint64
		f    float64
		want int
	}{
		// Float is exactly a whole number at the boundary
		// Note: float64(MaxUint64) rounds UP past MaxUint64, so MaxUint64 < float64(MaxUint64)
		{"MaxUint64 vs float representation", math.MaxUint64, float64(math.MaxUint64), -1},

		// Float that truncates to boundary
		{"uint equal to truncated fractional", 42, 42.9, -1}, // 42 < 42.9
		{"uint greater than truncated fractional", 43, 42.9, 1},

		// Very small fractional positive float
		{"uint vs tiny positive float", 0, 0.1, -1},
		{"uint vs zero", 0, 0.0, 0},

		// Large fractional float
		{"uint vs large fractional", 1000000, 1000000.5, -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := value.CompareUint64Float64(tt.u, tt.f)
			if got != tt.want {
				t.Errorf("CompareUint64Float64(%d, %v) = %d, want %d", tt.u, tt.f, got, tt.want)
			}
		})
	}
}

// TestOrder_MixedFloatInt tests Order with mixed float/int comparisons.
func TestOrder_MixedFloatInt(t *testing.T) {
	tests := []struct {
		name  string
		left  any
		right any
		want  int
	}{
		// float64 vs int64
		{"float vs int equal", float64(42.0), int64(42), 0},
		{"float vs int greater", float64(43.0), int64(42), 1},
		{"float vs int less", float64(41.0), int64(42), -1},
		{"float with frac vs int", float64(42.5), int64(42), 1},

		// int64 vs float64
		{"int vs float equal", int64(42), float64(42.0), 0},
		{"int vs float greater", int64(43), float64(42.0), 1},
		{"int vs float less", int64(41), float64(42.0), -1},

		// float64 vs uint64
		{"float vs uint equal", float64(42.0), uint64(42), 0},
		{"float vs uint greater", float64(43.0), uint64(42), 1},
		{"float vs uint less", float64(41.0), uint64(42), -1},

		// uint64 vs float64
		{"uint vs float equal", uint64(42), float64(42.0), 0},
		{"uint vs float greater", uint64(43), float64(42.0), 1},
		{"uint vs float less", uint64(41), float64(42.0), -1},

		// Large values around 2^53 (the critical precision boundary)
		{"large uint vs float equal", uint64(9007199254740992), float64(9007199254740992), 0},
		{"large uint > float (2^53+1 vs 2^53)", uint64(9007199254740993), float64(9007199254740992), 1},
		{"large int > float (2^53+1 vs 2^53)", int64(9007199254740993), float64(9007199254740992), 1},

		// Special float values
		{"int vs +Inf", int64(0), math.Inf(1), -1},
		{"int vs -Inf", int64(0), math.Inf(-1), 1},
		{"int vs NaN", int64(0), math.NaN(), -1},
		{"+Inf vs int", math.Inf(1), int64(0), 1},
		{"-Inf vs int", math.Inf(-1), int64(0), -1},
		{"NaN vs int", math.NaN(), int64(0), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := value.Order(tt.left, tt.right)
			if err != nil {
				t.Fatalf("Order(%v, %v) error: %v", tt.left, tt.right, err)
			}
			if got != tt.want {
				t.Errorf("Order(%v, %v) = %d, want %d", tt.left, tt.right, got, tt.want)
			}
		})
	}
}

// Property-based tests using testing/quick for numeric coercion
// These verify that Order maintains the required properties across
// randomly generated inputs.

// TestOrder_Antisymmetry_Int64_Quick verifies that for all int64 pairs,
// Order(a, b) == -Order(b, a).
func TestOrder_Antisymmetry_Int64_Quick(t *testing.T) {
	f := func(a, b int64) bool {
		cmpAB, errAB := value.Order(a, b)
		cmpBA, errBA := value.Order(b, a)
		if errAB != nil || errBA != nil {
			return false
		}
		return sign(cmpAB) == -sign(cmpBA)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Antisymmetry_Uint64_Quick verifies that for all uint64 pairs,
// Order(a, b) == -Order(b, a).
func TestOrder_Antisymmetry_Uint64_Quick(t *testing.T) {
	f := func(a, b uint64) bool {
		cmpAB, errAB := value.Order(a, b)
		cmpBA, errBA := value.Order(b, a)
		if errAB != nil || errBA != nil {
			return false
		}
		return sign(cmpAB) == -sign(cmpBA)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Antisymmetry_Float64_Quick verifies that for all float64 pairs,
// Order(a, b) == -Order(b, a), including NaN handling.
func TestOrder_Antisymmetry_Float64_Quick(t *testing.T) {
	f := func(a, b float64) bool {
		cmpAB, errAB := value.Order(a, b)
		cmpBA, errBA := value.Order(b, a)
		if errAB != nil || errBA != nil {
			return false
		}
		return sign(cmpAB) == -sign(cmpBA)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Antisymmetry_MixedInt64Float64_Quick verifies antisymmetry
// when comparing int64 and float64 values.
func TestOrder_Antisymmetry_MixedInt64Float64_Quick(t *testing.T) {
	f := func(i int64, f float64) bool {
		cmpIF, errIF := value.Order(i, f)
		cmpFI, errFI := value.Order(f, i)
		if errIF != nil || errFI != nil {
			return false
		}
		return sign(cmpIF) == -sign(cmpFI)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Antisymmetry_MixedUint64Float64_Quick verifies antisymmetry
// when comparing uint64 and float64 values.
func TestOrder_Antisymmetry_MixedUint64Float64_Quick(t *testing.T) {
	f := func(u uint64, f float64) bool {
		cmpUF, errUF := value.Order(u, f)
		cmpFU, errFU := value.Order(f, u)
		if errUF != nil || errFU != nil {
			return false
		}
		return sign(cmpUF) == -sign(cmpFU)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Antisymmetry_MixedInt64Uint64_Quick verifies antisymmetry
// when comparing int64 and uint64 values.
func TestOrder_Antisymmetry_MixedInt64Uint64_Quick(t *testing.T) {
	f := func(i int64, u uint64) bool {
		cmpIU, errIU := value.Order(i, u)
		cmpUI, errUI := value.Order(u, i)
		if errIU != nil || errUI != nil {
			return false
		}
		return sign(cmpIU) == -sign(cmpUI)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Transitivity_Int64_Quick verifies transitivity: if a < b and b < c, then a < c.
func TestOrder_Transitivity_Int64_Quick(t *testing.T) {
	f := func(a, b, c int64) bool {
		cmpAB, errAB := value.Order(a, b)
		cmpBC, errBC := value.Order(b, c)
		cmpAC, errAC := value.Order(a, c)
		if errAB != nil || errBC != nil || errAC != nil {
			return false
		}
		// If a < b and b < c, then a < c
		if cmpAB < 0 && cmpBC < 0 {
			return cmpAC < 0
		}
		// If a > b and b > c, then a > c
		if cmpAB > 0 && cmpBC > 0 {
			return cmpAC > 0
		}
		// If a == b, then cmp(a, c) == cmp(b, c)
		if cmpAB == 0 {
			return cmpAC == cmpBC
		}
		// If b == c, then cmp(a, b) == cmp(a, c)
		if cmpBC == 0 {
			return cmpAB == cmpAC
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Transitivity_Float64_Quick verifies transitivity for float64,
// including special handling for NaN.
func TestOrder_Transitivity_Float64_Quick(t *testing.T) {
	f := func(a, b, c float64) bool {
		cmpAB, errAB := value.Order(a, b)
		cmpBC, errBC := value.Order(b, c)
		cmpAC, errAC := value.Order(a, c)
		if errAB != nil || errBC != nil || errAC != nil {
			return false
		}
		// If a < b and b < c, then a < c
		if cmpAB < 0 && cmpBC < 0 {
			return cmpAC < 0
		}
		// If a > b and b > c, then a > c
		if cmpAB > 0 && cmpBC > 0 {
			return cmpAC > 0
		}
		// If a == b, then cmp(a, c) == cmp(b, c)
		if cmpAB == 0 {
			return cmpAC == cmpBC
		}
		// If b == c, then cmp(a, b) == cmp(a, c)
		if cmpBC == 0 {
			return cmpAB == cmpAC
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Transitivity_MixedNumeric_Quick verifies transitivity across
// mixed numeric types (int64, uint64, float64).
func TestOrder_Transitivity_MixedNumeric_Quick(t *testing.T) {
	f := func(i int64, u uint64, flt float64) bool {
		// Test transitivity with i, u, flt in different roles
		// If i < u and u < flt, then i < flt
		cmpIU, errIU := value.Order(i, u)
		cmpUF, errUF := value.Order(u, flt)
		cmpIF, errIF := value.Order(i, flt)
		if errIU != nil || errUF != nil || errIF != nil {
			return false
		}

		if cmpIU < 0 && cmpUF < 0 {
			if cmpIF >= 0 {
				return false
			}
		}
		if cmpIU > 0 && cmpUF > 0 {
			if cmpIF <= 0 {
				return false
			}
		}
		if cmpIU == 0 && cmpUF != 0 {
			if cmpIF != cmpUF {
				return false
			}
		}
		if cmpUF == 0 && cmpIU != 0 {
			if cmpIF != cmpIU {
				return false
			}
		}
		return true
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestOrder_Reflexivity_Quick verifies that Order(x, x) == 0 for all values.
func TestOrder_Reflexivity_Quick(t *testing.T) {
	t.Run("int64", func(t *testing.T) {
		f := func(x int64) bool {
			cmp, err := value.Order(x, x)
			return err == nil && cmp == 0
		}
		if err := quick.Check(f, nil); err != nil {
			t.Error(err)
		}
	})

	t.Run("uint64", func(t *testing.T) {
		f := func(x uint64) bool {
			cmp, err := value.Order(x, x)
			return err == nil && cmp == 0
		}
		if err := quick.Check(f, nil); err != nil {
			t.Error(err)
		}
	})

	t.Run("float64", func(t *testing.T) {
		f := func(x float64) bool {
			cmp, err := value.Order(x, x)
			// NaN == NaN in our ordering
			return err == nil && cmp == 0
		}
		if err := quick.Check(f, nil); err != nil {
			t.Error(err)
		}
	})

	t.Run("string", func(t *testing.T) {
		f := func(x string) bool {
			cmp, err := value.Order(x, x)
			return err == nil && cmp == 0
		}
		if err := quick.Check(f, nil); err != nil {
			t.Error(err)
		}
	})
}

// TestCompareInt64Float64_Quick tests CompareInt64Float64 properties.
func TestCompareInt64Float64_Quick(t *testing.T) {
	// Antisymmetry: CompareInt64Float64(i, f) == -CompareFloat64Int64(f, i)
	// Note: We don't have CompareFloat64Int64, but we can verify through Order
	f := func(i int64, flt float64) bool {
		cmp1 := value.CompareInt64Float64(i, flt)
		// Compare through Order which should be consistent
		vo, err := value.Order(i, flt)
		if err != nil {
			return false
		}
		return sign(cmp1) == sign(vo)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

// TestCompareUint64Float64_Quick tests CompareUint64Float64 properties.
func TestCompareUint64Float64_Quick(t *testing.T) {
	f := func(u uint64, flt float64) bool {
		cmp1 := value.CompareUint64Float64(u, flt)
		// Compare through Order which should be consistent
		vo, err := value.Order(u, flt)
		if err != nil {
			return false
		}
		return sign(cmp1) == sign(vo)
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestIsWholeNumber(t *testing.T) {
	tests := []struct {
		name   string
		input  float64
		expect bool
	}{
		// Whole numbers
		{"zero", 0.0, true},
		{"positive_one", 1.0, true},
		{"negative_one", -1.0, true},
		{"large_positive", 1000000.0, true},
		{"large_negative", -1000000.0, true},
		{"max_safe_integer", 9007199254740992.0, true},          // 2^53
		{"negative_max_safe", -9007199254740992.0, true},        // -2^53
		{"near_max_int64", float64(math.MaxInt64 - 1024), true}, // Close to max (loses precision)
		{"near_min_int64", float64(math.MinInt64), true},        // Exactly -2^63
		{"at_max_boundary", float64(1 << 62), true},             // Large but safely within range
		{"negative_at_boundary", -float64(1 << 63), true},       // Exactly -2^63
		// Note: float64(1<<63 - 1) rounds UP to 2^63, which is out of int64 range

		// Non-whole numbers
		{"fractional_half", 0.5, false},
		{"fractional_pi", 3.14159, false},
		{"fractional_negative", -2.5, false},
		{"fractional_small", 0.0001, false},

		// Non-finite values
		{"positive_infinity", math.Inf(1), false},
		{"negative_infinity", math.Inf(-1), false},
		{"nan", math.NaN(), false},

		// Out of int64 range
		{"too_large", float64(1 << 63), false}, // 2^63 is too large for int64
		{"too_large_positive", 1e20, false},    // Way too large
		{"too_large_negative", -1e20, false},   // Way too negative (but still in range!)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := value.IsWholeNumber(tt.input)
			if result != tt.expect {
				t.Errorf("IsWholeNumber(%v) = %v, want %v", tt.input, result, tt.expect)
			}
		})
	}
}

func TestGetInt64FromFloat(t *testing.T) {
	tests := []struct {
		name    string
		input   float64
		wantVal int64
		wantOK  bool
	}{
		// Valid whole numbers
		{"zero", 0.0, 0, true},
		{"positive_one", 1.0, 1, true},
		{"negative_one", -1.0, -1, true},
		{"forty_two", 42.0, 42, true},
		{"negative_forty_two", -42.0, -42, true},
		{"large_positive", 1000000.0, 1000000, true},
		{"large_negative", -1000000.0, -1000000, true},
		{"max_safe_integer", 9007199254740992.0, 9007199254740992, true}, // 2^53

		// Invalid: fractional
		{"fractional_half", 0.5, 0, false},
		{"fractional_pi", 3.14, 0, false},
		{"fractional_negative", -2.5, 0, false},

		// Invalid: non-finite
		{"positive_infinity", math.Inf(1), 0, false},
		{"negative_infinity", math.Inf(-1), 0, false},
		{"nan", math.NaN(), 0, false},

		// Invalid: out of range
		{"too_large", float64(1 << 63), 0, false}, // 2^63 is too large
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotVal, gotOK := value.GetInt64FromFloat(tt.input)
			if gotOK != tt.wantOK {
				t.Errorf("GetInt64FromFloat(%v) ok = %v, want %v", tt.input, gotOK, tt.wantOK)
			}
			if gotVal != tt.wantVal {
				t.Errorf("GetInt64FromFloat(%v) = %d, want %d", tt.input, gotVal, tt.wantVal)
			}
		})
	}
}

func TestGetInt64FromFloat_Quick(t *testing.T) {
	// Property: if GetInt64FromFloat returns (v, true), then float64(v) == f
	f := func(i int64) bool {
		// Use int64 as source to ensure we test valid whole numbers
		flt := float64(i)
		v, ok := value.GetInt64FromFloat(flt)
		if !ok {
			// If conversion failed, that's ok - might be precision loss
			return true
		}
		// The extracted int64 should equal the original (within float64 precision)
		return float64(v) == flt
	}
	if err := quick.Check(f, nil); err != nil {
		t.Error(err)
	}
}

func TestIsWholeNumber_BoundaryConditions(t *testing.T) {
	// Test specific boundary values around int64 limits

	// float64(1<<63) is exactly 2^63, which is > MaxInt64 (2^63 - 1)
	t.Run("at_2_63_is_false", func(t *testing.T) {
		f := float64(1 << 63)
		if value.IsWholeNumber(f) {
			t.Errorf("IsWholeNumber(2^63) should be false, 2^63 exceeds MaxInt64")
		}
	})

	// float64(-1<<63) is exactly -2^63, which equals MinInt64
	t.Run("at_negative_2_63_is_true", func(t *testing.T) {
		f := -float64(1 << 63)
		if !value.IsWholeNumber(f) {
			t.Errorf("IsWholeNumber(-2^63) should be true, -2^63 equals MinInt64")
		}
	})

	// Verify the extracted value is correct
	t.Run("extract_negative_2_63", func(t *testing.T) {
		f := -float64(1 << 63)
		v, ok := value.GetInt64FromFloat(f)
		if !ok {
			t.Fatalf("GetInt64FromFloat(-2^63) should succeed")
		}
		if v != math.MinInt64 {
			t.Errorf("GetInt64FromFloat(-2^63) = %d, want %d", v, math.MinInt64)
		}
	})
}

// A time.Time is a struct, so it lands in no strata and every comparison over
// it errors. Timestamp is the DSL's only two-representation kind: the same
// property holds a comparable string or an incomparable time.Time depending on
// what the caller submitted.
func TestTypeStrata_TimeIsInvalid(t *testing.T) {
	ts := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	if got := value.TypeStrata(ts); got != value.InvalidStrata {
		t.Errorf("TypeStrata(time.Time) = %d, want InvalidStrata (%d)", got, value.InvalidStrata)
	}
	if got := value.TypeStrata(ts.Format(time.RFC3339)); got != value.StringStrata {
		t.Errorf("TypeStrata(RFC 3339 string) = %d, want StringStrata (%d)", got, value.StringStrata)
	}
}

func TestOrder_TimeErrorsWhileItsStringFormDoesNot(t *testing.T) {
	early := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	late := time.Date(2021, 1, 2, 3, 4, 5, 0, time.UTC)

	if _, err := value.Order(early, late); err == nil {
		t.Error("Order(time.Time, time.Time) returned no error")
	}
	if _, err := value.Order(early, late.Format(time.RFC3339)); err == nil {
		t.Error("Order(time.Time, string) returned no error")
	}

	got, err := value.Order(early.Format(time.RFC3339), late.Format(time.RFC3339))
	if err != nil {
		t.Fatalf("Order over two RFC 3339 strings: %v", err)
	}
	if got != -1 {
		t.Errorf("Order(earlier, later) = %d, want -1", got)
	}
}
