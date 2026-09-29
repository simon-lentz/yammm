package schema_test

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// A float literal the DSL reads is finite or draws E_INVALID_CONSTRAINT, so a
// Builder-built schema holding an infinite or NaN bound would be one no
// .yammm source can state.
func TestBuilder_RefusesNonFiniteFloatBound(t *testing.T) {
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
		t.Run("property "+tc.name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("weight", tc.c).Done().Build()
			assertRefusedNonFinite(t, s, res)
		})
		t.Run("datatype "+tc.name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddDataType("Weight", tc.c).
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done().Build()
			assertRefusedNonFinite(t, s, res)
		})
	}

	t.Run("finite bounds build", func(t *testing.T) {
		s, res := schema.NewBuilder().WithName("fleet").
			AddDataType("Weight", schema.FloatBetween(-math.MaxFloat64, math.MaxFloat64)).
			AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
			WithProperty("weight", schema.NewListConstraint(schema.FloatMin(0))).Done().Build()
		if res.HasErrors() || s == nil {
			t.Fatalf("finite bounds: Build = %v, %v", s, res.Err())
		}
	})
}

func assertRefusedNonFinite(t *testing.T, s *schema.Schema, res diag.Result) {
	t.Helper()
	if s != nil {
		t.Fatal("Build returned a schema holding a non-finite Float bound")
	}
	issues := slices.Collect(res.Issues())
	if len(issues) != 1 {
		t.Fatalf("Build reported %d issues, want 1: %v", len(issues), res.Err())
	}
	if got := issues[0].Code(); got != diag.E_INVALID_CONSTRAINT {
		t.Errorf("code = %v, want E_INVALID_CONSTRAINT", got)
	}
	if !strings.Contains(issues[0].Message(), "non-finite float bound") {
		t.Errorf("message %q does not name the non-finite bound", issues[0].Message())
	}
}

// Every constraint argument the DSL refuses with E_INVALID_CONSTRAINT, the
// Builder refuses with the same code. Each case is stated both ways, so the
// two front doors are checked against each other rather than against a list.
func TestBuilder_RefusesEveryConstraintTheDSLRefuses(t *testing.T) {
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
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", tc.c).Done().Build()
			if s != nil {
				t.Fatalf("the Builder built %s", tc.dsl)
			}
			if !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), tc.want) {
				t.Errorf("Build reported %s, want E_INVALID_CONSTRAINT naming %q", res.String(), tc.want)
			}
		})
	}
}

// A negative length has no DSL spelling — the lexer gives an unsigned bound
// no minus sign — so the Builder is the only door that can offer one.
func TestBuilder_RefusesANegativeLength(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"string minimum": schema.StringMinLen(-1),
		"list maximum":   schema.ListMaxLen(schema.NewStringConstraint(), -1),
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			if s != nil || !strings.Contains(res.String(), "cannot be negative") {
				t.Errorf("Build = %v, %s; want a refusal naming the negative length", s, res.String())
			}
		})
	}
}

// The check reads a declared DataType's constraint, so a fault behind an alias
// is refused at the DataType the alias names.
func TestBuilder_RefusesAFaultBehindAnAlias(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"alias to inverted bounds": schema.NewAliasConstraint("Code", nil),
		"list of such an alias":    schema.NewListConstraint(schema.NewAliasConstraint("Code", nil)),
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddDataType("Code", schema.IntegerBetween(5, 1)).
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) {
				t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT", s, res.String())
			}
		})
	}
}

// A Pattern with no pattern, which the DSL cannot write, is refused where it
// stands. The schema holds no other fault, so the refusal is the pattern's.
func TestBuilder_RefusesAPatternWithNone(t *testing.T) {
	s, res := schema.NewBuilder().WithName("fleet").
		AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
		WithProperty("v", schema.NewPatternConstraint(nil)).Done().Build()
	if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), "needs at least one pattern") {
		t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT naming the missing pattern", s, res.String())
	}
}

