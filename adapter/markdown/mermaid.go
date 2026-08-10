package markdown

import (
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
func (g *generator) emitClassDiagram() {
	g.anchors["class-diagram"] = true
	g.buf.WriteString("## Class Diagram\n\n")

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
	writeFence(&g.buf, "mermaid", b.String())
}

// writeClass writes one class declaration. Members are the type's own
// properties only — inherited members are conveyed by the inheritance
// edges, not repeated in every subtype's box. A class with no annotation
// and no members declares in the compact single-line form.
func (g *generator) writeClass(b *strings.Builder, t *schema.Type) {
	e := g.types[t.ID()]
	id := mermaidID(e.display)
	head := "class " + id
	if e.display != id {
		head += `["` + e.display + `"]`
	}

	annotation := ""
	switch {
	case t.IsAbstract():
		annotation = "<<Abstract>>"
	case t.IsPart():
		annotation = "<<Part>>"
	}

	props := t.PropertiesSlice()
	if annotation == "" && len(props) == 0 {
		b.WriteString("    " + head + "\n")
		return
	}
	b.WriteString("    " + head + " {\n")
	if annotation != "" {
		b.WriteString("        " + annotation + "\n")
	}
	for _, p := range props {
		member := p.Name()
		if label := kindLabel(p.Constraint()); label != "" {
			member += " " + label
		}
		b.WriteString("        " + member + "\n")
	}
	b.WriteString("    }\n")
}

// writeEdges writes the type's inheritance edges followed by its own
// association and composition edges. Inherited relations are not redrawn
// on subtypes — the inheritance edge conveys them.
func (g *generator) writeEdges(b *strings.Builder, t *schema.Type) {
	id := mermaidID(g.types[t.ID()].display)
	for _, ref := range t.InheritsSlice() {
		if parent, ok := g.resolveSuper(t, ref); ok {
			b.WriteString("    " + mermaidID(parent.display) + " <|-- " + id + "\n")
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
	label := rel.Name() + " (" + multiplicity(rel.IsOptional(), rel.IsMany()) + ")"
	b.WriteString("    " + ownerID + " " + arrow + " " + mermaidID(target.display) + " : " + label + "\n")
}

// kindLabel returns the diagram member label for a constraint: named
// DataTypes display their name, everything else the bare ConstraintKind
// word.
func kindLabel(c schema.Constraint) string {
	if c == nil {
		return ""
	}
	if alias, ok := c.(schema.AliasConstraint); ok {
		return alias.String()
	}
	return c.Kind().String()
}
