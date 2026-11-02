package schema_test

import (
	"reflect"
	"regexp"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// Each hook's doc states that it panics on any other argument.
func TestExpressionHooks_PanicOnAnotherType(t *testing.T) {
	t.Parallel()
	yammmtest.AssertPanics(t, func() { expr.InvariantExpression(42) })
	yammmtest.AssertPanics(t, func() { expr.PatternRegexps(42) })
	yammmtest.AssertPanics(t, func() { expr.PatternRegexps(schema.NewEnumConstraint([]string{"a"})) })
}

// The tree and the compiled patterns have no public read.
func TestExpressionReads_LeaveTheExportedSurface(t *testing.T) {
	t.Parallel()
	for typ, name := range map[reflect.Type]string{
		reflect.TypeFor[schema.Invariant]():         "Expression",
		reflect.TypeFor[schema.PatternConstraint](): "CompiledPatterns",
	} {
		if _, ok := typ.MethodByName(name); ok {
			t.Errorf("%s exports %s", typ, name)
		}
	}
}

func TestPatternConstraint_PatternsIsAFreshSlice(t *testing.T) {
	t.Parallel()
	c := schema.NewPatternConstraint([]*regexp.Regexp{regexp.MustCompile("^a")})
	c.Patterns()[0] = "changed"
	if got := c.Patterns(); !slices.Equal(got, []string{"^a"}) {
		t.Errorf("Patterns = %v after a write to an earlier result, want [^a]", got)
	}
}

func TestAnnotation_ArgsIsAFreshSlice(t *testing.T) {
	t.Parallel()
	s, res := schema.LoadString(t.Context(), "schema \"m\"\ntype A {\n@@index(id)\nid String primary\n}\n", "m.yammm")
	if !res.OK() {
		t.Fatal(res)
	}
	a, ok := s.Type("A")
	if !ok {
		t.Fatal("no type A")
	}
	for ann := range a.Annotations() {
		held := ann.Args()
		if len(held) == 0 {
			t.Fatalf("@%s holds no argument", ann.Name())
		}
		held[0] = schema.AnnotationArg{}
		if ann.Args()[0] == held[0] {
			t.Errorf("@%s: Args handed out its stored slice", ann.Name())
		}
	}
}
