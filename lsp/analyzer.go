package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"
)

// Snapshot represents an immutable analysis result for a single entry file.
// It captures the complete state needed for LSP features: parsed schema,
// diagnostics, symbol indices, and source content for position conversion.
//
// Snapshots are created by [Analyzer.Analyze] and stored in [Workspace]
// keyed by entry file URI. Each edit triggers a new snapshot, replacing
// the previous one.
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

	// Schema is the parsed schema, or nil if parsing failed catastrophically.
	// May be non-nil even when Result contains errors (partial parse).
	Schema *schema.Schema

	// Result contains all diagnostics from analysis. Check Result.OK() to
	// determine if the schema is semantically valid.
	Result diag.Result

	// Sources holds the content of all files in the import closure.
	// Used for UTF-16 position conversion via LineStartByte.
	Sources *source.Registry

	// LSPDiagnostics contains diagnostics converted to LSP protocol format,
	// ready for publishing via textDocument/publishDiagnostics.
	LSPDiagnostics []URIDiagnostic

	// SymbolsBySource maps each source file to its symbol index.
	// Includes the entry file and all transitively imported files.
	SymbolsBySource map[location.SourceID]*SymbolIndex

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
func (s *Snapshot) SymbolIndexAt(sourceID location.SourceID) *SymbolIndex {
	if s == nil || s.SymbolsBySource == nil {
		return nil
	}
	return s.SymbolsBySource[sourceID]
}

// FindSymbolByName finds a symbol by name within a specific source.
func (s *Snapshot) FindSymbolByName(sourceID location.SourceID, name string, kind SymbolKind) *Symbol {
	idx := s.SymbolIndexAt(sourceID)
	if idx == nil {
		return nil
	}
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		if sym.Name == name && sym.Kind == kind {
			return sym
		}
	}
	return nil
}

