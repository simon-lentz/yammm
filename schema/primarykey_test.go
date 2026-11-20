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
		{"String", newStringConstraint(), true},
		{"UUID", newUUIDConstraint(), true},
		{"Date", newDateConstraint(), true},
		{"Timestamp", newTimestampConstraint(), true},
		// Disallowed kinds.
		{"Integer", newIntegerConstraint(), false},
		{"Float", newFloatConstraint(), false},
		{"Boolean", newBooleanConstraint(), false},
		{"Enum", parsedEnumConstraint([]string{"a", "b"}), false},
		{"Pattern", PatternConstraint{}, false},
		{"Vector", newVectorConstraint(3), false},
		{"List", newListConstraint(newStringConstraint()), false},
		// An alias unwraps to its resolved kind; an unresolved alias is not allowed.
		{"Alias->String", newAliasConstraint("X", newStringConstraint()), true},
		{"Alias->Integer", newAliasConstraint("X", newIntegerConstraint()), false},
		{"Alias-unresolved", newAliasConstraint("X", nil), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isPrimaryKeyAllowed(tc.c); got != tc.want {
				t.Errorf("isPrimaryKeyAllowed(%s) = %v, want %v", tc.name, got, tc.want)
			}
		})
	}
}
