package neo4j

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/google/uuid"
	"github.com/neo4j/neo4j-go-driver/v6/neo4j/dbtype"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
)

// The scalar rule takes a value in the form the validator stores at the kind
// and refuses every other Go form, a width, a named type and a pointer among
// them. It judges the form alone: a value outside the constraint's bounds,
// members or pattern passes.
func TestCoerceValue_TakesTheStoredFormAlone(t *testing.T) {
	t.Parallel()
	type (
		namedText string
		namedFlag bool
	)
	text, n := "a", int64(7)
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	var nilText *string
	for _, c := range []struct {
		name    string
		c       schema.Constraint
		stored  []any
		refused []any
	}{
		{
			"Integer", schematest.Constraint(t, "Integer[0, 10]"),
			[]any{int64(7), int64(math.MaxInt64), int64(math.MinInt64)},
			[]any{7, int8(7), int16(7), int32(7), uint(7), uint8(7), uint16(7), uint32(7), uint64(7), uint64(math.MaxInt64) + 1, float64(5), float32(5), 5.5, math.NaN(), json.Number("7"), "7", true, &n},
		},
		{
			"Float", schematest.Constraint(t, "Float[0, 1]"),
			[]any{1.5, 5.0, math.Copysign(0, -1)},
			[]any{int64(5), 5, uint8(5), float32(5), json.Number("1.5"), "1.5", true},
		},
		{"String", schematest.Constraint(t, "String[2, 3]"), []any{"a", ""}, []any{namedText("a"), json.Number("5"), 5, &text, nilText}},
		{"Enum", schematest.Constraint(t, `Enum["a", "z"]`), []any{"a", "no member"}, []any{namedText("a"), 5}},
		{"Pattern", schematest.Constraint(t, `Pattern["^a$"]`), []any{"a", "no match"}, []any{namedText("a"), 5}},
		{"Boolean", schematest.Constraint(t, "Boolean"), []any{true, false}, []any{"true", 1, namedFlag(true)}},
		{"Timestamp", schematest.Constraint(t, "Timestamp"), []any{"2024-01-02T03:04:05Z"}, []any{at, &at, int64(123), true}},
		{"Date", schematest.Constraint(t, "Date"), []any{"2024-01-02"}, []any{dbtype.Date(at), at, int64(123), true}},
		{"UUID", schematest.Constraint(t, "UUID"), []any{id.String()}, []any{id, 5}},
		{"Vector", schematest.Constraint(t, "Vector[2]"), []any{[]float64{1, 2}, []float64{1}}, []any{[]float32{1, 2}, []int64{1, 2}, []string{"a"}, 1.5}},
		{"List", schematest.Constraint(t, "List<String>"), nil, []any{[]string{"a"}, "a"}},
	} {
		for _, v := range c.stored {
			if _, err := coerceValue(c.c, v); err != nil {
				t.Errorf("%s: the stored form %#v is refused: %v", c.name, v, err)
			}
		}
		for _, v := range c.refused {
			got, err := coerceValue(c.c, v)
			if !errors.Is(err, ErrUnrepresentable) {
				t.Errorf("%s: %#v = %#v, %v; want a refusal that matches ErrUnrepresentable", c.name, v, got, err)
			} else if typ := fmt.Sprintf("%T", v); !strings.Contains(err.Error(), typ) {
				t.Errorf("%s: the refusal %q does not name %s", c.name, err, typ)
			}
		}
	}
}

// A value that holds none is nil at every kind, and no constraint passes a
// value as given.
func TestCoerceValue_AValueThatHoldsNoneIsNil(t *testing.T) {
	t.Parallel()
	for _, c := range []schema.Constraint{schematest.Constraint(t, "Integer"), schematest.Constraint(t, "Timestamp"), schematest.Constraint(t, "Vector[2]")} {
		for _, v := range []any{nil, []string(nil), map[string]any(nil)} {
			if got, err := coerceScalar(c, v); err != nil || got != nil {
				t.Errorf("coerceScalar(%s, %#v) = %#v, %v; want nil, nil", c.Kind(), v, got, err)
			}
		}
	}
	if got, err := coerceValue(nil, int32(5)); err != nil || got != int32(5) {
		t.Errorf("coerceValue(nil, int32(5)) = %#v, %v; want the value as given", got, err)
	}
}

