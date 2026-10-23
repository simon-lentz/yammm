// Package analysis wraps schema loading and validation for LSP analysis snapshots.
package analysis

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// diagnosticSource is the value used for the Source field in LSP diagnostics.
const diagnosticSource = "yammm"

// canonicalSourceID mints a SourceID whose key matches the loader's, so an
// overlay is registered once, as text the client sent, and not again under the
// loader's key as a file read from disk.
// The server already hands Analyze canonical paths; this holds the property
// for a caller that passes an absolute path that is not canonical.
func canonicalSourceID(path string) (location.SourceID, error) {
	id, err := location.SourceIDFromPath(path)
	if err != nil {
		return location.SourceID{}, fmt.Errorf("mint source ID for %q: %w", path, err)
	}
	return id, nil
}

// Snapshot represents the analysis result for a single entry file. Once the
// workspace publishes it, nothing changes it. It holds what the LSP features
// read of an analysis: the parsed schema, diagnostics, symbol indices, source
// content for position conversion, the module root, the entry's version and
// its imported paths.
//
// Snapshots are created by [Analyzer.Analyze] and [Analyzer.SnapshotForResult].
// [github.com/simon-lentz/yammm/lsp/internal/workspace.Workspace] keeps the
// latest one per entry file URI, and a markdown document's block snapshots in
// a list of its own. An edit schedules a new analysis, which replaces the
// previous snapshot when it completes; an analysis a later edit overtakes is
// cancelled and its result dropped.
type Snapshot struct {
	// CreatedAt records when this snapshot was created.
	CreatedAt time.Time

	// EntrySourceID identifies the entry file that was analyzed.
	EntrySourceID location.SourceID

	// EntryVersion is the document version at analysis time, used to
	// detect stale snapshots when the document has been edited.
	EntryVersion int

	// Root is the module root directory used for import resolution.
	Root string

	// Schema is the loaded schema. Non-nil only when the load succeeded:
	// any error in Result means a nil Schema (the loader's all-or-nothing
	// contract). Warnings may still be present alongside a non-nil Schema.
	Schema *schema.Schema

	// Result contains all diagnostics from analysis. Check Result.OK() to
	// determine if the schema is semantically valid.
	Result diag.Result

	// Sources holds every overlay the analysis was given, as text the client
	// sent, and every other source the load read, whether or not it produced
	// a schema. Position conversion reads it.
	Sources *lsputil.Texts

	// LSPDiagnostics contains diagnostics converted to LSP protocol format,
	// ready for publishing via textDocument/publishDiagnostics.
	LSPDiagnostics []URIDiagnostic

	// SymbolsBySource maps each source file to its symbol index.
	// Includes the entry file and all transitively imported files.
	SymbolsBySource map[location.SourceID]*symbols.SymbolIndex

	// ImportedPaths lists absolute paths of all files in the import closure
	// (excluding the entry file). Used for file watcher registration.
	ImportedPaths []string
}

// URIDiagnostic pairs a file URI with an LSP diagnostic for that file.
// This allows diagnostics to be grouped by URI for efficient publishing,
// since a single analysis may produce diagnostics across multiple files
// (e.g., errors in imported schemas).
type URIDiagnostic struct {
	// URI is the file:// URI of the document containing the diagnostic.
	URI string

	// Diagnostic is the LSP-formatted diagnostic with 0-based positions.
	Diagnostic protocol.Diagnostic
}

// SymbolIndexAt returns the symbol index for the given source ID.
func (s *Snapshot) SymbolIndexAt(sourceID location.SourceID) *symbols.SymbolIndex {
	if s == nil || s.SymbolsBySource == nil {
		return nil
	}
	return s.SymbolsBySource[sourceID]
}

// FindSymbolByName finds a symbol by name within a specific source.
func (s *Snapshot) FindSymbolByName(sourceID location.SourceID, name string, kind symbols.SymbolKind) *symbols.Symbol {
	return s.SymbolIndexAt(sourceID).FindByName(name, kind)
}

