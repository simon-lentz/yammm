package workspace

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsperr"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/markdown"
)

// markdownDocumentOpened creates a markdownDocument with normalized text and version.
// Block extraction is deferred to AnalyzeMarkdownAndPublish.
func (w *Workspace) markdownDocumentOpened(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.markdownDocs[uri] = &markdownDocument{
		URI:     uri,
		Version: version,
		Text:    docstate.NormalizeLineEndings(text),
	}
}

// markdownDocumentChanged updates text and version for a markdown document.
// Ignores stale updates (version <= current unless either is 0).
// Does NOT re-extract blocks — that is done atomically by AnalyzeMarkdownAndPublish.
func (w *Workspace) markdownDocumentChanged(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	md := w.markdownDocs[uri]
	if md == nil {
		return
	}

	if version != 0 && md.Version != 0 && version <= md.Version {
		w.logger.Debug(
			"ignoring stale markdown document change",
			slog.String("uri", uri),
			slog.Int("incoming_version", version),
			slog.Int("current_version", md.Version),
		)
		return
	}
	md.Version = version
	md.Text = docstate.NormalizeLineEndings(text)
}

// markdownDocumentClosed removes a markdown document and clears its diagnostics.
func (w *Workspace) markdownDocumentClosed(uri string) {
	w.mu.Lock()
	delete(w.markdownDocs, uri)

	hadPublished := w.mapper.publishedByEntry[uri] != nil
	delete(w.mapper.publishedByEntry, uri)
	w.mu.Unlock()

	if hadPublished {
		w.publishDiagnostics(uri, nil, nil)
	}
	w.clearDiagHash(uri)

	w.cancelPendingAnalysis(uri)
}

// GetMarkdownDocumentSnapshot returns an immutable snapshot of a markdown document.
func (w *Workspace) GetMarkdownDocumentSnapshot(uri string) *MarkdownDocumentSnapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()

	md := w.markdownDocs[uri]
	if md == nil {
		return nil
	}

	blocks := make([]markdown.CodeBlock, len(md.Blocks))
	copy(blocks, md.Blocks)

	snapshots := make([]*analysis.Snapshot, len(md.Snapshots))
	copy(snapshots, md.Snapshots)

	return &MarkdownDocumentSnapshot{
		URI:       md.URI,
		Version:   md.Version,
		Blocks:    blocks,
		Snapshots: snapshots,
	}
}

// GetMarkdownCurrentText returns the current text of a markdown document.
func (w *Workspace) GetMarkdownCurrentText(uri string) (string, bool) {
	w.mu.RLock()
	defer w.mu.RUnlock()

	md := w.markdownDocs[uri]
	if md == nil {
		return "", false
	}
	return md.Text, true
}

// scheduleMarkdownAnalysis schedules a debounced analysis for a markdown document.
func (w *Workspace) scheduleMarkdownAnalysis(uri string) {
	w.sched.schedule(uri, w.AnalyzeMarkdownAndPublish)
}

// AnalyzeMarkdownAndPublish analyzes a markdown document's code blocks and publishes diagnostics.
func (w *Workspace) AnalyzeMarkdownAndPublish(analyzeCtx context.Context, uri string) {
	// Read text and version under lock
	w.mu.RLock()
	md := w.markdownDocs[uri]
	if md == nil {
		w.mu.RUnlock()
		return
	}
	text := md.Text
	entryVersion := md.Version
	w.mu.RUnlock()

	// Extract blocks
	blocks := markdown.ExtractCodeBlocks(text)

	// Assign virtual SourceIDs
	path, err := lsputil.URIToPath(uri)
	if err != nil {
		w.logger.Warn(
			"failed to parse markdown URI",
			slog.String("uri", uri),
			slog.Any("error", fmt.Errorf("%w: %w", lsperr.ErrInvalidURI, err)),
		)
		return
	}

	validBlocks := make([]markdown.CodeBlock, 0, len(blocks))
	for i, block := range blocks {
		id, err := markdown.VirtualSourceID(path, i)
		if err != nil {
			w.logger.Warn("failed to create virtual source ID",
				slog.String("uri", uri), slog.Int("block", i), slog.Any("error", err))
			continue
		}
		block.SourceID = id
		validBlocks = append(validBlocks, block)
	}

	// Prepend synthetic schema declaration for snippet blocks that lack one.
	const snippetPrefix = "schema \"_snippet\"\n"
	for i := range validBlocks {
		if !markdown.HasSchemaDeclaration(validBlocks[i].Content) {
			validBlocks[i].Content = snippetPrefix + validBlocks[i].Content
			validBlocks[i].PrefixLines = 1
		}
	}

	// Analyze each block
	snapshots := make([]*analysis.Snapshot, len(validBlocks))
	for i, block := range validBlocks {
		virtualPath := block.SourceID.String()
		overlays := map[string][]byte{
			virtualPath: []byte(block.Content),
		}
		snapshot, err := w.sched.analyzer.Analyze(analyzeCtx, virtualPath, overlays, "", w.PositionEncoding(), schema.WithImportsAllowed(false))
		if err != nil {
			w.logger.Warn("markdown block analysis failed",
				slog.String("uri", uri), slog.Int("block", i), slog.Any("error", err))
		}
		snapshots[i] = snapshot
	}

	// Post-analysis cancellation check
	if analyzeCtx.Err() != nil {
		w.logger.Debug(
			"markdown analysis cancelled",
			slog.String("uri", uri),
			slog.Any("error", analyzeCtx.Err()),
		)
		return
	}

	if hook := w.analysisCompletedHookFn(); hook != nil {
		hook(uri)
	}

	// Version-gate and store results atomically
	w.mu.Lock()
	md = w.markdownDocs[uri]
	if md == nil || md.Version != entryVersion {
		w.mu.Unlock()
		w.logger.Debug("skipping stale markdown analysis results", slog.String("uri", uri))
		return
	}
	md.Blocks = validBlocks
	md.Snapshots = snapshots

	snap := &MarkdownDocumentSnapshot{
		URI:       md.URI,
		Version:   md.Version,
		Blocks:    make([]markdown.CodeBlock, len(validBlocks)),
		Snapshots: make([]*analysis.Snapshot, len(snapshots)),
	}
	copy(snap.Blocks, validBlocks)
	copy(snap.Snapshots, snapshots)
	w.mu.Unlock()

	// Publish diagnostics (no lock held)
	w.publishMarkdownDiagnostics(snap)
}

