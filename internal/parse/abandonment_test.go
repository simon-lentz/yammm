package parse

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// These rows record what ten sites do today, so a replacement is scored against
// a measured bar rather than a remembered one. They are neither a survey of the
// grammar nor a statement of the rule that decides which construct survives.
// Every row draws exactly one diagnostic; the duplicates two of them once
// recorded went with the recovery predicate's repair.
var abandonedGroups = []struct {
	name   string
	src    string
	want   string // the first diagnostic's message
	anchor string // the source text its span covers
	types  int    // declarations surviving in the tree
	count  int    // diagnostics the input draws
}{
	{
		name:   "length bounds on String",
		src:    "schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
		want:   `unexpected token "[" in a type body`,
		anchor: "[",
		types:  1,
		count:  1,
	},
	{
		name:   "length bounds on List",
		src:    "schema \"s\"\ntype T {\n\tl List<String>[-1, 2]\n}\n",
		want:   `unexpected token "[" in a type body`,
		anchor: "[",
		types:  1,
		count:  1,
	},
	{
		// The second diagnostic lands on the ',' inside the member recovery
		// just left. Recorded, not endorsed: it is one defect reported twice.
		name:   "numeric bounds",
		src:    "schema \"s\"\ntype T {\n\tn Integer[abc, 2]\n}\n",
		want:   `unexpected token "[" in a type body`,
		anchor: "[",
		types:  1,
		count:  1,
	},
	{
		name:   "timestamp format",
		src:    "schema \"s\"\ntype T {\n\tt Timestamp[\"x\"\n}\n",
		want:   `unexpected token "[" in a type body`,
		anchor: "[",
		types:  1,
		count:  1,
	},
	{
		name:   "import alias",
		src:    "schema \"s\"\nimport \"a.yammm\" as one\n",
		want:   `unexpected token "as" in a declaration`,
		anchor: "as",
		types:  0,
		count:  1,
	},
	{
		name:   "reverse clause",
		src:    "schema \"s\"\ntype T {\n\t--> r B /\n}\n",
		want:   `unexpected token "/" in a type body`,
		anchor: "/",
		types:  1,
		count:  1,
	},
	{
		name:   "relation body",
		src:    "schema \"s\"\ntype T {\n\t--> r B { nil String }\n}\n",
		want:   `unexpected token "{" in a type body`,
		anchor: "{",
		types:  1,
		count:  1,
	},
	{
		name:   "annotation arguments",
		src:    "schema \"s\"\ntype T {\n\tid String primary @a(*)\n}\n",
		want:   `unexpected token "(" in a type body`,
		anchor: "(",
		types:  1,
		count:  1,
	},
	{
		name:   "multiplicity",
		src:    "schema \"s\"\ntype T {\n\t--> r (many:one) B\n}\n",
		want:   `unexpected token "(" in a type body`,
		anchor: "(",
		types:  1,
		count:  1,
	},
	{
		name:   "extends clause",
		src:    "schema \"s\"\ntype T extends {\n\tid String primary\n}\n",
		want:   `unexpected token "extends" in a declaration`,
		anchor: "extends",
		types:  0,
		count:  1,
	},
}

func TestAbandonment_GroupsReportAgainstTheEnclosingConstruct(t *testing.T) {
	for _, tc := range abandonedGroups {
		t.Run(tc.name, func(t *testing.T) {
			f, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if len(issues) != tc.count {
				t.Fatalf("got %d diagnostics, want %d: %v", len(issues), tc.count, issues)
			}
			if len(issues) == 0 {
				return // a row may record a site that reports nothing
			}
			// diag.Result.OK() counts only Fatal and Error, so a severity
			// downgrade here would let every one of these sources load clean.
			for i, iss := range issues {
				if iss.Code() != diag.E_SYNTAX || iss.Severity() != diag.Error {
					t.Errorf("diagnostic %d is %s at %v, want E_SYNTAX at error",
						i, iss.Code(), iss.Severity())
				}
			}
			if issues[0].Message() != tc.want {
				t.Errorf("message = %q, want %q", issues[0].Message(), tc.want)
			}
			sp := issues[0].Span()
			if covered := tc.src[sp.Start.Byte:sp.End.Byte]; covered != tc.anchor {
				t.Errorf("anchored on %q, want %q", covered, tc.anchor)
			}
			if len(f.Types) != tc.types {
				t.Errorf("recorded %d type declarations, want %d", len(f.Types), tc.types)
			}
		})
	}
}

// TestAbandonment_RecoveryStaysInsideTheDeclaration pins that abandoning a
// group costs only that group: later declarations survive, the next member
// keeps its own kind, and a rejected multiplicity records no relation.
func TestAbandonment_RecoveryStaysInsideTheDeclaration(t *testing.T) {
	t.Run("an unterminated bound list keeps later declarations", func(t *testing.T) {
		src := "schema \"s\"\ntype Code = String [5, 5\ntype Alpha {\n\tid String primary\n}\ntype Beta {\n\tid String primary\n}\n"
		f, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
		var names []string
		for _, ty := range f.Types {
			names = append(names, ty.Name)
		}
		if strings.Join(names, ",") != "Alpha,Beta" {
			t.Errorf("types = %v, want [Alpha Beta]", names)
		}
	})

	t.Run("a failed reverse clause keeps the next association", func(t *testing.T) {
		src := "schema \"s\"\ntype T {\n\t--> r B /\n\t--> q C\n}\n"
		f, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if len(f.Types) != 1 {
			t.Fatalf("got %d types, want 1", len(f.Types))
		}
		var rels []string
		for _, r := range f.Types[0].Relations {
			rels = append(rels, r.Name)
		}
		if strings.Join(rels, ",") != "r,q" {
			t.Errorf("relations = %v, want [r q]", rels)
		}
		if n := len(f.Types[0].Properties); n != 0 {
			t.Errorf("recorded %d properties, want 0 — an association must not be reclassified", n)
		}
	})

	t.Run("a rejected multiplicity records no relation", func(t *testing.T) {
		src := "schema \"s\"\ntype T {\n\tid String primary\n\t--> r (many:one) B\n}\n"
		f, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
		if len(f.Types) != 1 {
			t.Fatalf("got %d types, want 1", len(f.Types))
		}
		// multNode admits no tail after 'many', so the association fails to
		// parse and no relation is built with a cardinality nobody wrote.
		if n := len(f.Types[0].Relations); n != 0 {
			t.Errorf("recorded %d relations, want 0", n)
		}
	})
}
