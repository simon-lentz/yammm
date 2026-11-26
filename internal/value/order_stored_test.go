package value_test

import (
	"bytes"
	"cmp"
	"encoding/json"
	"math"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
)

// TestOrderStored_RanksAMapAboveEveryOtherValueAndOrdersTwoByContent holds the
// map arm: a document can store a JSON object at any property, so the order
// a graph sorts its records by ranks one.
func TestOrderStored_RanksAMapAboveEveryOtherValueAndOrdersTwoByContent(t *testing.T) {
	t.Parallel()
	m := immutable.WrapProperties
	ordered := []struct {
		name   string
		lo, hi any
	}{
		{"a list below a map", []any{"z"}, m(map[string]any{"a": int64(1)})},
		{"nil below a map", nil, m(map[string]any{})},
		{"a nil map below an empty map", immutable.WrapProperties(nil), m(map[string]any{})},
		{"names first", m(map[string]any{"a": int64(9)}), m(map[string]any{"b": int64(1)})},
		{"then values, by value", m(map[string]any{"a": int64(2)}), m(map[string]any{"a": float64(10)})},
		{"a prefix first", m(map[string]any{"a": int64(1)}), m(map[string]any{"a": int64(1), "b": int64(1)})},
		{"a map inside a list", []any{m(map[string]any{"a": int64(1)})}, []any{m(map[string]any{"a": int64(2)})}},
		{"a map inside a map", m(map[string]any{"a": map[string]any{"b": "x"}}), m(map[string]any{"a": map[string]any{"b": "y"}})},
	}
	for _, o := range ordered {
		if c, err := value.OrderStored(o.lo, o.hi); err != nil || c >= 0 {
			t.Errorf("%s: OrderStored(lo, hi) = %d, %v; want negative", o.name, c, err)
		}
		if c, err := value.OrderStored(o.hi, o.lo); err != nil || c <= 0 {
			t.Errorf("%s: OrderStored(hi, lo) = %d, %v; want positive", o.name, c, err)
		}
	}
	equal := m(map[string]any{"a": int64(1), "b": []any{"x"}})
	if c, err := value.OrderStored(equal, m(map[string]any{"b": []any{"x"}, "a": int64(1)})); err != nil || c != 0 {
		t.Errorf("two equal maps built apart: OrderStored = %d, %v; want 0", c, err)
	}
	if c, err := value.OrderStored(immutable.WrapProperties(nil), nil); err != nil || c != 0 {
		t.Errorf("a nil map against nil: OrderStored = %d, %v; want 0", c, err)
	}
	for name, pair := range map[string][2]any{
		"a map against a channel": {m(map[string]any{}), make(chan int)},
		"a channel against a map": {make(chan int), m(map[string]any{})},
		"a channel inside a map":  {m(map[string]any{"a": make(chan int)}), m(map[string]any{"a": make(chan int)})},
	} {
		if _, err := value.OrderStored(pair[0], pair[1]); err == nil {
			t.Errorf("%s: OrderStored ranked a value of no order", name)
		}
	}
}

// TestOrderStored_SeparatesNumbersASnapshotWritesApart holds the order to what
// a snapshot writes: two numbers of one value tie only when they are written
// alike, while the expression order still compares them by value alone.
func TestOrderStored_SeparatesNumbersASnapshotWritesApart(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	for name, pair := range map[string][2]any{
		"-0.0 before 0.0":                      {negZero, 0.0},
		"-0.0 before the integer 0":            {negZero, int64(0)},
		"an integer before a float":            {int64(1), 1.0},
		"an unsigned integer before a float":   {uint64(1), 1.0},
		"an integer before a literal 1.0":      {int64(1), json.Number("1.0")},
		"a literal -0 before the integer 0":    {json.Number("-0"), int64(0)},
		"the integer 0 before a literal 0e400": {int64(0), json.Number("0e400")},
		"two literals by their text":           {json.Number("10e399"), json.Number("1e400")},
	} {
		if c, err := value.OrderStored(pair[0], pair[1]); err != nil || c >= 0 {
			t.Errorf("%s: OrderStored(lo, hi) = %d, %v; want negative", name, c, err)
		}
		if c, err := value.OrderStored(pair[1], pair[0]); err != nil || c <= 0 {
			t.Errorf("%s: OrderStored(hi, lo) = %d, %v; want positive", name, c, err)
		}
		if c, err := value.Order(pair[0], pair[1]); err != nil || c != 0 {
			t.Errorf("%s: Order = %d, %v; want 0, the values being equal", name, c, err)
		}
	}
	for name, pair := range map[string][2]any{
		"an int64 and a literal written 1":    {int64(1), json.Number("1")},
		"a float and a literal written 1.0":   {1.0, json.Number("1.0")},
		"a float and a literal written 1.5":   {1.5, json.Number("1.5")},
		"an int8 and a uint64 both written 7": {int8(7), uint64(7)},
	} {
		if c, err := value.OrderStored(pair[0], pair[1]); err != nil || c != 0 {
			t.Errorf("%s: OrderStored = %d, %v; want 0", name, c, err)
		}
	}
}