// The list rule takes each element in the form the validator stores and builds
// the typed slice the driver sends. An element of another Go form is refused
// with its index.
func TestCoerceSlice_TakesTheStoredFormAtEachElement(t *testing.T) {
	t.Parallel()
	type namedText string
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.FixedZone(driverOffsetZoneName, 0))
	for _, c := range []struct {
		name    string
		c       schema.Constraint
		stored  []any
		want    any
		refused []any
	}{
		{"List<Integer>", schematest.Constraint(t, "List<Integer[0, 1]>"), []any{int64(1), int64(20)}, []int64{1, 20}, []any{1, int32(2), uint16(3), uint64(1 << 63), float64(1), float32(3), 2.5, json.Number("1"), "x", nil}},
		{"List<Float>", schematest.Constraint(t, "List<Float>"), []any{1.5, 2.0}, []float64{1.5, 2}, []any{int64(1), 1, float32(1), json.Number("1.5"), "x", nil}},
		{"List<String>", schematest.Constraint(t, "List<String[5, 9]>"), []any{"a", ""}, []string{"a", ""}, []any{namedText("a"), int64(2), nil}},
		{"List<Boolean>", schematest.Constraint(t, "List<Boolean>"), []any{true, false}, []bool{true, false}, []any{"x", 1, nil}},
		{"List<Timestamp>", schematest.Constraint(t, "List<Timestamp>"), []any{"2024-01-02T03:04:05Z"}, []time.Time{at}, []any{at, int64(5), "nope", nil}},
		{"List<Date>", schematest.Constraint(t, "List<Date>"), []any{"2024-01-02"}, []dbtype.Date{dbtype.Date(time.Date(2024, 1, 2, 0, 0, 0, 0, time.UTC))}, []any{at, int64(5), "nope", nil}},
		{"List<UUID>", schematest.Constraint(t, "List<UUID>"), []any{"12345678-1234-1234-1234-123456789abc"}, []string{"12345678-1234-1234-1234-123456789abc"}, []any{int64(5), "zz", nil}},
		{"Vector", schematest.Constraint(t, "Vector[3]"), []any{1.0, 3.5}, []float64{1, 3.5}, []any{int64(1), float32(1), json.Number("1"), "x", nil}},
	} {
		got, err := coerceValue(c.c, c.stored)
		if err != nil || !sameSent(got, c.want) {
			t.Errorf("%s: %#v = %#v (%T), %v; want %#v", c.name, c.stored, got, got, err, c.want)
		}
		for _, v := range c.refused {
			got, err := coerceValue(c.c, []any{c.stored[0], v})
			if !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), "list element 1") {
				t.Errorf("%s: the element %#v = %#v, %v; want a refusal naming list element 1", c.name, v, got, err)
			}
		}
	}
	if got, err := coerceValue(schematest.Constraint(t, "List<String>"), []any{}); err != nil || !reflect.DeepEqual(got, []string{}) {
		t.Errorf("an empty list = %#v, %v; want an empty []string", got, err)
	}
}

// A UUID is sent as its canonical text, as a scalar and as a list element, and
// a string that is no UUID is refused at both: 38 bytes that hold a UUID
// between two bytes that are not braces included.
func TestCoerceValue_AUUIDIsSentAsItsCanonicalText(t *testing.T) {
	t.Parallel()
	const canonical = "12345678-1234-1234-1234-123456789abc"
	scalar, list := schematest.Constraint(t, "UUID"), schematest.Constraint(t, "List<UUID>")
	for _, s := range []string{
		canonical,
		"12345678-1234-1234-1234-123456789ABC",
		"{12345678-1234-1234-1234-123456789abc}",
		"urn:uuid:12345678-1234-1234-1234-123456789abc",
		"12345678123412341234123456789abc",
	} {
		if got, err := coerceValue(scalar, s); err != nil || got != canonical {
			t.Errorf("coerceValue(UUID, %q) = %#v, %v; want %q", s, got, err, canonical)
		}
		if got, err := coerceValue(list, []any{s}); err != nil || !reflect.DeepEqual(got, []string{canonical}) {
			t.Errorf("coerceValue(List<UUID>, [%q]) = %#v, %v; want [%q]", s, got, err, canonical)
		}
	}
	for _, s := range []string{"zz", "x" + canonical + "y", "{" + canonical + ")"} {
		if got, err := coerceValue(scalar, s); !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), "cannot parse "+diag.QuoteValue(s)+" as a UUID") {
			t.Errorf("coerceValue(UUID, %q) = %#v, %v; want a refusal that quotes the text", s, got, err)
		}
	}
	if got, err := coerceValue(list, []any{canonical, "zz"}); !errors.Is(err, ErrUnrepresentable) || !strings.Contains(err.Error(), `list element 1`) || !strings.Contains(err.Error(), `"zz"`) {
		t.Errorf(`coerceValue(List<UUID>, [.., "zz"]) = %#v, %v; want a refusal that names list element 1 and quotes the text`, got, err)
	}
}

