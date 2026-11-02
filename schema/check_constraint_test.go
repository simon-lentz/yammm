package schema_test

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/schema"
)

// A float literal the DSL reads is finite or draws E_INVALID_CONSTRAINT, so an
// infinite or NaN bound is a constraint no .yammm source can state.
func TestCheckConstraint_RefusesNonFiniteFloatBound(t *testing.T) {
	cases := []struct {
		name string
		c    schema.Constraint
	}{
		{"infinite max", schema.FloatMax(math.Inf(1))},
		{"negative infinite min", schema.FloatMin(math.Inf(-1))},
		{"NaN min", schema.FloatBetween(math.NaN(), 1)},
		{"NaN max", schema.FloatBetween(0, math.NaN())},
		{"list element", schema.NewListConstraint(schema.FloatMax(math.Inf(1)))},
		{"nested list element", schema.NewListConstraint(schema.NewListConstraint(schema.FloatMin(math.Inf(-1))))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := schema.CheckConstraint(tc.c)
			if err == nil || !strings.Contains(err.Error(), "non-finite float bound") {
				t.Errorf("CheckConstraint = %v, want a refusal naming the non-finite bound", err)
			}
		})
	}

	t.Run("finite bounds pass", func(t *testing.T) {
		for _, c := range []schema.Constraint{
			schema.FloatBetween(-math.MaxFloat64, math.MaxFloat64),
			schema.NewListConstraint(schema.FloatMin(0)),
		} {
			if err := schema.CheckConstraint(c); err != nil {
				t.Errorf("CheckConstraint(%v) = %v", c, err)
			}
		}
	})
}

// Every constraint argument the DSL refuses with E_INVALID_CONSTRAINT,
// CheckConstraint refuses too. Each case is stated both ways, so the two are
// checked against each other rather than against a list.
func TestCheckConstraint_RefusesEveryConstraintTheDSLRefuses(t *testing.T) {
	cases := []struct {
		name string
		dsl  string
		c    schema.Constraint
		want string
	}{
		{"inverted integer bounds", "Integer[5, 1]", schema.IntegerBetween(5, 1), "integer bounds inverted"},
		{"inverted float bounds", "Float[2.5, 1.0]", schema.FloatBetween(2.5, 1), "float bounds inverted"},
		{"inverted string length", "String[5, 1]", schema.StringLenBetween(5, 1), "string length bounds inverted"},
		{"inverted list length", "List<String>[5, 1]", schema.ListLenBetween(schema.NewStringConstraint(), 5, 1), "list length bounds inverted"},
		{"inverted bounds on a list element", "List<Integer[5, 1]>", schema.NewListConstraint(schema.IntegerBetween(5, 1)), "integer bounds inverted"},
		{"an empty enum value", `Enum["a", ""]`, schema.NewEnumConstraint([]string{"a", ""}), "enum value cannot be empty"},
		{"a duplicate enum value", `Enum["a", "b", "a"]`, schema.NewEnumConstraint([]string{"a", "b", "a"}), "duplicate enum value"},
		{"a one-value enum", `Enum["a"]`, schema.NewEnumConstraint([]string{"a"}), "at least two values"},
		{"a zero-dimension vector", "Vector[0]", schema.NewVectorConstraint(0), "vector dimensions"},
		{"a vector past the maximum", "Vector[65537]", schema.NewVectorConstraint(65537), "vector dimensions"},
		{"three patterns", `Pattern["a", "b", "c"]`, schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("a"), regexp.MustCompile("b"), regexp.MustCompile("c")}), "exceeds maximum of 2 patterns"},
		{"a pattern Perl syntax refuses", `Pattern["a**"]`, schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompilePOSIX("a**")}), `invalid regex pattern "a**"`},
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

