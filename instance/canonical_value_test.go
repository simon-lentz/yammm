package instance_test

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// CanonicalValue is the stored-form rule: for every kind, what it returns is
// what the validator stores for the same input.
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
	input := map[string]any{
		"id": "t", "n": int32(5), "f": int(7), "b": flag(true), "s": carrier("v"),
		"ts": time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC), "d": time.Date(2020, 1, 2, 0, 0, 0, 0, time.UTC),
		"u": "550E8400-E29B-41D4-A716-446655440000", "xs": []any{int8(1), int16(2)}, "vec": []any{1, 2.5},
	}
	inst, res := instance.NewValidator(s).ValidateOne(t.Context(), "T", instance.RawInstance{Properties: input})
	if !res.OK() {
		t.Fatal(res)
	}
	typ, _ := s.Type("T")
	for name, raw := range input {
		prop, ok := typ.Property(name)
		if !ok {
			t.Fatalf("no property %s", name)
		}
		stored, ok := inst.Property(name)
		if !ok {
			t.Fatalf("%s not stored", name)
		}
		got, err := instance.CanonicalValue(raw, prop.Constraint())
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		// The validator hands its stored value to immutable.WrapProperties;
		// wrapping CanonicalValue's result the same way compares the two Go
		// values, container kinds included.
		yammmtest.Diff(t, stored.Unwrap(), immutable.Wrap(got, immutable.WithClone(true)).Unwrap(),
			cmp.AllowUnexported(immutable.Value{}, immutable.Slice{}))
	}
}
