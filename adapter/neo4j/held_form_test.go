package neo4j

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

func noConstraint(string) schema.Constraint { return nil }

// The check on an edge's properties allocates nothing when no value changes form,
// so BatchEdgeQueries pays nothing per edge for it.
func TestFirstRefusedProp_SoundPropertiesAllocateNothing(t *testing.T) {
	raceskip.Skip(t)
	props := map[string]any{
		"a": "x",
		"b": []any{"y", "�"},
		"c": map[string]any{"k": "v"},
		"d": int64(1),
		"e": nil,
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if k, err := firstRefusedProp(props, noConstraint); err != nil {
			t.Fatalf("sound properties refused at %q: %v", k, err)
		}
	}); allocs != 0 {
		t.Errorf("firstRefusedProp allocates %v times over sound properties; want 0", allocs)
	}
}

// Of several refused properties, the first in sorted order is named, on every
// run.
func TestFirstRefusedProp_NamesTheFirstRefusedKeyInSortedOrder(t *testing.T) {
	t.Parallel()
	const huge = json.Number("1e400")
	props := map[string]any{"z": huge, "m": []any{"ok", huge}, "a": "fine", "q": map[string]any{"k": huge}}
	for range 50 {
		if k, err := firstRefusedProp(props, noConstraint); !errors.Is(err, errUnheldNumber) || k != "m" {
			t.Fatalf("firstRefusedProp = %q, %v; want \"m\" and errUnheldNumber", k, err)
		}
	}
}

// A list or a map nested 257 levels deep is refused, the bound the package doc
// states; one 256 deep is handed on.
func TestHeldForm_AValueNestedPastTheDepthIsRefused(t *testing.T) {
	t.Parallel()
	for name, nest := range map[string]func(int, any) any{"a list": deepList, "a map": deepMap} {
		if _, err := heldForm(nest(256, "x"), nil); err != nil {
			t.Errorf("%s at the depth: heldForm = %v", name, err)
		}
		if _, err := heldForm(nest(257, "x"), nil); !errors.Is(err, errNoForm) {
			t.Errorf("%s past the depth: heldForm = %v; want errNoForm", name, err)
		}
	}
}

// A JSON number literal is handed on as the driver's number, read as the
// validator reads it at its constraint, so the coercion that follows writes it
// as every other writer of this module does; a literal no driver number holds
// is refused.
func TestHeldForm_ANumberLiteralIsTheDriversNumber(t *testing.T) {
	t.Parallel()
	negZero := math.Copysign(0, -1)
	integer, float, text := schema.NewIntegerConstraint(), schema.NewFloatConstraint(), schema.NewStringConstraint()
	for _, c := range []struct {
		name    string
		in      any
		c       schema.Constraint
		want    any
		refused bool
	}{
		{"an integer literal at an Integer", json.Number("5"), integer, int64(5), false},
		{"an integer literal at a Float", json.Number("5"), float, 5.0, false},
		{"-0 at a Float", json.Number("-0"), float, negZero, false},
		{"a float literal at a Float", json.Number("0.5"), float, 0.5, false},
		{"a wide integer literal at a Float", json.Number("99999999999999999999"), float, 1e20, false},
		{"an integer literal at a String", json.Number("5"), text, int64(5), true},
		{"an integer literal undeclared", json.Number("5"), nil, int64(5), false},
		{"-0 in a Vector", []any{json.Number("-0"), json.Number("1")}, schema.NewVectorConstraint(2), []any{negZero, 1.0}, false},
		{"an integer literal in a List<Integer>", []any{json.Number("3")}, schema.NewListConstraint(integer), []any{int64(3)}, false},
	} {
		got, err := heldForm(c.in, c.c)
		if err != nil || !reflect.DeepEqual(got, c.want) || (c.want == negZero && !math.Signbit(got.(float64))) {
			t.Errorf("%s: heldForm = %#v, %v; want %#v", c.name, got, err, c.want)
			continue
		}
		if c.c == nil {
			continue
		}
		// The coercion takes the number at a numeric kind and refuses it at any other.
		if _, err := coerceValue(c.c, got); (err != nil) != c.refused || (c.refused && !errors.Is(err, ErrUnrepresentable)) {
			t.Errorf("%s: the coercion of the handed form %#v = %v; want refused = %v", c.name, got, err, c.refused)
		}
	}
	for _, c := range []struct {
		name string
		in   json.Number
		c    schema.Constraint
	}{
		{"a wide integer literal at an Integer", "99999999999999999999", integer},
		{"a wide integer literal undeclared", "99999999999999999999", nil},
		{"a literal no finite float64 holds at a Float", "1e400", float},
		{"a literal no finite float64 holds at a String", "-1e400", text},
	} {
		if got, err := heldForm(c.in, c.c); !errors.Is(err, errUnheldNumber) {
			t.Errorf("%s: heldForm = %#v, %v; want errUnheldNumber", c.name, got, err)
		}
	}
	if got, err := heldForm(json.Number("0.5"), integer); err == nil {
		if _, err := coerceValue(integer, got); err == nil {
			t.Errorf("a float literal at an Integer reached the driver as %#v", got)
		}
	}
}

// A list or a map whose element changes form is copied, so the snapshot's own
// value is never rewritten.
func TestHeldForm_ARewrittenElementLeavesTheHeldValueAsItWas(t *testing.T) {
	t.Parallel()
	const five = json.Number("5")
	list := []any{"a", five}
	m := map[string]any{"k": five}
	if got, err := heldForm(list, nil); err != nil || !reflect.DeepEqual(got, []any{"a", int64(5)}) {
		t.Errorf("heldForm(list) = %#v, %v", got, err)
	}
	if got, err := heldForm(m, nil); err != nil || !reflect.DeepEqual(got, map[string]any{"k": int64(5)}) {
		t.Errorf("heldForm(map) = %#v, %v", got, err)
	}
	if list[1] != five || m["k"] != five {
		t.Errorf("the held values were rewritten: %#v, %#v", list, m)
	}
}

// A constraint reads through its aliases: a number at a DataType over a Float is the
// float the Float reads.
func TestHeldForm_AnAliasReadsAsTheKindItNames(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "alias"

type Ratio = Float

type Holder {
	id String primary
	ratio Ratio
}
`, "alias.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	holder, _ := s.Type("Holder")
	ratio, _ := holder.Property("ratio")
	if got, err := heldForm(json.Number("5"), ratio.Constraint()); err != nil || got != 5.0 {
		t.Errorf("an integer literal at a Ratio: heldForm = %#v, %v; want the float", got, err)
	}
}

// A key is coerced in its form, as a property is: a number literal at an Integer
// key is sent as the integer, and a Timestamp's text as the instant.
func TestExtractKeyProps_AKeyIsCoercedInItsForm(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	shape := &nodeShape{label: "Holder", primaryKeys: []string{"n", "at"}, keyConstraints: map[string]schema.Constraint{
		"n":  schema.NewIntegerConstraint(),
		"at": schema.NewTimestampConstraint(),
	}}
	keys, err := extractKeyProps(immutable.WrapProperties(map[string]any{"n": json.Number("7"), "at": "2024-01-02T03:04:05Z"}), shape)
	if err != nil {
		t.Fatalf("extractKeyProps: %v", err)
	}
	if got, ok := keys["at"].(time.Time); !ok || !got.Equal(at) || keys["n"] != int64(7) {
		t.Errorf("keys = %#v, want 7 and the instant %v", keys, at)
	}
}
