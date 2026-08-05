package parse

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// groupSites is the acceptance criterion for the optional-group captures: one
// row per group, written so its inner parse fails. Each pins that the
// diagnostic names that group's construct and anchors on the token inside it
// that failed, not on the marker the group begins with.
var groupSites = []struct {
	name   string
	group  string // the capture under test; rows sharing one share its wording
	src    string
	want   string // the whole message
	anchor string // the source text the span must cover
	marker string // the group's own opening token, which must not be the anchor
}{
	{
		name: "integer bounds", group: "numBounds", marker: "[", anchor: "abc",
		src:  "schema \"s\"\ntype T {\n\tn Integer[abc, 2]\n}\n",
		want: `unexpected token "abc" in a numeric bound list`,
	},
	{
		name: "float bounds", group: "numBounds", marker: "[", anchor: "abc",
		src:  "schema \"s\"\ntype T {\n\tf Float[abc, 2]\n}\n",
		want: `unexpected token "abc" in a numeric bound list`,
	},
	{
		name: "string length bounds", group: "lenBounds", marker: "[", anchor: "-",
		src:  "schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
		want: `unexpected token "-" in a length bound list`,
	},
	{
		name: "list length bounds", group: "lenBounds", marker: "[", anchor: "-",
		src:  "schema \"s\"\ntype T {\n\tl List<String>[-1, 2]\n}\n",
		want: `unexpected token "-" in a length bound list`,
	},
	{
		name: "timestamp format", group: "timFormat", marker: "[", anchor: "123",
		src:  "schema \"s\"\ntype T {\n\tt Timestamp[123]\n}\n",
		want: `unexpected token "123" in a timestamp format`,
	},
	{
		name: "import alias", group: "alias", marker: "as", anchor: "one",
		src:  "schema \"s\"\nimport \"a.yammm\" as one\n",
		want: `unexpected token "one" in an import alias`,
	},
	{
		name: "association reverse", group: "reverse", marker: "/", anchor: "}",
		src:  "schema \"s\"\ntype T {\n\t--> a B /\n}\n",
		want: `unexpected token "}" in a reverse clause`,
	},
	{
		name: "composition reverse", group: "reverse", marker: "/", anchor: "}",
		src:  "schema \"s\"\ntype T {\n\t*-> c B /\n}\n",
		want: `unexpected token "}" in a reverse clause`,
	},
	{
		name: "relation body", group: "relBody", marker: "{", anchor: "nil",
		src:  "schema \"s\"\ntype T {\n\t--> r B { nil String }\n}\n",
		want: `unexpected token "nil" in a relation body`,
	},
	{
		name: "extends clause", group: "extends", marker: "extends", anchor: "{",
		src:  "schema \"s\"\ntype T extends {\n\tid String primary\n}\n",
		want: `unexpected token "{" in an extends clause`,
	},
}

func TestGroups_FailureNamesItsConstructAndAnchorsInsideIt(t *testing.T) {
	for _, tc := range groupSites {
		t.Run(tc.name, func(t *testing.T) {
			_, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			// One defect per source, so a second diagnostic means the resync
			// left tokens the enclosing construct then failed on separately.
			if len(issues) != 1 {
				var got []string
				for _, iss := range issues {
					got = append(got, iss.Message())
				}
				t.Fatalf("got %d diagnostics, want exactly 1: %q", len(issues), got)
			}
			if issues[0].Message() != tc.want {
				t.Fatalf("message = %q, want %q", issues[0].Message(), tc.want)
			}
			span := issues[0].Span()
			covered := tc.src[span.Start.Byte:span.End.Byte]
			if covered != tc.anchor {
				t.Errorf("anchored on %q, want %q", covered, tc.anchor)
			}
			if covered == tc.marker {
				t.Errorf("anchored on the group's own marker %q", tc.marker)
			}
		})
	}
}

// TestGroups_ConstructNamesAreDistinct keeps two groups from reporting under one
// name, which would leave a diagnostic ambiguous about what the author got
// wrong. It reads the names off the criterion table, so a group added there
// without its own wording fails.
func TestGroups_ConstructNamesAreDistinct(t *testing.T) {
	nameOf := map[string]string{}
	groupOf := map[string]string{}
	for _, tc := range groupSites {
		construct, ok := strings.CutPrefix(tc.want, `unexpected token "`+tc.anchor+`" `)
		if !ok {
			t.Errorf("%s: message %q does not name its anchor", tc.name, tc.want)
			continue
		}
		if !strings.HasPrefix(construct, "in a") {
			t.Errorf("%s: construct %q does not read as a place in the source", tc.name, construct)
		}
		if prev, seen := nameOf[tc.group]; seen && prev != construct {
			t.Errorf("group %q reports as both %q and %q", tc.group, prev, construct)
		}
		if prev, seen := groupOf[construct]; seen && prev != tc.group {
			t.Errorf("construct %q is used by groups %q and %q", construct, prev, tc.group)
		}
		nameOf[tc.group], groupOf[construct] = construct, tc.group
	}
	if len(nameOf) != len(groupOf) {
		t.Errorf("%d groups map onto %d construct names", len(nameOf), len(groupOf))
	}
}

