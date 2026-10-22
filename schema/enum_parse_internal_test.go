package schema

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// The loader's Enum holds the values the parser kept, with no fault, and is
// built without a second judgement: beside reading the values it allocates
// only the constraint it returns.
func TestSchemaParser_TakesTheParsersEnumValues(t *testing.T) {
	src := "schema \"s\"\n\ntype Kind = Enum[\"a\", \"b\", \"c\", \"d\", \"e\", \"f\", \"g\", \"h\", \"i\"]\n"
	id := location.MustNewSourceID("test://s.yammm")
	file, issues := parse.Parse([]byte(src), id)
	if len(issues) != 0 {
		t.Fatalf("parse: %v", issues)
	}
	node := file.DataTypes[0].Constraint
	p := newParser(id, diag.NewCollector(0))
	c, _ := p.constraint(node)
	held, ok := c.(EnumConstraint)
	if !ok || !slices.Equal(held.values, node.EnumValues()) || held.invalid != "" {
		t.Fatalf("constraint = %#v; want the parser's nine values and no fault", c)
	}
	if _, found := constraintFault(c); found {
		t.Fatal("the judge refuses an Enum the parser kept")
	}

	raceskip.Skip(t)
	var values []string
	var built Constraint
	reading := testing.AllocsPerRun(100, func() { values = node.EnumValues() })
	building := testing.AllocsPerRun(100, func() { built, _ = p.constraint(node) })
	if building != reading+1 {
		t.Errorf("the loader allocates %v building an Enum where reading its values allocates %v; want one more", building, reading)
	}
	_, _ = values, built
}
