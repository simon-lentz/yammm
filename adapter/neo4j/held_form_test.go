package neo4j

import (
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/schema"
)

func noConstraint(string) schema.Constraint { return nil }

// The check on an edge's properties allocates nothing when every value is
// already in its form, so BatchEdgeQueries pays nothing per edge for it.
func TestFormProps_SoundPropertiesAllocateNothing(t *testing.T) {
	raceskip.Skip(t)
	props := map[string]any{
		"a": "x",
		"b": []any{"y", "�"},
		"c": map[string]any{"k": "v"},
		"d": int64(1),
		"e": nil,
	}
	if allocs := testing.AllocsPerRun(100, func() {
		if k, err := formProps(props, noConstraint); err != nil {
			t.Fatalf("sound properties refused at %q: %v", k, err)
		}
	}); allocs != 0 {
		t.Errorf("formProps allocates %v times over sound properties; want 0", allocs)
	}
}

// Of several refused properties, the first in sorted order is named, on every
// run.
func TestFormProps_NamesTheFirstRefusedKeyInSortedOrder(t *testing.T) {
	t.Parallel()
	props := map[string]any{"z": "a\xffb", "m": []any{"ok", "c\xfe"}, "a": "fine", "q": map[string]any{"k\xff": "v"}}
	for range 50 {
		if k, err := formProps(props, noConstraint); !errors.Is(err, errInvalidText) || k != "m" {
			t.Fatalf("formProps = %q, %v; want \"m\" and errInvalidText", k, err)
		}
	}
}

type textNode struct {
	Next *textNode
	S    string
}

// A value that is no scalar, list or string-keyed map is refused as one of no
// form, whatever text it holds; one that refers to itself is among them.
func TestHeldForm_AValueOfNoFormIsRefusedAsSuch(t *testing.T) {
	t.Parallel()
	n := &textNode{S: "ok"}
	n.Next = n
	for name, v := range map[string]any{
		"a struct":                      struct{ S string }{"ok"},
		"an array":                      [1]string{"ok"},
		"a map keyed by an integer":     map[int]string{1: "ok"},
		"a value that refers to itself": n,
		"a list holding a struct":       []any{struct{ S string }{"ok"}},
	} {
		if _, err := heldForm(v, nil); !errors.Is(err, errNoForm) {
			t.Errorf("%s: heldForm = %v; want errNoForm", name, err)
		}
	}
	looped := []any{"ok", nil}
	looped[1] = looped
	if _, err := heldForm(looped, nil); !errors.Is(err, errNoForm) {
		t.Errorf("a list that holds itself: heldForm = %v; want errNoForm", err)
	}
}

type namedText string

