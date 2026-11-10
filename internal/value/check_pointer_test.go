package value_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

type selfPointer *selfPointer

// A pointer is no value of any kind, a nil one and one that points to itself
// too: the check refuses it as a type mismatch that names its Go type, and the
// coercion stores nothing for it.
func TestCheck_APointerIsNoValueOfAnyKind(t *testing.T) {
	t.Parallel()
	var self selfPointer
	self = &self
	text, n, f, b := "a", int64(5), 1.5, true
	number := json.Number("5")
	at := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	id := uuid.MustParse("12345678-1234-1234-1234-123456789abc")
	list, vector := []any{"a"}, []float64{1, 2}
	var held any = "a"
	var nilText *string
	var nilTime *time.Time
	pointers := []any{&text, &n, &f, &b, &number, &at, &id, &list, &vector, &held, nilText, nilTime, self}
	for _, c := range []schema.Constraint{
		schema.NewStringConstraint(), schema.NewIntegerConstraint(), schema.NewFloatConstraint(),
		schema.NewBooleanConstraint(), schema.NewTimestampConstraint(), schema.NewDateConstraint(),
		schema.NewUUIDConstraint(), schema.NewEnumConstraint([]string{"a"}),
		schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("a")}),
		schema.NewVectorConstraint(2), schema.NewListConstraint(schema.NewStringConstraint()),
		schema.NewAliasConstraint("Name", schema.NewStringConstraint()),
	} {
		for _, p := range pointers {
			err := value.Check(p, c)
			var ce *value.CheckError
			if !errors.As(err, &ce) || ce.Kind != value.KindTypeMismatch {
				t.Errorf("Check(%T, %s) = %v, want a type mismatch", p, c, err)
			} else if want := fmt.Sprintf("got %T", p); len(ce.Msg) < len(want) || ce.Msg[len(ce.Msg)-len(want):] != want {
				t.Errorf("Check(%T, %s) = %q, want it to end %q", p, c, ce.Msg, want)
			}
			if got, err := value.Coerce(p, c); err == nil || got != nil {
				t.Errorf("Coerce(%T, %s) = %#v, %v; want nil and a refusal", p, c, got, err)
			}
		}
	}
}

// A pointer in a list or a vector is refused at its element, nil or not: no
// element is read through one, and a nil one is no null element.
func TestCheck_APointerElementIsRefusedAtItsIndex(t *testing.T) {
	t.Parallel()
	text, f := "b", 2.5
	var nilText *string
	var nilFloat *float64
	for _, c := range []struct {
		name string
		val  any
		c    schema.Constraint
		want string
	}{
		{"a pointer in a list", []any{"a", &text}, schema.NewListConstraint(schema.NewStringConstraint()), "element [1]: expected string, got *string"},
		{"a nil pointer in a list", []any{"a", nilText}, schema.NewListConstraint(schema.NewStringConstraint()), "element [1]: expected string, got *string"},
		{"a list of pointers", []*string{&text}, schema.NewListConstraint(schema.NewStringConstraint()), "element [0]: expected string, got *string"},
		{"a pointer in a vector", []any{1.5, &f}, schema.NewVectorConstraint(2), "vector element [1]: expected float, got *float64"},
		{"a nil pointer in a vector", []any{1.5, nilFloat}, schema.NewVectorConstraint(2), "vector element [1]: expected float, got *float64"},
	} {
		err := value.Check(c.val, c.c)
		var ce *value.CheckError
		if !errors.As(err, &ce) || ce.Kind != value.KindTypeMismatch || ce.Msg != c.want {
			t.Errorf("%s: Check = %v, want the type mismatch %q", c.name, err, c.want)
		}
		if got, err := value.Coerce(c.val, c.c); err == nil || got != nil {
			t.Errorf("%s: Coerce = %#v, %v; want nil and a refusal", c.name, got, err)
		}
	}
}
