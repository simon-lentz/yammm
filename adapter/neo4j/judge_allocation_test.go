package neo4j

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/raceskip"
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
		if allocs := testing.AllocsPerRun(100, func() {
			if _, err := Coerce(c, nil); err != nil {
				t.Fatal(err)
			}
		}); allocs != 0 {
			t.Errorf("Coerce allocates %v per call judging %s (%s); want 0", allocs, name, c)
		}
	}
}

// Coerce passes a typed collection a Vector or a scalar List holds at no cost
// beyond reading its form: only a List can nest, so a Vector is never judged for
// it, and judging a List's element allocates nothing.
func TestCoerce_ATypedCollectionCostsOnlyItsForm(t *testing.T) {
	raceskip.Skip(t)
	for name, tc := range map[string]struct {
		c   schema.Constraint
		raw any
	}{
		"Vector[3]":    {schema.NewVectorConstraint(3), []float64{1, 2, 3}},
		"List<Float>":  {schema.NewListConstraint(schema.NewFloatConstraint()), []float64{1, 2, 3}},
		"List<String>": {schema.NewListConstraint(schema.NewStringConstraint()), []string{"a"}},
	} {
		form := testing.AllocsPerRun(100, func() { _ = holdsNone(tc.raw) })
		if allocs := testing.AllocsPerRun(100, func() {
			if _, err := Coerce(tc.c, tc.raw); err != nil {
				t.Fatal(err)
			}
		}); allocs != form {
			t.Errorf("Coerce allocates %v per call at %s; want %v, the cost of reading its form", allocs, name, form)
		}
	}
}