// The driver is handed each value in its one form: a carrier as the scalar
// it carries, a named type as its base value, and a uuid.UUID its constraint
// renders as its text.
func TestHeldForm_EachValueIsHandedInItsForm(t *testing.T) {
	t.Parallel()
	f := 5.0
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	for _, c := range []struct {
		name string
		in   any
		c    schema.Constraint
		want any
	}{
		{"a uintptr", uintptr(7), schema.NewIntegerConstraint(), uint64(7)},
		{"a pointer to a float", &f, schema.NewFloatConstraint(), 5.0},
		{"a pointer to a time at a Timestamp", &at, schema.NewTimestampConstraint(), at},
		{"a time at a Date", at, schema.NewDateConstraint(), at},
		{"a named string", namedText("x"), schema.NewStringConstraint(), "x"},
		{"a uuid at a UUID", id, schema.NewUUIDConstraint(), id.String()},
		{"a uuid in a list of UUIDs", []any{id}, schema.NewListConstraint(schema.NewUUIDConstraint()), []any{id.String()}},
	} {
		got, err := heldForm(c.in, c.c)
		if err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: heldForm = %#v, %v; want %#v", c.name, got, err, c.want)
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
		name string
		in   any
		c    schema.Constraint
		want any
	}{
		{"an integer literal at an Integer", json.Number("5"), integer, int64(5)},
		{"a pointer to one", new(json.Number("-7")), integer, int64(-7)},
		{"an integer literal at a Float", json.Number("5"), float, 5.0},
		{"-0 at a Float", json.Number("-0"), float, negZero},
		{"a float literal at a Float", json.Number("0.5"), float, 0.5},
		{"a wide integer literal at a Float", json.Number("99999999999999999999"), float, 1e20},
		{"an integer literal at a String", json.Number("5"), text, int64(5)},
		{"an integer literal undeclared", json.Number("5"), nil, int64(5)},
		{"-0 in a Vector", []any{json.Number("-0"), json.Number("1")}, schema.NewVectorConstraint(2), []any{negZero, 1.0}},
		{"an integer literal in a List<Integer>", []any{json.Number("3")}, schema.NewListConstraint(integer), []any{int64(3)}},
	} {
		got, err := heldForm(c.in, c.c)
		if err != nil || !reflect.DeepEqual(got, c.want) || (c.want == negZero && !math.Signbit(got.(float64))) {
			t.Errorf("%s: heldForm = %#v, %v; want %#v", c.name, got, err, c.want)
			continue
		}
		if c.c == nil {
			continue
		}
		if _, err := coerceValue(c.c, got); err != nil {
			t.Errorf("%s: the coercion refuses the handed form %#v: %v", c.name, got, err)
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

// A time or a UUID is handed on only where its constraint renders it, as every
// other writer of this module writes it.
func TestHeldForm_ATimeOrAUUIDItsConstraintDoesNotRenderIsRefused(t *testing.T) {
	t.Parallel()
	at := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	for _, c := range []struct {
		name string
		in   any
		c    schema.Constraint
	}{
		{"a time at a String", at, schema.NewStringConstraint()},
		{"a time undeclared", at, nil},
		{"a time inside a map", map[string]any{"k": at}, nil},
		{"a uuid at a String", id, schema.NewStringConstraint()},
		{"a uuid at a Timestamp", id, schema.NewTimestampConstraint()},
		{"a time at a UUID", at, schema.NewUUIDConstraint()},
	} {
		if _, err := heldForm(c.in, c.c); !errors.Is(err, errUnrendered) {
			t.Errorf("%s: heldForm = %v; want errUnrendered", c.name, err)
		}
	}
}

// A list or a map whose element changes form is copied, so the snapshot's own
// value is never rewritten.
func TestHeldForm_ARewrittenElementLeavesTheHeldValueAsItWas(t *testing.T) {
	t.Parallel()
	list := []any{"a", namedText("b")}
	m := map[string]any{"k": namedText("v")}
	if got, err := heldForm(list, nil); err != nil || !reflect.DeepEqual(got, []any{"a", "b"}) {
		t.Errorf("heldForm(list) = %#v, %v", got, err)
	}
	if got, err := heldForm(m, nil); err != nil || !reflect.DeepEqual(got, map[string]any{"k": "v"}) {
		t.Errorf("heldForm(map) = %#v, %v", got, err)
	}
	if list[1] != namedText("b") || m["k"] != namedText("v") {
		t.Errorf("the held values were rewritten: %#v, %#v", list, m)
	}
}

// A constraint renders through its aliases: a DataType over a Timestamp
// renders a time, and one over a UUID renders a uuid.UUID.
func TestHeldForm_AnAliasRendersAsTheKindItNames(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), `schema "alias"

type Stamp = Timestamp
type Ident = UUID

type Holder {
	id String primary
	at Stamp
	uid Ident
}
`, "alias.yammm")
	if res.HasErrors() {
		t.Fatalf("load schema: %s", res)
	}
	holder, _ := s.Type("Holder")
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	atProp, _ := holder.Property("at")
	uidProp, _ := holder.Property("uid")
	if got, err := heldForm(at, atProp.Constraint()); err != nil || got != at {
		t.Errorf("a time at a Stamp: heldForm = %#v, %v; want the time", got, err)
	}
	if got, err := heldForm(id, uidProp.Constraint()); err != nil || got != id.String() {
		t.Errorf("a uuid at an Ident: heldForm = %#v, %v; want its text", got, err)
	}
}

// A key is coerced in its form, as a property is: a uintptr at an Integer key
// is sent as the integer, and a pointer to a time at a Timestamp key as the
// instant.
func TestExtractKeyProps_AKeyIsCoercedInItsForm(t *testing.T) {
	t.Parallel()
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	shape := &NodeShape{Label: "Holder", PrimaryKeys: []string{"n", "at"}, keyConstraints: map[string]schema.Constraint{
		"n":  schema.NewIntegerConstraint(),
		"at": schema.NewTimestampConstraint(),
	}}
	keys, err := extractKeyProps(immutable.WrapProperties(map[string]any{"n": uintptr(7), "at": &at}), shape)
	if err != nil {
		t.Fatalf("extractKeyProps: %v", err)
	}
	if want := map[string]any{"n": int64(7), "at": at}; !reflect.DeepEqual(keys, want) {
		t.Errorf("keys = %#v, want %#v", keys, want)
	}
}