// Every List the DSL writes names its element, so a List with none — from a
// constructor handed nil, or the zero ListConstraint — is refused at any depth,
// on a property and on a datatype, and no later reader meets it.
func TestBuilder_RefusesAListWithNoElement(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"NewListConstraint(nil)":        schema.NewListConstraint(nil),
		"the zero ListConstraint":       schema.ListConstraint{},
		"ListMinLen(nil, 1)":            schema.ListMinLen(nil, 1),
		"ListMaxLen(nil, 1)":            schema.ListMaxLen(nil, 1),
		"ListLenBetween(nil, 0, 1)":     schema.ListLenBetween(nil, 0, 1),
		"a nested List with no element": schema.NewListConstraint(schema.NewListConstraint(nil)),
	} {
		t.Run("property "+name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			assertRefusedNoElement(t, s, res)
		})
		t.Run("datatype "+name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddDataType("Tags", c).
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done().Build()
			assertRefusedNoElement(t, s, res)
		})
	}
}

func assertRefusedNoElement(t *testing.T, s *schema.Schema, res diag.Result) {
	t.Helper()
	if s != nil {
		t.Fatal("Build returned a schema holding a List with no element")
	}
	if !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), "list has no element constraint") {
		t.Errorf("Build reported %s; want E_INVALID_CONSTRAINT naming the missing element", res.String())
	}
}

// Every constraint method has a value receiver, so a pointer to a constraint
// satisfies the interface and names no constraint the DSL states. The Builder
// refuses it, at any List depth, rather than build a property every value fails.
func TestBuilder_RefusesAConstraintThePackageDoesNotConstruct(t *testing.T) {
	list := schema.NewListConstraint(schema.NewStringConstraint())
	str := schema.NewStringConstraint()
	for name, c := range map[string]schema.Constraint{
		"a pointer to a List":             &list,
		"a pointer to a String":           &str,
		"a List of a pointer to a String": schema.NewListConstraint(&str),
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), "none this package constructs") {
				t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT naming the Go type", s, res.String())
			}
		})
	}
}

// The DSL declares a datatype over a built-in type or a List; "type B = A" is a
// syntax error. The Builder refuses the same declaration, so no reader meets a
// datatype that is only another datatype's name.
func TestBuilder_RefusesADataTypeDeclaredAsAnotherAlone(t *testing.T) {
	src := "schema \"fleet\"\n\ntype A = Integer\ntype B = A\n\ntype Car {\n\tvin String primary\n}\n"
	if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); !lres.HasErrors() {
		t.Fatal("the DSL accepts a datatype declared as another alone")
	}
	s, res := schema.NewBuilder().WithName("fleet").
		AddDataType("A", schema.NewIntegerConstraint()).
		AddDataType("B", schema.NewAliasConstraint("A", nil)).
		AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done().Build()
	if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) || !strings.Contains(res.String(), `datatype "B" is declared as datatype "A" alone`) {
		t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT naming B and A", s, res.String())
	}
}

