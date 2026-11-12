package gogen

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

func TestGoBaseType(t *testing.T) {
	g := &generator{}
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
		{"date", schema.NewDateConstraint(), "Date"},
		{"uuid", schema.NewUUIDConstraint(), "string"},
		{"enum", schema.NewEnumConstraint([]string{"a", "b"}), "string"},
		{"pattern", schema.NewPatternConstraint(nil), "string"},
		{"vector", schema.NewVectorConstraint(8), "[]float64"},
		{"list_float", schema.NewListConstraint(schema.NewFloatConstraint()), "[]float64"},
		{"list_date", schema.NewListConstraint(schema.NewDateConstraint()), "[]Date"},
		{"list_timestamp", schema.NewListConstraint(schema.NewTimestampConstraint()), "[]time.Time"},
		{"alias", schema.NewAliasConstraint("FIPS", schema.NewStringConstraint()), "string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := g.goBaseType(tc.c)
			if err != nil {
				t.Fatalf("goBaseType: %v", err)
			}
			if got != tc.want {
				t.Errorf("goBaseType(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestGoBaseType_ATemporalNameDeclaresWhatItNeeds pins that the one place that
// writes a temporal name records what the file must declare for it: the time
// import for a Timestamp, and the Date type for a Date.
func TestGoBaseType_ATemporalNameDeclaresWhatItNeeds(t *testing.T) {
	for name, tc := range map[string]struct {
		c                    schema.Constraint
		needsTime, needsDate bool
	}{
		"string":            {schema.NewStringConstraint(), false, false},
		"timestamp":         {schema.NewTimestampConstraint(), true, false},
		"list of timestamp": {schema.NewListConstraint(schema.NewTimestampConstraint()), true, false},
		"date":              {schema.NewDateConstraint(), false, true},
		"list of date":      {schema.NewListConstraint(schema.NewDateConstraint()), false, true},
		"alias of date":     {schema.NewAliasConstraint("Day", schema.NewDateConstraint()), false, true},
	} {
		g := &generator{}
		if _, err := g.goBaseType(tc.c); err != nil {
			t.Fatalf("%s: goBaseType: %v", name, err)
		}
		if g.needsTime != tc.needsTime || g.needsDate != tc.needsDate {
			t.Errorf("%s: needsTime %t, needsDate %t; want %t, %t", name, g.needsTime, g.needsDate, tc.needsTime, tc.needsDate)
		}
	}
}

// TestGoFieldType_UnregisteredListElementDataTypeIsAnError pins that a List
// property whose element names a DataType the pre-pass did not record fails,
// as the scalar DataType position does, rather than degrading to the
// primitive.
func TestGoFieldType_UnregisteredListElementDataTypeIsAnError(t *testing.T) {
	g, err := newGenerator(loadFixture(t, "named"))
	if err != nil {
		t.Fatal(err)
	}
	county, ok := g.schema.Type("County")
	if !ok {
		t.Fatal("the fixture declares no County")
	}
	codes, ok := county.Property("codes")
	if !ok {
		t.Fatal("County declares no codes")
	}
	if _, err := g.goFieldType(g.typeOwner(county), codes); err == nil {
		t.Error("goFieldType over an unregistered List<FipsCode> returned no error")
	}
}
