package hover

import (
	"log/slog"
	"strings"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// AtPosition returns hover info for the given position within a document.
// The line and char parameters are LSP-encoding coordinates.
// Returns nil, nil when no hover info is found.
//
//nolint:nilnil // LSP protocol: nil result means "no hover info"
func AtPosition(
	snapshot *analysis.Snapshot,
	doc *docstate.Snapshot,
	line, char int,
	enc lsputil.PositionEncoding,
	logger *slog.Logger,
) (*protocol.Hover, error) {
	// Annotation hover is parse-independent: an annotation name is not a symbol,
	// so recognize @name / @@name under the cursor from the line text and return
	// the registry documentation directly, before any symbol lookup (which would
	// otherwise resolve to the enclosing type symbol).
	if h := annotationHoverAt(doc, line, char); h != nil {
		return h, nil
	}

	if snapshot == nil {
		return nil, nil
	}
	if snapshot.EntryVersion != doc.Version {
		logger.Debug(
			"serving stale snapshot for hover",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	idx := snapshot.SymbolIndexAt(doc.SourceID)
	if idx == nil {
		return nil, nil
	}

	internalPos, ok := lsputil.PositionFromLSP(
		snapshot.Sources,
		doc.SourceID,
		line,
		char,
		enc,
	)
	if !ok {
		return nil, nil
	}

	ref := idx.ReferenceAtPosition(internalPos)
	if ref != nil {
		targetSym := snapshot.ResolveTypeReference(ref, doc.SourceID)
		if targetSym != nil {
			return BuildHoverForSymbolWithRange(targetSym, snapshot, &ref.Span, enc)
		}
	}

	sym := idx.SymbolAtPosition(internalPos)
	if sym == nil {
		return nil, nil
	}

	return BuildHoverForSymbolWithRange(sym, snapshot, nil, enc)
}

// annotationHoverAt returns hover content for an annotation name under the
// cursor, or nil if the cursor is not on one. It is a line heuristic — it reads
// only the document text, so it works without a parsed schema. The char offset
// is treated as a byte column (annotation names and their lines are ASCII).
func annotationHoverAt(doc *docstate.Snapshot, line, char int) *protocol.Hover {
	if doc == nil {
		return nil
	}
	lines := strings.Split(doc.Text, "\n")
	if line < 0 || line >= len(lines) {
		return nil
	}
	l := lines[line]
	col := min(char, len(l))
	placement, name, start, end, ok := annotationTokenAt(l, col)
	if !ok {
		return nil
	}
	spec, ok := lookupAnnotationSpec(placement, name)
	if !ok {
		return nil
	}
	value := "**" + placement.String() + spec.Name() + "**\n\n" + spec.Documentation()
	if spec.ArgHint() != "" {
		value += "\n\n*Arguments:* " + spec.ArgHint()
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{Kind: protocol.MarkupKindMarkdown, Value: value},
		Range: &protocol.Range{
			Start: protocol.Position{Line: lsputil.ToUInteger(line), Character: lsputil.ToUInteger(start)},
			End:   protocol.Position{Line: lsputil.ToUInteger(line), Character: lsputil.ToUInteger(end)},
		},
	}
}

// annotationTokenAt finds an @name / @@name token on the line spanning the byte
// column, returning its placement, name, and start/end byte columns.
func annotationTokenAt(line string, col int) (placement schema.AnnotationPlacement, name string, start, end int, ok bool) {
	for i := 0; i < len(line); {
		if line[i] != '@' {
			i++
			continue
		}
		tokenStart := i
		p := schema.PlacementProperty
		i++
		if i < len(line) && line[i] == '@' {
			p = schema.PlacementType
			i++
		}
		nameStart := i
		for i < len(line) && isNameByte(line[i]) {
			i++
		}
		if i > nameStart && col >= tokenStart && col <= i {
			return p, line[nameStart:i], tokenStart, i, true
		}
	}
	return 0, "", 0, 0, false
}

// lookupAnnotationSpec finds a registry spec by placement and name.
func lookupAnnotationSpec(placement schema.AnnotationPlacement, name string) (schema.AnnotationSpec, bool) {
	for _, spec := range schema.AnnotationSpecs() {
		if spec.Placement() == placement && spec.Name() == name {
			return spec, true
		}
	}
	return schema.AnnotationSpec{}, false
}

func isNameByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// BuildHoverForSymbolWithRange generates hover content for a symbol.
// If overrideRange is provided, it is used for the hover range instead of the symbol's span.
// This is used when hovering a reference to use the reference's location, not the target's location.
//
//nolint:nilnil // nil result means no hover info for this symbol
func BuildHoverForSymbolWithRange(
	sym *symbols.Symbol,
	snapshot *analysis.Snapshot,
	overrideRange *location.Span,
	enc lsputil.PositionEncoding,
) (*protocol.Hover, error) {
	if sym == nil || snapshot == nil {
		return nil, nil
	}

	content := RenderSymbol(sym, snapshot.Root)
	if content == "" {
		return nil, nil
	}

	// Always use Markdown: all hover renderers emit Markdown formatting (bold, backticks,
	// fenced blocks, etc.). All mainstream LSP clients support Markdown.
	contentKind := protocol.MarkupKindMarkdown

	// Use override range if provided (e.g., when hovering a reference),
	// otherwise use the symbol's own selection span.
	rangeSpan := sym.Selection
	if overrideRange != nil {
		rangeSpan = *overrideRange
	}

	// Use proper UTF-16 conversion for the hover range
	start, end, ok := lsputil.SpanToLSPRange(snapshot.Sources, rangeSpan, enc)
	if !ok {
		// Fallback to naive conversion if span conversion fails
		return &protocol.Hover{
			Contents: protocol.MarkupContent{
				Kind:  contentKind,
				Value: content,
			},
			Range: &protocol.Range{
				Start: protocol.Position{
					Line:      lsputil.ToUInteger(rangeSpan.Start.Line - 1),
					Character: lsputil.ToUInteger(rangeSpan.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      lsputil.ToUInteger(rangeSpan.End.Line - 1),
					Character: lsputil.ToUInteger(rangeSpan.End.Column - 1),
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
			Start: protocol.Position{Line: lsputil.ToUInteger(start[0]), Character: lsputil.ToUInteger(start[1])},
			End:   protocol.Position{Line: lsputil.ToUInteger(end[0]), Character: lsputil.ToUInteger(end[1])},
		},
	}, nil
}