// Every declared name the DSL refuses, the Builder refuses as E_INVALID_NAME.
// Each case is stated both ways, and the two spellings the DSL accepts in the
// same positions build.
func TestBuilder_RefusesANameTheDSLRefuses(t *testing.T) {
	car := schema.NewTypeRef("", "Car", location.Span{})
	cases := []struct {
		name  string
		dsl   string
		build func(*schema.Builder) *schema.Builder
	}{
		{"a type named List", "type List {\n\tid String primary\n}", func(b *schema.Builder) *schema.Builder {
			return b.AddType("List").WithPrimaryKey("id", schema.NewStringConstraint()).Done()
		}},
		{"a datatype named String", "type String = Integer", func(b *schema.Builder) *schema.Builder {
			return b.AddDataType("String", schema.NewIntegerConstraint())
		}},
		{"a relation named UUID", "type Pal {\n\tid String primary\n\t--> UUID (one) Car\n}", func(b *schema.Builder) *schema.Builder {
			return b.AddType("Pal").WithPrimaryKey("id", schema.NewStringConstraint()).WithRelation("UUID", car, false, false).Done()
		}},
	}
	for _, word := range []string{"as", "part", "in", "nil", "true", "false"} {
		cases = append(cases, struct {
			name  string
			dsl   string
			build func(*schema.Builder) *schema.Builder
		}{"a property named " + word, "type Pal {\n\tid String primary\n\t" + word + " String\n}", func(b *schema.Builder) *schema.Builder {
			return b.AddType("Pal").WithPrimaryKey("id", schema.NewStringConstraint()).WithProperty(word, schema.NewStringConstraint()).Done()
		}})
	}
	base := func() *schema.Builder {
		return schema.NewBuilder().WithName("fleet").
			AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).Done()
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := "schema \"fleet\"\n\ntype Car {\n\tvin String primary\n}\n\n" + tc.dsl + "\n"
			if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); !lres.HasErrors() {
				t.Fatalf("the DSL accepts %s", tc.name)
			}
			s, res := tc.build(base()).Build()
			if s != nil || !hasCode(res, diag.E_INVALID_NAME) {
				t.Errorf("Build = %v, %s; want E_INVALID_NAME", s, res.String())
			}
		})
	}
	t.Run("a relation named LIST and a property named schema build", func(t *testing.T) {
		src := "schema \"fleet\"\n\ntype Car {\n\tvin String primary\n\tschema String\n\t--> LIST (one) Car\n}\n"
		if _, lres := schema.LoadString(context.Background(), src, "fleet.yammm"); lres.HasErrors() {
			t.Fatalf("the DSL refuses them: %v", lres.Err())
		}
		s, res := schema.NewBuilder().WithName("fleet").
			AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
			WithProperty("schema", schema.NewStringConstraint()).
			WithRelation("LIST", car, false, false).Done().Build()
		if s == nil || res.HasErrors() {
			t.Errorf("Build refused: %s", res.String())
		}
	})
}

// TestBuilder_ResolvesAnAliasByItsNameAlone pins that a resolution the caller
// passes to NewAliasConstraint is not read by the Builder, at any List depth:
// an undeclared name is refused whatever resolution rides with it, and a
// declared DataType governs over a resolution that contradicts it.
func TestBuilder_ResolvesAnAliasByItsNameAlone(t *testing.T) {
	build := func(t *testing.T, declared, supplied schema.Constraint) (*schema.Schema, diag.Result) {
		t.Helper()
		b := schema.NewBuilder().WithName("fleet")
		if declared != nil {
			b = b.AddDataType("Code", declared)
		}
		return b.AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
			WithProperty("v", schema.NewAliasConstraint("Code", supplied)).
			WithProperty("vs", schema.NewListConstraint(schema.NewAliasConstraint("Code", supplied))).
			Done().Build()
	}
	t.Run("an undeclared name is refused with a resolution supplied", func(t *testing.T) {
		s, res := build(t, nil, schema.IntegerBetween(1, 5))
		if s != nil || !hasCode(res, diag.E_UNKNOWN_TYPE) {
			t.Fatalf("Build = %v, %s; want E_UNKNOWN_TYPE", s, res.String())
		}
	})
	for name, supplied := range map[string]schema.Constraint{
		"a contradicting bound":   schema.IntegerBetween(10, 20),
		"a contradicting kind":    schema.NewStringConstraint(),
		"a fault no source holds": schema.IntegerBetween(5, 1),
	} {
		t.Run("the declaration governs over "+name, func(t *testing.T) {
			s, res := build(t, schema.IntegerBetween(1, 5), supplied)
			if s == nil {
				t.Fatalf("Build refused: %s", res.String())
			}
			car, _ := s.Type("Car")
			for _, field := range []string{"v", "vs"} {
				p, _ := car.Property(field)
				want := schema.IntegerBetween(1, 5)
				if got := schema.ResolveAlias(p.Constraint()); field == "v" && !got.Equal(want) {
					t.Errorf("%s resolves to %v; want %v", field, got, want)
				}
				if lc, ok := p.Constraint().(schema.ListConstraint); ok && !schema.ResolveAlias(lc.Element()).Equal(want) {
					t.Errorf("%s element resolves to %v; want %v", field, schema.ResolveAlias(lc.Element()), want)
				}
			}
		})
	}
}

