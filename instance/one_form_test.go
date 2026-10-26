package instance_test

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// The validator judges a value as the one form every writer writes it in: for
// each carrier and each scalar kind, CheckValue reads the value as it reads its
// form, and CanonicalValue stores what it stores for the form, so a writer
// never writes a value the validator would judge or store as another.
func TestCheckValue_JudgesAValueAsItsForm(t *testing.T) {
	t.Parallel()
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
		form, hasForm := instance.ScalarOf(c.v)
		for _, k := range []schema.Constraint{
			schema.NewStringConstraint(), schema.NewIntegerConstraint(),
			schema.NewFloatConstraint(), schema.NewBooleanConstraint(),
			schema.NewTimestampConstraint(), schema.NewDateConstraint(), schema.NewUUIDConstraint(),
			schema.NewEnumConstraint([]string{"x", "12"}), schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^[0-9x]+$")}),
		} {
			got := instance.CheckValue(c.v, k) == nil
			want := hasForm && instance.CheckValue(form, k) == nil
			if got != want {
				t.Errorf("%s at %s: CheckValue accepts = %v, its form %#v (%v) = %v", c.name, k.Kind(), got, form, hasForm, want)
			}
			if !got {
				continue
			}
			stored, err := instance.CanonicalValue(c.v, k)
			fromForm, ferr := instance.CanonicalValue(form, k)
			if err != nil || ferr != nil || stored != fromForm {
				t.Errorf("%s at %s: CanonicalValue = %#v, %v; of its form %#v, %v", c.name, k.Kind(), stored, err, fromForm, ferr)
			}
		}
	}
}

// A nil value a carrier holds is a null list or vector element to CheckValue
// and to CanonicalValue, each on its own path, as a bare nil is.
func TestCheckValue_ANilCarrierInAListIsANullElement(t *testing.T) {
	t.Parallel()
	list := schema.NewListConstraint(schema.NewStringConstraint())
	var nilText *string
	var heldNil any
	var heldNilPtr any = nilText
	var nilSlice []string
	for name, elem := range map[string]any{
		"a nil":                            nil,
		"a nil pointer":                    nilText,
		"a nil interface behind a pointer": &heldNil,
		"an interface holding a nil pointer, behind a pointer": &heldNilPtr,
		"a pointer to a nil slice":                             &nilSlice,
		"a nil channel":                                        (chan int)(nil),
	} {
		for c, v := range map[schema.Constraint][]any{list: {"a", elem}, schema.NewVectorConstraint(2): {1.5, elem}} {
			if err := instance.CheckValue(v, c); err == nil || !strings.Contains(err.Error(), "element [1]: expected ") || !strings.Contains(err.Error(), ", got null") {
				t.Errorf("CheckValue with %s as a %s element = %v, want a null element", name, c.Kind(), err)
			}
			if _, err := instance.CanonicalValue(v, c); err == nil || !strings.Contains(err.Error(), "element [1]: expected ") || !strings.Contains(err.Error(), ", got null") {
				t.Errorf("CanonicalValue with %s as a %s element = %v, want a null element", name, c.Kind(), err)
			}
		}
	}
}

// A pointer or an interface that is nil, and a nil value a pointer holds, hold
// no value, as every writer writes them: the Validator reads it as an absent property, so an optional one is
// accepted and stores nothing and a required one is missing, and as a list
// element it is a null element.
func TestValidateOne_ANilCarrierIsAnAbsentValue(t *testing.T) {
	t.Parallel()
	s := loadT(t, "schema \"p\"\n\ntype T {\n\tid String primary\n\tname String\n\tmust String required\n\tns List<String>\n}\n")
	v := instance.NewValidator(s)
	var nilText *string
	var heldNil any
	var heldNilPtr any = nilText
	var nilSlice []string
	for name, carrier := range map[string]any{
		"a nil interface behind a pointer":                     &heldNil,
		"an interface holding a nil pointer, behind a pointer": &heldNilPtr,
		"a pointer to a nil slice":                             &nilSlice,
		"a nil channel":                                        (chan int)(nil),
		"a nil map keyed by no string":                         map[int]int(nil),
	} {
		inst, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "name": carrier}})
		if res.Err() != nil || inst == nil {
			t.Errorf("%s at an optional property: %v", name, codes(res))
		} else if p, ok := inst.Property("name"); ok && !p.IsNil() {
			t.Errorf("%s at an optional property stored %#v", name, p.Unwrap())
		}
		if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": carrier}}); !res.HasCode(diag.E_MISSING_REQUIRED) {
			t.Errorf("%s at a required property: want E_MISSING_REQUIRED, got %v", name, codes(res))
		}
		if _, res := v.ValidateOne(t.Context(), "T", instance.RawInstance{Properties: map[string]any{"id": "x", "must": "m", "ns": []any{"a", carrier}}}); !res.HasCode(diag.E_TYPE_MISMATCH) {
			t.Errorf("%s as a list element: want E_TYPE_MISMATCH, got %v", name, codes(res))
		}
	}
}

// A nil value a carrier holds is named null as an edge target, as a bare nil
// is.
func TestValidateOne_ANilCarrierAsAnEdgeTargetIsNamedNull(t *testing.T) {
	t.Parallel()
	v := instance.NewValidator(fkSchema(t, true, true))
	var heldNil any
	var nilMap map[string]any
	for name, elem := range map[string]any{
		"a nil":                            nil,
		"a nil interface behind a pointer": &heldNil,
		"a pointer to a nil map":           &nilMap,
	} {
		_, res := v.ValidateOne(t.Context(), "Person", instance.RawInstance{Properties: map[string]any{"id": "1", "employer": []any{elem}}})
		if !strings.Contains(res.String(), "expected object for edge target, got null") {
			t.Errorf("%s as an edge target: %s", name, res)
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
