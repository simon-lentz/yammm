package hover

import (
	"log/slog"

	"github.com/simon-lentz/yammm/location"

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