// Bounds that meet are legal in the DSL (Integer[5, 5], String[3, 3]), so the
// Builder builds them.
func TestBuilder_BuildsBoundsThatMeet(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"integer":          schema.IntegerBetween(5, 5),
		"float":            schema.FloatBetween(1.5, 1.5),
		"string":           schema.StringLenBetween(3, 3),
		"list":             schema.ListLenBetween(schema.NewStringConstraint(), 2, 2),
		"a two-value enum": schema.NewEnumConstraint([]string{"a", "b"}),
		"vector of one":    schema.NewVectorConstraint(1),
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			if s == nil || res.HasErrors() {
				t.Errorf("Build refused a legal constraint: %s", res.String())
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

// TestBuilder_ResolvesADataTypeAliasByItsNameAlone pins the rule on a
// DataType's List element and on a property: an alias resolves by the name it
// references, not by a resolution the caller supplied with it.
func TestBuilder_ResolvesADataTypeAliasByItsNameAlone(t *testing.T) {
	t.Run("an undeclared name in a DataType is refused with a resolution supplied", func(t *testing.T) {
		s, res := schema.NewBuilder().WithName("fleet").
			AddDataType("Wides", schema.NewListConstraint(schema.NewAliasConstraint("Nope", schema.IntegerBetween(1, 5)))).
			AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
			WithProperty("vs", schema.NewAliasConstraint("Wides", nil)).Done().Build()
		if s != nil || !hasCode(res, diag.E_UNKNOWN_TYPE) {
			t.Fatalf("Build = %v, %s; want E_UNKNOWN_TYPE", s, res.String())
		}
	})
	t.Run("the declaration governs a property alias and a DataType's List of one", func(t *testing.T) {
		s, res := schema.NewBuilder().WithName("fleet").
			AddDataType("Code", schema.IntegerBetween(1, 5)).
			AddDataType("Wides", schema.NewListConstraint(schema.NewAliasConstraint("Code", schema.NewStringConstraint()))).
			AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
			WithProperty("v", schema.NewAliasConstraint("Code", schema.NewStringConstraint())).
			WithProperty("vs", schema.NewAliasConstraint("Wides", nil)).Done().Build()
		if s == nil {
			t.Fatalf("Build refused: %s", res.String())
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

// TestBuilder_KeepsAListsLengthBoundsAroundAnAlias pins that dropping a
// supplied alias resolution inside a List keeps the List's own length bounds.
func TestBuilder_KeepsAListsLengthBoundsAroundAnAlias(t *testing.T) {
	for name, c := range map[string]struct {
		in           schema.Constraint
		lo, hi       int64
		hasLo, hasHi bool
	}{
		"between":                        {in: schema.ListLenBetween(schema.NewAliasConstraint("Code", nil), 1, 3), lo: 1, hi: 3, hasLo: true, hasHi: true},
		"min":                            {in: schema.ListMinLen(schema.NewAliasConstraint("Code", nil), 2), lo: 2, hasLo: true},
		"max":                            {in: schema.ListMaxLen(schema.NewAliasConstraint("Code", nil), 4), hi: 4, hasHi: true},
		"between around a plain element": {in: schema.ListLenBetween(schema.NewStringConstraint(), 1, 3), lo: 1, hi: 3, hasLo: true, hasHi: true},
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddDataType("Code", schema.IntegerBetween(1, 5)).
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("vs", c.in).Done().Build()
			if s == nil {
				t.Fatalf("Build refused: %s", res.String())
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
