// Package hover renders Markdown hover content for yammm schema symbols.
package hover

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// RenderSymbol generates Markdown hover content for the given symbol.
// The moduleRoot is used for relative path display in type hovers.
// Returns empty string if the symbol kind is unknown or has no renderable data.
func RenderSymbol(sym *symbols.Symbol, moduleRoot string) string {
	switch sym.Kind {
	case symbols.SymbolSchema:
		return renderSchema(sym)
	case symbols.SymbolImport:
		return renderImport(sym)
	case symbols.SymbolType:
		return renderType(sym, moduleRoot)
	case symbols.SymbolDataType:
		return renderDataType(sym)
	case symbols.SymbolProperty:
		return renderProperty(sym)
	case symbols.SymbolAssociation, symbols.SymbolComposition:
		return renderRelation(sym)
	case symbols.SymbolInvariant:
		return renderInvariant(sym)
	default:
		return ""
	}
}

// renderSchema generates hover content for a schema symbol.
func renderSchema(sym *symbols.Symbol) string {
	var b strings.Builder
	b.WriteString("**schema** `")
	b.WriteString(sym.Name)
	b.WriteString("`\n")
	return b.String()
}

// renderImport generates hover content for an import symbol.
func renderImport(sym *symbols.Symbol) string {
	imp, ok := sym.Data.(*schema.Import)
	if !ok {
		return ""
	}

	var b strings.Builder
	b.WriteString("**import** `")
	b.WriteString(imp.Path())
	b.WriteString("` as `")
	b.WriteString(imp.Alias())
	b.WriteString("`\n")

	if imp.Schema() != nil {
		b.WriteString("\n")
		b.WriteString("Resolved to: `")
		b.WriteString(imp.ResolvedPath())
		b.WriteString("`")
	}

	return b.String()
}

