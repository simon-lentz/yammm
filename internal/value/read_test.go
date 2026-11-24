package value_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// sameRead reports whether got is want, each float64 compared by its bits so a
// negative zero is not zero.
func sameRead(got, want any) bool {
	g, gf := got.(float64)
	w, wf := want.(float64)
	if gf || wf {
		return gf && wf && math.Float64bits(g) == math.Float64bits(w)
	}
	switch wv := want.(type) {
	case []any:
		gv, ok := got.([]any)
		if !ok || len(gv) != len(wv) {
			return false
		}
		for i := range wv {
			if !sameRead(gv[i], wv[i]) {
				return false
			}
		}
		return true
	case map[string]any:
		gv, ok := got.(map[string]any)
		if !ok || len(gv) != len(wv) {
			return false
		}
		for k := range wv {
			if !sameRead(gv[k], wv[k]) {
				return false
			}
		}
		return true
	}
	return reflect.DeepEqual(got, want)
}

func TestRead_ReadsEveryNumberAsClassifyDoes(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		name string
		in   any
		want any
	}{
		{"an integer literal", json.Number("42"), int64(42)},
		{"-0, an integer literal", json.Number("-0"), int64(0)},
		{"a float literal", json.Number("2.5"), 2.5},
		{"an exponent", json.Number("1e2"), float64(100)},
		{"an integer literal past int64", json.Number("9223372036854775808"), json.Number("9223372036854775808")},
		{"a literal no float64 holds", json.Number("1e400"), json.Number("1e400")},
		{"text that is no literal", json.Number("0x10"), json.Number("0x10")},
		{"a string", "5", "5"},
		{"an int64", int64(5), int64(5)},
		{"nil", nil, nil},
		{"a nested list and map", []any{json.Number("1"), map[string]any{"a": []any{json.Number("2.0")}}}, []any{int64(1), map[string]any{"a": []any{float64(2)}}}},
	} {
		if got := value.Read(c.in); !sameRead(got, c.want) {
			t.Errorf("%s: Read(%#v) = %#v, want %#v", c.name, c.in, got, c.want)
		}
	}
}

// Read rewrites a list and a map where they are and returns them.
func TestRead_RewritesInPlace(t *testing.T) {
	t.Parallel()
	inner := []any{json.Number("1.5")}
	in := map[string]any{"n": json.Number("7"), "s": inner}
	out, ok := value.Read(in).(map[string]any)
	if !ok {
		t.Fatal("Read did not return a map[string]any")
	}
	if in["n"] != int64(7) || inner[0] != 1.5 {
		t.Errorf("the caller's containers were not rewritten: %#v, %#v", in, inner)
	}
	out["probe"] = true
	if _, same := in["probe"]; !same {
		t.Error("Read returned a different map from the one it was given")
	}
}

// A number deeper than any fixed bound is still read.
func TestRead_HasNoDepthBound(t *testing.T) {
	t.Parallel()
	var deep any = json.Number("99")
	for range 1000 {
		deep = []any{deep}
	}
	got := value.Read(deep)
	for range 1000 {
		got = got.([]any)[0]
	}
	if got != int64(99) {
		t.Errorf("the number 1000 levels down is %#v, want int64(99)", got)
	}
}

func TestReadAt_ReadsANumberAtTheKindItsPositionDeclares(t *testing.T) {
	t.Parallel()
	float := schematest.Constraint(t, "Float")
	negZero := math.Copysign(0, -1)
	for _, c := range []struct {
		name string
		in   any
		c    schema.Constraint
		want any
	}{
		{"an integer at a Float", json.Number("5"), float, float64(5)},
		{"-0 at a Float", json.Number("-0"), float, negZero},
		{"an integer past 2^53 at a Float", json.Number("9007199254740993"), float, float64(9007199254740992)},
		{"an integer past int64 at a Float", json.Number("99999999999999999999"), float, float64(1e20)},
		{"a literal no float64 holds at a Float", json.Number("1e400"), float, json.Number("1e400")},
		{"text that is no literal at a Float", json.Number("0x10"), float, json.Number("0x10")},
		{"a signed spelling at a Float", json.Number("+5"), float, json.Number("+5")},
		{"a leading zero at a Float", json.Number("007"), float, json.Number("007")},
		{"NaN at a Float", json.Number("NaN"), float, json.Number("NaN")},
		{"an int64 at a Float", int64(-3), float, float64(-3)},
		{"an int64 past 2^53 at a Float", int64(9007199254740993), float, float64(9007199254740992)},
		{"an int64 at a Vector", []any{int64(3)}, schematest.Constraint(t, "Vector[1]"), []any{float64(3)}},
		{"an int64 at an Integer", int64(3), schematest.Constraint(t, "Integer"), int64(3)},
		{"a list at a Float", []any{json.Number("1")}, float, []any{int64(1)}},
		{"an integer at an alias of Float", json.Number("7"), schematest.Constraint(t, "Ratio", "type Ratio = Float"), float64(7)},
		{"an integer at an Integer", json.Number("5"), schematest.Constraint(t, "Integer"), int64(5)},
		{"-0 at an Integer", json.Number("-0"), schematest.Constraint(t, "Integer"), int64(0)},
		{"an integer past int64 at an Integer", json.Number("9223372036854775808"), schematest.Constraint(t, "Integer"), json.Number("9223372036854775808")},
		{"an integer at no constraint", json.Number("5"), nil, int64(5)},
		{"integers at a Vector", []any{json.Number("3"), json.Number("-0"), []any{json.Number("4")}}, schematest.Constraint(t, "Vector[3]"), []any{float64(3), negZero, []any{int64(4)}}},
		{"a string at a Vector", "v", schematest.Constraint(t, "Vector[1]"), "v"},
		{"a map at a Vector", map[string]any{"a": json.Number("1")}, schematest.Constraint(t, "Vector[1]"), map[string]any{"a": int64(1)}},
		{"integers at a List of Float", []any{json.Number("1"), json.Number("2.5")}, schematest.Constraint(t, "List<Float>"), []any{float64(1), 2.5}},
		{"integers at a List of List of Float", []any{[]any{json.Number("1")}}, schematest.Constraint(t, "List<List<Float>>"), []any{[]any{float64(1)}}},
		{"integers at a List of Integer", []any{json.Number("1"), json.Number("18446744073709551616")}, schematest.Constraint(t, "List<Integer>"), []any{int64(1), json.Number("18446744073709551616")}},
		{"a map at a List of Float", map[string]any{"a": json.Number("1")}, schematest.Constraint(t, "List<Float>"), map[string]any{"a": int64(1)}},
		{"a list at a String", []any{json.Number("1")}, schematest.Constraint(t, "String"), []any{int64(1)}},
	} {
		if got := value.ReadAt(c.in, c.c); !sameRead(got, c.want) {
			t.Errorf("%s: ReadAt = %#v, want %#v", c.name, got, c.want)
		}
	}
}
