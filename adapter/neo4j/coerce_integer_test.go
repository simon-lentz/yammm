package neo4j

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A scalar Integer position widens every Go integer width to int64, exactly as
// a List<Integer> element does.
func TestCoerce_IntegerWidensToInt64(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  any
		want int64
	}{
		{"int", int(7), 7},
		{"int8", int8(7), 7},
		{"int16", int16(7), 7},
		{"int32", int32(7), 7},
		{"int64 passes through", int64(7), 7},
		{"uint", uint(7), 7},
		{"uint8", uint8(7), 7},
		{"uint16", uint16(7), 7},
		{"uint32", uint32(7), 7},
		{"uint64", uint64(7), 7},
		{"int64 max", int64(math.MaxInt64), math.MaxInt64},
		{"int64 min", int64(math.MinInt64), math.MinInt64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Coerce(schema.NewIntegerConstraint(), tt.raw)
			if err != nil {
				t.Fatalf("Coerce(Integer, %#v) error: %v", tt.raw, err)
			}
			n, ok := got.(int64)
			if !ok {
				t.Fatalf("Coerce(Integer, %#v) = %#v (%T), want int64", tt.raw, got, got)
			}
			if n != tt.want {
				t.Errorf("Coerce(Integer, %#v) = %d, want %d", tt.raw, n, tt.want)
			}
		})
	}
}

// A float is never an Integer, whole or not, as the validator's Integer rule
// states, so it is an error rather than an integer the caller never wrote.
func TestCoerce_IntegerRejectsWhatItCannotRepair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  any
	}{
		{"whole float64", float64(5)},
		{"whole float32", float32(5)},
		{"negative whole float", float64(-5)},
		{"zero float", float64(0)},
		{"float at int64 min", -math.Ldexp(1, 63)},
		{"fractional float64", float64(5.5)},
		{"fractional float32", float32(5.5)},
		{"float past int64 max", math.Ldexp(1, 63)},
		{"float past int64 min", -math.Ldexp(1, 64)},
		{"uint64 past int64 max", uint64(math.MaxInt64) + 1},
		{"NaN", math.NaN()},
		{"positive infinity", math.Inf(1)},
		{"string", "7"},
		{"bool", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := Coerce(schema.NewIntegerConstraint(), tt.raw)
			if err == nil {
				t.Fatalf("Coerce(Integer, %#v) = %#v, want an error", tt.raw, got)
			}
			if !strings.Contains(err.Error(), "int64") {
				t.Errorf("error = %q, want it to name the target type", err)
			}
		})
	}
}

// A nil value is an absent property, not a failure — unchanged by this rule.
func TestCoerce_IntegerNilPassesThrough(t *testing.T) {
	t.Parallel()
	got, err := Coerce(schema.NewIntegerConstraint(), nil)
	if err != nil || got != nil {
		t.Errorf("Coerce(Integer, nil) = %#v, %v; want nil, nil", got, err)
	}
}

// The list path widens as the scalar path does and refuses a float element,
// so an element and a scalar of the same kind cannot disagree.
func TestCoerceSlice_IntegerElementWidensIntegersAndRefusesFloats(t *testing.T) {
	t.Parallel()
	c := schema.NewListConstraint(schema.NewIntegerConstraint())
	got, err := coerceSlice([]any{int64(1), int32(2), uint16(3), uint8(4)}, c)
	if err != nil {
		t.Fatalf("coerceSlice error: %v", err)
	}
	ints, ok := got.([]int64)
	if !ok {
		t.Fatalf("coerceSlice = %#v (%T), want []int64", got, got)
	}
	want := []int64{1, 2, 3, 4}
	if len(ints) != len(want) {
		t.Fatalf("coerceSlice = %#v, want %#v", ints, want)
	}
	for i := range ints {
		if ints[i] != want[i] {
			t.Errorf("element %d = %d, want %d", i, ints[i], want[i])
		}
	}
	for _, f := range []any{float64(1), float32(3)} {
		if got, err := coerceSlice([]any{int64(0), f}, c); err == nil {
			t.Errorf("coerceSlice accepted the whole float %#v as %#v", f, got)
		} else if !strings.Contains(err.Error(), "element 1") {
			t.Errorf("error = %q, want it to name element 1", err)
		}
	}
}

func TestCoerceSlice_IntegerElementStillRejectsFractions(t *testing.T) {
	t.Parallel()
	c := schema.NewListConstraint(schema.NewIntegerConstraint())
	if got, err := coerceSlice([]any{int64(1), float64(2.5)}, c); err == nil {
		t.Fatalf("coerceSlice = %#v, want an error naming element 1", got)
	} else if !strings.Contains(err.Error(), "element 1") {
		t.Errorf("error = %q, want it to name element 1", err)
	}
}

// CoerceParams applies Coerce's rule, through the scalar path they share, at
// the boundary a direct-Cypher caller crosses: an integer widens, a float is
// refused.
func TestCoerceParams_IntegerRuleHoldsAtTheParamBoundary(t *testing.T) {
	t.Parallel()
	types := ParamTypes{"count": schema.NewIntegerConstraint()}
	out, err := CoerceParams(map[string]any{"count": int32(12)}, types)
	if err != nil {
		t.Fatalf("CoerceParams error: %v", err)
	}
	if n, ok := out["count"].(int64); !ok || n != 12 {
		t.Errorf("CoerceParams count = %#v (%T), want int64(12)", out["count"], out["count"])
	}
	if out, err := CoerceParams(map[string]any{"count": float64(12)}, types); err == nil {
		t.Errorf("CoerceParams accepted a float at an Integer as %#v", out["count"])
	}
}

// An unsigned value past int64 is refused at every width that can hold one,
// not only uint64.
func TestCoerce_IntegerRefusesAnUnsignedValuePastInt64(t *testing.T) {
	t.Parallel()
	for _, raw := range []any{uint(math.MaxInt64) + 1, uint64(math.MaxInt64) + 1} {
		if got, err := Coerce(schema.NewIntegerConstraint(), raw); err == nil {
			t.Errorf("Coerce(Integer, %T %v) = %#v, want an error", raw, raw, got)
		}
	}
}