// renderType generates hover content for a type symbol.
func renderType(sym *symbols.Symbol, moduleRoot string) string {
	t, ok := sym.Data.(*schema.Type)
	if !ok {
		return ""
	}

	var b strings.Builder

	// Type header with modifiers
	b.WriteString("**")
	if t.IsAbstract() {
		b.WriteString("abstract ")
	}
	if t.IsPart() {
		b.WriteString("part ")
	}
	b.WriteString("type** `")
	b.WriteString(t.Name())
	b.WriteString("`")

	// Inheritance
	inherits := t.InheritsSlice()
	if len(inherits) > 0 {
		b.WriteString(" extends ")
		for i, ref := range inherits {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString("`")
			b.WriteString(ref.String())
			b.WriteString("`")
		}
	}
	b.WriteString("\n")

	// Documentation: embedded as-is, allowing markdown formatting in comments.
	// DSL block comments (/* ... */) may contain markdown for rich hover display.
	if doc := t.Documentation(); doc != "" {
		b.WriteString("\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}

	// Summary counts
	propCount := len(t.PropertiesSlice())
	assocCount := len(t.AssociationsSlice())
	compCount := len(t.CompositionsSlice())

	b.WriteString("\n---\n\n")

	if propCount > 0 {
		fmt.Fprintf(&b, "- Properties: %d\n", propCount)
	}
	if assocCount > 0 {
		fmt.Fprintf(&b, "- Associations: %d\n", assocCount)
	}
	if compCount > 0 {
		fmt.Fprintf(&b, "- Compositions: %d\n", compCount)
	}

	// Source location
	if !sym.SourceID.IsZero() {
		fmt.Fprintf(&b, "- Source: `%s`\n", relativeSourcePath(sym.SourceID, moduleRoot))
	}

	return b.String()
}

// renderDataType generates hover content for a datatype symbol.
func renderDataType(sym *symbols.Symbol) string {
	dt, ok := sym.Data.(*schema.DataType)
	if !ok {
		return ""
	}

	var b strings.Builder
	b.WriteString("**datatype** `")
	b.WriteString(sym.Name)
	b.WriteString("` = `")
	b.WriteString(dt.Constraint().String())
	b.WriteString("`\n")

	if doc := dt.Documentation(); doc != "" {
		b.WriteString("\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}

	return b.String()
}

// renderProperty generates hover content for a property symbol.
func renderProperty(sym *symbols.Symbol) string {
	p, ok := sym.Data.(*schema.Property)
	if !ok {
		return ""
	}

	var b strings.Builder

	// Property header with owner
	b.WriteString("**property** `")
	if sym.ParentName != "" {
		b.WriteString(sym.ParentName)
		b.WriteString(".")
	}
	b.WriteString(p.Name())
	b.WriteString("`\n\n")

	// Type/constraint
	if c := p.Constraint(); c != nil {
		b.WriteString("- Type: `")
		b.WriteString(c.String())
		b.WriteString("`\n")
	}

	// Modifiers: only show actual DSL keywords (primary, required).
	// Optional is the default state, not a keyword—omit it to avoid confusion.
	var modifiers []string
	if p.IsPrimaryKey() {
		modifiers = append(modifiers, "primary")
	}
	if p.IsRequired() {
		modifiers = append(modifiers, "required")
	}
	if len(modifiers) > 0 {
		b.WriteString("- Modifiers: ")
		b.WriteString(strings.Join(modifiers, ", "))
		b.WriteString("\n")
	}

	// Documentation
	if doc := p.Documentation(); doc != "" {
		b.WriteString("\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}

	return b.String()
}

// renderRelation generates hover content for a relation symbol.
func renderRelation(sym *symbols.Symbol) string {
	r, ok := sym.Data.(*schema.Relation)
	if !ok {
		return ""
	}

	var b strings.Builder

	// Relation header
	kind := "association"
	arrow := "-->"
	if r.IsComposition() {
		kind = "composition"
		arrow = "*->"
	}

	b.WriteString("**")
	b.WriteString(kind)
	b.WriteString("** `")
	if sym.ParentName != "" {
		b.WriteString(sym.ParentName)
		b.WriteString(".")
	}
	b.WriteString(r.Name())
	b.WriteString("`\n\n")

	// Target and multiplicity
	mult := "one"
	if r.IsMany() {
		mult = "many"
	}

	b.WriteString("```yammm\n")
	b.WriteString(arrow)
	b.WriteString(" ")
	b.WriteString(r.Name())
	b.WriteString(" (")
	b.WriteString(mult)
	b.WriteString(") ")
	b.WriteString(r.Target().String())
	b.WriteString("\n```\n\n")

	b.WriteString("- Target: `")
	b.WriteString(r.Target().String())
	b.WriteString("`\n")
	b.WriteString("- Multiplicity: ")
	b.WriteString(mult)
	b.WriteString("\n")

	if r.IsOptional() {
		b.WriteString("- Optional\n")
	}

	// Documentation
	if doc := r.Documentation(); doc != "" {
		b.WriteString("\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}

	return b.String()
}

// renderInvariant generates hover content for an invariant symbol.
func renderInvariant(sym *symbols.Symbol) string {
	inv, ok := sym.Data.(*schema.Invariant)
	if !ok {
		return ""
	}

	var b strings.Builder

	b.WriteString("**invariant** `")
	b.WriteString(inv.Name())
	b.WriteString("`\n\n")

	b.WriteString("```yammm\n")
	b.WriteString("! \"")
	b.WriteString(inv.Name())
	b.WriteString("\" <expr>\n```\n")

	if doc := inv.Documentation(); doc != "" {
		b.WriteString("\n")
		b.WriteString(doc)
		b.WriteString("\n")
	}

	return b.String()
}

// relativeSourcePath returns a relative path for display in hover.
// Paths are normalized to forward slashes for consistent cross-platform display.
func relativeSourcePath(sourceID location.SourceID, moduleRoot string) string {
	if moduleRoot == "" {
		return sourceID.String()
	}

	// SourceID.String() always uses forward slashes (via CanonicalPath).
	// moduleRoot uses OS-native separators. Normalize both to OS-native
	// for filepath.Rel, then convert result to forward slashes for display.
	path := filepath.FromSlash(sourceID.String())
	root := filepath.FromSlash(moduleRoot)

	rel, err := filepath.Rel(root, path)
	if err != nil {
		return sourceID.String()
	}

	if strings.HasPrefix(rel, "..") {
		return sourceID.String()
	}

	// Use forward slashes for consistent display across platforms
	return "./" + filepath.ToSlash(rel)
}
