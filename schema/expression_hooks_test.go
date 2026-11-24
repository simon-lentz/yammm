package schema_test

import (
	"reflect"
	"testing"

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// Each hook's doc states that it panics on any other argument.
func TestExpressionHooks_PanicOnAnotherType(t *testing.T) {
	t.Parallel()
	yammmtest.AssertPanics(t, func() { expr.InvariantExpression(42) })
	yammmtest.AssertPanics(t, func() { expr.PatternRegexp(42) })
	yammmtest.AssertPanics(t, func() { expr.PatternRegexp(schema.TestParsedEnumConstraint([]string{"a"})) })
}

// The tree and the compiled patterns have no public read: an invariant's
// expression is read as text.
func TestExpressionReads_LeaveTheExportedSurface(t *testing.T) {
	t.Parallel()
	if _, ok := reflect.TypeFor[schema.PatternConstraint]().MethodByName("CompiledPatterns"); ok {
		t.Error("schema.PatternConstraint exports CompiledPatterns")
	}
	m, ok := reflect.TypeFor[schema.Invariant]().MethodByName("Expression")
	if !ok || m.Type.NumOut() != 1 || m.Type.Out(0) != reflect.TypeFor[string]() {
		t.Errorf("schema.Invariant.Expression is %v; want a method returning the text", m.Type)
	}
}

func TestAnnotation_ArgsIsAFreshSlice(t *testing.T) {
	t.Parallel()
	s, err := schema.LoadString(t.Context(), "schema \"m\"\ntype A {\n@@index(name)\nid String primary\nname String\n}\n", "m.yammm")
	if err != nil {
		t.Fatal(err)
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