// ToSymbolView returns the subset of Snapshot fields needed by the symbols
// package for document symbol generation. This avoids manual field copying
// at call sites and breaks the import cycle (analysis imports symbols, so
// symbols cannot import analysis — SnapshotView is the adapter).
func (s *Snapshot) ToSymbolView() symbols.SnapshotView {
	return symbols.SnapshotView{
		EntryVersion:    s.EntryVersion,
		SymbolsBySource: s.SymbolsBySource,
		Sources:         s.Sources,
	}
}

// ResolveTypeReference resolves a type reference to its definition symbol.
// It handles both local and imported types, as well as datatype references.
func (s *Snapshot) ResolveTypeReference(ref *symbols.ReferenceSymbol, fromSourceID location.SourceID) *symbols.Symbol {
	if s == nil || ref == nil {
		return nil
	}

	// Determine the target symbol kind based on reference kind
	targetKind := symbols.SymbolType
	if ref.Kind == symbols.RefDataType {
		targetKind = symbols.SymbolDataType
	}

	// If qualified (e.g., "parts.Wheel"), resolve through import
	if ref.Qualifier != "" {
		return s.resolveQualifiedRef(ref, fromSourceID, targetKind)
	}

	// Local reference - look in the same source
	return s.FindSymbolByName(fromSourceID, ref.TargetName, targetKind)
}

// resolveQualifiedRef resolves a qualified reference like "parts.Wheel".
// The targetKind specifies whether to look for a type or datatype symbol.
func (s *Snapshot) resolveQualifiedRef(ref *symbols.ReferenceSymbol, fromSourceID location.SourceID, targetKind symbols.SymbolKind) *symbols.Symbol {
	// Find the import with the matching alias in the source file
	idx := s.SymbolIndexAt(fromSourceID)
	if idx == nil {
		return nil
	}

	// Find the import symbol with this alias
	var importSym *symbols.Symbol
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		if sym.Kind == symbols.SymbolImport && sym.Name == ref.Qualifier {
			importSym = sym
			break
		}
	}

	if importSym == nil || importSym.Data == nil {
		return nil
	}

	// Get the resolved schema from the import
	imp, ok := importSym.Data.(*schema.Import)
	if !ok || imp.Schema() == nil {
		return nil
	}

	// Find the symbol in the imported schema
	targetSourceID := imp.Schema().SourceID()
	return s.FindSymbolByName(targetSourceID, ref.TargetName, targetKind)
}

// Analyzer wraps schema loading for LSP analysis.
type Analyzer struct {
	logger *slog.Logger
}

// NewAnalyzer creates a new analyzer.
// If logger is nil, slog.Default() is used.
func NewAnalyzer(logger *slog.Logger) *Analyzer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Analyzer{
		logger: logger.With(slog.String("component", "analyzer")),
	}
}

