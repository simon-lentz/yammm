package lsp

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// textDocumentHover handles textDocument/hover requests.
//
//nolint:nilnil // LSP protocol: nil result means "no hover info"
func (s *Server) textDocumentHover(_ *glsp.Context, params *protocol.HoverParams) (*protocol.Hover, error) {
	uri := params.TextDocument.URI

	s.logger.Debug("hover request",
		"uri", uri,
		"line", params.Position.Line,
		"character", params.Position.Character,
	)

	snapshot := s.workspace.LatestSnapshot(uri)
	if snapshot == nil {
		return nil, nil
	}

	doc := s.workspace.GetDocumentSnapshot(uri)
	if doc == nil {
		return nil, nil
	}

	return s.hoverAtPosition(snapshot, doc,
		int(params.Position.Line), int(params.Position.Character))
}

// hoverAtPosition returns hover info for the given position within a document.
// The line and char parameters are LSP-encoding coordinates.
// Returns nil, nil when no hover info is found.
//
//nolint:nilnil // LSP protocol: nil result means "no hover info"
func (s *Server) hoverAtPosition(snapshot *Snapshot, doc *DocumentSnapshot, line, char int) (*protocol.Hover, error) {
	if snapshot.EntryVersion != doc.Version {
		s.logger.Debug("serving stale snapshot for hover",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	idx := snapshot.SymbolIndexAt(doc.SourceID)
	if idx == nil {
		return nil, nil
	}

	internalPos, ok := PositionFromLSP(
		snapshot.Sources,
		doc.SourceID,
		line,
		char,
		s.workspace.PositionEncoding(),
	)
	if !ok {
		return nil, nil
	}

	ref := idx.ReferenceAtPosition(internalPos)
	if ref != nil {
		targetSym := snapshot.ResolveTypeReference(ref, doc.SourceID)
		if targetSym != nil {
			return s.buildHoverForSymbolWithRange(targetSym, snapshot, &ref.Span)
		}
	}

	sym := idx.SymbolAtPosition(internalPos)
	if sym == nil {
		return nil, nil
	}

	return s.buildHoverForSymbolWithRange(sym, snapshot, nil)
}

// buildHoverForSymbolWithRange generates hover content for a symbol.
// If overrideRange is provided, it is used for the hover range instead of the symbol's span.
// This is used when hovering a reference to use the reference's location, not the target's location.
//
//nolint:nilnil // nil result means no hover info for this symbol
func (s *Server) buildHoverForSymbolWithRange(sym *Symbol, snapshot *Snapshot, overrideRange *location.Span) (*protocol.Hover, error) {
	if sym == nil || snapshot == nil {
		return nil, nil
	}

	var content string

	switch sym.Kind {
	case SymbolSchema:
		content = s.hoverForSchema(sym)
	case SymbolImport:
		content = s.hoverForImport(sym)
	case SymbolType:
		content = s.hoverForType(sym, snapshot)
	case SymbolDataType:
		content = s.hoverForDataType(sym)
	case SymbolProperty:
		content = s.hoverForProperty(sym)
	case SymbolAssociation, SymbolComposition:
		content = s.hoverForRelation(sym)
	case SymbolInvariant:
		content = s.hoverForInvariant(sym)
	default:
		return nil, nil
	}

	if content == "" {
		return nil, nil
	}

	// Always use Markdown: all hover renderers emit Markdown formatting (bold, backticks,
	// fenced blocks, etc.). All mainstream LSP clients support Markdown. Capability
	// negotiation was removed because returning Markdown content with Kind=PlainText
	// is strictly worse than declaring Markdown—clients would display literal ** and ```.
	contentKind := protocol.MarkupKindMarkdown

	// Use override range if provided (e.g., when hovering a reference),
	// otherwise use the symbol's own selection span.
	rangeSpan := sym.Selection
	if overrideRange != nil {
		rangeSpan = *overrideRange
	}

	// Use proper UTF-16 conversion for the hover range
	start, end, ok := SpanToLSPRange(snapshot.Sources, rangeSpan, s.workspace.PositionEncoding())
	if !ok {
		// Fallback to naive conversion if span conversion fails
		return &protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  contentKind,
				Value: content,
			},
			Range: &protocol.Range{
				Start: protocol.Position{
					Line:      toUInteger(rangeSpan.Start.Line - 1),
					Character: toUInteger(rangeSpan.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      toUInteger(rangeSpan.End.Line - 1),
					Character: toUInteger(rangeSpan.End.Column - 1),
				},
			},
		}, nil
	}

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  contentKind,
			Value: content,
		},
		Range: &protocol.Range{
			Start: protocol.Position{Line: toUInteger(start[0]), Character: toUInteger(start[1])},
			End:   protocol.Position{Line: toUInteger(end[0]), Character: toUInteger(end[1])},
		},
	}, nil
}

// hoverForSchema generates hover content for a schema symbol.
func (s *Server) hoverForSchema(sym *Symbol) string {
	var b strings.Builder
	b.WriteString("**schema** `")
	b.WriteString(sym.Name)
	b.WriteString("`\n")
	return b.String()
}

// hoverForImport generates hover content for an import symbol.
func (s *Server) hoverForImport(sym *Symbol) string {
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

// hoverForType generates hover content for a type symbol.
func (s *Server) hoverForType(sym *Symbol, snapshot *Snapshot) string {
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
	propCount := 0
	for range t.Properties() {
		propCount++
	}
	assocCount := 0
	for range t.Associations() {
		assocCount++
	}
	compCount := 0
	for range t.Compositions() {
		compCount++
	}

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
		fmt.Fprintf(&b, "- Source: `%s`\n", s.relativeSourcePath(sym.SourceID, snapshot))
	}

	return b.String()
}

// hoverForDataType generates hover content for a datatype symbol.
func (s *Server) hoverForDataType(sym *Symbol) string {
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

// hoverForProperty generates hover content for a property symbol.
func (s *Server) hoverForProperty(sym *Symbol) string {
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

// hoverForRelation generates hover content for a relation symbol.
func (s *Server) hoverForRelation(sym *Symbol) string {
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

// hoverForInvariant generates hover content for an invariant symbol.
func (s *Server) hoverForInvariant(sym *Symbol) string {
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
func (s *Server) relativeSourcePath(sourceID location.SourceID, snapshot *Snapshot) string {
	if snapshot == nil || snapshot.Root == "" {
		return sourceID.String()
	}

	// SourceID.String() always uses forward slashes (via CanonicalPath).
	// snapshot.Root uses OS-native separators. Normalize both to OS-native
	// for filepath.Rel, then convert result to forward slashes for display.
	path := filepath.FromSlash(sourceID.String())
	root := filepath.FromSlash(snapshot.Root)

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
