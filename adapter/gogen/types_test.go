package gogen

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestGoBaseType(t *testing.T) {
	cases := []struct {
		name string
		c    schema.Constraint
		want string
	}{
		{"string", schema.NewStringConstraint(), "string"},
		{"integer", schema.NewIntegerConstraint(), "int64"},
		{"float", schema.NewFloatConstraint(), "float64"},
		{"boolean", schema.NewBooleanConstraint(), "bool"},
		{"timestamp", schema.NewTimestampConstraint(), "time.Time"},
		{"date", schema.NewDateConstraint(), "time.Time"},
		{"uuid", schema.NewUUIDConstraint(), "string"},
		{"enum", schema.NewEnumConstraint([]string{"a", "b"}), "string"},
		{"pattern", schema.NewPatternConstraint(nil), "string"},
		{"vector", schema.NewVectorConstraint(8), "[]float64"},
		{"list_float", schema.NewListConstraint(schema.NewFloatConstraint()), "[]float64"},
		{"alias", schema.NewAliasConstraint("FIPS", schema.NewStringConstraint()), "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := goBaseType(tc.c)
			if err != nil {
				t.Fatalf("goBaseType: %v", err)
			}
			if got != tc.want {
				t.Errorf("goBaseType(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}