// Analyze performs analysis on a schema file and returns a snapshot.
//
// The return values follow the standard entry point pattern:
//   - error != nil: the result holds a Fatal issue, such as an I/O failure, a
//     cancellation, input the load cannot begin from, or a header that does
//     not parse. The snapshot is still returned, with the diagnostics.
//   - error == nil && !snapshot.Result.OK(): Semantic failure. The schema
//     has parse or validation errors, but analysis completed normally.
//     The snapshot contains diagnostics describing the issues.
//   - error == nil && snapshot.Result.OK(): Success. The schema is valid.
//     The snapshot may still contain warnings (check Result.HasWarnings()).
//
// The opts parameter accepts optional [schema.LoadOption] values that are forwarded
// to [schema.LoadSourcesWithEntry]. For example, callers may pass
// [schema.WithImportsAllowed] with false to reject import declarations.
//
// Source-registry invariance: The Analyzer creates its own [lsputil.Texts],
// registers overlay content as text the client sent, and — after the load —
// registers every other source the load read as a file, whether or not the
// load produced a schema. The Texts is stored in the resulting [Snapshot]
// and is authoritative for position conversion and symbol indexing.
//
// The overlays map provides in-memory content that takes precedence over
// disk files. Keys should be canonical absolute paths (matching SourceID.String()).
// Files not in overlays are read from disk during import resolution, under a
// non-empty module root and unless opts restrict the load to the given sources.
//
// The ctx parameter supports cancellation; if cancelled, Analyze returns the
// partial snapshot with an error.
func (a *Analyzer) Analyze(ctx context.Context, entryPath string, overlays map[string][]byte, moduleRoot string, posEncoding lsputil.PositionEncoding, opts ...schema.LoadOption) (*Snapshot, error) {
	a.logger.Debug(
		"starting analysis",
		slog.String("entry", entryPath),
		slog.String("module_root", moduleRoot),
		slog.Int("overlay_count", len(overlays)),
	)

	// Create fresh source registry for this analysis
	sourceRegistry := lsputil.NewTexts()

	// Pre-register overlay content
	for path, content := range overlays {
		id, err := canonicalSourceID(path)
		if err != nil {
			a.logger.Warn(
				"failed to create source ID",
				slog.String("path", path),
				slog.Any("error", err),
			)
			continue
		}
		if err := sourceRegistry.Register(id, content); err != nil {
			a.logger.Warn(
				"failed to register source",
				slog.String("path", path),
				slog.Any("error", err),
			)
		}
	}

	// Build sources map for SourcesWithEntry using maps.Copy
	sources := make(map[string][]byte, len(overlays))
	maps.Copy(sources, overlays)

	// Perform the load with explicit entry path.
	// This ensures the correct document is analyzed even when multiple
	// documents are open (overlays from different files).
	var loaded *schema.Sources
	schemaResult, diagResult := schema.LoadSourcesWithEntry(
		ctx,
		sources,
		entryPath,
		moduleRoot,
		append(slices.Clone(opts), schema.CaptureSources(&loaded))...,
	)

	// Register every source the load read beyond the overlays: imports
	// resolved from disk, which position conversion and symbol indexing
	// need. The capture holds them for a load that produced no schema too,
	// and a diagnostic in such a file converts from its content like any
	// other. An overlay is already registered and keeps its origin.
	for _, id := range loaded.SourceIDs() {
		content, ok := loaded.ContentBySource(id)
		if !ok {
			continue
		}
		if err := sourceRegistry.RegisterFile(id, content); err != nil {
			a.logger.Warn(
				"failed to register loaded source",
				slog.String("source", id.String()),
				slog.Any("error", err),
			)
		}
	}

	// Diagnostics past the collector's issue limit are dropped before they
	// reach the editor; the drop is only observable server-side, so log it.
	if diagResult.LimitReached() {
		a.logger.Warn(
			"diagnostics truncated at issue limit",
			slog.String("entry", entryPath),
			slog.Int("dropped", diagResult.DroppedCount()),
		)
	}

	entrySourceID, idErr := canonicalSourceID(entryPath)
	if idErr != nil {
		a.logger.Warn(
			"failed to create entry source ID",
			slog.String("path", entryPath),
			slog.String("error", idErr.Error()),
		)
	}

	snapshot := &Snapshot{
		CreatedAt:       time.Now(),
		EntrySourceID:   entrySourceID,
		Root:            moduleRoot,
		Schema:          schemaResult,
		Result:          diagResult,
		Sources:         sourceRegistry,
		SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex),
	}

	if diagResult.HasFatal() {
		a.logger.Warn(
			"load failed with fatal diagnostics",
			slog.String("entry", entryPath),
		)
		// Return partial snapshot with diagnostics
		snapshot.LSPDiagnostics = a.convertDiagnostics(diagResult, sourceRegistry, entryPath, posEncoding)
		return snapshot, errors.New("load schema: fatal diagnostics")
	}

	// Convert diagnostics to LSP format
	snapshot.LSPDiagnostics = a.convertDiagnostics(diagResult, sourceRegistry, entryPath, posEncoding)

	// Build symbol indices for navigation
	if schemaResult != nil {
		seenSymbols := make(map[location.SourceID]struct{})
		a.buildSymbolIndices(snapshot, schemaResult, seenSymbols)

		// Extract import paths for dependency tracking
		seen := make(map[string]struct{})
		snapshot.ImportedPaths = a.extractImportPaths(schemaResult, seen)
		slices.Sort(snapshot.ImportedPaths) // Ensure deterministic order for logs and tests
	}

	a.logger.Debug(
		"analysis complete",
		slog.String("entry", entryPath),
		slog.Bool("ok", diagResult.OK()),
		slog.Int("issues", diagResult.Len()),
	)

	return snapshot, nil
}

