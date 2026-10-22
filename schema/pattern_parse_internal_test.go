package schema

import (
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/internal/raceskip"
	"github.com/simon-lentz/yammm/location"
)

// The loader's Pattern holds the regexps the parser compiled, each the same
// object, so a source's pattern is compiled once a load.
func TestSchemaParser_KeepsTheParsersRegexps(t *testing.T) {
	src := "schema \"s\"\n\ntype Zip = Pattern[\"^[0-9]{5}$\"]\n\ntype T {\n\tid String primary\n\tboth Pattern[\"^a\", \"b$\"]\n\tzips List<Pattern[\"^z\"]>\n}\n"
	id := location.MustNewSourceID("test://s.yammm")
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
		want := node.PatternRegexps()
		if len(held.compiled) != len(want) || len(held.patterns) != len(want) || held.invalid != "" {
			t.Fatalf("%s holds %d regexps, %d sources and the fault %q; want %d and none", held, len(held.compiled), len(held.patterns), held.invalid, len(want))
		}
		for i, re := range want {
			if held.compiled[i] != re {
				t.Errorf("%s: regexp %d is not the one the parser compiled", held, i)
			}
			if held.patterns[i] != re.String() {
				t.Errorf("%s: source %d = %q; want %q", held, i, held.patterns[i], re)
			}
		}
	}
	raceskip.Skip(t)
	node := parsed[1]
	var compiled []*regexp.Regexp
	var built Constraint
	reading := testing.AllocsPerRun(100, func() { compiled = node.PatternRegexps() })
	building := testing.AllocsPerRun(100, func() { built, _ = p.constraint(node) })
	if building != reading+2 {
		t.Errorf("the loader allocates %v building a Pattern where reading its regexps allocates %v; want two more, the sources and the constraint", building, reading)
	}
	_, _ = compiled, built
}
