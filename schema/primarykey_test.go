package schema

import "testing"

// TestIsPrimaryKeyAllowed pins the per-kind primary-key eligibility decision. The
// exhaustiveness guard on isPrimaryKeyAllowed proves every kind is listed; this test
// proves each kind returns the correct true/false, so a future kind addition forces a
// deliberate PK decision rather than defaulting silently.
func TestIsPrimaryKeyAllowed(t *testing.T) {
	cases := []struct {
		name string
		c    Constraint
		want bool
	}{
		// Allowed PK kinds.
		{"String", NewStringConstraint(), true},
		{"UUID", NewUUIDConstraint(), true},
		{"Date", NewDateConstraint(), true},
		{"Timestamp", NewTimestampConstraint(), true},
		// Disallowed kinds.
		{"Integer", NewIntegerConstraint(), false},
		{"Float", NewFloatConstraint(), false},
		{"Boolean", NewBooleanConstraint(), false},
		{"Enum", NewEnumConstraint([]string{"a", "b"}), false},
		{"Pattern", NewPatternConstraint(nil), false},
		{"Vector", NewVectorConstraint(3), false},
		{"List", NewListConstraint(NewStringConstraint()), false},
		// An alias unwraps to its resolved kind; an unresolved alias and nil are not allowed.
		{"Alias->String", NewAliasConstraint("X", NewStringConstraint()), true},
		{"Alias->Integer", NewAliasConstraint("X", NewIntegerConstraint()), false},
		{"Alias-unresolved", NewAliasConstraint("X", nil), false},
		{"nil", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPrimaryKeyAllowed(tc.c); got != tc.want {
				t.Errorf("isPrimaryKeyAllowed(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