// TestGroups_WellFormedSourceRecordsTheParsedGroup covers the half the failure
// table cannot: a group that is written correctly must reach the tree as a
// parsed value, not merely fail to report. A capture that returned the absent
// state on every input would satisfy the table above and lose every bound.
func TestGroups_WellFormedSourceRecordsTheParsedGroup(t *testing.T) {
	src := "schema \"s\"\n" +
		"type T {\n" +
		"\tid String[1, 8] primary\n" +
		"\tn Integer[0, 10]\n" +
		"\tt Timestamp[\"2006\"]\n" +
		"\t--> a T /back (many)\n" +
		"}\n"

	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	props := file.Types[0].Properties
	if got := props[0].Constraint.LenMin; got == nil || *got != 1 {
		t.Errorf("string length bounds not recorded: %v", got)
	}
	if got := props[1].Constraint.IntMax; got == nil || *got != 10 {
		t.Errorf("integer bounds not recorded: %v", got)
	}
	if format, ok := props[2].Constraint.Format(); !ok || format != "2006" {
		t.Errorf("timestamp format not recorded: %q %v", format, ok)
	}
	rel := file.Types[0].Relations[0]
	if rel.Backref != "back" {
		t.Errorf("reverse clause not recorded: %q", rel.Backref)
	}
	if !rel.ReverseMany {
		t.Error("reverse multiplicity not recorded")
	}
}

// TestGroups_FailedExtendsKeepsItsDeclaration pins that a group failing does not
// cost the construct around it. typeHead needs the '{' the extends clause failed
// on, so the capture consumes its own marker and nothing else; consuming the
// brace instead drops the whole type, which is what the oracle does not do.
func TestGroups_FailedExtendsKeepsItsDeclaration(t *testing.T) {
	src := "schema \"s\"\ntype T extends {\n\tid String primary\n}\n"

	file, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(file.Types) != 1 {
		t.Fatalf("Types = %d, want the declaration to survive its failed extends clause", len(file.Types))
	}
	ty := file.Types[0]
	if ty.Name != "T" {
		t.Errorf("type name = %q, want \"T\"", ty.Name)
	}
	if len(ty.Extends) != 0 {
		t.Errorf("Extends = %d, want none recorded when the clause failed", len(ty.Extends))
	}
	if len(ty.Properties) != 1 {
		t.Errorf("Properties = %d, want the body to survive too", len(ty.Properties))
	}
}

// TestGroups_UnterminatedGroupMatchesProduction pins where a delimited group
// with no closer of its own stops: it consumes the enclosing construct's closer
// and the body then ends at EOF. Production reports the same shape at the same
// anchors, so this is parity rather than a recovery choice this package made.
func TestGroups_UnterminatedGroupMatchesProduction(t *testing.T) {
	tests := []struct{ name, src, want string }{
		{
			"relation body", "schema \"s\"\ntype T {\n\tid String primary\n\t--> r B {\n}\n",
			"unexpected end of input in type body",
		},
		{
			"length bounds", "schema \"s\"\ntype T {\n\ts String[1\n}\n",
			`unexpected token "}" in a length bound list`,
		},
		{
			"annotation arguments", "schema \"s\"\ntype T {\n\tid String primary @a(\n}\n",
			`unexpected token "}" in an annotation argument list`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			file, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(file.Types) != 1 {
				t.Errorf("Types = %d, want the declaration to survive", len(file.Types))
			}
			for _, iss := range issues {
				if iss.Message() == tc.want {
					return
				}
			}
			t.Errorf("no diagnostic reads %q; got %v", tc.want, issues)
		})
	}
}

// TestGroups_MalformedNumericStillWins pins the one site the group captures must
// not take over: a malformed literal inside an annotation's argument list keeps
// the message production emits for it, which the migration already matches.
func TestGroups_MalformedNumericStillWins(t *testing.T) {
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
