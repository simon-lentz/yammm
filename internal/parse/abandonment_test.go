package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// clusterAFixed reports whether an abandoned optional group is diagnosed on the
// token that failed inside it rather than on the marker it left standing.
const clusterAFixed = true

// abandonmentSite is one row of the acceptance criterion. present separates the
// two ways a row can stop reporting: every markerAt function gives up on a tree
// it cannot find its node in, so a remedy that drops the declaration outright
// scores as a fix without present to catch it.
type abandonmentSite struct {
	name string
	src  string

	// dropsDeclaration marks a site whose declaration recovery discards rather
	// than misrecords, so the row asserts the diagnostic and not the tree.
	dropsDeclaration bool

	present func(f *File) bool

	// markerAt returns the offset of the optional group's opening token when the
	// tree records that group as absent, and -1 when the part is recorded.
	markerAt func(f *File, toks []Token) int
}

// abandonmentSites is one row per optional group wrapping a construct that can
// fail. The shared shape is a marker token left standing, because participle
// abandons a group without consuming it.
var abandonmentSites = []abandonmentSite{
	{
		name:     "length bounds",
		src:      "schema \"s\"\ntype T {\n\ts String[-1, 2]\n}\n",
		present:  func(f *File) bool { return firstProperty(f) != nil },
		markerAt: boundsMarker,
	},
	{
		name:     "numeric bounds",
		src:      "schema \"s\"\ntype T {\n\tn Integer[abc, 2]\n}\n",
		present:  func(f *File) bool { return firstProperty(f) != nil },
		markerAt: boundsMarker,
	},
	{
		name:    "timestamp format",
		src:     "schema \"s\"\ntype T {\n\tt Timestamp[\"x\"\n}\n",
		present: func(f *File) bool { return firstProperty(f) != nil },
		markerAt: func(f *File, toks []Token) int {
			p := firstProperty(f)
			if p == nil || p.Constraint == nil || p.Constraint.FormatLit != nil {
				return -1
			}
			return standingAfter(toks, p.Constraint.Span, "[")
		},
	},
	{
		name:    "import alias",
		src:     "schema \"s\"\nimport \"a.yammm\" as one\n",
		present: func(f *File) bool { return len(f.Imports) == 1 },
		markerAt: func(f *File, toks []Token) int {
			if len(f.Imports) != 1 || f.Imports[0].HasAlias {
				return -1
			}
			return standingAfter(toks, f.Imports[0].Span, "as")
		},
	},
	{
		name:    "relation reverse",
		src:     "schema \"s\"\ntype T {\n\t--> a B /\n}\n",
		present: func(f *File) bool { return firstRelation(f) != nil },
		markerAt: func(f *File, toks []Token) int {
			r := firstRelation(f)
			if r == nil || r.Backref != "" {
				return -1
			}
			return standingAfter(toks, r.Span, "/")
		},
	},
	{
		name:    "relation body",
		src:     "schema \"s\"\ntype T {\n\t--> r B { nil String }\n}\n",
		present: func(f *File) bool { return firstRelation(f) != nil },
		markerAt: func(f *File, toks []Token) int {
			r := firstRelation(f)
			if r == nil || len(r.Properties) > 0 {
				return -1
			}
			return standingAfter(toks, r.Span, "{")
		},
	},
	{
		name:    "annotation arguments",
		src:     "schema \"s\"\ntype T {\n\tid String primary @vector(0x10)\n}\n",
		present: func(f *File) bool { p := firstProperty(f); return p != nil && len(p.Annotations) > 0 },
		markerAt: func(f *File, toks []Token) int {
			p := firstProperty(f)
			if p == nil || len(p.Annotations) == 0 {
				return -1
			}
			a := p.Annotations[len(p.Annotations)-1]
			if a.HasParens {
				return -1
			}
			return standingAfter(toks, a.Span, "(")
		},
	},
	{
		name:             "extends",
		src:              "schema \"s\"\ntype T extends {\n\tid String primary\n\tx Integer\n}\n",
		dropsDeclaration: true,
		present:          func(f *File) bool { return len(f.Types) > 0 },
		markerAt: func(f *File, toks []Token) int {
			if len(f.Types) == 0 {
				return offsetOf(toks, "extends")
			}
			ty := f.Types[0]
			if len(ty.Extends) > 0 {
				return -1
			}
			return standingAfter(toks, ty.NameSpan, "extends")
		},
	},
}

// TestAbandonment_TreeNeverContradictsTheSource is the acceptance criterion for
// the optional-group remedy. An abandoned group must be diagnosed on the token
// that failed inside it: reporting on the marker itself says only that the
// enclosing construct stopped there, which is true of every parse failure and
// tells the author nothing about what they wrote wrong.
func TestAbandonment_TreeNeverContradictsTheSource(t *testing.T) {
	for _, tc := range abandonmentSites {
		t.Run(tc.name, func(t *testing.T) {
			file, issues := Parse([]byte(tc.src), location.NewSourceID("s.yammm"))
			if defect := abandonmentDefect(tc, file, Lex(tc.src), issues); defect != "" {
				if clusterAFixed {
					t.Error(defect)
				} else {
					t.Logf("known defect, unfixed: %s", defect)
				}
			} else if !clusterAFixed {
				t.Error("no defect found, but clusterAFixed is false — flip the constant")
			}
		})
	}
}

func abandonmentDefect(tc abandonmentSite, f *File, toks []Token, issues []diag.Issue) string {
	if !tc.dropsDeclaration && !tc.present(f) {
		return "the construct never reached the tree"
	}
	at := tc.markerAt(f, toks)
	if at < 0 {
		return ""
	}
	var after bool
	for _, iss := range issues {
		switch start := iss.Span().Start.Byte; {
		case start == at:
			return "a diagnostic points at the abandoned group's marker instead of at what failed inside it"
		case start > at:
			after = true
		}
	}
	if !after {
		return "the group was abandoned with nothing reported inside it"
	}
	return ""
}

// TestAbandonment_ExtendsRowSeesARecordedTypeWithNoSupertype covers the shape
// no parse reaches yet. A remedy that records the type without its supertype
// must still report, and keying on an empty type list alone passed it.
func TestAbandonment_ExtendsRowSeesARecordedTypeWithNoSupertype(t *testing.T) {
	row := siteNamed(t, "extends")
	toks := Lex(row.src)
	name := tokenNamed(t, toks, "T")

	remedied := &File{Types: []*TypeDecl{{
		Name: "T",
		NameSpan: location.RangeWithBytes(location.SourceID{},
			1, 1, name.Start, 1, 1, name.End),
	}}}

	if row.markerAt(remedied, toks) < 0 {
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

// boundsMarker is shared by the two bound-pair sites, which differ only in the
// constraint kind that carries them.
func boundsMarker(f *File, toks []Token) int {
	p := firstProperty(f)
	if p == nil || p.Constraint == nil || p.Constraint.Bounds != nil {
		return -1
	}
	return standingAfter(toks, p.Constraint.Span, "[")
}

// standingAfter returns the offset of marker when it is the first significant
// token at or after the end of span, and -1 otherwise. That is where an
// abandoned group's opening token is left, because participle discards the
// branch without consuming it.
func standingAfter(toks []Token, span location.Span, marker string) int {
	for _, t := range toks {
		if t.Start < span.End.Byte || isTrivia(t) {
			continue
		}
		if t.Value == marker {
			return t.Start
		}
		return -1
	}
	return -1
}

func offsetOf(toks []Token, value string) int {
	for _, t := range toks {
		if t.Value == value {
			return t.Start
		}
	}
	return -1
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
