package instance_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/instance"
)

// CanonicalValue is the stored-form rule: for every kind, its result is the
// concrete Go value the validator stores — pinned here as literals, so an
// arm that changed its form turns a row red rather than moving both sides.
func TestCanonicalValue_IsWhatTheValidatorStores(t *testing.T) {
	s := loadSrc(t, `schema "p"

type Named = String

type T {
    id String primary
    n Integer
    f Float
    b Boolean
    s Named
    ts Timestamp
    d Date
    u UUID
    xs List<Integer>
    vec Vector[2]
}
`)
	type carrier string
	type flag bool
	rows := map[string]struct {
		raw  any
		want any
	}{
		"n":   {int32(5), int64(5)},
		"f":   {int(7), float64(7)},
		"b":   {flag(true), true},
		"s":   {carrier("v"), "v"},
		"ts":  {time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), "2020-01-02T03:04:05Z"},
		"d":   {time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC), "2020-01-02"},
		"u":   {"550E8400-E29B-41D4-A716-446655440000", "550e8400-e29b-41d4-a716-446655440000"},
		"xs":  {[]any{int8(1), int16(2)}, []any{int64(1), int64(2)}},
		"vec": {[]any{1, 2.5}, []float64{1, 2.5}},
	}
	input := map[string]any{"id": "t"}
	for name, row := range rows {
		input[name] = row.raw
	}
	inst, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: input})
	if !res.OK() {
		t.Fatal(res)
	}
	typ, _ := s.Type("T")
	for name, row := range rows {
		t.Run(name, func(t *testing.T) {
			prop, _ := typ.Property(name)
			got, err := instance.CanonicalValue(row.raw, prop.Constraint())
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, row.want) {
				t.Errorf("CanonicalValue(%#v) = %#v (%T), want %#v (%T)", row.raw, got, got, row.want, row.want)
			}
			// The validator stores the same form.
			stored, _ := inst.Property(name)
			var storedGo any
			if sl, ok := stored.Slice(); ok {
				storedGo = sl.Clone()
			} else {
				storedGo = stored.Unwrap()
			}
			if !reflect.DeepEqual(storedGo, toAnyForm(row.want)) {
				t.Errorf("stored %s = %#v, want %#v", name, storedGo, row.want)
			}
		})
	}
}

// toAnyForm is the []any shape a stored slice clones back to.
func toAnyForm(v any) any {
	if fs, ok := v.([]float64); ok {
		out := make([]any, len(fs))
		for i, f := range fs {
			out[i] = f
		}
		return out
	}
	return v
}