// TestOrderStored_RanksANumberByTheExactValueOfItsText holds the order to the
// text a snapshot writes, not to the float64 nearest it: a literal no float64
// holds exactly ranks by its own value against the float it rounds to.
func TestOrderStored_RanksANumberByTheExactValueOfItsText(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	for name, pair := range map[string][2]any{
		"0 below 1e-400":                           {int64(0), json.Number("1e-400")},
		"0.0 below 1e-400":                         {0.0, json.Number("1e-400")},
		"-0.0 below 1e-400":                        {negZero, json.Number("1e-400")},
		"-1e-400 below -0.0":                       {json.Number("-1e-400"), negZero},
		"the largest float below a longer literal": {math.MaxFloat64, json.Number("1.7976931348623158e308")},
		"0.1 below a literal past its float":       {0.1, json.Number("0.1000000000000000055511151231257827")},
	} {
		if c, err := value.OrderStored(pair[0], pair[1]); err != nil || c >= 0 {
			t.Errorf("%s: OrderStored(lo, hi) = %d, %v; want negative", name, c, err)
		}
		if c, err := value.OrderStored(pair[1], pair[0]); err != nil || c <= 0 {
			t.Errorf("%s: OrderStored(hi, lo) = %d, %v; want positive", name, c, err)
		}
	}
}

// TestOrderStored_TiesNumbersOnlyWhenWrittenAlikeAndIsTransitive holds the
// number arm over every carrier a graph can hold against the oracle,
// encoding/json's text with a float64's indicator kept: two numbers tie only
// when that text is equal, and the order is total over every triple.
func TestOrderStored_TiesNumbersOnlyWhenWrittenAlikeAndIsTransitive(t *testing.T) {
	t.Parallel()
	type named float64
	values := []any{
		int64(0), int64(1), int64(-1), uint64(1), int8(7), uint64(7), uint64(math.MaxUint64),
		0.0, math.Copysign(0, -1), 1.0, 1.5, 0.1, 1e21, 1e-7, math.MaxFloat64, -math.MaxFloat64,
		float32(0.1), float32(1), float32(1e21), named(1), named(1.5),
		json.Number("0"), json.Number("-0"), json.Number("0e400"), json.Number("1"), json.Number("1.0"),
		json.Number("1.5"), json.Number("1e0"), json.Number("10e-1"), json.Number("0.1"),
		json.Number("1e-400"), json.Number("-1e-400"), json.Number("1e400"), json.Number("-1e400"),
		json.Number("1.7976931348623158e308"), json.Number("18446744073709551615"),
		json.Number("18446744073709551616"), json.Number("1e21"), json.Number("1e+21"),
		math.Inf(1), math.Inf(-1),
	}
	written := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			return "" // a float that is not finite, which no snapshot writes
		}
		if _, ok := v.(float64); ok && !bytes.ContainsAny(b, ".eE") {
			b = append(b, '.', '0')
		}
		return string(b)
	}
	order := func(a, b any) int {
		c, err := value.OrderStored(a, b)
		if err != nil {
			t.Fatalf("OrderStored(%#v, %#v): %v", a, b, err)
		}
		return c
	}
	for _, a := range values {
		for _, b := range values {
			c := order(a, b)
			if d := order(b, a); c != -d {
				t.Errorf("OrderStored(%#v, %#v) = %d but reversed = %d", a, b, c, d)
			}
			if wa, wb := written(a), written(b); wa != "" && wb != "" && (c == 0) != (wa == wb) {
				t.Errorf("OrderStored(%#v, %#v) = %d, written %q and %q", a, b, c, wa, wb)
			}
			for _, x := range values {
				if c <= 0 && order(b, x) <= 0 && order(a, x) > 0 {
					t.Errorf("not transitive: %#v <= %#v <= %#v but %#v > %#v", a, b, x, a, x)
				}
			}
		}
	}
}

// TestOrderStored_RanksANumberLiteralPastEveryFloat holds the order total over
// the literals a reader keeps as written: one no finite float64 holds ranks
// beyond every finite number on its side of zero, and two compare as
// decimals, at a cost that follows their length.
func TestOrderStored_RanksANumberLiteralPastEveryFloat(t *testing.T) {
	t.Parallel()
	ascending := []any{
		math.Inf(-1),
		json.Number("-1e400"),
		-math.MaxFloat64,
		json.Number("-1.5e308"),
		json.Number("-18446744073709551616"),
		int64(-1),
		0.0,
		json.Number("18446744073709551616"),
		json.Number("1.5e308"),
		math.MaxFloat64,
		json.Number("2e308"),
		json.Number("1e400"),
		json.Number("1.0000000000000000001e400"),
		json.Number("1e1000000000"),
		json.Number("1e1000000000000000000000"),
		math.Inf(1),
	}
	for i := range ascending {
		for j := range ascending {
			c, err := value.OrderStored(ascending[i], ascending[j])
			if err != nil {
				t.Fatalf("OrderStored(%v, %v): %v", ascending[i], ascending[j], err)
			}
			if want := cmp.Compare(i, j); c != want {
				t.Errorf("OrderStored(%v, %v) = %d, want %d", ascending[i], ascending[j], c, want)
			}
		}
	}
}

// TestOrder_StillRefusesAMap holds the expression language's rule: an ordered
// comparison on a map is an evaluation error, so only a stored value takes the
// map arm.
func TestOrder_StillRefusesAMap(t *testing.T) {
	t.Parallel()
	p := immutable.WrapProperties(map[string]any{"a": int64(1)})
	for name, other := range map[string]any{"itself": p, "a string": "x", "a list": []any{"x"}} {
		if _, err := value.Order(p, other); err == nil {
			t.Errorf("Order(map, %s) did not refuse", name)
		}
		if _, err := value.Order([]any{p}, []any{other}); err == nil {
			t.Errorf("Order over lists holding a map and %s did not refuse", name)
		}
	}
}