// publishMarkdownDiagnostics collects diagnostics from all block snapshots,
// remaps positions from block-local to markdown coordinates, and publishes.
func (w *Workspace) publishMarkdownDiagnostics(snap *MarkdownDocumentSnapshot) {
	var allDiagnostics []protocol.Diagnostic

	for i, snapshot := range snap.Snapshots {
		if snapshot == nil {
			continue
		}

		for _, uriDiag := range snapshot.LSPDiagnostics {
			d := uriDiag.Diagnostic

			// Skip diagnostics that reference synthetic prefix content
			if int(d.Range.Start.Line) < snap.Blocks[i].PrefixLines {
				continue
			}

			// Import-declaration diagnostics are soft hints in markdown blocks:
			// a snippet's imports are categorically not processed, so a
			// rejected, duplicated, or colliding import alias is informational
			// here, not an error in the example. The block's own semantic
			// diagnostics keep their severity.
			if d.Code != nil {
				if codeVal, ok := d.Code.Value.(string); ok && diag.IsImportDeclarationCode(codeVal) {
					hint := protocol.DiagnosticSeverityHint
					d.Severity = &hint
				}
			}

			// Convert primary range from block-local to markdown coordinates
			startLine, startChar := snap.BlockPositionToMarkdown(i,
				int(d.Range.Start.Line), int(d.Range.Start.Character))
			endLine, endChar := snap.BlockPositionToMarkdown(i,
				int(d.Range.End.Line), int(d.Range.End.Character))

			d.Range = protocol.Range{
				Start: protocol.Position{
					Line:      lsputil.ToUInteger(startLine),
					Character: lsputil.ToUInteger(startChar),
				},
				End: protocol.Position{
					Line:      lsputil.ToUInteger(endLine),
					Character: lsputil.ToUInteger(endChar),
				},
			}

			// Remap RelatedInformation URIs and ranges
			if len(d.RelatedInformation) > 0 {
				block := snap.Blocks[i]
				expectedURI := lsputil.PathToURI(block.SourceID.String())
				var remapped []protocol.DiagnosticRelatedInformation
				for _, rel := range d.RelatedInformation {
					if rel.Location.URI != expectedURI {
						w.logger.Warn("related info URI does not match expected block SourceID; skipping remap",
							slog.String("expected", expectedURI), slog.String("got", rel.Location.URI))
						remapped = append(remapped, rel)
						continue
					}

					relStartLine, relStartChar := snap.BlockPositionToMarkdown(i,
						int(rel.Location.Range.Start.Line), int(rel.Location.Range.Start.Character))
					relEndLine, relEndChar := snap.BlockPositionToMarkdown(i,
						int(rel.Location.Range.End.Line), int(rel.Location.Range.End.Character))

					remapped = append(remapped, protocol.DiagnosticRelatedInformation{
						Location: protocol.Location{
							URI: snap.URI,
							Range: protocol.Range{
								Start: protocol.Position{
									Line:      lsputil.ToUInteger(relStartLine),
									Character: lsputil.ToUInteger(relStartChar),
								},
								End: protocol.Position{
									Line:      lsputil.ToUInteger(relEndLine),
									Character: lsputil.ToUInteger(relEndChar),
								},
							},
						},
						Message: rel.Message,
					})
				}
				d.RelatedInformation = remapped
			}

			allDiagnostics = append(allDiagnostics, d)
		}
	}

	// Update publishedByEntry tracking
	w.mu.Lock()
	w.mapper.publishedByEntry[snap.URI] = map[string]struct{}{snap.URI: {}}
	w.mu.Unlock()

	v := protocol.Integer(snap.Version) //nolint:gosec // LSP version fits int32
	w.publishDiagnostics(snap.URI, &v, allDiagnostics)
}
