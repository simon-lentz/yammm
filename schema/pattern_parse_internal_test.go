package schema

import (
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// The loader's Pattern holds the regexp the parser compiled, the same object,
// so a source's pattern is compiled once a load.
func TestSchemaParser_KeepsTheParsersRegexp(t *testing.T) {
	src := "schema \"s\"\n\ntype Zip = Pattern[\"^[0-9]{5}$\"]\n\ntype T {\n\tid String primary\n\tone Pattern[\"^a\"]\n\tzips List<Pattern[\"^z\"]>\n}\n"
	id := location.NewSourceID("test://s.yammm")
	file, issues := parse.Parse([]byte(src), id)
	if len(issues) != 0 {
		t.Fatalf("parse: %v", issues)
	}
	p := newParser(id, diag.NewCollector(0))
	parsed := []*parse.Constraint{
		file.DataTypes[0].Constraint,
		file.Types[0].Properties[1].Constraint,
		file.Types[0].Properties[2].Constraint.Elem,
	}
	for _, node := range parsed {
		c, _ := p.constraint(node)
		held, ok := c.(PatternConstraint)
		if !ok {
			t.Fatalf("constraint = %T; want a PatternConstraint", c)
		}
		want := node.PatternRegexp()
		if want == nil || held.compiled != want || held.invalid != "" {
			t.Fatalf("%s holds %p and the fault %q; want the parser's regexp %p and none", held, held.compiled, held.invalid, want)
		}
		if held.pattern != want.String() {
			t.Errorf("%s: source = %q; want %q", held, held.pattern, want)
		}
	}
	raceskip.Skip(t)
	node := parsed[1]
	var compiled *regexp.Regexp
	var built Constraint
	reading := testing.AllocsPerRun(100, func() { compiled = node.PatternRegexp() })
	building := testing.AllocsPerRun(100, func() { built, _ = p.constraint(node) })
	if reading != 0 || building != 1 {
		t.Errorf("reading the regexp allocates %v and building the Pattern %v; want 0 and 1, the constraint", reading, building)
	}
	_, _ = compiled, built
}
