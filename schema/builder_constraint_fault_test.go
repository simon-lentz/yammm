package schema_test

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
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

// The check reads through an alias's resolved constraint, and refuses a
// Pattern with no pattern, which the DSL cannot write.
func TestBuilder_RefusesAFaultBehindAnAliasAndAnEmptyPattern(t *testing.T) {
	for name, c := range map[string]schema.Constraint{
		"alias to inverted bounds": schema.NewAliasConstraint("Code", schema.IntegerBetween(5, 1)),
		"list of such an alias":    schema.NewListConstraint(schema.NewAliasConstraint("Code", schema.IntegerBetween(5, 1))),
		"a pattern with none":      schema.NewPatternConstraint(nil),
	} {
		t.Run(name, func(t *testing.T) {
			s, res := schema.NewBuilder().WithName("fleet").
				AddType("Car").WithPrimaryKey("vin", schema.NewStringConstraint()).
				WithProperty("v", c).Done().Build()
			if s != nil || !hasCode(res, diag.E_INVALID_CONSTRAINT) {
				t.Errorf("Build = %v, %s; want E_INVALID_CONSTRAINT", s, res.String())
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
