package markdown

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/simon-lentz/yammm/schema"
)

// emitClassDiagram writes the "## Class Diagram" section: one Mermaid
// classDiagram fence covering the entire import closure. The diagram keeps
// constraint detail out — class members carry only the property name and
// its kind label; the per-type tables own the detail. Abstract and part
// types carry the <<Abstract>> / <<Part>> stereotype annotations. Edge
// labels reuse the DSL relation vocabulary (NAME plus parenthesized
// multiplicity) rather than Mermaid cardinality notation, so the whole
// document speaks one vocabulary. Qualified display names are invalid as
// Mermaid class identifiers, so those classes emit the sanitized-id form
// with a display label; namespace grouping is deliberately not used — some
// Markdown renderers do not support classDiagram namespaces.
func (g *generator) emitClassDiagram(h outlineEntry) {
	var b strings.Builder
	b.WriteString("classDiagram\n")
	b.WriteString("    direction TB\n")
	for _, sch := range g.closure {
		for _, t := range sch.TypesSlice() {
			g.writeClass(&b, t)
		}
	}
	for _, sch := range g.closure {
		for _, t := range sch.TypesSlice() {
			g.writeEdges(&b, t)
		}
	}

	g.buf.WriteString("## " + h.md + "\n\n")
	if g.labelled {
		g.buf.WriteString(mermaidFloorSentence + "\n\n")
	}
	g.diagramAt = g.buf.Len()
	writeFence(&g.buf, "mermaid", b.String())
}

// mermaidFloorSentence states the renderer floor the labelled class form
// needs. The emitter is the only party that knows it wrote one, so the
// emitter says so, in the document, before the fence. Mermaid 10.1.0 is the
// first release whose class-diagram grammar carries the classLabel
// production; 9.x fails the form with a lexical error.
const mermaidFloorSentence = "This diagram uses Mermaid's labelled class form and needs Mermaid 10.1.0 or later."

// writeClass writes one class declaration. Members are the type's own
// properties only (inheritance edges convey the rest), and none when
// classMembers is off. A class with no annotation and no members declares
// in the compact single-line form.
func (g *generator) writeClass(b *strings.Builder, t *schema.Type) {
	e := g.types[t.ID()]
	head := "class " + e.mermaidID
	if e.display != e.mermaidID {
		head += `["` + mermaidText(e.display) + `"]`
		g.labelled = true
	}

	annotation := ""
	switch {
	case t.IsAbstract():
		annotation = "<<Abstract>>"
	case t.IsPart():
		annotation = "<<Part>>"
	}

	var props []*schema.Property
	if g.cfg.classMembers {
		props = t.PropertiesSlice()
	}
	if annotation == "" && len(props) == 0 {
		b.WriteString("    " + head + "\n")
		return
	}
	b.WriteString("    " + head + " {\n")
	if annotation != "" {
		b.WriteString("        " + annotation + "\n")
	}
	for _, p := range props {
		b.WriteString("        " + mermaidChars(p.Name()+" "+kindLabel(p.Constraint())) + "\n")
	}
	b.WriteString("    }\n")
}

// writeEdges writes the type's inheritance edges followed by its own
// association and composition edges. Inherited relations are not redrawn
// on subtypes — the inheritance edge conveys them.
func (g *generator) writeEdges(b *strings.Builder, t *schema.Type) {
	id := g.types[t.ID()].mermaidID
	for _, ref := range t.InheritsSlice() {
		if parent, ok := g.resolveSuper(t, ref); ok {
			b.WriteString("    " + parent.mermaidID + " <|-- " + id + "\n")
		}
	}
	for _, rel := range t.AssociationsSlice() {
		g.writeRelationEdge(b, id, rel, "-->")
	}
	for _, rel := range t.CompositionsSlice() {
		g.writeRelationEdge(b, id, rel, "*--")
	}
}

// writeRelationEdge writes one labeled relation edge. A target absent from
// the document's type map is skipped — the type's section still lists the
// reference textually.
func (g *generator) writeRelationEdge(b *strings.Builder, ownerID string, rel *schema.Relation, arrow string) {
	target, ok := g.types[rel.TargetID()]
	if !ok {
		return
	}
	label := mermaidText(rel.Name() + " (" + multiplicity(rel.IsOptional(), rel.IsMany()) + ")")
	b.WriteString("    " + ownerID + " " + arrow + " " + target.mermaidID + " : " + label + "\n")
}

// kindLabel returns the diagram member label for a constraint: named
// DataTypes display their name, everything else the bare ConstraintKind
// word.
func kindLabel(c schema.Constraint) string {
	if alias, ok := c.(schema.AliasConstraint); ok {
		return alias.String()
	}
	return c.Kind().String()
}

// The line forms the emitter writes, each read by Mermaid's class-diagram
// lexer as the emitter means it (Mermaid 10.1.0 and later): an id lexes as
// \w+, which is ASCII; a class label is a string, which ends at the next
// double quote; an edge label is a colon and then text up to the next colon,
// semicolon or line break; and a member line inside a class body is text up to
// the next brace or line break. The forms are a subset of the grammar, stricter
// than it. Mermaid's render replaces each entity code with placeholder
// characters none of these forms reads as syntax, so the check replaces each
// code with a letter before it matches.
var (
	mermaidEntity     = regexp.MustCompile(`#\w+;`)
	diagramClassLine  = regexp.MustCompile(`^    class [A-Za-z0-9_]+(\["[^"]*"\])?( \{)?$`)
	diagramEdgeLine   = regexp.MustCompile(`^    [A-Za-z0-9_]+ (<\|--|-->|\*--) [A-Za-z0-9_]+( : [^:;]+)?$`)
	diagramMemberLine = regexp.MustCompile(`^        [^{}]+$`)
)

// checkDiagram reports a line of the class diagram's body that is not one of
// the forms the emitter writes, or a class body left open. Two rules are
// stricter than the grammar: no line holds "%%", since Mermaid's render reads
// "%%{" anywhere in the text as a directive and a token starting "%%" outside
// a string as a comment; and no line outside a class body holds a direction
// statement, which Mermaid reads wherever it stands on such a line.
func checkDiagram(body string) error {
	lines := strings.Split(strings.TrimSuffix(mermaidEntity.ReplaceAllString(body, "e"), "\n"), "\n")
	if len(lines) < 2 || lines[0] != "classDiagram" || lines[1] != "    direction TB" {
		return errors.New("the class diagram does not open with its header lines")
	}
	inClass := false
	for _, line := range lines[2:] {
		ok := false
		switch {
		case strings.Contains(line, "%%"), !inClass && mermaidDirection.MatchString(line):
		case inClass && line == "    }":
			inClass, ok = false, true
		case inClass:
			ok = diagramMemberLine.MatchString(line)
		case diagramClassLine.MatchString(line):
			inClass, ok = strings.HasSuffix(line, " {"), true
		default:
			ok = diagramEdgeLine.MatchString(line)
		}
		if !ok {
			return fmt.Errorf("class diagram line %q is not a form the emitter writes", line)
		}
	}
	if inClass {
		return errors.New("the class diagram leaves a class body open")
	}
	return nil
}