// A refusal names the kind, the Go type it was given and the form the kind
// stores, at a scalar and at a list element.
func TestCoerceValue_ARefusalNamesTheFormTheKindStores(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		c    schema.Constraint
		v    any
		want string
	}{
		{schematest.Constraint(t, "Float"), "x", "coerce Float: cannot use string (want float64)"},
		{schematest.Constraint(t, "Integer"), 1.5, "coerce Integer: cannot use float64 (want int64)"},
		{schematest.Constraint(t, "String"), 5, "coerce String: cannot use int (want string)"},
		{schematest.Constraint(t, "Boolean"), "x", "coerce Boolean: cannot use string (want bool)"},
		{schematest.Constraint(t, "UUID"), 5, "coerce UUID: cannot use int (want string)"},
		{schematest.Constraint(t, "Timestamp"), 5, "coerce Timestamp: cannot use int (want string)"},
		{schematest.Constraint(t, "Date"), 5, "coerce Date: cannot use int (want a YYYY-MM-DD string)"},
		{schematest.Constraint(t, "Vector[2]"), "x", "coerce Vector: cannot use string (want a list of floats)"},
		{schematest.Constraint(t, "List<Float>"), []any{1.5, "x"}, "list element 1: coerce Float: cannot use string (want float64)"},
		{schematest.Constraint(t, "List<Integer>"), []any{1.5}, "list element 0: coerce Integer: cannot use float64 (want int64)"},
		{schematest.Constraint(t, "List<String>"), []any{nil}, "list element 0: coerce String: cannot use <nil> (want string)"},
		{schematest.Constraint(t, "List<Boolean>"), []any{"x"}, "list element 0: coerce Boolean: cannot use string (want bool)"},
		{schematest.Constraint(t, "List<UUID>"), []any{5}, "list element 0: coerce UUID: cannot use int (want string)"},
		{schematest.Constraint(t, "List<Timestamp>"), []any{nil}, "list element 0: coerce Timestamp: cannot use <nil> (want string)"},
		{schematest.Constraint(t, "List<Date>"), []any{nil}, "list element 0: coerce Date: cannot use <nil> (want a YYYY-MM-DD string)"},
		{schematest.Constraint(t, "Vector[1]"), []any{"x"}, "list element 0: coerce Float: cannot use string (want float64)"},
	} {
		if _, err := coerceValue(c.c, c.v); err == nil || err.Error() != c.want {
			t.Errorf("coerceValue(%s, %#v): %v; want %q", c.c, c.v, err, c.want)
		}
	}
}

// CoerceParams reads a number as the validator does where the scalar rule takes
// an int64 alone: every Go integer width, a named integer and an integer
// literal reach the driver as int64, and a float is refused, whole or not.
func TestCoerceParams_ReadsAnIntegerOfEveryWidth(t *testing.T) {
	t.Parallel()
	type count int16
	types := ParamTypes{"count": schematest.Constraint(t, "Integer"), "counts": schematest.Constraint(t, "List<Integer>")}
	for _, v := range []any{7, int8(7), int16(7), int32(7), int64(7), uint(7), uint8(7), uint16(7), uint32(7), uint64(7), uintptr(7), count(7), json.Number("7")} {
		out, err := CoerceParams(map[string]any{"count": v, "counts": []any{v}}, types)
		if err != nil {
			t.Errorf("CoerceParams(%T): %v", v, err)
			continue
		}
		if out["count"] != int64(7) || !reflect.DeepEqual(out["counts"], []int64{7}) {
			t.Errorf("CoerceParams(%T) = %#v and %#v; want int64(7) and []int64{7}", v, out["count"], out["counts"])
		}
	}
	for _, v := range []any{float64(12), float32(12), 5.5, math.NaN(), uint64(math.MaxInt64) + 1, json.Number("12.0"), json.Number("9223372036854775808"), "12", true} {
		if out, err := CoerceParams(map[string]any{"count": v}, types); !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("CoerceParams accepted %#v at an Integer as %#v (%v)", v, out["count"], err)
		}
		if out, err := CoerceParams(map[string]any{"counts": []any{int64(1), v}}, types); !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("CoerceParams accepted %#v at an Integer element as %#v (%v)", v, out["counts"], err)
		}
	}
}