// SnapshotForResult builds a snapshot from diagnostics alone, for a document
// the workspace could not load at all.
//
// It exists for the one failure that happens BEFORE a load: the module root
// could not be resolved, so there is nothing to hand LoadSourcesWithEntry. The
// diagnostic still belongs on the document, and this is what puts it there —
// a snapshot with a nil schema and no symbols, whose diagnostics convert
// through the same path Analyze uses, so a span-less issue lands at the entry
// document's first position.
//
// The contract is Analyze's, deliberately: an error is returned only when the
// result carries a Fatal issue, so an Error-severity finding publishes through
// the caller's normal success path with no "analysis failed" log line.
//
// The analyzer stays root-agnostic. Resolving the root is the workspace's job,
// and this method takes the result of that failure rather than repeating it.
func (a *Analyzer) SnapshotForResult(entryPath string, result diag.Result, posEncoding lsputil.PositionEncoding) (*Snapshot, error) {
	entrySourceID, idErr := canonicalSourceID(entryPath)
	if idErr != nil {
		a.logger.Warn(
			"failed to create entry source ID",
			slog.String("path", entryPath),
			slog.String("error", idErr.Error()),
		)
	}

	sourceRegistry := lsputil.NewTexts()
	snapshot := &Snapshot{
		CreatedAt:       time.Now(),
		EntrySourceID:   entrySourceID,
		Result:          result,
		Sources:         sourceRegistry,
		SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex),
		LSPDiagnostics:  a.convertDiagnostics(result, sourceRegistry, entryPath, posEncoding),
	}

	if result.HasFatal() {
		return snapshot, errors.New("load schema: fatal diagnostics")
	}
	return snapshot, nil
}

// buildSymbolIndices builds symbol indices for the schema and its imports.
// The seen map prevents infinite recursion if the schema loader permits cycles
// (or if imports resolve to the same canonical file via different paths).
func (a *Analyzer) buildSymbolIndices(snapshot *Snapshot, s *schema.Schema, seen map[location.SourceID]struct{}) {
	sourceID := s.SourceID()

	// Check for cycle/duplicate - skip if already processed
	if _, ok := seen[sourceID]; ok {
		return
	}
	seen[sourceID] = struct{}{}

	// Build index for this schema (pass sources for precise name span computation)
	snapshot.SymbolsBySource[sourceID] = symbols.BuildSymbolIndex(s, snapshot.Sources)

	// Build indices for imported schemas
	for imp := range s.Imports() {
		resolved := imp.Schema()
		if resolved != nil {
			a.buildSymbolIndices(snapshot, resolved, seen)
		}
	}
}

// extractImportPaths collects all import paths from the schema's import closure.
// Uses Import.SourceID() for resolved identity.
func (a *Analyzer) extractImportPaths(s *schema.Schema, seen map[string]struct{}) []string {
	if s == nil {
		return nil
	}

	var paths []string
	for imp := range s.Imports() {
		resolved := imp.Schema()
		if resolved == nil {
			continue
		}

		// Use resolved SourceID for canonical path
		sourceID := resolved.SourceID()
		path := sourceID.String()

		// Avoid duplicates
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}

		paths = append(paths, path)

		// Recurse into nested imports
		paths = append(paths, a.extractImportPaths(resolved, seen)...)
	}
	return paths
}

