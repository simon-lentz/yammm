package lsp

import (
	"path/filepath"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// textDocumentDefinition handles textDocument/definition requests.
// Returns nil, nil when no definition is found (standard LSP behavior).
//
//nolint:nilnil // LSP protocol: nil result means "no definition found"
func (s *Server) textDocumentDefinition(_ *glsp.Context, params *protocol.DefinitionParams) (any, error) {
	uri := params.TextDocument.URI

	s.logger.Debug("definition request",
		"uri", uri,
		"line", params.Position.Line,
		"character", params.Position.Character,
	)

	if mdSnap := s.workspace.GetMarkdownDocumentSnapshot(uri); mdSnap != nil {
		return s.markdownDefinition(params, mdSnap)
	}

	snapshot := s.workspace.LatestSnapshot(uri)
	if snapshot == nil {
		s.logger.Debug("no snapshot for definition", "uri", uri)
		return nil, nil
	}

	doc := s.workspace.GetDocumentSnapshot(uri)
	if doc == nil {
		s.logger.Debug("document not open for definition", "uri", uri)
		return nil, nil
	}

	return s.definitionAtPosition(snapshot, doc,
		int(params.Position.Line), int(params.Position.Character))
}

// markdownDefinition handles definition requests within yammm code blocks in markdown files.
//
//nolint:nilnil // LSP protocol: nil result means "no definition found"
func (s *Server) markdownDefinition(params *protocol.DefinitionParams, mdSnap *MarkdownDocumentSnapshot) (any, error) {
	blockPos := mdSnap.MarkdownPositionToBlock(int(params.Position.Line), int(params.Position.Character))
	if blockPos == nil {
		return nil, nil
	}

	if blockPos.BlockIndex >= len(mdSnap.Snapshots) || blockPos.BlockIndex >= len(mdSnap.Blocks) ||
		mdSnap.Snapshots[blockPos.BlockIndex] == nil {
		return nil, nil
	}
	snapshot := mdSnap.Snapshots[blockPos.BlockIndex]
	block := mdSnap.Blocks[blockPos.BlockIndex]

	blockDocSnap := s.buildBlockDocumentSnapshot(mdSnap, block)

	result, err := s.definitionAtPosition(snapshot, blockDocSnap, blockPos.LocalLine, blockPos.LocalChar)
	if err != nil || result == nil {
		return result, err
	}

	// Remap the location URI and range if it points to the virtual block SourceID
	loc, ok := result.(*protocol.Location)
	if !ok || loc == nil {
		return result, nil
	}

	// Decode the location URI to check if it matches our virtual block path.
	// symbolToLocation calls RemapPathToURI which percent-encodes '#' in virtual
	// paths (e.g., /path/to/README.md%23block-0). URIToPath reverses this.
	locPath, pathErr := URIToPath(loc.URI)
	if pathErr == nil && filepath.ToSlash(locPath) == block.SourceID.String() {
		loc.URI = mdSnap.URI
		startLine, startChar := mdSnap.BlockPositionToMarkdown(blockPos.BlockIndex,
			int(loc.Range.Start.Line), int(loc.Range.Start.Character))
		endLine, endChar := mdSnap.BlockPositionToMarkdown(blockPos.BlockIndex,
			int(loc.Range.End.Line), int(loc.Range.End.Character))
		loc.Range = protocol.Range{
			Start: protocol.Position{Line: toUInteger(startLine), Character: toUInteger(startChar)},
			End:   protocol.Position{Line: toUInteger(endLine), Character: toUInteger(endChar)},
		}
	}

	return loc, nil
}

// definitionAtPosition returns the definition location for the symbol at the given position.
// The line and char parameters are LSP-encoding coordinates.
// Returns nil, nil when no definition is found.
//
//nolint:nilnil // LSP protocol: nil result means "no definition found"
func (s *Server) definitionAtPosition(snapshot *Snapshot, doc *DocumentSnapshot, line, char int) (any, error) {
	if snapshot.EntryVersion != doc.Version {
		s.logger.Debug("serving stale snapshot for definition",
			"uri", doc.URI,
			"snapshot_version", snapshot.EntryVersion,
			"doc_version", doc.Version,
		)
	}

	idx := snapshot.SymbolIndexAt(doc.SourceID)
	if idx == nil {
		s.logger.Debug("no symbol index for source", "source", doc.SourceID)
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
		return s.resolveReferenceDefinition(snapshot, ref, doc.SourceID)
	}

	sym := idx.SymbolAtPosition(internalPos)
	if sym != nil {
		return s.resolveSymbolDefinition(snapshot, sym)
	}

	s.logger.Debug("no symbol or reference at position",
		"uri", doc.URI,
		"position", internalPos,
	)
	return nil, nil
}

// resolveReferenceDefinition resolves a type reference to its definition location.
//
//nolint:nilnil // LSP protocol: nil result means "no definition found"
func (s *Server) resolveReferenceDefinition(snapshot *Snapshot, ref *ReferenceSymbol, fromSourceID location.SourceID) (any, error) {
	// Resolve the reference to its target symbol
	targetSym := snapshot.ResolveTypeReference(ref, fromSourceID)
	if targetSym == nil {
		s.logger.Debug("could not resolve reference",
			"target", ref.TargetName,
			"qualifier", ref.Qualifier,
		)
		return nil, nil
	}

	return s.symbolToLocation(snapshot, targetSym), nil
}

// resolveSymbolDefinition handles definition requests on symbol declarations.
// For most symbols, returns the symbol's own location. For import aliases,
// navigates to the imported file.
//
//nolint:nilnil // LSP protocol: nil result means "no definition found"
func (s *Server) resolveSymbolDefinition(snapshot *Snapshot, sym *Symbol) (any, error) {
	switch sym.Kind {
	case SymbolImport:
		// For imports, navigate to the imported schema's declaration.
		// Uses RemapPathToURI to ensure the URI matches the client's open document URI
		// (important for symlink scenarios).
		if imp, ok := sym.Data.(*schema.Import); ok && imp.Schema() != nil {
			importedSchema := imp.Schema()
			uri := s.workspace.RemapPathToURI(importedSchema.SourceID().String())
			schemaSpan := importedSchema.Span()

			// Try proper UTF-16 conversion if sources are available
			start, end, ok := SpanToLSPRange(snapshot.Sources, schemaSpan, s.workspace.PositionEncoding())
			if ok {
				return &protocol.Location{
					URI: uri,
					Range: protocol.Range{
						Start: protocol.Position{Line: toUInteger(start[0]), Character: toUInteger(start[1])},
						End:   protocol.Position{Line: toUInteger(end[0]), Character: toUInteger(end[1])},
					},
				}, nil
			}

			// Fallback to naive conversion from span (1-indexed to 0-indexed)
			if !schemaSpan.IsZero() {
				return &protocol.Location{
					URI: uri,
					Range: protocol.Range{
						Start: protocol.Position{
							Line:      toUInteger(schemaSpan.Start.Line - 1),
							Character: toUInteger(schemaSpan.Start.Column - 1),
						},
						End: protocol.Position{
							Line:      toUInteger(schemaSpan.End.Line - 1),
							Character: toUInteger(schemaSpan.End.Column - 1),
						},
					},
				}, nil
			}

			// Last resort: point to beginning of file
			return &protocol.Location{
				URI:   uri,
				Range: protocol.Range{},
			}, nil
		}
		return nil, nil

	default:
		// For other symbols, return the symbol's own location
		return s.symbolToLocation(snapshot, sym), nil
	}
}

// symbolToLocation converts a Symbol to an LSP Location using proper UTF-16 conversion.
// Uses RemapPathToURI to ensure the URI matches the client's open document URI
// (important for symlink scenarios).
func (s *Server) symbolToLocation(snapshot *Snapshot, sym *Symbol) *protocol.Location {
	if sym == nil || sym.Range.IsZero() {
		return nil
	}

	uri := s.workspace.RemapPathToURI(sym.SourceID.String())

	// Use proper UTF-16 conversion for the range
	start, end, ok := SpanToLSPRange(snapshot.Sources, sym.Selection, s.workspace.PositionEncoding())
	if !ok {
		// Fallback to naive conversion if span conversion fails
		return &protocol.Location{
			URI: uri,
			Range: protocol.Range{
				Start: protocol.Position{
					Line:      toUInteger(sym.Selection.Start.Line - 1),
					Character: toUInteger(sym.Selection.Start.Column - 1),
				},
				End: protocol.Position{
					Line:      toUInteger(sym.Selection.End.Line - 1),
					Character: toUInteger(sym.Selection.End.Column - 1),
				},
			},
		}
	}

	return &protocol.Location{
		URI: uri,
		Range: protocol.Range{
			Start: protocol.Position{Line: toUInteger(start[0]), Character: toUInteger(start[1])},
			End:   protocol.Position{Line: toUInteger(end[0]), Character: toUInteger(end[1])},
		},
	}
}
