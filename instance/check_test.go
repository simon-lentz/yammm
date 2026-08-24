package instance_test

import (
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
)

// TestCheckValue_AppliesTheValidatorsRule pins that the exported check is the
// validator's own per-value rule: kind, bounds, enum membership, pattern and
// alias resolution, with nil accepted at this altitude.
func TestCheckValue_AppliesTheValidatorsRule(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		val     any
		c       schema.Constraint
		wantErr string // "" means valid
	}{
		{"integer within bounds", int64(5), schema.IntegerBetween(0, 10), ""},
		{"integer above bound", int64(11), schema.IntegerBetween(0, 10), "10"},
		{"wrong kind", "x", schema.NewIntegerConstraint(), "integer"},
		{"enum member", "a", schema.NewEnumConstraint([]string{"a", "b"}), ""},
		{"enum outsider", "c", schema.NewEnumConstraint([]string{"a", "b"}), "c"},
		{"pattern match", "AB", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[A-Z]+$`)}), ""},
		{"pattern miss", "ab", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile(`^[A-Z]+$`)}), "pattern"},
		{"alias resolves", int64(50), schema.NewAliasConstraint("Score", schema.IntegerBetween(0, 100)), ""},
		{"alias rejects", int64(500), schema.NewAliasConstraint("Score", schema.IntegerBetween(0, 100)), "100"},
		{"nil value is valid", nil, schema.IntegerBetween(0, 10), ""},
		{"nil constraint is valid", int64(5), nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := instance.CheckValue(tc.val, tc.c)
			switch {
			case tc.wantErr == "" && err != nil:
				t.Errorf("CheckValue(%v) = %v, want nil", tc.val, err)
			case tc.wantErr != "" && err == nil:
				t.Errorf("CheckValue(%v) = nil, want an error naming %q", tc.val, tc.wantErr)
			case tc.wantErr != "" && !strings.Contains(err.Error(), tc.wantErr):
				t.Errorf("CheckValue(%v) = %v, want an error naming %q", tc.val, err, tc.wantErr)
			}
			if err != nil && errors.Is(err, instance.ErrInternalFailure) {
				t.Errorf("a plain violation reported as an internal failure: %v", err)
			}
		})
	}
}

// TestCheckValue_RecoversAPanicIntoAnInternalError pins the recovery the
// validator applies to every check. The sealed Constraint interface offers no
// checker that panics on purpose, so the lever is a malformed constraint: a
// zero ListConstraint carries a nil element, and the element check
// dereferences it.
func TestCheckValue_RecoversAPanicIntoAnInternalError(t *testing.T) {
	t.Parallel()
	err := instance.CheckValue([]any{"x"}, schema.ListConstraint{})
	if err == nil {
		t.Fatal("expected the malformed constraint to surface as an error")
	}
	if !errors.Is(err, instance.ErrInternalFailure) {
		t.Errorf("error does not match ErrInternalFailure: %v", err)
	}
	internal, ok := errors.AsType[*instance.InternalError](err)
	if !ok {
		t.Fatalf("error is %T, want *instance.InternalError", err)
	}
	if internal.Kind != instance.KindConstraintPanic {
		t.Errorf("kind = %v, want KindConstraintPanic", internal.Kind)
	}
}