// convertDiagnostics converts diag.Result to LSP diagnostics.
// entryPath is used as the fallback URI for span-less diagnostics (e.g., I/O errors).
func (a *Analyzer) convertDiagnostics(result diag.Result, sources *lsputil.Texts, entryPath string, enc lsputil.PositionEncoding) []URIDiagnostic {
	// Compute entry URI once for span-less diagnostics
	entryURI := entryPath
	if !lsputil.HasURIScheme(entryPath) {
		entryURI = lsputil.PathToURI(entryPath)
	}

	var uriDiags []URIDiagnostic

	for issue := range result.Issues() {
		span := issue.Span()
		var uri string
		var diagRange protocol.Range
		var relatedInfo []protocol.DiagnosticRelatedInformation

		if span.IsZero() {
			// Span-less issues (e.g., file not found, I/O errors) are attached
			// to the entry file at position 0:0 so they appear in the Problems panel.
			uri = entryURI
			diagRange = protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			}
		} else {
			// Convert path to file:// URI (guard against double-encoding if already a URI)
			sourcePath := span.Source.String()
			uri = sourcePath
			if !lsputil.HasURIScheme(sourcePath) {
				uri = lsputil.PathToURI(sourcePath)
			}

			diagRange = lsputil.SpanToProtocolRange(sources, span, enc)

			relatedInfo = a.ConvertRelatedInfo(issue.Related(), sources, enc)
		}

		src := diagnosticSource
		uriDiags = append(uriDiags, URIDiagnostic{
			URI: uri,
			Diagnostic: protocol.Diagnostic{
				Range:              diagRange,
				Severity:           convertSeverity(issue.Severity()),
				Code:               &protocol.IntegerOrString{Value: issue.Code().String()},
				Source:             &src,
				Message:            issue.Message(),
				RelatedInformation: relatedInfo,
			},
		})
	}

	return uriDiags
}

// convertSeverity converts a yammm diagnostic severity to an LSP protocol severity.
func convertSeverity(sev diag.Severity) *protocol.DiagnosticSeverity {
	var s protocol.DiagnosticSeverity
	switch sev {
	case diag.Fatal, diag.Error:
		s = protocol.DiagnosticSeverityError
	case diag.Warning:
		s = protocol.DiagnosticSeverityWarning
	case diag.Info:
		s = protocol.DiagnosticSeverityInformation
	case diag.Hint:
		s = protocol.DiagnosticSeverityHint
	default:
		s = protocol.DiagnosticSeverityError
	}
	return &s
}

// ConvertRelatedInfo converts location.RelatedInfo to LSP DiagnosticRelatedInformation.
func (a *Analyzer) ConvertRelatedInfo(related []location.RelatedInfo, sources *lsputil.Texts, enc lsputil.PositionEncoding) []protocol.DiagnosticRelatedInformation {
	if len(related) == 0 {
		return nil
	}

	result := make([]protocol.DiagnosticRelatedInformation, 0, len(related))
	for _, rel := range related {
		if rel.Span.IsZero() || !rel.Span.Start.IsKnown() {
			continue
		}

		// Convert source to URI
		uri := rel.Span.Source.String()
		if !lsputil.HasURIScheme(uri) {
			uri = lsputil.PathToURI(uri)
		}

		// Convert span to LSP range
		relRange := lsputil.SpanToProtocolRange(sources, rel.Span, enc)

		result = append(result, protocol.DiagnosticRelatedInformation{
			Location: protocol.Location{
				URI:   uri,
				Range: relRange,
			},
			Message: rel.Message,
		})
	}

	if len(result) == 0 {
		return nil
	}
	return result
}
