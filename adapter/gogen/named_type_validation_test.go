package gogen_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
)

// The generator emits `type <Name> <base>` for every DataType and every inline
// enum, so a caller who decodes into a generated struct holds named types
// wherever the schema named one. Feeding those values back to the validator is
// the direction nothing exercised: TestRoundTrip_Temporal runs the generated
// direction outward only — plain values in, decode into the generated Graph —
// and every gogen fixture with a named type stops at byte-comparing the golden.
//
// The consequence was a closed loop that did not close. The library's own
// generator produced types its own validator refused with E_TYPE_MISMATCH,
// where the plain base value passed.
//
// These local declarations mirror named.go.golden rather than importing it:
// only the temporal fixture is compiled into this module.
// TestNamedTypes_MirrorTheGeneratedCarriers below keeps the mirror honest.
type (
	fipsCode     string // DataType over a bounded String — the non-enum carrier
	tier         string // DataType over an Enum
	countyStatus string // inline enum on the property
)

// A value of every carrier shape the generator emits validates against the
// schema it was generated from.
//
// Mutation: reverting classifyNamedBase in internal/value, or toStringComparable's
// reflect fallback, turns this red with the E_TYPE_MISMATCH the defect produced.
func TestNamedTypes_GeneratedCarriersValidate(t *testing.T) {
	s := loadSchema(t, "named")
	v := instance.NewValidator(s)

	for _, tc := range []struct {
		name  string
		props map[string]any
	}{
		{
			"named carriers throughout",
			map[string]any{
				"fips":   fipsCode("12345"),
				"tier":   tier("gold"),
				"status": countyStatus("active"),
				"codes":  []any{fipsCode("54321"), fipsCode("11111")},
			},
		},
		{
			"plain strings, the shape that always worked",
			map[string]any{
				"fips":   "12345",
				"tier":   "gold",
				"status": "active",
				"codes":  []any{"54321"},
			},
		},
		{
			"mixed, because a caller assembles both",
			map[string]any{
				"fips":   fipsCode("12345"),
				"tier":   "silver",
				"status": countyStatus("merged"),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, res := v.ValidateOne(t.Context(), "County", instance.RawInstance{Properties: tc.props})
			if !res.OK() {
				t.Errorf("a generated carrier did not validate: %s", res)
			}
		})
	}
}

// A named carrier is STORED as its base type. Accepting the carrier without
// canonicalising it would move the asymmetry one layer down: the instance would
// persist a Go type the wire, both writers and the snapshot rebuild never see,
// and key identity is a function of the stored representation.
//
// Mutation: reverting CoerceValue's string arm to `return val, nil` turns this
// red while TestNamedTypes_GeneratedCarriersValidate stays green — which is
// exactly the half-fixed state worth guarding against.
func TestNamedTypes_AreStoredAsTheirBaseType(t *testing.T) {
	s := loadSchema(t, "named")
	inst, res := instance.NewValidator(s).ValidateOne(t.Context(), "County",
		instance.RawInstance{Properties: map[string]any{
			"fips":   fipsCode("12345"),
			"tier":   tier("gold"),
			"status": countyStatus("active"),
			"codes":  []any{fipsCode("54321")},
		}})
	if !res.OK() {
		t.Fatalf("validate: %s", res)
	}
	for _, name := range []string{"fips", "tier", "status"} {
		v, ok := inst.Properties().Get(name)
		if !ok {
			t.Fatalf("property %q missing from the validated instance", name)
		}
		if got, isString := v.Unwrap().(string); !isString {
			t.Errorf("property %q stored as %T, want string", name, v.Unwrap())
		} else if got == "" {
			t.Errorf("property %q stored empty", name)
		}
	}
}

// A named carrier is still checked against its constraint. Accepting the type
// must not mean accepting any value of it.
func TestNamedTypes_StillCheckedAgainstTheirConstraint(t *testing.T) {
	s := loadSchema(t, "named")
	v := instance.NewValidator(s)

	for _, tc := range []struct {
		name  string
		props map[string]any
	}{
		{"enum value outside the set", map[string]any{
			"fips": fipsCode("12345"), "tier": tier("bronze"), "status": countyStatus("active"),
		}},
		{"string outside its bounds", map[string]any{
			"fips": fipsCode("1"), "tier": tier("gold"), "status": countyStatus("active"),
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, res := v.ValidateOne(t.Context(), "County", instance.RawInstance{Properties: tc.props}); res.OK() {
				t.Error("a named carrier bypassed its constraint")
			}
		})
	}
}

// The local declarations above stand in for the generated ones, so this asserts
// the generator still emits the shape they mirror. A change to the emitted base
// type breaks the mirror silently otherwise.
func TestNamedTypes_MirrorTheGeneratedCarriers(t *testing.T) {
	golden, err := os.ReadFile(filepath.Clean(filepath.Join("testdata", "named.go.golden")))
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	for _, decl := range []string{
		"type FipsCode string",
		"type Tier string",
		"type CountyStatus string",
	} {
		if !strings.Contains(string(golden), decl) {
			t.Errorf("named.go.golden no longer emits %q; the mirror above is stale", decl)
		}
	}
}
