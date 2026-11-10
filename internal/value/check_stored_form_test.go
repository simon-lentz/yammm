package value_test

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema"
)

// The check and the coercion read a list position in the stored form: an
// immutable.Slice, which a List or Vector property unwraps to off a validated
// instance, is a list to both, as it is to Canonical. An array is not.
func TestCheckAndCoerce_ReadAStoredList(t *testing.T) {
	t.Parallel()
	lc := schema.NewListConstraint(schema.NewStringConstraint())
	vc := schema.NewVectorConstraint(2)
	for _, c := range []struct {
		name string
		in   []any
		c    schema.Constraint
		want any
	}{
		{"a List", []any{"a", "b"}, lc, []any{"a", "b"}},
		{"a Vector", []any{1.5, 2.5}, vc, []float64{1.5, 2.5}},
	} {
		in := immutable.Wrap(c.in).Unwrap()
		if _, ok := in.(immutable.Slice); !ok {
			t.Fatalf("fixture: Wrap([]any).Unwrap() is %T, want immutable.Slice", in)
		}
		if err := value.Check(in, c.c); err != nil {
			t.Errorf("Check(immutable.Slice) at %s = %v, want nil", c.name, err)
		}
		if got, err := value.Coerce(in, c.c); err != nil || !reflect.DeepEqual(got, c.want) {
			t.Errorf("Coerce(immutable.Slice) at %s = %#v, %v; want %#v", c.name, got, err, c.want)
		}
	}
	// A fixed-size array is how a scalar is spelled (uuid.UUID is [16]byte); no
	// decoder produces one for a list position.
	if _, err := value.Coerce([2]string{"a", "b"}, lc); err == nil {
		t.Error("Coerce([2]string) under List accepted an array as a list")
	}
}

// A named type over a number, schema/gogen's shape for a DataType over int64,
// float64 or float32, is checked and coerced at Integer, Float and Vector as
// its base value is, and the stored form is the base.
func TestCheckAndCoerce_ANamedNumberIsItsBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   any
		c    schema.Constraint
		want any
	}{
		{"a named int64 at Integer", namedInt64(42), schema.NewIntegerConstraint(), int64(42)},
		{"a named float64 at Float", namedFloat64(2.5), schema.NewFloatConstraint(), 2.5},
		{"a named float32 at Float", namedFloat32(1.5), schema.NewFloatConstraint(), 1.5},
		{"a named int64 at Float", namedInt64(3), schema.NewFloatConstraint(), 3.0},
		{"a named uint at Integer", namedUint(5), schema.NewIntegerConstraint(), int64(5)},
		{"a named uint at Float", namedUint(5), schema.NewFloatConstraint(), 5.0},
		{"named numbers at Vector", []any{namedFloat64(1.0), namedInt64(2), namedFloat32(3.0)}, schema.NewVectorConstraint(3), []float64{1, 2, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if err := value.Check(tc.in, tc.c); err != nil {
				t.Errorf("Check(%T) = %v, want nil", tc.in, err)
			}
			got, err := value.Coerce(tc.in, tc.c)
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("Coerce(%T) = %#v (%T), %v; want %#v", tc.in, got, got, err, tc.want)
			}
		})
	}
}

// One rule for every kind held as text: a named string reads at Date and
// Timestamp as it does at String, Enum, Pattern and UUID, and the stored form
// is the base string.
func TestCheckAndCoerce_ANamedStringReadsAtATemporalKind(t *testing.T) {
	t.Parallel()
	d, ts := namedString("2030-01-01"), namedString("2030-01-01T00:00:00Z")
	if err := value.Check(d, schema.NewDateConstraint()); err != nil {
		t.Errorf("Check(a named string, Date) = %v", err)
	}
	if err := value.Check(ts, schema.NewTimestampConstraint()); err != nil {
		t.Errorf("Check(a named string, Timestamp) = %v", err)
	}
	if got, err := value.Coerce(d, schema.NewDateConstraint()); err != nil || got != "2030-01-01" {
		t.Errorf("Coerce(a named string, Date) = %#v, %v; want the base string", got, err)
	}
	if got, err := value.Coerce(ts, schema.NewTimestampConstraint()); err != nil || got != "2030-01-01T00:00:00Z" {
		t.Errorf("Coerce(a named string, Timestamp) = %#v, %v; want the base string", got, err)
	}
}

// Every Coerce arm returns (nil, error) on a value of the wrong kind, the
// temporal and UUID arm included: a wrong-typed value is never reported as its
// own stored form.
func TestCoerce_EveryArmRefusesTheWrongKind(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		val  any
		c    schema.Constraint
	}{
		{"a struct at Timestamp", struct{ X int }{1}, schema.NewTimestampConstraint()},
		{"a struct at Date", struct{ X int }{1}, schema.NewDateConstraint()},
		{"a struct at UUID", struct{ X int }{1}, schema.NewUUIDConstraint()},
		{"a struct at String", struct{ X int }{1}, schema.NewStringConstraint()},
		{"a struct at Integer", struct{ X int }{1}, schema.NewIntegerConstraint()},
		{"an int at String", 42, schema.NewStringConstraint()},
		{"a string at Boolean", "true", schema.NewBooleanConstraint()},
		{"an int at Enum", 7, schema.NewEnumConstraint([]string{"a"})},
		{"an int at Pattern", 7, schema.NewPatternConstraint(nil)},
		{"a json.Number at String", json.Number("42"), schema.NewStringConstraint()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got, err := value.Coerce(tc.val, tc.c); err == nil || got != nil {
				t.Errorf("Coerce(%#v) = %#v, %v; want nil and an error", tc.val, got, err)
			}
		})
	}
	if got, err := value.Coerce(namedString("x"), schema.NewStringConstraint()); err != nil || got != "x" {
		t.Errorf("a named string must coerce to its base: got %#v, %v", got, err)
	}
	if got, err := value.Coerce(namedBool(true), schema.NewBooleanConstraint()); err != nil || got != true {
		t.Errorf("a named bool must coerce to its base: got %#v, %v", got, err)
	}
}

// A value that holds none passes the check and coerces to nil at every kind:
// nil, a nil slice and a nil map.
func TestCheckAndCoerce_AValueThatHoldsNoneIsNil(t *testing.T) {
	t.Parallel()
	for _, c := range []schema.Constraint{
		schema.NewStringConstraint(), schema.NewIntegerConstraint(), schema.NewTimestampConstraint(),
		schema.NewVectorConstraint(2), schema.NewListConstraint(schema.NewStringConstraint()),
	} {
		for _, v := range []any{nil, []any(nil), []string(nil), map[string]any(nil), immutable.Wrap([]any(nil)).Unwrap()} {
			if err := value.Check(v, c); err != nil {
				t.Errorf("Check(%#v) at %s = %v; want nil", v, c.Kind(), err)
			}
			if got, err := value.Coerce(v, c); err != nil || got != nil {
				t.Errorf("Coerce(%#v) at %s = %#v, %v; want nil, nil", v, c.Kind(), got, err)
			}
		}
	}
}
