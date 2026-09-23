package schema_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// CheckConstraint refuses what Build refuses in a constraint, and a DataType
// reference that resolves to no constraint, which a raw constraint never
// meets completion to resolve.
func TestCheckConstraint_RefusesWhatNoSchemaHolds(t *testing.T) {
	str := schema.NewStringConstraint()
	three := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("a"), regexp.MustCompile("b"), regexp.MustCompile("c")})
	for name, tc := range map[string]struct {
		c    schema.Constraint
		want string
	}{
		"nil":                                                 {nil, "no constraint"},
		"inverted integer bounds":                             {schema.IntegerBetween(5, 1), "integer bounds inverted"},
		"a List with no element":                              {schema.NewListConstraint(nil), "list has no element constraint"},
		"three patterns":                                      {three, "exceeds maximum of 2 patterns"},
		"a pattern Perl syntax refuses":                       {schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**")}), `invalid regex pattern "a**"`},
		"a pointer to a constraint":                           {&str, "none this package constructs"},
		"an unresolved DataType reference":                    {schema.NewAliasConstraint("Code", nil), `datatype "Code" resolves to no constraint`},
		"a List of an unresolved reference":                   {schema.NewListConstraint(schema.NewAliasConstraint("Code", nil)), `datatype "Code" resolves to no constraint`},
		"a reference resolved to a fault":                     {schema.NewAliasConstraint("Code", schema.IntegerBetween(5, 1)), "integer bounds inverted"},
		"a reference resolved to an unresolved":               {schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Inner", nil)), `datatype "Inner" resolves to no constraint`},
		"a List of a reference resolved to a no-element List": {schema.NewListConstraint(schema.NewAliasConstraint("Tags", schema.NewListConstraint(nil))), "list has no element constraint"},
	} {
		t.Run(name, func(t *testing.T) {
			err := schema.CheckConstraint(tc.c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckConstraint = %v; want an error naming %q", err, tc.want)
			}
		})
	}
}

// Every constraint a loaded schema holds, on a datatype or a property, is one
// CheckConstraint accepts, and so is a hand-built one a schema could hold.
func TestCheckConstraint_AcceptsWhatASchemaHolds(t *testing.T) {
	src := `schema "fleet"

type Code = Integer[1, 5]
type Codes = List<Code>[1, 3]

type Car {
	vin String primary
	code Code
	codes Codes
	nested List<List<Code>>
	tag Pattern["^a", "b$"]
	kind Enum["x", "y"]
	at Timestamp
	on Date
	id2 UUID
	ok Boolean
	vec Vector[3]
	weight Float[0.5, 2.5]
}
`
	s, res := schema.LoadString(t.Context(), src, "fleet.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	for _, dt := range s.DataTypesSlice() {
		if err := schema.CheckConstraint(dt.Constraint()); err != nil {
			t.Errorf("datatype %s: %v", dt.Name(), err)
		}
	}
	car, _ := s.Type("Car")
	for p := range car.AllProperties() {
		if err := schema.CheckConstraint(p.Constraint()); err != nil {
			t.Errorf("property %s: %v", p.Name(), err)
		}
	}
	if err := schema.CheckConstraint(schema.NewAliasConstraint("Code", schema.IntegerBetween(1, 5))); err != nil {
		t.Errorf("a resolved hand-built reference: %v", err)
	}
}

// A source Perl syntax refuses is kept as its caller built it, so a raw reader
// that skips the judge still holds a working regexp; the judge refuses it.
func TestNewPatternConstraint_KeepsASourcePerlSyntaxRefuses(t *testing.T) {
	posix := regexp.MustCompilePOSIX("a**")
	c := schema.NewPatternConstraint([]*regexp.Regexp{posix})
	if got := c.CompiledPatterns(); len(got) != 1 || got[0] != posix {
		t.Errorf("CompiledPatterns = %v; want the caller's regexp", got)
	}
	if got := c.Patterns(); len(got) != 1 || got[0] != "a**" {
		t.Errorf("Patterns = %v; want [a**]", got)
	}
}
