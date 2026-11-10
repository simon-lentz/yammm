package instance_test

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// oneFormSchema declares one optional property of each scalar kind.
const oneFormSchema = `schema "p"

type T {
	id String primary
	s String
	i Integer
	f Float
	b Boolean
	ts Timestamp
	d Date
	u UUID
	e Enum["x", "12"]
	p Pattern["^[0-9x]+$"]
}
`

// The validator judges a value as its one form: at each scalar kind, CheckValue
// accepts a value only when Scalar reads it in a form CheckValue accepts, and
// the validator stores what it stores for the form. A pointer has no form, so
// every kind refuses it.
func TestCheckValue_JudgesAValueAsItsForm(t *testing.T) {
	t.Parallel()
	s := loadT(t, oneFormSchema)
	typ, _ := s.Type("T")
	v := instance.NewValidator(s)
	stored := func(prop string, val any) (any, bool) {
		inst, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", prop: val}})
		if res.Err() != nil {
			t.Errorf("%s = %#v: %s", prop, val, res)
			return nil, false
		}
		p, ok := inst.Property(prop)
		if !ok || p.IsNil() {
			return nil, false
		}
		return p.Unwrap(), true
	}
	type named string
	text := "x"
	number := json.Number("12")
	signed := json.Number("+5")
	day := json.Number("2024-01-02")
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	var nilText *string
	var heldText any = "x"
	var heldNumber any = json.Number("12")
	var heldInt any = int64(5)
	var heldBool any = true
	var heldNamed any = named("x")
	var heldPtr any = &text
	var heldTime any = at
	var heldUUID any = id
	var heldNil any
	var heldNilPtr any = nilText
	var nilSlice []string
	var nilMap map[string]any
	var heldNilSlice any = nilSlice
	for _, c := range []struct {
		name string
		v    any
	}{
		{"a pointer to a string", &text},
		{"a pointer to a json.Number", &number},
		{"a pointer to a json.Number that is no literal", &signed},
		{"a pointer to a json.Number spelling a date", &day},
		{"an interface holding a string, behind a pointer", &heldText},
		{"an interface holding a json.Number, behind a pointer", &heldNumber},
		{"an interface holding an int64, behind a pointer", &heldInt},
		{"an interface holding a bool, behind a pointer", &heldBool},
		{"an interface holding a named string, behind a pointer", &heldNamed},
		{"an interface holding a pointer to a string, behind a pointer", &heldPtr},
		{"a pointer to a time", &at},
		{"an interface holding a time, behind a pointer", &heldTime},
		{"a pointer to a UUID", &id},
		{"an interface holding a UUID, behind a pointer", &heldUUID},
		{"a nil pointer", nilText},
		{"a nil interface behind a pointer", &heldNil},
		{"an interface holding a nil pointer, behind a pointer", &heldNilPtr},
		{"a pointer to a nil slice", &nilSlice},
		{"a pointer to a nil map", &nilMap},
		{"an interface holding a nil slice, behind a pointer", &heldNilSlice},
		{"a nil channel", (chan int)(nil)},
		{"a nil function", (func())(nil)},
		{"a nil slice", nilSlice},
		{"a nil map", nilMap},
		{"a nil map keyed by no string", map[int]int(nil)},
	} {
		form, hasForm := value.Scalar(c.v)
		for _, prop := range []string{"s", "i", "f", "b", "ts", "d", "u", "e", "p"} {
			p, _ := typ.Property(prop)
			k := p.Constraint()
			got := instance.CheckValue(c.v, k) == nil
			want := hasForm && instance.CheckValue(form, k) == nil
			if got != want {
				t.Errorf("%s at %s: CheckValue accepts = %v, its form %#v (%v) = %v", c.name, k.Kind(), got, form, hasForm, want)
			}
			if !got {
				continue
			}
			got1, ok1 := stored(prop, c.v)
			got2, ok2 := stored(prop, form)
			if ok1 != ok2 || got1 != got2 {
				t.Errorf("%s at %s: stored %#v (%v); of its form %#v (%v)", c.name, k.Kind(), got1, ok1, got2, ok2)
			}
		}
	}
}

// A list or vector element that holds none is a null element to CheckValue: nil,
// a nil slice or a nil map. A pointer or a channel is no null element, a nil one
// too: the element's kind refuses it by its Go type.
func TestCheckValue_ANullElementIsAValueThatHoldsNone(t *testing.T) {
	t.Parallel()
	list := schema.NewListConstraint(schema.NewStringConstraint())
	var nilText *string
	var heldNil any
	var nilSlice []string
	elements := func(elem any) map[schema.Constraint][]any {
		return map[schema.Constraint][]any{list: {"a", elem}, schema.NewVectorConstraint(2): {1.5, elem}}
	}
	for name, elem := range map[string]any{
		"a nil":       nil,
		"a nil slice": nilSlice,
		"a nil map":   map[string]any(nil),
	} {
		for c, v := range elements(elem) {
			if err := instance.CheckValue(v, c); err == nil || !strings.Contains(err.Error(), "element [1]: expected ") || !strings.HasSuffix(err.Error(), ", got null") {
				t.Errorf("CheckValue with %s as a %s element = %v, want a null element", name, c.Kind(), err)
			}
		}
	}
	for name, elem := range map[string]any{
		"a nil pointer":                    nilText,
		"a nil interface behind a pointer": &heldNil,
		"a pointer to a nil slice":         &nilSlice,
		"a nil channel":                    (chan int)(nil),
	} {
		for c, v := range elements(elem) {
			want := fmt.Sprintf(", got %T", elem)
			if err := instance.CheckValue(v, c); err == nil || !strings.Contains(err.Error(), "element [1]: expected ") || !strings.HasSuffix(err.Error(), want) {
				t.Errorf("CheckValue with %s as a %s element = %v, want a refusal that ends %q", name, c.Kind(), err, want)
			}
		}
	}
}

