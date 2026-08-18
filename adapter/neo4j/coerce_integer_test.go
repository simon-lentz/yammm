package neo4j

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A scalar Integer position repairs exactly as a List<Integer> element does.
// Before this rule, a hand-built float64 passed through as a Cypher FLOAT and
// an IS :: INTEGER MERGE matched nothing, reporting no error.
func TestCoerce_IntegerRepairsToInt64(t *testing.T) {
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
		{"whole float64", float64(5), 5},
		{"whole float32", float32(5), 5},
		{"negative whole float", float64(-5), -5},
		{"zero float", float64(0), 0},
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

func TestCoerce_IntegerRejectsWhatItCannotRepair(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		raw  any
	}{
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

// float64(math.MinInt64) is exactly -2^63, which is representable, so the lower
// bound is inclusive where the upper bound is not.
func TestCoerce_IntegerAcceptsTheExactLowerBound(t *testing.T) {
	t.Parallel()
	got, err := Coerce(schema.NewIntegerConstraint(), -math.Ldexp(1, 63))
	if err != nil {
		t.Fatalf("Coerce(Integer, -2^63) error: %v", err)
	}
	if n, _ := got.(int64); n != math.MinInt64 {
		t.Errorf("Coerce(Integer, -2^63) = %#v, want math.MinInt64", got)
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

// The list path gains the same acceptance, so an element and a scalar of the
// same kind cannot disagree.
func TestCoerceSlice_IntegerElementAcceptsWholeFloats(t *testing.T) {
	t.Parallel()
	c := schema.NewListConstraint(schema.NewIntegerConstraint())
	got, err := coerceSlice([]any{float64(1), int32(2), float32(3), uint8(4)}, c)
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

// CoerceParams inherits the rule through Coerce, which is the boundary a
// direct-Cypher caller actually crosses.
func TestCoerceParams_IntegerRepairsThroughTheParamBoundary(t *testing.T) {
	t.Parallel()
	out, err := CoerceParams(
		map[string]any{"count": float64(12)},
		ParamTypes{"count": schema.NewIntegerConstraint()},
	)
	if err != nil {
		t.Fatalf("CoerceParams error: %v", err)
	}
	if n, ok := out["count"].(int64); !ok || n != 12 {
		t.Errorf("CoerceParams count = %#v (%T), want int64(12)", out["count"], out["count"])
	}
}
