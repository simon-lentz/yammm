package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

const everyKindSource = `schema "fleet"

type Code = Integer[1, 5]
type Codes = List<Code>[1, 3]
type Colour = Enum["red", "green", "blue", "cyan", "magenta", "yellow", "black", "white"]

type Car {
	vin String[1, 17] primary
	code Code
	codes Codes
	nested List<List<Code>>
	colour Colour
	colours List<Colour>
	tag Pattern["^a", "b$"]
	kind Enum["x", "y"]
	at Timestamp
	stamped Timestamp["2006-01-02"]
	on Date
	id2 UUID
	ok Boolean
	vec Vector[3]
	weight Float[0.5, 2.5]
}
`

// Judging the constraints a loaded schema holds allocates nothing, so a caller
// that coerces a row at a time pays no garbage for the types it passes again.
func TestCoerceParams_JudgingALoadedSchemasTypesAllocatesNothing(t *testing.T) {
	raceskip.Skip(t)
	s, res := schema.LoadString(t.Context(), everyKindSource, "fleet.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	car, _ := s.Type("Car")
	types := ParamTypesForType(car, "")
	if len(types) != 15 {
		t.Fatalf("ParamTypesForType holds %d entries; want 15", len(types))
	}
	// Empty params return after the judge, so the judge is all that runs.
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := CoerceParams(nil, types); err != nil {
			t.Fatal(err)
		}
	}); allocs != 0 {
		t.Errorf("CoerceParams allocates %v per call judging accepted types; want 0", allocs)
	}
	for name, c := range types {
		one := ParamTypes{"v": c}
		if allocs := testing.AllocsPerRun(100, func() {
			if _, err := CoerceParams(nil, one); err != nil {
				t.Fatal(err)
			}
		}); allocs != 0 {
			t.Errorf("CoerceParams allocates %v per call judging %s (%s); want 0", allocs, name, c)
		}
	}
}

// The value rule passes a Vector's []float64, the form CoerceParams reads one
// in, at no cost beyond reading its form: only a List can nest, so a Vector is
// never judged for it.
func TestCoerceValue_ATypedVectorCostsOnlyItsForm(t *testing.T) {
	raceskip.Skip(t)
	c, raw := schema.NewVectorConstraint(3), any([]float64{1, 2, 3})
	form := testing.AllocsPerRun(100, func() { _ = value.Absent(raw) })
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := coerceValue(c, raw); err != nil {
			t.Fatal(err)
		}
	}); allocs != form {
		t.Errorf("coerceValue allocates %v per call at a Vector; want %v, the cost of reading its form", allocs, form)
	}
}

// The value rule sends a UUID's canonical text as it is held, so a stored UUID
// costs no more than reading its form.
func TestCoerceValue_AStoredUUIDCostsNoCopy(t *testing.T) {
	raceskip.Skip(t)
	c, raw := schema.NewUUIDConstraint(), any("12345678-1234-1234-1234-123456789abc")
	form := testing.AllocsPerRun(100, func() { _ = value.Absent(raw) })
	if allocs := testing.AllocsPerRun(100, func() {
		if _, err := coerceValue(c, raw); err != nil {
			t.Fatal(err)
		}
	}); allocs != form {
		t.Errorf("coerceValue allocates %v per call at a canonical UUID; want %v, the cost of reading its form", allocs, form)
	}
}

// CoerceParams reads a Vector's []float64 at a cost its length does not move:
// no element is allocated for.
func TestCoerceParams_AFloat64VectorCostsNothingPerElement(t *testing.T) {
	raceskip.Skip(t)
	cost := func(n int) float64 {
		types := ParamTypes{"v": schema.NewVectorConstraint(n)}
		params := map[string]any{"v": make([]float64, n)}
		return testing.AllocsPerRun(100, func() {
			if _, err := CoerceParams(params, types); err != nil {
				t.Fatal(err)
			}
		})
	}
	if short, long := cost(8), cost(1536); long != short {
		t.Errorf("CoerceParams allocates %v per call at a []float64 of 1536 and %v at one of 8; want the same", long, short)
	}
}