// ResolveTypeReference resolves a type reference to its definition symbol.
// It handles both local and imported types, as well as datatype references.
func (s *Snapshot) ResolveTypeReference(ref *ReferenceSymbol, fromSourceID location.SourceID) *Symbol {
	if s == nil || ref == nil {
		return nil
	}

	// Determine the target symbol kind based on reference kind
	targetKind := SymbolType
	if ref.Kind == RefDataType {
		targetKind = SymbolDataType
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
func (s *Snapshot) resolveQualifiedRef(ref *ReferenceSymbol, fromSourceID location.SourceID, targetKind SymbolKind) *Symbol {
	// Find the import with the matching alias in the source file
	idx := s.SymbolIndexAt(fromSourceID)
	if idx == nil {
		return nil
	}

	// Find the import symbol with this alias
	var importSym *Symbol
	for i := range idx.Symbols {
		sym := &idx.Symbols[i]
		if sym.Kind == SymbolImport && sym.Name == ref.Qualifier {
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

// Analyzer wraps schema/load for LSP analysis.
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

// Analyze performs analysis on a schema file and returns an immutable snapshot.
//
// The return values follow the standard entry point pattern:
//   - error != nil: Catastrophic failure (I/O error, internal corruption).
//     A partial snapshot may still be returned with available diagnostics.
//   - error == nil && !snapshot.Result.OK(): Semantic failure. The schema
//     has parse or validation errors, but analysis completed normally.
//     The snapshot contains diagnostics describing the issues.
//   - error == nil && snapshot.Result.OK(): Success. The schema is valid.
//     The snapshot may still contain warnings (check Result.Warnings()).
//
// The overlays map provides in-memory content that takes precedence over
// disk files. Keys should be canonical absolute paths (matching SourceID.String()).
// Files not in overlays are read from disk during import resolution.
//
// The ctx parameter supports cancellation; if cancelled, Analyze returns
// early with a partial or nil snapshot.
func (a *Analyzer) Analyze(ctx context.Context, entryPath string, overlays map[string][]byte, moduleRoot string) (*Snapshot, error) {
	a.logger.Debug("starting analysis",
		slog.String("entry", entryPath),
		slog.String("module_root", moduleRoot),
		slog.Int("overlay_count", len(overlays)),
	)

	// Create fresh source registry for this analysis
	sourceRegistry := source.NewRegistry()

	// Pre-register overlay content
	for path, content := range overlays {
		id, err := location.SourceIDFromAbsolutePath(path)
		if err != nil {
			a.logger.Warn("failed to create source ID",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
			continue
		}
		if err := sourceRegistry.Register(id, content); err != nil {
			a.logger.Warn("failed to register source",
				slog.String("path", path),
				slog.String("error", err.Error()),
			)
		}
	}

	// Build sources map for LoadSourcesWithEntry using maps.Copy
	sources := make(map[string][]byte, len(overlays))
	maps.Copy(sources, overlays)

	// Perform the load with explicit entry path.
	// This ensures the correct document is analyzed even when multiple
	// documents are open (overlays from different files).
	schemaResult, diagResult, loadErr := load.LoadSourcesWithEntry(
		ctx,
		sources,
		entryPath,
		moduleRoot,
		load.WithSourceRegistry(sourceRegistry),
	)

	entrySourceID, idErr := location.SourceIDFromAbsolutePath(entryPath)
	if idErr != nil {
		a.logger.Warn("failed to create entry source ID",
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
		SymbolsBySource: make(map[location.SourceID]*SymbolIndex),
	}

	if loadErr != nil {
		a.logger.Warn("load failed with error",
			slog.String("entry", entryPath),
			slog.String("error", loadErr.Error()),
		)
		// Return partial snapshot with diagnostics
		snapshot.LSPDiagnostics = a.convertDiagnostics(diagResult, sourceRegistry, entryPath)
		return snapshot, fmt.Errorf("load schema: %w", loadErr)
	}

	// Convert diagnostics to LSP format
	snapshot.LSPDiagnostics = a.convertDiagnostics(diagResult, sourceRegistry, entryPath)

	// Build symbol indices for navigation
	if schemaResult != nil {
		seenSymbols := make(map[location.SourceID]struct{})
		a.buildSymbolIndices(snapshot, schemaResult, seenSymbols)

		// Extract import paths for dependency tracking
		seen := make(map[string]struct{})
		snapshot.ImportedPaths = a.extractImportPaths(schemaResult, seen)
		slices.Sort(snapshot.ImportedPaths) // Ensure deterministic order for logs and tests
	}

	// Log analysis result
	issueCount := 0
	for range diagResult.Issues() {
		issueCount++
	}

	a.logger.Debug("analysis complete",
		slog.String("entry", entryPath),
		slog.Bool("ok", diagResult.OK()),
		slog.Int("issues", issueCount),
	)

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
	snapshot.SymbolsBySource[sourceID] = BuildSymbolIndex(s, snapshot.Sources)

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
func (a *Analyzer) convertDiagnostics(result diag.Result, sources *source.Registry, entryPath string) []URIDiagnostic {
	renderer := diag.NewRenderer(
		diag.WithSourceProvider(sources),
		diag.WithLSPByteFallback(diag.LSPByteFallbackApproximate),
	)

	// Compute entry URI once for span-less diagnostics
	entryURI := entryPath
	if !hasURIScheme(entryPath) {
		entryURI = PathToURI(entryPath)
	}

	uriDiags := make([]URIDiagnostic, 0)

	for issue := range result.Issues() {
		span := issue.Span()
		var uri string
		var diagRange protocol.Range
		var severity int
		var code, message string
		var relatedInfo []protocol.DiagnosticRelatedInformation

		if span.IsZero() {
			// Span-less issues (e.g., file not found, I/O errors) are attached
			// to the entry file at position 0:0 so they appear in the Problems panel.
			// We construct a minimal diagnostic directly since LSPDiagnostic returns nil
			// for span-less issues.
			uri = entryURI
			diagRange = protocol.Range{
				Start: protocol.Position{Line: 0, Character: 0},
				End:   protocol.Position{Line: 0, Character: 0},
			}
			severity = diag.SeverityToLSP(issue.Severity())
			code = issue.Code().String()
			message = issue.Message()
			// No related info for span-less issues (can't convert without spans)
		} else {
			lspDiag := renderer.LSPDiagnostic(issue)
			if lspDiag == nil {
				continue
			}
			// Convert path to file:// URI (guard against double-encoding if already a URI)
			sourcePath := span.Source.String()
			uri = sourcePath
			if !hasURIScheme(sourcePath) {
				uri = PathToURI(sourcePath)
			}
			diagRange = protocol.Range{
				Start: protocol.Position{
					Line:      toUInteger(lspDiag.Range.Start.Line),
					Character: toUInteger(lspDiag.Range.Start.Character),
				},
				End: protocol.Position{
					Line:      toUInteger(lspDiag.Range.End.Line),
					Character: toUInteger(lspDiag.Range.End.Character),
				},
			}
			severity = lspDiag.Severity
			code = lspDiag.Code
			message = lspDiag.Message
			relatedInfo = a.convertRelatedInfo(lspDiag.RelatedInformation)
		}

		source := "yammm"
		uriDiags = append(uriDiags, URIDiagnostic{
			URI: uri,
			Diagnostic: protocol.Diagnostic{
				Range:              diagRange,
				Severity:           a.convertSeverity(severity),
				Code:               &protocol.IntegerOrString{Value: code},
				Source:             &source,
				Message:            message,
				RelatedInformation: relatedInfo,
			},
		})
	}

	return uriDiags
}

// toUInteger safely converts an int to protocol.UInteger (uint32).
// Negative values are clamped to 0.
func toUInteger(n int) protocol.UInteger {
	if n < 0 {
		return 0
	}
	return protocol.UInteger(n) //nolint:gosec // clamped to non-negative
}

// convertSeverity converts diag severity to LSP protocol severity.
func (a *Analyzer) convertSeverity(severity int) *protocol.DiagnosticSeverity {
	var s protocol.DiagnosticSeverity
	switch severity {
	case diag.LSPSeverityError:
		s = protocol.DiagnosticSeverityError
	case diag.LSPSeverityWarning:
		s = protocol.DiagnosticSeverityWarning
	case diag.LSPSeverityInformation:
		s = protocol.DiagnosticSeverityInformation
	case diag.LSPSeverityHint:
		s = protocol.DiagnosticSeverityHint
	default:
		s = protocol.DiagnosticSeverityError
	}
	return &s
}

// convertRelatedInfo converts diag.LSPRelatedInfo to protocol.DiagnosticRelatedInformation.
func (a *Analyzer) convertRelatedInfo(related []diag.LSPRelatedInfo) []protocol.DiagnosticRelatedInformation {
	if len(related) == 0 {
		return nil
	}

	result := make([]protocol.DiagnosticRelatedInformation, 0, len(related))
	for _, rel := range related {
		// Guard against double-encoding: if the URI already has a scheme,
		// use it as-is. Otherwise, treat it as a filesystem path and convert
		// to a file:// URI. This is defensive against future changes to
		// diag.Renderer that might emit URIs directly instead of paths.
		uri := rel.Location.URI
		if !hasURIScheme(uri) {
			uri = PathToURI(uri)
		}

		result = append(result, protocol.DiagnosticRelatedInformation{
			Location: protocol.Location{
				URI: uri,
				Range: protocol.Range{
					Start: protocol.Position{
						Line:      toUInteger(rel.Location.Range.Start.Line),
						Character: toUInteger(rel.Location.Range.Start.Character),
					},
					End: protocol.Position{
						Line:      toUInteger(rel.Location.Range.End.Line),
						Character: toUInteger(rel.Location.Range.End.Character),
					},
				},
			},
			Message: rel.Message,
		})
	}
	return result
}

// hasURIScheme reports whether s appears to have a URI scheme prefix.
// It checks for the common "scheme://" pattern used by hierarchical URIs
// like file:// and http://. This is used to avoid double-encoding URIs
// that already have a scheme.
//
// The scheme is validated per RFC3986: scheme = ALPHA *( ALPHA / DIGIT / "+" / "-" / "." )
// This correctly identifies:
//   - "file:///path" → true (has scheme)
//   - "http://example.com" → true (has scheme)
//   - "custom-scheme://host/path" → true (long scheme is valid)
//   - "/path/to/file" → false (Unix filesystem path, no "://")
//   - "C:\path\file" → false (Windows path, no "://")
func hasURIScheme(s string) bool {
	idx := strings.Index(s, "://")
	if idx <= 0 {
		return false
	}
	scheme := s[:idx]
	// RFC3986: scheme must start with ALPHA
	if !isSchemeAlpha(scheme[0]) {
		return false
	}
	// RFC3986: subsequent chars can be ALPHA / DIGIT / "+" / "-" / "."
	for i := 1; i < len(scheme); i++ {
		c := scheme[i]
		if !isSchemeAlpha(c) && !isSchemeDigit(c) && c != '+' && c != '-' && c != '.' {
			return false
		}
	}
	return true
}

// isSchemeAlpha reports whether c is an ASCII letter (RFC3986 ALPHA).
func isSchemeAlpha(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// isSchemeDigit reports whether c is an ASCII digit (RFC3986 DIGIT).
func isSchemeDigit(c byte) bool {
	return c >= '0' && c <= '9'
}
