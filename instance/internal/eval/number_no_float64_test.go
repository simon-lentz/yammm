package eval_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/schema"
)

// A literal no finite float64 holds is no number of either numeric kind, and
// an Integer names it as a Float does: the Float's own message is the wanted
// text, with the kind's name in place of "float".
func TestCheckValue_ALiteralNoFloat64HoldsReadsAlikeAtAnIntegerAndAFloat(t *testing.T) {
	t.Parallel()
	for _, lit := range []string{
		"1e400", "-1e400", "1E400", "1.5e999", "0.1e500",
		strings.Repeat("9", 40) + ".5e400",
	} {
		n := json.Number(lit)
		for _, val := range []any{n, &n} {
			atFloat := eval.CheckValue(val, schema.NewFloatConstraint())
			if atFloat == nil || !strings.Contains(atFloat.Error(), "a number no float64 holds") {
				t.Fatalf("%s at a Float: %v, want a refusal naming a number no float64 holds", lit, atFloat)
			}
			want := strings.Replace(atFloat.Error(), "expected float", "expected integer", 1)

			atInteger := eval.CheckValue(val, schema.NewIntegerConstraint())
			if atInteger == nil || atInteger.Error() != want {
				t.Errorf("%s (%T) at an Integer: %v, want %q", lit, val, atInteger, want)
			}
			var checkErr *eval.CheckError
			if !errors.As(atInteger, &checkErr) || checkErr.Kind != eval.KindTypeMismatch {
				t.Errorf("%s (%T) at an Integer: %v is not a type mismatch", lit, val, atInteger)
			}
		}
	}
}

// The coercion refuses the same literal in the Float coercion's words.
func TestCoerceValue_ALiteralNoFloat64HoldsReadsAlikeAtAnIntegerAndAFloat(t *testing.T) {
	t.Parallel()
	for _, lit := range []string{"1e400", "-1.5e999", strings.Repeat("9", 40) + ".5e400"} {
		_, atFloat := eval.CoerceValue(json.Number(lit), schema.NewFloatConstraint())
		if atFloat == nil || !strings.Contains(atFloat.Error(), "no finite float64 holds it") {
			t.Fatalf("%s at a Float: %v, want a refusal naming that no finite float64 holds it", lit, atFloat)
		}
		want := strings.Replace(atFloat.Error(), "to float64", "to int64", 1)
		if _, atInteger := eval.CoerceValue(json.Number(lit), schema.NewIntegerConstraint()); atInteger == nil || atInteger.Error() != want {
			t.Errorf("%s at an Integer: %v, want %q", lit, atInteger, want)
		}
	}
}

// Every other number an Integer refuses keeps its own words: an integer
// literal past int64 is an integer, a float a float64 holds is a float, and
// text that is no JSON number literal is named as such, quoted.
func TestCheckValue_AnIntegerRefusalNamesWhatTheLiteralIs(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ lit, want string }{
		{strings.Repeat("9", 400), "expected integer, got an integer outside the int64 range: 99999999999999999999999999999999… (400 bytes)"},
		{"9223372036854775808", "expected integer, got an integer outside the int64 range: 9223372036854775808"},
		{"1.5", "expected integer, got float 1.5"},
		{"1e2", "expected integer, got float 100.0"},
		{"abc", `expected integer, got a json.Number that is no JSON number literal: "abc"`},
		{"99999999999999999999x", `expected integer, got a json.Number that is no JSON number literal: "99999999999999999999x"`},
		{"99999999999999999999\x1b[2J", `expected integer, got a json.Number that is no JSON number literal: "99999999999999999999\x1b[2J"`},
		{"", `expected integer, got a json.Number that is no JSON number literal: ""`},
	} {
		if err := eval.CheckValue(json.Number(c.lit), schema.NewIntegerConstraint()); err == nil || err.Error() != c.want {
			t.Errorf("%q at an Integer: %v, want %q", c.lit, err, c.want)
		}
	}
}

// The coercion reads a literal through a pointer as the check does, and names
// it in the same words.
func TestCoerceValue_APointerToALiteralNoFloat64HoldsIsNamed(t *testing.T) {
	t.Parallel()
	n := json.Number("1e400")
	_, err := eval.CoerceValue(&n, schema.NewIntegerConstraint())
	if want := "cannot coerce 1e400 to int64: no finite float64 holds it"; err == nil || err.Error() != want {
		t.Errorf("&1e400 at an Integer: %v, want %q", err, want)
	}
}
