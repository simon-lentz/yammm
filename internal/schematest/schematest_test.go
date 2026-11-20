package schematest_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/schematest"
	"github.com/simon-lentz/yammm/schema"
)

func TestConstraint_ReturnsTheConstraintTheTextStates(t *testing.T) {
	t.Parallel()

	for _, text := range []string{
		"String", "String[1, 10]", "Integer[_, 5]", "Float[0.5, _]", "Boolean",
		"Timestamp", "Date", "UUID", `Enum["a", "b"]`, `Pattern["^a$"]`,
		"Vector[3]", "List<String>[1, _]", "List<List<Date>>",
	} {
		if got := schematest.Constraint(t, text).String(); got != text {
			t.Errorf("Constraint(%q).String() = %q", text, got)
		}
	}
}

func TestConstraint_ResolvesADeclaredDataType(t *testing.T) {
	t.Parallel()

	c := schematest.Constraint(t, "Name", "type Name = String[1, _]")
	alias, ok := c.(schema.AliasConstraint)
	if !ok {
		t.Fatalf("Constraint = %T; want a schema.AliasConstraint", c)
	}
	if got := schema.ResolveAlias(alias).String(); got != "String[1, _]" {
		t.Errorf("the alias resolves to %q; want String[1, _]", got)
	}
}
