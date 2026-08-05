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
			src := relSource(fmt.Sprintf("--> wheels %s Wheel", tc.spelling))
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
			src := relSource("--> wheels " + spelling + " Wheel")
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
	--> wheels (many) Wheel / car (one) {
		/* When fitted. */
		fitted Timestamp required
		note String
	}
	--> spare Wheel
	--> tools (many) Wheel {}`)
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 3 {
		t.Fatalf("got %d relations, want 3", len(rels))
	}

	full := rels[0]
	if full.Kind != RelationAssociation || full.Name != "wheels" || full.Target.Name != "Wheel" {
		t.Errorf("relation = %+v", full)
	}
	if full.Doc != "Owns wheels." {
		t.Errorf("doc = %q, want %q", full.Doc, "Owns wheels.")
	}
	if !full.Optional || !full.Many {
		t.Errorf("forward = optional %v many %v, want true true", full.Optional, full.Many)
	}
	if full.Backref != "car" || full.ReverseOptional || full.ReverseMany {
		t.Errorf("reverse = %q optional %v many %v, want car false false",
			full.Backref, full.ReverseOptional, full.ReverseMany)
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
	if bare.Name != "spare" || bare.Backref != "" || !bare.ReverseOptional || bare.ReverseMany {
		t.Errorf("bare relation = %+v, want an optional single reverse by default", bare)
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
	file, issues := Parse([]byte(relSource("*-> WHEELS (many) Wheel / car")), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 1 || rels[0].Kind != RelationComposition || rels[0].Name != "WHEELS" {
		t.Fatalf("relations = %+v, want one composition named WHEELS", rels)
	}
	if rels[0].Backref != "car" {
		t.Errorf("backref = %q, want car", rels[0].Backref)
	}

	_, issues = Parse([]byte(relSource("*-> WHEELS (many) Wheel { note String }")), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Error("a composition with a body was accepted, want rejected")
	}
}

// TestRelation_EdgePropertiesRejectPrimary pins that an edge property admits
// 'required' and nothing else.
// TestRelation_EdgePropertyConstraintIsForwarded pins that an edge property
// carries the constraint its datatype built. Nothing else reads it, so an
// edge property that stopped forwarding would reach the graph model as an
// Integer whatever the source declared.
func TestRelation_EdgePropertyConstraintIsForwarded(t *testing.T) {
	src := "schema \"s\"\ntype T {\n\tid String primary\n\t--> r B { since Timestamp[\"2006\"] }\n}\n"

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

func TestRelation_EdgePropertiesRejectPrimary(t *testing.T) {
	_, issues := Parse([]byte(relSource("--> wheels Wheel {\n\t\tnote String primary\n\t}")), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Error("an edge property marked primary was accepted, want rejected")
	}
}

// TestRelation_NamesRejectReservedSpellings pins that a relation name is an
// any-name position: neither a reserved lowercase spelling nor a datatype name.
func TestRelation_NamesRejectReservedSpellings(t *testing.T) {
	for _, name := range []string{"type", "primary", "in", "many", "String", "Vector"} {
		t.Run(name, func(t *testing.T) {
			src := relSource("--> " + name + " Wheel")
			_, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
			if len(issues) == 0 {
				t.Errorf("relation name %q accepted, want rejected", name)
			}
		})
	}
}

// TestRelation_NamesAcceptEitherCase pins the other half: an any-name position
// takes an uppercase or a lowercase word.
func TestRelation_NamesAcceptEitherCase(t *testing.T) {
	for _, name := range []string{"wheels", "WHEELS", "Wheels", "w2"} {
		t.Run(name, func(t *testing.T) {
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
}

// TestRelation_ReverseSeparatorIsNotARegex pins the lexical hazard the reverse
// name introduces: the separator is a slash, and a slash pair on one line is a
// regex literal. A relation line holding a second slash must still lex as two
// separators, not as a regex.
func TestRelation_ReverseSeparatorIsNotARegex(t *testing.T) {
	src := relSource("--> wheels Wheel / car\n\t--> spares Wheel / owner")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 2 {
		t.Fatalf("got %d relations, want 2", len(rels))
	}
	if rels[0].Backref != "car" || rels[1].Backref != "owner" {
		t.Errorf("backrefs = %q, %q, want car, owner", rels[0].Backref, rels[1].Backref)
	}

	// Two reverse separators on ONE line is where the regex rule wins and
	// swallows everything between the slashes.
	oneLine := relSource("--> wheels Wheel / car\t--> spares Wheel / owner")
	_, issues = Parse([]byte(oneLine), location.NewSourceID("s.yammm"))
	if len(issues) == 0 {
		t.Fatal("two reverse separators on one line parsed cleanly, want the regex reading")
	}
	swallowed := "/ car\t--> spares Wheel /"
	if got := oneLine[issues[0].Span().Start.Byte:issues[0].Span().End.Byte]; got != swallowed {
		t.Errorf("first issue covers %q, want the swallowed regex %q", got, swallowed)
	}
}

// TestRelation_RecoveryTreatsArrowsAsMemberStarts pins that a broken member
// before a relation does not swallow it.
func TestRelation_RecoveryTreatsArrowsAsMemberStarts(t *testing.T) {
	src := relSource("broken @\n\t--> wheels Wheel\n\t*-> PARTS Wheel")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 || issues[0].Code() != diag.E_SYNTAX {
		t.Fatalf("got %v, want one E_SYNTAX", issues)
	}
	rels := file.Types[0].Relations
	if len(rels) != 2 {
		t.Fatalf("got %d relations, want 2 — recovery must stop at an arrow", len(rels))
	}
	if rels[0].Name != "wheels" || rels[1].Name != "PARTS" {
		t.Errorf("relations = %q, %q", rels[0].Name, rels[1].Name)
	}
}

// TestRelation_BrokenRelationCostsOnlyItself pins per-member recovery inside a
// body that mixes relations and properties.
func TestRelation_BrokenRelationCostsOnlyItself(t *testing.T) {
	src := relSource("--> \n\t--> wheels Wheel\n\tname String")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 1 {
		t.Fatalf("got %d issues, want 1: %v", len(issues), issues)
	}
	ty := file.Types[0]
	if len(ty.Relations) != 1 || ty.Relations[0].Name != "wheels" {
		t.Errorf("relations = %+v, want one named wheels", ty.Relations)
	}
	if len(ty.Properties) != 2 || ty.Properties[1].Name != "name" {
		t.Errorf("properties = %+v, want id and name", ty.Properties)
	}
}

// TestRelation_SpansCoverTheWholeDeclaration pins the extents a consumer reads.
func TestRelation_SpansCoverTheWholeDeclaration(t *testing.T) {
	src := relSource("--> wheels (many) Wheel / car")
	file, issues := Parse([]byte(src), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("unexpected issues: %v", issues)
	}
	rel := file.Types[0].Relations[0]
	if got := src[rel.Span.Start.Byte:rel.Span.End.Byte]; got != "--> wheels (many) Wheel / car" {
		t.Errorf("relation span covers %q", got)
	}
	if got := src[rel.NameSpan.Start.Byte:rel.NameSpan.End.Byte]; got != "wheels" {
		t.Errorf("name span covers %q, want wheels", got)
	}
	if got := src[rel.BackrefSpan.Start.Byte:rel.BackrefSpan.End.Byte]; got != "car" {
		t.Errorf("backref span covers %q, want car", got)
	}
}
