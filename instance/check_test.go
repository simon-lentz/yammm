package instance_test

import (
	"errors"
	"reflect"
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

// TestCheckValue_RefusesAConstraintCheckConstraintRefuses pins the judgement
// CheckValue and CanonicalValue make before any value is read: a constraint
// schema.CheckConstraint refuses is an error whatever the value, nil
// included, and never an internal failure. A List with no element, a nil
// pointer as its element, a reference resolved to another reference alone,
// inverted bounds and a List of a reference to no constraint each answer so.
func TestCheckValue_RefusesAConstraintCheckConstraintRefuses(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]schema.Constraint{
		"a List with no element":                    schema.ListConstraint{},
		"a List whose element is a nil pointer":     schema.NewListConstraint((*schema.StringConstraint)(nil)),
		"a reference resolved to another reference": schema.NewAliasConstraint("A", schema.NewAliasConstraint("B", schema.NewStringConstraint())),
		"inverted bounds":                           schema.IntegerBetween(5, 1),
		"a List of a reference resolved to nothing": schema.NewListConstraint(schema.NewAliasConstraint("A", nil)),
	} {
		want := schema.CheckConstraint(c)
		if want == nil {
			t.Fatalf("%s: CheckConstraint accepts it, so the row judges nothing", name)
		}
		for _, val := range []any{[]any{"x"}, "x", int64(3), nil} {
			err := instance.CheckValue(val, c)
			if err == nil || err.Error() != "instance: "+want.Error() {
				t.Errorf("%s: CheckValue(%v) = %v, want the refusal %q", name, val, err, want)
			}
			if errors.Is(err, instance.ErrInternalFailure) {
				t.Errorf("%s: CheckValue(%v) = %v, an internal failure", name, val, err)
			}
			got, err := instance.CanonicalValue(val, c)
			if err == nil || err.Error() != "instance: "+want.Error() {
				t.Errorf("%s: CanonicalValue(%v) = %v, want the refusal %q", name, val, err, want)
			}
			if errors.Is(err, instance.ErrInternalFailure) {
				t.Errorf("%s: CanonicalValue(%v) = %v, an internal failure", name, val, err)
			}
			if !reflect.DeepEqual(got, val) {
				t.Errorf("%s: CanonicalValue(%v) returned %v, want the value unchanged", name, val, got)
			}
		}
	}
}

// A Pattern means regexp.Compile of its source, as a .yammm source compiles
// it. A regexp built under POSIX reads ^ and $ at line breaks, so without that
// reading the value below would pass a hand-built constraint the DSL's
// Pattern["^b$"] refuses.
func TestCheckValue_ReadsAPatternAsItsSourceCompiles(t *testing.T) {
	t.Parallel()
	src := `schema "s"

type T {
	id String primary
	p Pattern["^b$"]
}
`
	s, res := schema.LoadString(t.Context(), src, "s.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	tt, _ := s.Type("T")
	p, _ := tt.Property("p")
	loaded := instance.CheckValue("a\nb", p.Constraint())
	built := instance.CheckValue("a\nb", schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX(`^b$`)}))
	if loaded == nil || built == nil {
		t.Errorf("CheckValue(%q) = %v from the DSL and %v from CompilePOSIX; want both refused", "a\nb", loaded, built)
	}
}