// A negative length has no DSL spelling: the lexer gives an unsigned bound no
// minus sign.
func TestCheckConstraint_RefusesANegativeLength(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"string minimum": schema.StringMinLen(-1),
		"list maximum":   schema.ListMaxLen(schema.NewStringConstraint(), -1),
	} {
		t.Run(name, func(t *testing.T) {
			if err := schema.CheckConstraint(c); err == nil || !strings.Contains(err.Error(), "cannot be negative") {
				t.Errorf("CheckConstraint = %v, want a refusal naming the negative length", err)
			}
		})
	}
}

// A Pattern with no pattern is one the DSL cannot write.
func TestCheckConstraint_RefusesAPatternWithNone(t *testing.T) {
	err := schema.CheckConstraint(schema.NewPatternConstraint(nil))
	if err == nil || !strings.Contains(err.Error(), "needs at least one pattern") {
		t.Errorf("CheckConstraint = %v, want a refusal naming the missing pattern", err)
	}
}

// Every List the DSL writes names its element, so a List with none, from a
// constructor handed nil or the zero ListConstraint, is refused at any depth.
func TestCheckConstraint_RefusesAListWithNoElement(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"NewListConstraint(nil)":        schema.NewListConstraint(nil),
		"the zero ListConstraint":       schema.ListConstraint{},
		"ListMinLen(nil, 1)":            schema.ListMinLen(nil, 1),
		"ListMaxLen(nil, 1)":            schema.ListMaxLen(nil, 1),
		"ListLenBetween(nil, 0, 1)":     schema.ListLenBetween(nil, 0, 1),
		"a nested List with no element": schema.NewListConstraint(schema.NewListConstraint(nil)),
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
	list := schema.NewListConstraint(schema.NewStringConstraint())
	str := schema.NewStringConstraint()
	for name, c := range map[string]schema.Constraint{
		"a pointer to a List":             &list,
		"a pointer to a String":           &str,
		"a List of a pointer to a String": schema.NewListConstraint(&str),
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
		want := schema.IntegerBetween(1, 5)
		car, _ := s.Type("Car")
		v, _ := car.Property("v")
		if got := schema.ResolveAlias(v.Constraint()); !got.Equal(want) {
			t.Errorf("v resolves to %v; want %v", got, want)
		}
		wides, _ := s.DataType("Wides")
		lc, ok := wides.Constraint().(schema.ListConstraint)
		if !ok {
			t.Fatalf("Wides is %T; want a ListConstraint", wides.Constraint())
		}
		if got := schema.ResolveAlias(lc.Element()); !got.Equal(want) {
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
		"a reference resolved to an unresolved":               {schema.NewAliasConstraint("Code", schema.NewAliasConstraint("Inner", nil)), `datatype "Code" resolves to datatype "Inner" alone`},
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
	if err := schema.CheckConstraint(schema.NewAliasConstraint("Code", schema.IntegerBetween(1, 5))); err != nil {
		t.Errorf("a resolved hand-built reference: %v", err)
	}
}

// A source Perl syntax refuses is kept as its caller built it, so a raw reader
// that skips the judge still holds a working regexp; the judge refuses it.
func TestNewPatternConstraint_KeepsASourcePerlSyntaxRefuses(t *testing.T) {
	posix := regexp.MustCompilePOSIX("a**")
	c := schema.NewPatternConstraint([]*regexp.Regexp{posix})
	if got := expr.PatternRegexps(c); len(got) != 1 || got[0] != posix {
		t.Errorf("PatternRegexps = %v; want the caller's regexp", got)
	}
	if got := c.Patterns(); len(got) != 1 || got[0] != "a**" {
		t.Errorf("Patterns = %v; want [a**]", got)
	}
}

// A range of one value is legal: bounds are inverted only when min exceeds max.
func TestCheckConstraint_AdmitsEqualBounds(t *testing.T) {
	for _, c := range []schema.Constraint{schema.IntegerBetween(1, 1), schema.FloatBetween(1.5, 1.5)} {
		if err := schema.CheckConstraint(c); err != nil {
			t.Errorf("CheckConstraint(%v) = %v", c, err)
		}
	}
}
