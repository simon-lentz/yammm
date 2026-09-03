package parse

import (
	"fmt"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

func relSource(members string) string {
	return "schema \"s\"\ntype T {\n\tid String primary\n\t" + members +
		"\n}\npart type Wheel {\n\tid String primary\n}\n"
}

// TestRelation_MultiplicityTable states the meaning of every spelling the
// grammar admits, and that an omitted multiplicity means optional and single.
func TestRelation_MultiplicityTable(t *testing.T) {
	tests := []struct {
		spelling     string
		wantOptional bool
		wantMany     bool
	}{
		{"", true, false},
		{"(_)", true, false},
		{"(_:one)", true, false},
		{"(_:many)", true, true},
		{"(one)", false, false},
		{"(one:one)", false, false},
		{"(one:many)", false, true},
		{"(many)", true, true},
	}

	for _, tc := range tests {
		name := tc.spelling
		if name == "" {
			name = "omitted"
		}
		t.Run(name, func(t *testing.T) {
			src := relSource(fmt.Sprintf("--> WHEELS %s Wheel", tc.spelling))
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("unexpected issues: %v", issues)
			}
			rels := file.Types[0].Relations
			if len(rels) != 1 {
				t.Fatalf("got %d relations, want 1", len(rels))
			}
			if rels[0].Optional != tc.wantOptional || rels[0].Many != tc.wantMany {
				t.Errorf("%q → optional=%v many=%v, want optional=%v many=%v",
					tc.spelling, rels[0].Optional, rels[0].Many, tc.wantOptional, tc.wantMany)
			}
		})
	}
}

// TestRelation_MultiplicitySpellingsOutsideTheTableAreRejected pins that
// "many" takes no tail, which the flattened reading of the pair would wrongly
// admit.
func TestRelation_MultiplicitySpellingsOutsideTheTableAreRejected(t *testing.T) {
	for _, spelling := range []string{"(many:one)", "(many:many)", "(:one)", "(one:)", "(two)", "(_:two)"} {
		t.Run(spelling, func(t *testing.T) {
			src := relSource("--> WHEELS " + spelling + " Wheel")
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("%q accepted, want rejected", spelling)
			}
		})
	}
}

// TestRelation_AssociationShapes covers the parts an association can carry:
// a reverse name with its own multiplicity, a body of edge properties, an
// empty body, and no body at all.
func TestRelation_AssociationShapes(t *testing.T) {
	src := relSource(`/* Owns wheels. */
	--> WHEELS (many) Wheel {
		/* When fitted. */
		fitted Timestamp required
		note String
	}
	--> SPARE Wheel
	--> TOOLS (many) Wheel {}`)
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 3 {
		t.Fatalf("got %d relations, want 3", len(rels))
	}

	full := rels[0]
	if full.Kind != RelationAssociation || full.Name != "WHEELS" || full.Target.Name != "Wheel" {
		t.Errorf("relation = %+v", full)
	}
	if full.Doc != "Owns wheels." {
		t.Errorf("doc = %q, want %q", full.Doc, "Owns wheels.")
	}
	if !full.Optional || !full.Many {
		t.Errorf("forward = optional %v many %v, want true true", full.Optional, full.Many)
	}
	if len(full.Properties) != 2 {
		t.Fatalf("got %d edge properties, want 2", len(full.Properties))
	}
	if p := full.Properties[0]; p.Name != "fitted" || !p.IsRequired || p.IsPrimaryKey || p.Doc != "When fitted." {
		t.Errorf("edge property = %+v", p)
	}
	if p := full.Properties[1]; p.Name != "note" || p.IsRequired {
		t.Errorf("edge property = %+v", p)
	}

	bare := rels[1]
	if bare.Name != "SPARE" {
		t.Errorf("bare relation = %+v, want spare", bare)
	}
	if len(bare.Properties) != 0 {
		t.Errorf("bare relation carries %d edge properties, want 0", len(bare.Properties))
	}
	if got := len(rels[2].Properties); got != 0 {
		t.Errorf("empty body produced %d edge properties, want 0", got)
	}
}

