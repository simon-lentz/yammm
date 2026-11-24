// Package definition provides pure definition-resolution logic for the LSP server.
// Functions here convert symbols and references to LSP Locations without
// depending on server state; they receive a URI-mapping function as a parameter.
package definition

import (
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/internal/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/internal/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/internal/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/internal/lsp/internal/symbols"
)

// SymbolToLocation converts a Symbol to an LSP Location using proper position conversion.
// mapURI maps a source path to the client's document URI (important for symlink scenarios).
func SymbolToLocation(
	sources *lsputil.Texts,
	sym *symbols.Symbol,
	mapURI func(string) string,
) *protocol.Location {
	if sym == nil || sym.Range.IsZero() {
		return nil
	}

	uri := mapURI(sym.SourceID.String())

	// Use proper UTF-16 conversion for the range
	start, end, ok := lsputil.SpanToLSPRange(sources, sym.Selection)
	if !ok {
		// Fallback to naive conversion if span conversion fails
		return &protocol.Location{
			URI: uri,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      lsputil.ToUInteger(sym.Selection.Start.Line - 1),
					Character: lsputil.ToUInteger(sym.Selection.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      lsputil.ToUInteger(sym.Selection.End.Line - 1),
					Character: lsputil.ToUInteger(sym.Selection.End.Column - 1),
				},
			},
		}
	}

	return &protocol.Location{
		URI: uri,
		Range: protocol.Range{
			Start: protocol.Position{Line: lsputil.ToUInteger(start[0]), Character: lsputil.ToUInteger(start[1])},
			End:   protocol.Position{Line: lsputil.ToUInteger(end[0]), Character: lsputil.ToUInteger(end[1])},
		},
	}
}

// ResolveReference resolves a type reference to its definition location.
// Returns nil when the reference cannot be resolved to a target symbol.
func ResolveReference(
	snapshot *analysis.Snapshot,
	ref *symbols.ReferenceSymbol,
	fromSourceID location.SourceID,
	mapURI func(string) string,
) *protocol.Location {
	targetSym := snapshot.ResolveTypeReference(ref, fromSourceID)
	if targetSym == nil {
		return nil
	}

	return SymbolToLocation(snapshot.Sources, targetSym, mapURI)
}

// ResolveSymbol handles definition requests on symbol declarations.
// For most symbols, returns the symbol's own location. For import aliases,
// navigates to the imported file.
// Returns nil when no definition is found.
func ResolveSymbol(
	snapshot *analysis.Snapshot,
	sym *symbols.Symbol,
	mapURI func(string) string,
) *protocol.Location {
	switch sym.Kind {
	case symbols.SymbolImport:
		return resolveImport(snapshot, sym, mapURI)
	default:
		return SymbolToLocation(snapshot.Sources, sym, mapURI)
	}
}

// resolveImport navigates to the imported schema's declaration.
func resolveImport(
	snapshot *analysis.Snapshot,
	sym *symbols.Symbol,
	mapURI func(string) string,
) *protocol.Location {
	imp, ok := sym.Data.(schema.Import)
	if !ok {
		return nil
	}

	importedSchema := imp.Schema()
	uri := mapURI(importedSchema.SourceID().String())
	schemaSpan := importedSchema.Span()

	// Try proper conversion if sources are available
	start, end, ok := lsputil.SpanToLSPRange(snapshot.Sources, schemaSpan)
	if ok {
		return &protocol.Location{
			URI: uri,
			Range: protocol.Range{
				Start: protocol.Position{Line: lsputil.ToUInteger(start[0]), Character: lsputil.ToUInteger(start[1])},
				End:   protocol.Position{Line: lsputil.ToUInteger(end[0]), Character: lsputil.ToUInteger(end[1])},
			},
		}
	}

	// Fallback to naive conversion from span (1-indexed to 0-indexed)
	if !schemaSpan.IsZero() {
		return &protocol.Location{
			URI: uri,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      lsputil.ToUInteger(schemaSpan.Start.Line - 1),
					Character: lsputil.ToUInteger(schemaSpan.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      lsputil.ToUInteger(schemaSpan.End.Line - 1),
					Character: lsputil.ToUInteger(schemaSpan.End.Column - 1),
				},
			},
		}
	}

	// Last resort: point to beginning of file
	return &protocol.Location{
		URI:   uri,
		Range: protocol.Range{},
	}
}