// The Validator reads a value that holds none as an absent property: nil, a nil
// slice or a nil map. An optional one is accepted and stores nothing, a required
// one is missing, and as a list element it is a null element.
func TestValidateOne_AValueThatHoldsNoneIsAnAbsentProperty(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tname String\n\tmust String required\n\tns List<String>\n}\n")
	v := instance.NewValidator(s)
	for name, none := range map[string]any{
		"nil":                          nil,
		"a nil slice":                  []string(nil),
		"a nil map":                    map[string]any(nil),
		"a nil map keyed by no string": map[int]int(nil),
		"a nil immutable container":    immutable.WrapProperties(nil),
	} {
		inst, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "name": none}})
		if res.Err() != nil || inst.IsZero() {
			t.Errorf("%s at an optional property: %v", name, codes(res))
		} else if p, ok := inst.Property("name"); ok && !p.IsNil() {
			t.Errorf("%s at an optional property stored %#v", name, p.Unwrap())
		}
		if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": none}}); !res.HasCode(diag.E_MISSING_REQUIRED) {
			t.Errorf("%s at a required property: want E_MISSING_REQUIRED, got %v", name, codes(res))
		}
		if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "ns": []any{"a", none}}}); !res.HasCode(diag.E_TYPE_MISMATCH) {
			t.Errorf("%s as a list element: want E_TYPE_MISMATCH, got %v", name, codes(res))
		}
	}
}

// The Validator refuses a pointer at a property of every kind, nil or not, as a
// type mismatch: a nil pointer at a required property is no missing value, and
// none is stored.
func TestValidateOne_APointerIsATypeMismatchAtEveryProperty(t *testing.T) {
	t.Parallel()
	s := loadT(t, oneFormSchema+"\ntype R {\n\tid String primary\n\tmust String required\n\tv Vector[2]\n\tns List<Integer>\n}\n")
	v := instance.NewValidator(s)
	text, n, f, b := "x", int64(12), 1.5, true
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	var nilText *string
	var held any = "x"
	for name, p := range map[string]any{
		"a pointer to a string":       &text,
		"a pointer to an int64":       &n,
		"a pointer to a float64":      &f,
		"a pointer to a bool":         &b,
		"a pointer to a time":         &at,
		"a pointer to a UUID":         &id,
		"a pointer to an interface":   &held,
		"a nil pointer":               nilText,
		"a nil pointer to a time":     (*time.Time)(nil),
		"a nil pointer to an integer": (*int64)(nil),
	} {
		for _, prop := range []string{"s", "i", "f", "b", "ts", "d", "u", "e", "p"} {
			inst, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", prop: p}})
			if !slices.Equal(codes(res), []string{diag.E_TYPE_MISMATCH.String()}) || !inst.IsZero() {
				t.Errorf("%s at %s: want E_TYPE_MISMATCH alone and no instance, got %v", name, prop, codes(res))
			} else if want := fmt.Sprintf("got %T", p); !strings.Contains(res.String(), want) {
				t.Errorf("%s at %s: %s, want it to name %q", name, prop, res, want)
			}
		}
		for prop, val := range map[string]any{"must": p, "v": []any{1.5, p}, "ns": []any{int64(1), p}} {
			props := map[string]any{"id": "x", "must": "m"}
			props[prop] = val
			if _, res := v.ValidateOne(t.Context(), "R", instance.RawInstance{Properties: props}); !slices.Equal(codes(res), []string{diag.E_TYPE_MISMATCH.String()}) {
				t.Errorf("%s at %s: want E_TYPE_MISMATCH alone, got %v", name, prop, codes(res))
			}
		}
	}
}

// An edge target that holds none is named null, and a pointer is named by its
// Go type, a nil one too: neither is an object.
func TestValidateOne_AnEdgeTargetThatHoldsNoneIsNamedNull(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(fkSchema(t, true, true))
	var heldNil any
	var nilMap map[string]any
	var nilTarget *map[string]any
	for name, c := range map[string]struct {
		elem any
		want string
	}{
		"a nil":                            {nil, "expected object for edge target, got null"},
		"a nil map":                        {nilMap, "expected object for edge target, got null"},
		"a nil slice":                      {[]any(nil), "expected object for edge target, got null"},
		"a nil interface behind a pointer": {&heldNil, "expected object for edge target, got *interface {}"},
		"a pointer to a nil map":           {&nilMap, "expected object for edge target, got *map[string]interface {}"},
		"a nil pointer to a map":           {nilTarget, "expected object for edge target, got *map[string]interface {}"},
		"a pointer to a target":            {&map[string]any{"_target_id": "c1"}, "expected object for edge target, got *map[string]interface {}"},
	} {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "1", "employer": []any{c.elem}}})
		if !res.HasCode(diag.E_EDGE_SHAPE_MISMATCH) || !strings.Contains(res.String(), c.want) {
			t.Errorf("%s as an edge target: %s, want %q", name, res, c.want)
		}
	}
}

// A json.Number is a number to the edge validator's shape message only when
// its text is a JSON number literal, as it is to every other reader.
func TestValidateOne_AnEdgeValueNamesAJSONNumberANumberOnlyWhenALiteral(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(fkSchema(t, true, true))
	for n, want := range map[json.Number]string{"5": "expected array, got number", "+5": "expected array, got json.Number", "": "expected array, got json.Number"} {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "1", "employer": n}})
		if !strings.Contains(res.String(), want) {
			t.Errorf("json.Number %q as an edge value: %s, want %q", n, res, want)
		}
	}
}
