package gogen

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

const wallLayout = "2006-01-02 15:04:05"

func TestGoBaseType(t *testing.T) {
	g := &generator{temporal: temporalTypes{
		date:    dateGoName,
		layouts: map[string]string{wallLayout: "Timestamp20060102150405"},
	}}
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
		{"timestamp_layout", schema.NewTimestampConstraintFormatted(wallLayout), "Timestamp20060102150405"},
		{"date", schema.NewDateConstraint(), "Date"},
		{"uuid", schema.NewUUIDConstraint(), "string"},
		{"enum", schema.NewEnumConstraint([]string{"a", "b"}), "string"},
		{"pattern", schema.NewPatternConstraint(nil), "string"},
		{"vector", schema.NewVectorConstraint(8), "[]float64"},
		{"list_float", schema.NewListConstraint(schema.NewFloatConstraint()), "[]float64"},
		{"list_date", schema.NewListConstraint(schema.NewDateConstraint()), "[]Date"},
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

// TestGoBaseType_UnregisteredTemporalIsAGeneratorBug pins that a temporal
// position the pre-pass did not register surfaces as an error rather than
// as an empty type name that would fail later in format or type-check.
func TestGoBaseType_UnregisteredTemporalIsAGeneratorBug(t *testing.T) {
	g := &generator{}
	for name, c := range map[string]schema.Constraint{
		"date":   schema.NewDateConstraint(),
		"layout": schema.NewTimestampConstraintFormatted(wallLayout),
	} {
		if _, err := g.goBaseType(c); err == nil {
			t.Errorf("%s: expected an error for an unregistered temporal position", name)
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
