package workspace

import (
	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
)

// Unit is a normalized view of an analysis context for a single
// document or code block. Abstracts the difference between standalone
// .yammm files and yammm code blocks embedded in markdown.
type Unit struct {
	Snapshot  *analysis.Snapshot // May be nil (completion gracefully degrades)
	Doc       *docstate.Snapshot
	LocalLine int // Position in document/block-local coordinates
	LocalChar int
	Remap     *BlockRemap // nil for standalone .yammm files
}

// BlockRemap translates block-local coordinates back to markdown-file coordinates.
type BlockRemap struct {
	mdSnap     *MarkdownDocumentSnapshot
	blockIndex int
}

// NewBlockRemap creates a BlockRemap for the given markdown snapshot and block index.
// Intended for use in tests; production code obtains BlockRemap via ResolveUnit.
func NewBlockRemap(mdSnap *MarkdownDocumentSnapshot, blockIndex int) *BlockRemap {
	return &BlockRemap{mdSnap: mdSnap, blockIndex: blockIndex}
}

// Block returns the code block associated with this remap.
func (r *BlockRemap) Block() markdown.CodeBlock {
	return r.mdSnap.Blocks[r.blockIndex]
}

// DocumentURI returns the URI of the markdown document.
func (r *BlockRemap) DocumentURI() string {
	return r.mdSnap.URI
}

// RemapRange remaps a block-local range to markdown-file coordinates.
func (r *BlockRemap) RemapRange(rng protocol.Range) protocol.Range {
	startLine, startChar := r.mdSnap.BlockPositionToMarkdown(r.blockIndex,
		int(rng.Start.Line), int(rng.Start.Character))
	endLine, endChar := r.mdSnap.BlockPositionToMarkdown(r.blockIndex,
		int(rng.End.Line), int(rng.End.Character))
	return protocol.Range{
		Start: protocol.Position{Line: lsputil.ToUInteger(startLine), Character: lsputil.ToUInteger(startChar)},
		End:   protocol.Position{Line: lsputil.ToUInteger(endLine), Character: lsputil.ToUInteger(endChar)},
	}
}

// RemapRangePtr remaps a range pointer. Returns nil for nil input.
func (r *BlockRemap) RemapRangePtr(rng *protocol.Range) *protocol.Range {
	if rng == nil {
		return nil
	}
	remapped := r.RemapRange(*rng)
	return &remapped
}

// RemapDocumentSymbolRanges remaps Range and SelectionRange of document symbols
// from block-local coordinates to markdown coordinates. Recursively processes Children.
// Returns nil for empty input. Creates copies to avoid mutating the originals.
func RemapDocumentSymbolRanges(docSymbols []protocol.DocumentSymbol, remap *BlockRemap) []protocol.DocumentSymbol {
	if len(docSymbols) == 0 {
		return nil
	}

	result := make([]protocol.DocumentSymbol, len(docSymbols))
	for i, sym := range docSymbols {
		result[i] = sym
		result[i].Range = remap.RemapRange(sym.Range)
		result[i].SelectionRange = remap.RemapRange(sym.SelectionRange)

		if len(sym.Children) > 0 {
			result[i].Children = RemapDocumentSymbolRanges(sym.Children, remap)
		}
	}

	return result
}

// ResolveUnit resolves the analysis unit for a cursor position.
// For .yammm: returns file snapshot + docSnapshot at the given position.
// For markdown: maps cursor to a code block, returns block snapshot + block docSnapshot at block-local position.
// snapshotRequired=true returns nil when snapshot is nil (hover, definition).
// snapshotRequired=false allows nil snapshot (completion degrades to keywords).
func (w *Workspace) ResolveUnit(uri string, line, char int, snapshotRequired bool) *Unit {
	// Try markdown first
	if mdSnap := w.GetMarkdownDocumentSnapshot(uri); mdSnap != nil {
		blockPos := mdSnap.MarkdownPositionToBlock(line, char)
		if blockPos == nil {
			return nil
		}

		if blockPos.BlockIndex >= len(mdSnap.Blocks) {
			return nil
		}

		var snapshot *analysis.Snapshot
		if blockPos.BlockIndex < len(mdSnap.Snapshots) {
			snapshot = mdSnap.Snapshots[blockPos.BlockIndex]
		}

		if snapshotRequired && snapshot == nil {
			return nil
		}

		blockDocSnap := BuildBlockDocumentSnapshot(mdSnap, mdSnap.Blocks[blockPos.BlockIndex])

		return &Unit{
			Snapshot:  snapshot,
			Doc:       blockDocSnap,
			LocalLine: blockPos.LocalLine,
			LocalChar: blockPos.LocalChar,
			Remap: &BlockRemap{
				mdSnap:     mdSnap,
				blockIndex: blockPos.BlockIndex,
			},
		}
	}

	// Standalone .yammm file
	snapshot := w.LatestSnapshot(uri)
	if snapshotRequired && snapshot == nil {
		return nil
	}

	doc := w.GetDocumentSnapshot(uri)
	if doc == nil {
		return nil
	}

	return &Unit{
		Snapshot:  snapshot,
		Doc:       doc,
		LocalLine: line,
		LocalChar: char,
		Remap:     nil,
	}
}

// ResolveAllUnits returns one Unit per analysis region in the document.
// For .yammm: single unit. For markdown: one per code block with non-nil snapshot.
// Used by symbols (document-wide, not cursor-centric).
func (w *Workspace) ResolveAllUnits(uri string) []Unit {
	// Try markdown first
	if mdSnap := w.GetMarkdownDocumentSnapshot(uri); mdSnap != nil {
		var units []Unit
		for i, snapshot := range mdSnap.Snapshots {
			if snapshot == nil || i >= len(mdSnap.Blocks) {
				continue
			}
			blockDocSnap := BuildBlockDocumentSnapshot(mdSnap, mdSnap.Blocks[i])
			units = append(units, Unit{
				Snapshot: snapshot,
				Doc:      blockDocSnap,
				Remap: &BlockRemap{
					mdSnap:     mdSnap,
					blockIndex: i,
				},
			})
		}
		return units
	}

	// Standalone .yammm file
	snapshot := w.LatestSnapshot(uri)
	if snapshot == nil {
		return nil
	}

	doc := w.GetDocumentSnapshot(uri)
	if doc == nil {
		return nil
	}

	return []Unit{{
		Snapshot: snapshot,
		Doc:      doc,
		Remap:    nil,
	}}
}

// BuildBlockDocumentSnapshot creates a docstate.Snapshot for a single code block
// within a markdown document. This is the shared utility used by all feature
// providers (hover, completion, definition, symbols) to bridge between
// markdown-level state and block-level analysis.
func BuildBlockDocumentSnapshot(mdSnap *MarkdownDocumentSnapshot, block markdown.CodeBlock) *docstate.Snapshot {
	depths, inComment := docstate.ComputeBraceDepths(block.Content)
	return &docstate.Snapshot{
		URI:      mdSnap.URI,
		SourceID: block.SourceID,
		Version:  mdSnap.Version,
		Text:     block.Content,
		LineState: &docstate.LineState{
			Version:        mdSnap.Version,
			BraceDepth:     depths,
			InBlockComment: inComment,
		},
	}
}
