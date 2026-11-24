package gogen

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"

	"github.com/simon-lentz/yammm/schema"
)

func TestGoBaseType(t *testing.T) {
	g := &generator{}
	cases := []struct {
		name string
		c    schema.Constraint
		want string
	}{
		{"string", schematest.Constraint(t, "String"), "string"},
		{"integer", schematest.Constraint(t, "Integer"), "int64"},
		{"float", schematest.Constraint(t, "Float"), "float64"},
		{"boolean", schematest.Constraint(t, "Boolean"), "bool"},
		{"timestamp", schematest.Constraint(t, "Timestamp"), "time.Time"},
		{"date", schematest.Constraint(t, "Date"), "Date"},
		{"uuid", schematest.Constraint(t, "UUID"), "string"},
		{"enum", schematest.Constraint(t, `Enum["a", "b"]`), "string"},
		{"pattern", schematest.Constraint(t, `Pattern["a"]`), "string"},
		{"vector", schematest.Constraint(t, "Vector[8]"), "[]float64"},
		{"list_float", schematest.Constraint(t, "List<Float>"), "[]float64"},
		{"list_date", schematest.Constraint(t, "List<Date>"), "[]Date"},
		{"list_timestamp", schematest.Constraint(t, "List<Timestamp>"), "[]time.Time"},
		{"alias", schematest.Constraint(t, "FIPS", "type FIPS = String"), "string"},
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
		"string":            {schematest.Constraint(t, "String"), false, false},
		"timestamp":         {schematest.Constraint(t, "Timestamp"), true, false},
		"list of timestamp": {schematest.Constraint(t, "List<Timestamp>"), true, false},
		"date":              {schematest.Constraint(t, "Date"), false, true},
		"list of date":      {schematest.Constraint(t, "List<Date>"), false, true},
		"alias of date":     {schematest.Constraint(t, "Day", "type Day = Date"), false, true},
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
