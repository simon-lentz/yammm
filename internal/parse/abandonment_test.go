package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// clusterAFixed reports whether an optional group whose inner parse fails can
// still reach the tree as a construct the source does not contain. While it is
// false the table below records each site without failing the suite; the
// remedy flips it in one edit.
const clusterAFixed = false

// abandonmentSite is one row of the acceptance criterion. lies reports what the
// tree recorded that the source does not say, or "" when the tree is honest.
type abandonmentSite struct {
	name string
	src  string
	lies func(f *File, toks []Token) string
}

// abandonmentSites is one row per optional group wrapping a construct that can
// fail. The shared shape is a marker token left standing, because participle
// abandons a group without consuming it.
var abandonmentSites = []abandonmentSite{
	{
		name: "length bounds",
		src:  "schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
		lies: boundsLie,
	},
	{
		name: "numeric bounds",
		src:  "schema \"s\"\ntype T {\n\tn Integer[abc, 2]\n}\n",
		lies: boundsLie,
	},
	{
		name: "timestamp format",
		src:  "schema \"s\"\ntype T {\n\tt Timestamp[\"x\"\n}\n",
		lies: func(f *File, toks []Token) string {
			p := firstProperty(f)
			if p == nil || p.Constraint == nil || p.Constraint.FormatLit != nil {
				return ""
			}
			if !standingAfter(toks, p.Constraint.Span, "[") {
				return ""
			}
			return "a format-less Timestamp, with the format's '[' still in the source"
		},
	},
	{
		name: "import alias",
		src:  "schema \"s\"\nimport \"a.yammm\" as one\n",
		lies: func(f *File, toks []Token) string {
			if len(f.Imports) != 1 || f.Imports[0].HasAlias {
				return ""
			}
			if !standingAfter(toks, f.Imports[0].Span, "as") {
				return ""
			}
			return "an alias-less import, with 'as' still in the source"
		},
	},
	{
		name: "relation reverse",
		src:  "schema \"s\"\ntype T {\n\t--> a B /\n}\n",
		lies: func(f *File, toks []Token) string {
			r := firstRelation(f)
			if r == nil || r.Backref != "" {
				return ""
			}
			if !standingAfter(toks, r.Span, "/") {
				return ""
			}
			return "a relation with no reverse, with the reverse's '/' still in the source"
		},
	},
	{
		name: "relation body",
		src:  "schema \"s\"\ntype T {\n\t--> r B { nil String }\n}\n",
		lies: func(f *File, toks []Token) string {
			r := firstRelation(f)
			if r == nil || len(r.Properties) > 0 {
				return ""
			}
			if !standingAfter(toks, r.Span, "{") {
				return ""
			}
			return "a relation with no edge properties, with the body's '{' still in the source"
		},
	},
	{
		name: "annotation arguments",
		src:  "schema \"s\"\ntype T {\n\tid String primary @vector(0x10)\n}\n",
		lies: func(f *File, toks []Token) string {
			p := firstProperty(f)
			if p == nil || len(p.Annotations) == 0 {
				return ""
			}
			a := p.Annotations[len(p.Annotations)-1]
			if a.HasParens || !standingAfter(toks, a.Span, "(") {
				return ""
			}
			return "an annotation with no argument list, with its '(' still in the source"
		},
	},
	{
		name: "extends",
		src:  "schema \"s\"\ntype T extends {\n\tid String primary\n\tx Integer\n}\n",
		// Two shapes lie: no type recorded at all, and a type recorded with no
		// supertype while 'extends' still stands after its name.
		lies: func(f *File, toks []Token) string {
			if !present(toks, "type") || !present(toks, "extends") {
				return ""
			}
			if len(f.Types) == 0 {
				return "no type at all, with 'type' and 'extends' both in the source"
			}
			ty := f.Types[0]
			if len(ty.Extends) > 0 || !standingAfter(toks, ty.NameSpan, "extends") {
				return ""
			}
			return "a supertype-less type, with 'extends' still standing after its name"
		},
	},
}

// TestAbandonment_TreeNeverContradictsTheSource is the acceptance criterion
// for the optional-group remedy. Until clusterAFixed flips it reports the
// defect and passes; after, every row is an assertion.
func TestAbandonment_TreeNeverContradictsTheSource(t *testing.T) {
	for _, tc := range abandonmentSites {
		t.Run(tc.name, func(t *testing.T) {
			file, _ := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			lie := tc.lies(file, Lex(tc.src))
			switch {
			case lie == "":
				if !clusterAFixed {
					t.Errorf("no lie found, but clusterAFixed is false — flip the constant")
				}
			case clusterAFixed:
				t.Errorf("tree records %s", lie)
			default:
				t.Logf("known defect, unfixed: tree records %s", lie)
			}
		})
	}
}

// TestAbandonment_ExtendsRowSeesARecordedTypeWithNoSupertype covers the shape
// no parse reaches yet. A remedy that records the type without its supertype
// must still fail the row, and keying on an empty type list alone passed it.
func TestAbandonment_ExtendsRowSeesARecordedTypeWithNoSupertype(t *testing.T) {
	row := siteNamed(t, "extends")
	toks := Lex(row.src)
	name := tokenNamed(t, toks, "T")

	remedied := &File{Types: []*TypeDecl{{
		Name: "T",
		NameSpan: location.RangeWithBytes(location.SourceID{},
			1, 1, name.Start, 1, 1, name.End),
	}}}

	if lie := row.lies(remedied, toks); lie == "" {
		t.Error("the extends row calls a type recorded without its supertype honest")
	}
}

func siteNamed(t *testing.T, name string) abandonmentSite {
	t.Helper()
	for _, tc := range abandonmentSites {
		if tc.name == name {
			return tc
		}
	}
	t.Fatalf("no %q row in abandonmentSites", name)
	return abandonmentSite{}
}

func tokenNamed(t *testing.T, toks []Token, value string) Token {
	t.Helper()
	for _, tok := range toks {
		if tok.Value == value {
			return tok
		}
	}
	t.Fatalf("no %q token in the source", value)
	return Token{}
}

// boundsLie is shared by the two bound-pair sites, which differ only in the
// constraint kind that carries them.
func boundsLie(f *File, toks []Token) string {
	p := firstProperty(f)
	if p == nil || p.Constraint == nil || p.Constraint.Bounds != nil {
		return ""
	}
	if !standingAfter(toks, p.Constraint.Span, "[") {
		return ""
	}
	return "an unbounded constraint, with the bound list's '[' still in the source"
}

// standingAfter reports whether marker is the first significant token at or
// after the end of span. That is where an abandoned group's opening token is
// left, because participle discards the branch without consuming it.
func standingAfter(toks []Token, span location.Span, marker string) bool {
	for _, t := range toks {
		if t.Start < span.End.Byte || isTrivia(t) {
			continue
		}
		return t.Value == marker
	}
	return false
}

func present(toks []Token, value string) bool {
	for _, t := range toks {
		if t.Value == value {
			return true
		}
	}
	return false
}

func isTrivia(t Token) bool {
	switch t.Kind {
	case "WS", "SL_COMMENT", "DOC_COMMENT":
		return true
	}
	return false
}

func firstProperty(f *File) *Property {
	for _, ty := range f.Types {
		for _, p := range ty.Properties {
			return p
		}
	}
	return nil
}

func firstRelation(f *File) *Relation {
	for _, ty := range f.Types {
		for _, r := range ty.Relations {
			return r
		}
	}
	return nil
}
