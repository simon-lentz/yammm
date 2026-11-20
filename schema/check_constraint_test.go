package schema_test

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/schema"
)

// An Enum's value count and a Vector's dimension are the arguments a caller can
// hold out of range, through a zero value; the DSL refuses the same counts with
// E_INVALID_CONSTRAINT. Each case is stated both ways.
func TestCheckConstraint_RefusesACountTheDSLRefuses(t *testing.T) {
	cases := []struct {
		name string
		dsl  string
		c    schema.Constraint
		want string
	}{
		{"a one-value enum", `Enum["a"]`, schema.TestParsedEnumConstraint([]string{"a"}), "at least two values"},
		{"a zero-dimension vector", "Vector[0]", schema.TestNewVectorConstraint(0), "vector dimensions"},
		{"a vector past the maximum", "Vector[65537]", schema.TestNewVectorConstraint(65537), "vector dimensions"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"fleet\"\n\ntype Car {\n\tvin String primary\n\tv %s\n}\n", tc.dsl)
			_, lres := schema.LoadString(context.Background(), src, "fleet.yammm")
			if !hasCode(lres, diag.E_INVALID_CONSTRAINT) {
				t.Fatalf("the DSL accepts %s: %v", tc.dsl, lres.Err())
			}
			err := schema.CheckConstraint(tc.c)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("CheckConstraint = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

// A Pattern with no pattern is one the DSL cannot write.
func TestCheckConstraint_RefusesAPatternWithNone(t *testing.T) {
	err := schema.CheckConstraint(schema.TestParsedPatternConstraint(nil))
	if err == nil || !strings.Contains(err.Error(), "needs a pattern") {
		t.Errorf("CheckConstraint = %v, want a refusal naming the missing pattern", err)
	}
}

// Every List the DSL writes names its element, so a List with none, from a
// constructor handed nil or the zero ListConstraint, is refused at any depth.
func TestCheckConstraint_RefusesAListWithNoElement(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"NewListConstraint(nil)":        schema.TestNewListConstraint(nil),
		"the zero ListConstraint":       schema.ListConstraint{},
		"ListMinLen(nil, 1)":            schema.TestListMinLen(nil, 1),
		"ListMaxLen(nil, 1)":            schema.TestListMaxLen(nil, 1),
		"ListLenBetween(nil, 0, 1)":     schema.TestListLenBetween(nil, 0, 1),
		"a nested List with no element": schema.TestNewListConstraint(schema.TestNewListConstraint(nil)),
	} {
		t.Run(name, func(t *testing.T) {
			err := schema.CheckConstraint(c)
			if err == nil || !strings.Contains(err.Error(), "list has no element constraint") {
				t.Errorf("CheckConstraint = %v, want a refusal naming the missing element", err)
			}
		})
	}
}

// Every constraint method has a value receiver, so a pointer to a constraint
// satisfies the interface and names no constraint the DSL states.
func TestCheckConstraint_RefusesAConstraintThePackageDoesNotConstruct(t *testing.T) {
	list := schema.TestNewListConstraint(schema.TestNewStringConstraint())
	str := schema.TestNewStringConstraint()
	for name, c := range map[string]schema.Constraint{
		"a pointer to a List":             &list,
		"a pointer to a String":           &str,
		"a List of a pointer to a String": schema.TestNewListConstraint(&str),
	} {
		t.Run(name, func(t *testing.T) {
			err := schema.CheckConstraint(c)
			if err == nil || !strings.Contains(err.Error(), "none this package constructs") {
				t.Errorf("CheckConstraint = %v, want a refusal naming the Go type", err)
			}
		})
	}
}

// A datatype's fault is refused at the declaration, so a property that names
// the datatype draws no second reading of it.
func TestLoad_RefusesAFaultBehindAnAlias(t *testing.T) {
	for name, property := range map[string]string{
		"alias to inverted bounds": "Code",
		"list of such an alias":    "List<Code>",
	} {
		t.Run(name, func(t *testing.T) {
			src := "schema \"fleet\"\n\ntype Code = Integer[5, 1]\n\ntype Car {\n\tvin String primary\n\tv " + property + "\n}\n"
			s, res := schema.LoadString(context.Background(), src, "fleet.yammm")
			if !s.IsZero() || !hasCode(res, diag.E_INVALID_CONSTRAINT) {
				t.Errorf("load = %v, %s; want E_INVALID_CONSTRAINT", s, res.String())
			}
		})
	}
}

// The DSL declares a datatype over a built-in type or a List; "type B = A" is a
// syntax error, so no reader meets a datatype that is only another's name.
func TestLoad_RefusesADataTypeDeclaredAsAnotherAlone(t *testing.T) {
	src := "schema \"fleet\"\n\ntype A = Integer\ntype B = A\n\ntype Car {\n\tvin String primary\n}\n"
	if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); !lres.HasErrors() {
		t.Fatal("the DSL accepts a datatype declared as another alone")
	}
}

// Every declared name the grammar reserves is refused, and the two spellings it
// accepts in the same positions load.
func TestLoad_RefusesAReservedDeclaredName(t *testing.T) {
	type row struct{ name, dsl string }
	rows := []row{
		{"a type named List", "type List {\n\tid String primary\n}"},
		{"a datatype named String", "type String = Integer"},
		{"a relation named UUID", "type Pal {\n\tid String primary\n\t--> UUID (one) Car\n}"},
	}
	for _, word := range []string{"as", "part", "in", "nil", "true", "false"} {
		rows = append(rows, row{"a property named " + word, "type Pal {\n\tid String primary\n\t" + word + " String\n}"})
	}
	for _, tc := range rows {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"fleet\"\n\ntype Car {\n\tvin String primary\n}\n\n" + tc.dsl + "\n"
			if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); !lres.HasErrors() {
				t.Fatalf("the DSL accepts %s", tc.name)
			}
		})
	}
	t.Run("a relation named LIST and a property named schema load", func(t *testing.T) {
		src := "schema \"fleet\"\n\ntype Car {\n\tvin String primary\n\tschema String\n\t--> LIST (one) Car\n}\n"
		if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); lres.HasErrors() {
			t.Fatalf("the DSL refuses them: %v", lres.Err())
		}
	})
}