// CoerceParams reads every Go integer width, a float32 and a number literal at a
// Float as the float64 the validator stores, so no Float reaches the driver as
// an integer.
func TestCoerceParams_ReadsAFloatOfEveryWidth(t *testing.T) {
	t.Parallel()
	types := ParamTypes{"f": schematest.Constraint(t, "Float")}
	for _, v := range []any{5, int8(5), int16(5), int32(5), int64(5), uint(5), uint8(5), uint16(5), uint32(5), uint64(5), float32(5), float64(5), json.Number("5"), json.Number("5.0")} {
		out, err := CoerceParams(map[string]any{"f": v}, types)
		if f, ok := out["f"].(float64); err != nil || !ok || f != 5 {
			t.Errorf("CoerceParams(%T) = %#v, %v; want float64(5)", v, out["f"], err)
		}
	}
}

// CoerceParams judges a value as the validator does, bounds, members and
// pattern included, where the write path judges the form alone.
func TestCoerceParams_JudgesBoundsMembersAndPatterns(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		c       schema.Constraint
		ok, bad any
	}{
		"an Integer's bounds": {schematest.Constraint(t, "Integer[0, 10]"), 5, 11},
		"a Float's bounds":    {schematest.Constraint(t, "Float[0, 1]"), 0.5, 1.5},
		"a String's length":   {schematest.Constraint(t, "String[1, 3]"), "abc", "abcd"},
		"an Enum's members":   {schematest.Constraint(t, `Enum["a", "c"]`), "a", "b"},
		"a Pattern":           {schematest.Constraint(t, `Pattern["^a$"]`), "a", "b"},
		"a Vector's width":    {schematest.Constraint(t, "Vector[2]"), []any{1.0, 2.0}, []any{1.0}},
		"a List's length":     {schematest.Constraint(t, "List<String>[1, 2]"), []any{"a"}, []any{}},
		"a List's element":    {schematest.Constraint(t, "List<Integer[0, 10]>"), []any{5}, []any{11}},
	} {
		if _, err := CoerceParams(map[string]any{"v": c.ok}, ParamTypes{"v": c.c}); err != nil {
			t.Errorf("%s: %#v is refused: %v", name, c.ok, err)
		}
		if got, err := CoerceParams(map[string]any{"v": c.bad}, ParamTypes{"v": c.c}); !errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: %#v = %#v, %v; want a refusal that matches ErrUnrepresentable", name, c.bad, got["v"], err)
		}
	}
}

// CoerceParams reads what it returned as it read what it was given, so a
// second pass sends the same values: an instant, a typed slice, and a map or
// rows it passed as given. A date returns as a dbtype.Date, which it refuses
// as the validator does ([TestCoerceParams_RefusesADriverDate]).
func TestCoerceParams_ReadsItsOwnOutput(t *testing.T) {
	t.Parallel()
	types := ParamTypes{
		"f": schematest.Constraint(t, "Float"), "i": schematest.Constraint(t, "Integer"), "s": schematest.Constraint(t, "String"),
		"ts": schematest.Constraint(t, "Timestamp"), "u": schematest.Constraint(t, "UUID"),
		"v": schematest.Constraint(t, "Vector[2]"), "ls": schematest.Constraint(t, "List<String>"),
		"lf": schematest.Constraint(t, "List<Float>"), "lt": schematest.Constraint(t, "List<Timestamp>"),
	}
	first, err := CoerceParams(map[string]any{
		"f": 3, "i": int32(4), "s": "x", "ts": "2024-01-02T03:04:05.5+02:00",
		"u": "12345678-1234-1234-1234-123456789ABC", "v": []any{1, 2.5}, "ls": []any{"a"}, "lf": []any{1, 2.5},
		"lt":   []any{"2024-01-02T03:04:05Z", "2024-01-02T03:04:05-07:00"},
		"rows": []map[string]any{{"ts": "2024-01-02T03:04:05Z"}, nil}, "props": map[string]any{"ts": "2024-01-02T03:04:05Z"},
	}, types)
	if err != nil {
		t.Fatalf("CoerceParams: %v", err)
	}
	second, err := CoerceParams(first, types)
	if err != nil {
		t.Fatalf("CoerceParams over its own output: %v", err)
	}
	if len(second) != len(first) {
		t.Errorf("the second pass sends %d params, the first %d", len(second), len(first))
	}
	for k, want := range first {
		if !sameSent(second[k], want) {
			t.Errorf("%s: the second pass sends %#v, the first %#v", k, second[k], want)
		}
	}
}
