package schema_test

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

const patternLoadSource = `schema "patterns"

type Zip = Pattern["^[0-9]{5}$"]
type Zips = List<Zip>[1, 3]

type Site {
	id String primary
	code Pattern["^[A-Z]{2}-[0-9]+$"]
	both Pattern["^a", "b$"]
	first Pattern["a|ab"]
	fold Pattern["(?i)hello"]
	class Pattern["^[\\p{Greek}\\d]+$"]
	escaped Pattern["^\\x{1F600}\\Q.*\\E$"]
	lazy Pattern["a+?"]
	zip Zip
	zips Zips
	nested List<List<Pattern["^x"]>>
	--> NEAR (many) Site {
		note Pattern["^n"]
	}
}
`

var patternLoadValues = []string{"", "a", "ab", "aab", "b", "NY-12", "ny-12", "HeLLo", "αβγ9", "😀.*", "12345", "123456", "xab", "n"}

// patternOf returns the Pattern a constraint holds under references and Lists.
func patternOf(t *testing.T, c schema.Constraint) schema.PatternConstraint {
	t.Helper()
	for {
		switch held := schema.ResolveAlias(c).(type) {
		case schema.PatternConstraint:
			return held
		case schema.ListConstraint:
			c = held.Element()
		default:
			t.Fatalf("%s holds no Pattern", c)
		}
	}
}

// A Pattern a load holds is the one the exported constructor builds from the
// same sources: by Equal, by String, in the structural hash, and in what each
// compiled regexp matches and finds.
func TestLoad_HoldsThePatternsItsSourceStates(t *testing.T) {
	s, res := schema.LoadString(t.Context(), patternLoadSource, "patterns.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	site, _ := s.Type("Site")
	near, _ := site.Relation("NEAR")
	note, _ := near.Property("note")

	var b strings.Builder
	record := func(name string, c schema.Constraint) {
		p := patternOf(t, c)
		compiled := make([]*regexp.Regexp, 0, p.PatternCount())
		for _, source := range p.Patterns() {
			compiled = append(compiled, regexp.MustCompile(source))
		}
		built := schema.NewPatternConstraint(compiled)
		fmt.Fprintf(&b, "%s: %s as %s\n", name, c, p)
		fmt.Fprintf(&b, "\tequal to the constructor's: %t, %t\n", p.Equal(built), built.Equal(p))
		fmt.Fprintf(&b, "\tCheckConstraint: %v\n", schema.CheckConstraint(c))
		for i, re := range p.CompiledPatterns() {
			if re.String() != p.Patterns()[i] {
				t.Errorf("%s: compiled pattern %d reads %q; its source is %q", name, i, re, p.Patterns()[i])
			}
			var answers []string
			for _, v := range patternLoadValues {
				if got, want := re.FindString(v), compiled[i].FindString(v); got != want {
					t.Errorf("%s: %q finds %q in %q; a compile of its source finds %q", name, re, got, v, want)
				}
				answers = append(answers, fmt.Sprintf("%t:%q", re.MatchString(v), re.FindString(v)))
			}
			fmt.Fprintf(&b, "\t%q: %s\n", re, strings.Join(answers, " "))
		}
	}
	for p := range site.AllProperties() {
		if p.Name() != "id" {
			record(p.Name(), p.Constraint())
		}
	}
	record("NEAR.note", note.Constraint())
	for _, dt := range s.DataTypesSlice() {
		record("type "+dt.Name(), dt.Constraint())
	}
	fmt.Fprintf(&b, "structural hash: %s\n", schema.StructuralHash(s))
	yammmtest.Golden(t, "pattern_load", []byte(b.String()))
}