// Bounds that meet are legal in the DSL.
func TestLoad_AcceptsBoundsThatMeet(t *testing.T) {
	for name, dsl := range map[string]string{
		"integer":          "Integer[5, 5]",
		"float":            "Float[1.5, 1.5]",
		"string":           "String[3, 3]",
		"list":             "List<String>[2, 2]",
		"a two-value enum": `Enum["a", "b"]`,
		"vector of one":    "Vector[1]",
	} {
		t.Run(name, func(t *testing.T) {
			src := fmt.Sprintf("schema \"fleet\"\n\ntype Car {\n\tvin String primary\n\tv %s\n}\n", dsl)
			s, res := schema.LoadString(context.Background(), src, "fleet.yammm")
			if s.IsZero() || res.HasErrors() {
				t.Errorf("load refused a legal constraint: %s", res.String())
			}
		})
	}
}

func hasCode(r diag.Result, code diag.Code) bool {
	for issue := range r.Issues() {
		if issue.Code() == code {
			return true
		}
	}
	return false
}

// A datatype reference resolves by its name on a property and inside a
// datatype's List, and an undeclared name is refused.
func TestLoad_ResolvesADataTypeAliasByItsName(t *testing.T) {
	t.Run("an undeclared name in a DataType is refused", func(t *testing.T) {
		src := "schema \"fleet\"\n\ntype Wides = List<Nope>\n\ntype Car {\n\tvin String primary\n\tvs Wides\n}\n"
		s, res := schema.LoadString(context.Background(), src, "fleet.yammm")
		if !s.IsZero() || !hasCode(res, diag.E_UNKNOWN_TYPE) {
			t.Fatalf("load = %v, %s; want E_UNKNOWN_TYPE", s, res.String())
		}
	})
	t.Run("the declaration governs a property alias and a DataType's List of one", func(t *testing.T) {
		src := "schema \"fleet\"\n\ntype Code = Integer[1, 5]\n\ntype Wides = List<Code>\n\ntype Car {\n\tvin String primary\n\tv Code\n\tvs Wides\n}\n"
		s, res := schema.LoadString(context.Background(), src, "fleet.yammm")
		if s.IsZero() {
			t.Fatalf("load refused: %s", res.String())
		}
		want := schema.TestIntegerBetween(1, 5)
		car, _ := s.Type("Car")
		v, _ := car.Property("v")
		if got := schema.ResolveAlias(v.Constraint()); !schema.TestConstraintEqual(got, want) {
			t.Errorf("v resolves to %v; want %v", got, want)
		}
		wides, _ := s.DataType("Wides")
		lc, ok := wides.Constraint().(schema.ListConstraint)
		if !ok {
			t.Fatalf("Wides is %T; want a ListConstraint", wides.Constraint())
		}
		if got := schema.ResolveAlias(lc.Element()); !schema.TestConstraintEqual(got, want) {
			t.Errorf("Wides element resolves to %v; want %v", got, want)
		}
	})
}

