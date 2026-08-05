package parse

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// TestReconcile_AbandonedGroupsNameTheirConstruct pins the text each abandoned
// group is reported under. The acceptance criterion checks only that the anchor
// moved off the marker, so without this a group could be re-parsed and then
// labelled as any construct at all.
func TestReconcile_AbandonedGroupsNameTheirConstruct(t *testing.T) {
	tests := []struct {
		name, src, want string
	}{
		{
			"length bounds",
			"schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
			`unexpected token "-" in a length bound list`,
		},
		{
			"numeric bounds",
			"schema \"s\"\ntype T {\n\tn Integer[abc, 2]\n}\n",
			`unexpected token "abc" in a numeric bound list`,
		},
		{
			"timestamp format",
			"schema \"s\"\ntype T {\n\tt Timestamp[\"x\"\n}\n",
			`unexpected token "}" in a timestamp format`,
		},
		{
			"import alias",
			"schema \"s\"\nimport \"a.yammm\" as one\n",
			`unexpected token "one" in an import alias`,
		},
		{
			"reverse clause",
			"schema \"s\"\ntype T {\n\t--> a B /\n}\n",
			`unexpected token "}" in a reverse clause`,
		},
		{
			"relation body",
			"schema \"s\"\ntype T {\n\t--> r B { nil String }\n}\n",
			`unexpected token "nil" in a relation body`,
		},
		{
			"extends clause",
			"schema \"s\"\ntype T extends {\n\tid String primary\n}\n",
			`unexpected token "{" in an extends clause`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			var got []string
			for _, iss := range issues {
				if iss.Message() == tc.want {
					return
				}
				got = append(got, iss.Message())
			}
			t.Errorf("no diagnostic reads %q; got %q", tc.want, got)
		})
	}
}

// TestReconcile_MalformedNumericStillWins pins the one site reconciliation must
// not touch: a malformed literal inside an abandoned group keeps the message
// production emits for it, which is the anchor the migration already matches.
func TestReconcile_MalformedNumericStillWins(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary @vector(0x10)\n}\n"

	_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	if want := InvalidNumberMessage("0x10"); issues[0].Message() != want {
		t.Errorf("message = %q, want %q", issues[0].Message(), want)
	}
	if got := src[issues[0].Span().Start.Byte:issues[0].Span().End.Byte]; got != "0x10" {
		t.Errorf("anchored on %q, want the literal", got)
	}
}

// TestReconcile_LeavesWellFormedSourcesAlone pins that reconciliation reads the
// tree, not the text: a construct that legitimately omits an optional group
// must record nothing to re-parse, or every clean parse pays for a lookup.
func TestReconcile_LeavesWellFormedSourcesAlone(t *testing.T) {
	file, issues := Parse([]byte(smokeSource), location.NewSourceID("demo.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(file.Types) != 1 {
		t.Fatalf("Types = %d, want 1", len(file.Types))
	}
}

// TestReconcile_EveryGroupHasADistinctConstructName keeps two sites from being
// reported under one name, which would make a diagnostic ambiguous about which
// construct the author got wrong.
func TestReconcile_EveryGroupHasADistinctConstructName(t *testing.T) {
	groups := []markedGroup{
		numBoundsGroup, lenBoundsGroup, timFormatGroup, aliasGroup,
		reverseGroup, relBodyGroup, argsGroup, extendsGroup,
	}
	seen := map[string]bool{}
	for _, g := range groups {
		if g.marker == "" || g.construct == "" || g.reparse == nil {
			t.Errorf("group %q is incompletely declared", g.construct)
		}
		if !strings.HasPrefix(g.construct, "in a") {
			t.Errorf("construct %q does not read as a place in the source", g.construct)
		}
		if seen[g.construct] {
			t.Errorf("construct name %q is used twice", g.construct)
		}
		seen[g.construct] = true
	}
	if len(seen) != len(groups) {
		t.Errorf("got %d distinct construct names, want %d", len(seen), len(groups))
	}
}
