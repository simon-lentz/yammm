package instance_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// notLiterals are spellings strconv reads as numbers and no JSON reader does.
var notLiterals = []string{
	"+5", "007", "-007", "-01", "1_000", "0x1p4", "0x10", ".5", "5.", "-.5",
	"NaN", "Inf", "infinity", "0x1p5000", "1_0e400", "", " 5", "5 ", "1e", "-",
}

// A json.Number whose text is no JSON number literal is no number at an
// Integer or a Float, bare, behind a pointer or inside an interface, and the
// refusal names it as such, whatever strconv would read it as.
func TestCheckValue_AJSONNumberThatIsNoLiteralIsRefused(t *testing.T) {
	t.Parallel()
	for _, text := range notLiterals {
		n := json.Number(text)
		var held any = n
		for _, carrier := range []struct {
			name string
			v    any
		}{{"bare", n}, {"a pointer", &n}, {"an interface behind a pointer", &held}} {
			for _, c := range []schema.Constraint{schema.NewIntegerConstraint(), schema.NewFloatConstraint()} {
				err := instance.CheckValue(carrier.v, c)
				if err == nil || !strings.Contains(err.Error(), "a json.Number that is no JSON number literal") {
					t.Errorf("%q %s at %s: %v, want a refusal naming no JSON number literal", text, carrier.name, c.Kind(), err)
				}
				if v, err := instance.CanonicalValue(carrier.v, c); err == nil {
					t.Errorf("%q %s at %s: CanonicalValue = %#v, want a refusal", text, carrier.name, c.Kind(), v)
				}
			}
			if v, ok := instance.ScalarOf(carrier.v); ok {
				t.Errorf("%q %s: ScalarOf = %#v, true; want no form", text, carrier.name, v)
			}
		}
	}
}

// The Validator refuses such a json.Number at an Integer, at a Float and as a
// list element, and accepts the literal a JSON reader would hand it instead.
func TestValidateOne_AJSONNumberThatIsNoLiteralIsRefused(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tn Integer\n\tf Float\n\tns List<Integer>\n}\n")
	v := instance.NewValidator(s)
	for _, c := range []struct {
		prop    string
		bad, ok any
	}{
		{"n", json.Number("+5"), json.Number("5")},
		{"n", json.Number("007"), json.Number("7")},
		{"f", json.Number("0x1p4"), json.Number("16")},
		{"f", json.Number("1_000"), json.Number("1000")},
		{"f", json.Number(".5"), json.Number("0.5")},
		{"ns", []any{json.Number("+5")}, []any{json.Number("5")}},
	} {
		_, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", c.prop: c.bad}})
		if !res.HasCode(diag.E_TYPE_MISMATCH) {
			t.Errorf("%s = %#v: want E_TYPE_MISMATCH, got %v", c.prop, c.bad, codes(res))
		}
		if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", c.prop: c.ok}}); res.Err() != nil {
			t.Errorf("control %s = %#v refused: %v", c.prop, c.ok, res.Err())
		}
	}
}

// A literal no finite float64 holds keeps its own words, and text that is no
// literal is named as no literal even where no float64 would hold its value.
func TestCheckValue_OnlyALiteralIsCalledANumberNoFloat64Holds(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ text, want string }{
		{"1e400", "expected float, got a number no float64 holds: 1e400"},
		{"0x1p5000", `expected float, got a json.Number that is no JSON number literal: "0x1p5000"`},
		{"1_0e400", `expected float, got a json.Number that is no JSON number literal: "1_0e400"`},
	} {
		if err := instance.CheckValue(json.Number(c.text), schema.NewFloatConstraint()); err == nil || err.Error() != c.want {
			t.Errorf("%q at a Float: %v, want %q", c.text, err, c.want)
		}
	}
}