// A List keeps its own length bounds around an alias element.
func TestLoad_KeepsAListsLengthBoundsAroundAnAlias(t *testing.T) {
	for name, c := range map[string]struct {
		dsl          string
		lo, hi       int64
		hasLo, hasHi bool
	}{
		"between":                        {dsl: "List<Code>[1, 3]", lo: 1, hi: 3, hasLo: true, hasHi: true},
		"min":                            {dsl: "List<Code>[2, _]", lo: 2, hasLo: true},
		"max":                            {dsl: "List<Code>[_, 4]", hi: 4, hasHi: true},
		"between around a plain element": {dsl: "List<String>[1, 3]", lo: 1, hi: 3, hasLo: true, hasHi: true},
	} {
		t.Run(name, func(t *testing.T) {
			src := "schema \"fleet\"\n\ntype Code = Integer[1, 5]\n\ntype Car {\n\tvin String primary\n\tvs " + c.dsl + "\n}\n"
			s, res := schema.LoadString(context.Background(), src, "fleet.yammm")
			if s.IsZero() {
				t.Fatalf("load refused: %s", res.String())
			}
			car, _ := s.Type("Car")
			p, _ := car.Property("vs")
			lc, ok := p.Constraint().(schema.ListConstraint)
			if !ok {
				t.Fatalf("vs is %T; want a ListConstraint", p.Constraint())
			}
			lo, hasLo := lc.MinLen()
			hi, hasHi := lc.MaxLen()
			if hasLo != c.hasLo || hasHi != c.hasHi || (hasLo && lo != c.lo) || (hasHi && hi != c.hi) {
				t.Errorf("bounds = (%d,%v) (%d,%v); want (%d,%v) (%d,%v)", lo, hasLo, hi, hasHi, c.lo, c.hasLo, c.hi, c.hasHi)
			}
		})
	}
}

// CheckConstraint refuses what the DSL refuses in a constraint, and a DataType
// reference that resolves to no constraint, which a raw constraint never
// meets completion to resolve.
func TestCheckConstraint_RefusesWhatNoSchemaHolds(t *testing.T) {
	str := schema.TestNewStringConstraint()
	for name, tc := range map[string]struct {
		c    schema.Constraint
		want string
	}{
		"nil":                                                 {nil, "no constraint"},
		"a List with no element":                              {schema.TestNewListConstraint(nil), "list has no element constraint"},
		"a pattern constraint with no pattern":                {schema.TestParsedPatternConstraint(nil), "needs a pattern"},
		"a pointer to a constraint":                           {&str, "none this package constructs"},
		"an unresolved DataType reference":                    {schema.TestNewAliasConstraint("Code", nil), `datatype "Code" resolves to no constraint`},
		"a List of an unresolved reference":                   {schema.TestNewListConstraint(schema.TestNewAliasConstraint("Code", nil)), `datatype "Code" resolves to no constraint`},
		"a reference resolved to a fault":                     {schema.TestNewAliasConstraint("Code", schema.TestParsedPatternConstraint(nil)), "needs a pattern"},
		"a reference resolved to an unresolved":               {schema.TestNewAliasConstraint("Code", schema.TestNewAliasConstraint("Inner", nil)), `datatype "Code" resolves to datatype "Inner" alone`},
		"a List of a reference resolved to a no-element List": {schema.TestNewListConstraint(schema.TestNewAliasConstraint("Tags", schema.TestNewListConstraint(nil))), "list has no element constraint"},
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
	tag Pattern["^a"]
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
	for dt := range s.DataTypes() {
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
	if err := schema.CheckConstraint(schema.TestNewAliasConstraint("Code", schema.TestIntegerBetween(1, 5))); err != nil {
		t.Errorf("a resolved hand-built reference: %v", err)
	}
}

// A source Perl syntax refuses is kept as its caller built it, so a raw reader
// that skips the judge still holds a working regexp; the judge refuses it.
func TestNewPatternConstraint_KeepsASourcePerlSyntaxRefuses(t *testing.T) {
	posix := regexp.MustCompilePOSIX("a**")
	c := schema.TestParsedPatternConstraint(posix)
	if got := expr.PatternRegexp(c); got != posix {
		t.Errorf("PatternRegexp = %v; want the caller's regexp", got)
	}
	if got := c.Pattern(); got != "a**" {
		t.Errorf("Pattern = %q; want a**", got)
	}
}