// TestRelation_CompositionTakesNoBody pins the one structural difference
// between the two forms.
func TestRelation_CompositionTakesNoBody(t *testing.T) {
	file, issues := Parse([]byte(relSource("*-> WHEELS (many) Wheel")), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 1 || rels[0].Kind != RelationComposition || rels[0].Name != "WHEELS" {
		t.Fatalf("relations = %+v, want one composition named WHEELS", rels)
	}
	// The composition arm of multiplicityOf has no other reader. (one) is the
	// spelling to assert: (many) is (true, true), which a hardcoded pair also
	// satisfies, so it cannot tell a real read of c.Mult from a constant.
	// An asymmetric spelling, so swapping the two fields is detectable: every
	// symmetric pair survives a swap, and (many) is one.
	asym, issues := Parse([]byte(relSource("*-> PARTS (one:many) Wheel")), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	if len(asym.Types) == 0 || len(asym.Types[0].Relations) != 1 {
		t.Fatalf("got %d types, want one carrying one relation", len(asym.Types))
	}
	if got := asym.Types[0].Relations[0]; got.Optional || !got.Many {
		t.Errorf("(one:many) → optional=%v many=%v, want false true", got.Optional, got.Many)
	}
	if !rels[0].Optional || !rels[0].Many {
		t.Errorf("(many) → optional=%v many=%v, want both true", rels[0].Optional, rels[0].Many)
	}

	_, issues = Parse([]byte(relSource("*-> WHEELS (many) Wheel { note String }")), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Error("a composition with a body was accepted, want rejected")
	}
}

// TestRelation_EdgePropertyConstraintIsForwarded pins that an edge property
// carries the constraint its datatype built. Nothing else reads it, so an
// edge property that stopped forwarding would reach the graph model as an
// Integer whatever the source declared.
func TestRelation_EdgePropertyConstraintIsForwarded(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\t--> R B { since Timestamp[\"2006\"] }\n}\n"

	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))

	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	props := file.Types[0].Relations[0].Properties
	if len(props) != 1 {
		t.Fatalf("edge properties = %d, want 1", len(props))
	}
	c := props[0].Constraint
	if c == nil || c.Kind != ConstraintTimestamp {
		t.Fatalf("edge property constraint = %+v, want a Timestamp", c)
	}
	if got, ok := c.Format(); !ok || got != "2006" {
		t.Errorf("format = %q ok=%v, want \"2006\" true", got, ok)
	}
}

// TestRelation_EdgePropertiesRejectPrimary pins that an edge property admits
// 'required' and nothing else.
func TestRelation_EdgePropertiesRejectPrimary(t *testing.T) {
	_, issues := Parse([]byte(relSource("--> WHEELS Wheel {\n\t\tnote String primary\n\t}")), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Error("an edge property marked primary was accepted, want rejected")
	}
}

// TestRelation_NamesAreUpperSnake pins the relation-name production: an
// uppercase letter, then uppercase letters, digits or underscores. The
// lower-case spelling is the field name, so any other casing is refused at
// the name's span and the declaration still projects.
func TestRelation_NamesAreUpperSnake(t *testing.T) {
	for _, name := range []string{"WHEELS", "W2", "HAS_2_PARTS", "X"} {
		t.Run("accepts_"+name, func(t *testing.T) {
			src := relSource("--> " + name + " Wheel")
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 0 {
				t.Fatalf("relation name %q rejected: %v", name, issues)
			}
			if got := file.Types[0].Relations[0].Name; got != name {
				t.Errorf("name = %q, want %q", got, name)
			}
		})
	}
	for _, name := range []string{"wheels", "Wheels", "w2", "WHEELs"} {
		t.Run("refuses_"+name, func(t *testing.T) {
			src := relSource("--> " + name + " Wheel")
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) != 1 || issues[0].Code() != diag.E_INVALID_NAME {
				t.Fatalf("relation name %q: got %v, want one E_INVALID_NAME", name, issues)
			}
			if got := src[issues[0].Span().Start.Byte:issues[0].Span().End.Byte]; got != name {
				t.Errorf("diagnostic span covers %q, want the name %q", got, name)
			}
			if len(file.Types[0].Relations) != 1 {
				t.Errorf("the declaration must still project; got %d relations", len(file.Types[0].Relations))
			}
		})
	}
}

// TestRelation_ReverseSeparatorIsNotARegex pins the lexical hazard the
// removed clause's recognizer keeps: its separator is a slash, and a slash
// pair on one line is a regex literal. A clause per line must still lex as
// a separator — evidenced by the named removal diagnostic, never a regex
// or bare syntax error.
func TestRelation_ReverseSeparatorIsNotARegex(t *testing.T) {
	src := relSource("--> WHEELS Wheel / car\n\t--> SPARES Wheel / owner")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 2 {
		t.Fatalf("got %d issues, want one removal diagnostic per clause: %v", len(issues), issues)
	}
	for i, iss := range issues {
		if iss.Code() != diag.E_REVERSE_CLAUSE_REMOVED {
			t.Errorf("issue %d = %s, want %s", i, iss.Code(), diag.E_REVERSE_CLAUSE_REMOVED)
		}
	}
	rels := file.Types[0].Relations
	if len(rels) != 2 {
		t.Fatalf("got %d relations, want 2 — the relations survive their removed clauses", len(rels))
	}

	// Two reverse separators on ONE line is where the regex rule wins and
	// swallows everything between the slashes.
	oneLine := relSource("--> WHEELS Wheel / car\t--> SPARES Wheel / owner")
	_, issues = Parse([]byte(oneLine), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Fatal("two reverse separators on one line parsed cleanly, want the regex reading")
	}
	swallowed := "/ car\t--> SPARES Wheel /"
	if got := oneLine[issues[0].Span().Start.Byte:issues[0].Span().End.Byte]; got != swallowed {
		t.Errorf("first issue covers %q, want the swallowed regex %q", got, swallowed)
	}
}

// TestRelation_RecoveryTreatsArrowsAsMemberStarts pins that a broken member
// before a relation does not swallow it.
func TestRelation_RecoveryTreatsArrowsAsMemberStarts(t *testing.T) {
	src := relSource("broken @\n\t--> WHEELS Wheel\n\t*-> PARTS Wheel")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
		t.Fatalf("got %v, want one E_SYNTAX", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 2 {
		t.Fatalf("got %d relations, want 2 — recovery must stop at an arrow", len(rels))
	}
	if rels[0].Name != "WHEELS" || rels[1].Name != "PARTS" {
		t.Errorf("relations = %q, %q", rels[0].Name, rels[1].Name)
	}
}

// TestRelation_BrokenRelationCostsOnlyItself pins per-member recovery inside a
// body that mixes relations and properties.
func TestRelation_BrokenRelationCostsOnlyItself(t *testing.T) {
	src := relSource("--> \n\t--> WHEELS Wheel\n\tname String")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	ty := file.Types[0]
	if len(ty.Relations) != 1 || ty.Relations[0].Name != "WHEELS" {
		t.Errorf("relations = %+v, want one named WHEELS", ty.Relations)
	}
	if len(ty.Properties) != 2 || ty.Properties[1].Name != "name" {
		t.Errorf("properties = %+v, want id and name", ty.Properties)
	}
}

// TestRelation_SpansCoverTheWholeDeclaration pins the extents a consumer reads.
func TestRelation_SpansCoverTheWholeDeclaration(t *testing.T) {
	src := relSource("--> WHEELS (many) Wheel")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rel := file.Types[0].Relations[0]
	if got := src[rel.Span.Start.Byte:rel.Span.End.Byte]; got != "--> WHEELS (many) Wheel" {
		t.Errorf("relation span covers %q", got)
	}
	if got := src[rel.NameSpan.Start.Byte:rel.NameSpan.End.Byte]; got != "WHEELS" {
		t.Errorf("name span covers %q, want WHEELS", got)
	}
}

// TestRelation_ReverseClauseIsRejected pins the removal: every clause
// shape draws E_REVERSE_CLAUSE_REMOVED at the backref name's span, and the
// relation survives — its own semantic checks must still run.
func TestRelation_ReverseClauseIsRejected(t *testing.T) {
	for _, tc := range []struct {
		name, member string
		wantRemoved  bool
	}{
		{"bare name", "--> R Wheel /back", true},
		{"name with multiplicity", "--> R Wheel /back (one:many)", true},
		{"refused multiplicity still names the removal", "--> R Wheel /back (many:one)", true},
		{"composition clause", "*-> C Wheel /back (one)", true},
		{"no clause at all", "--> R Wheel", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			src := relSource(tc.member)
			file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(file.Types) == 0 || len(file.Types[0].Relations) != 1 {
				t.Fatal("the relation must survive its removed clause")
			}
			var removed bool
			for _, iss := range issues {
				if iss.Code() == diag.E_REVERSE_CLAUSE_REMOVED {
					removed = true
					if got := src[iss.Span().Start.Byte:iss.Span().End.Byte]; got != "back" {
						t.Errorf("diagnostic span covers %q, want the backref name", got)
					}
				}
			}
			if removed != tc.wantRemoved {
				t.Errorf("removal diagnostic present = %v, want %v: %v", removed, tc.wantRemoved, issues)
			}
			if !tc.wantRemoved && len(issues) != 0 {
				t.Errorf("clause-free member drew %v", issues)
			}
		})
	}
}
