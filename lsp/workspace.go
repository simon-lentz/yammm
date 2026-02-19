package lsp

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/tliron/glsp"
	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/load"
)

// PositionEncoding represents the position encoding used for LSP communication.
// LSP 3.17 introduced position encoding negotiation; prior versions assumed UTF-16.
type PositionEncoding string

const (
	// PositionEncodingUTF16 counts positions in UTF-16 code units.
	// This is the default for LSP compatibility: VS Code and most editors
	// use UTF-16 internally (JavaScript strings), and LSP < 3.17 mandates it.
	// Multi-byte Unicode characters (e.g., emoji, CJK) require conversion
	// from YAMMM's internal rune-based Column to UTF-16 code units.
	PositionEncodingUTF16 PositionEncoding = "utf-16"

	// PositionEncodingUTF8 counts positions in UTF-8 bytes.
	// Some newer editors (e.g., Neovim with LSP 3.17) prefer this encoding
	// as it avoids UTF-16 surrogate pair complexity. When negotiated,
	// positions map directly to byte offsets within lines.
	PositionEncodingUTF8 PositionEncoding = "utf-8"
)

// debounceDelay is the delay before triggering analysis after a change.
const debounceDelay = 150 * time.Millisecond

// debounceEntry tracks a pending analysis for a single document.
// Using a struct with pointer identity allows callbacks to safely clean up
// only their own entries, avoiding the race where a stale callback deletes
// a newer entry that was scheduled while analysis was running.
type debounceEntry struct {
	timer  *time.Timer
	cancel context.CancelFunc
}

// Notifier is a function that sends LSP notifications.
// This type allows capturing only the notification capability from a glsp.Context,
// rather than the entire context object. This reduces coupling in closures
// (e.g., debounce timers) and makes explicit what capability is being captured.
type Notifier func(method string, params any)

// Document represents an open text document.
// LineState holds cached per-line analysis results for completion context detection.
// This enables O(1) lookup for isInsideTypeBody instead of O(n) scanning from line 0.
type LineState struct {
	Version        int    // Document version this state was computed for
	BraceDepth     []int  // BraceDepth[i] = nesting depth at END of line i
	InBlockComment []bool // InBlockComment[i] = true if line i ends inside a block comment
}

// Document represents an open document in the workspace.
type Document struct {
	URI       string
	SourceID  location.SourceID
	Version   int
	Text      string
	OpenOrder int // Order in which document was opened (for deterministic URI selection)

	// lineState caches per-line brace depth for completion context.
	// Lazily computed and invalidated when Version changes.
	lineState *LineState
}

// DocumentSnapshot is an immutable view of a document at a point in time.
// Use this when you need to access document state outside of locks to avoid
// data races with concurrent DocumentChanged calls.
type DocumentSnapshot struct {
	URI       string
	SourceID  location.SourceID
	Version   int
	Text      string
	LineState *LineState // Cached brace depth per line (may be nil)
}

// ComputeBraceDepths computes the brace nesting depth and block comment state
// at the end of each line. This is used for completion context detection (isInsideTypeBody).
// The function properly skips braces inside comments and string literals.
//
// Returns two parallel slices:
//   - depths[i] = brace nesting depth at end of line i
//   - inComment[i] = true if line i ends inside a block comment
func ComputeBraceDepths(text string) (depths []int, inComment []bool) {
	// Normalize line endings to LF for consistent processing.
	// Windows clients may send CRLF (\r\n), which would leave trailing \r
	// in each line after splitting on \n.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	depths = make([]int, len(lines))
	inComment = make([]bool, len(lines))
	braceDepth := 0
	inBlockComment := false

	for i, line := range lines {
		j := 0
		for j < len(line) {
			// Handle block comment continuation
			if inBlockComment {
				if j+1 < len(line) && line[j] == '*' && line[j+1] == '/' {
					j += 2
					inBlockComment = false
					continue
				}
				j++
				continue
			}

			ch := line[j]

			// Skip line comments (rest of line)
			if j+1 < len(line) && line[j] == '/' && line[j+1] == '/' {
				break
			}

			// Start block comment
			if j+1 < len(line) && line[j] == '/' && line[j+1] == '*' {
				inBlockComment = true
				j += 2
				continue
			}

			// Skip string literals
			if ch == '"' || ch == '\'' {
				quote := ch
				j++
				for j < len(line) {
					if line[j] == '\\' && j+1 < len(line) {
						j += 2 // skip escape sequence
						continue
					}
					if line[j] == quote {
						j++
						break
					}
					j++
				}
				continue
			}

			// Count braces
			switch ch {
			case '{':
				braceDepth++
			case '}':
				braceDepth--
			}
			j++
		}
		depths[i] = braceDepth
		inComment[i] = inBlockComment
	}

	return depths, inComment
}

// Workspace manages the state of open documents and analysis results.
type Workspace struct {
	mu sync.RWMutex

	logger *slog.Logger
	config Config

	// Workspace roots (from workspaceFolders)
	roots []string

	// Open documents keyed by URI
	open map[string]*Document

	// Open markdown documents keyed by URI
	markdownDocs map[string]*MarkdownDocument

	// Counter for deterministic document ordering (symlink disambiguation)
	openCounter int

	// Cached mapping from canonical paths to client URIs (for symlink resolution).
	//
	// Path normalization invariants:
	//   - Keys use forward-slash normalized paths matching SourceID.String() format
	//   - This enables cross-platform consistency (Windows paths converted to forward slashes)
	//   - The location.CanonicalPath type guarantees: absolute, clean, NFC-normalized, forward slashes
	//   - findModuleRoot operates on OS-native paths separately (uses filepath.Separator)
	//
	// Nil when invalidated; lazily rebuilt on first access after mutation.
	// Invalidated by DocumentOpened/DocumentClosed.
	canonicalToURI map[string]string

	// Latest analysis snapshots keyed by entry URI
	snapshots map[string]*Snapshot

	// Position encoding negotiated with client
	posEncoding PositionEncoding

	// Forward dependencies: entry URI -> set of imported URIs
	importsByEntry map[string]map[string]struct{}

	// Reverse dependencies: imported URI -> set of entry URIs that import it
	reverseDeps map[string]map[string]struct{}

	// Debounce entries for analysis, keyed by URI.
	// Each entry tracks a pending analysis timer and its cancellation function.
	// Using a single map of entry pointers enables safe cleanup via pointer identity.
	debounces  map[string]*debounceEntry
	debounceMu sync.Mutex

	// Previously published diagnostics URIs, keyed by entry URI.
	// This per-entry tracking ensures that analyzing one entry doesn't
	// clear diagnostics published by a different entry file.
	publishedByEntry map[string]map[string]struct{}

	// Analyzer for schema loading
	analyzer *Analyzer
}

// NewWorkspace creates a new workspace.
// If logger is nil, slog.Default() is used.
func NewWorkspace(logger *slog.Logger, cfg Config) *Workspace {
	if logger == nil {
		logger = slog.Default()
	}

	// Canonicalize ModuleRoot if set, to ensure consistent path comparisons
	// with workspace roots (which are also canonicalized in AddRoot).
	if cfg.ModuleRoot != "" {
		if resolved, err := filepath.EvalSymlinks(cfg.ModuleRoot); err == nil {
			cfg.ModuleRoot = filepath.Clean(resolved)
		} else {
			// Fallback to cleaned path when symlink resolution fails
			cfg.ModuleRoot = filepath.Clean(cfg.ModuleRoot)
		}
	}

	return &Workspace{
		logger:           logger.With(slog.String("component", "workspace")),
		config:           cfg,
		roots:            make([]string, 0),
		open:             make(map[string]*Document),
		markdownDocs:     make(map[string]*MarkdownDocument),
		snapshots:        make(map[string]*Snapshot),
		posEncoding:      PositionEncodingUTF16,
		importsByEntry:   make(map[string]map[string]struct{}),
		reverseDeps:      make(map[string]map[string]struct{}),
		debounces:        make(map[string]*debounceEntry),
		publishedByEntry: make(map[string]map[string]struct{}),
		analyzer:         NewAnalyzer(logger), // Pass base logger; analyzer adds its own component
	}
}

// AddRoot adds a workspace root.
func (w *Workspace) AddRoot(uri string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path, err := URIToPath(uri)
	if err != nil {
		w.logger.Warn("failed to parse workspace root URI",
			slog.String("uri", uri),
			slog.String("error", err.Error()),
		)
		return
	}

	// Resolve symlinks to get canonical path matching DocumentOpened behavior.
	// This ensures roots and open documents live in the same namespace,
	// making findModuleRoot reliable under symlinks (e.g., /var → /private/var on macOS).
	canonicalPath := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonicalPath = filepath.Clean(resolved)
	}

	// Check for duplicates before appending to prevent unbounded root growth
	if slices.Contains(w.roots, canonicalPath) {
		w.logger.Debug("workspace root already exists", slog.String("path", canonicalPath))
		return
	}

	w.roots = append(w.roots, canonicalPath)
	w.logger.Debug("added workspace root", slog.String("path", canonicalPath))
}

// RemoveRoot removes a workspace root.
func (w *Workspace) RemoveRoot(uri string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path, err := URIToPath(uri)
	if err != nil {
		w.logger.Warn("failed to parse workspace root URI for removal",
			slog.String("uri", uri),
			slog.String("error", err.Error()),
		)
		return
	}

	// Resolve symlinks to match how roots are stored
	canonicalPath := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonicalPath = filepath.Clean(resolved)
	}

	// Remove all matching roots (in case of prior duplicates)
	newRoots := w.roots[:0] // reuse backing array
	removed := false
	for _, root := range w.roots {
		if root != canonicalPath {
			newRoots = append(newRoots, root)
		} else {
			removed = true
		}
	}
	w.roots = newRoots

	if removed {
		w.logger.Debug("removed workspace root", slog.String("path", canonicalPath))
	} else {
		w.logger.Debug("workspace root not found for removal", slog.String("path", canonicalPath))
	}
}

// SetPositionEncoding sets the position encoding to use.
func (w *Workspace) SetPositionEncoding(enc PositionEncoding) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.posEncoding = enc
}

// PositionEncoding returns the negotiated position encoding.
func (w *Workspace) PositionEncoding() PositionEncoding {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.posEncoding
}

// DocumentOpened handles a document being opened.
func (w *Workspace) DocumentOpened(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path, err := URIToPath(uri)
	if err != nil {
		w.logger.Warn("failed to parse document URI",
			slog.String("uri", uri),
			slog.String("error", err.Error()),
		)
		return
	}

	// Resolve symlinks to get canonical path matching the loader's behavior.
	// The loader uses makeCanonicalPath which resolves symlinks, so we need
	// to do the same here to ensure SourceID matches loader output.
	// Note: EvalSymlinks also cleans the path, but we call Clean explicitly
	// to make the invariant visible (canonical paths are always clean).
	canonicalPath := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonicalPath = filepath.Clean(resolved)
	}

	sourceID, err := location.SourceIDFromAbsolutePath(canonicalPath)
	if err != nil {
		w.logger.Warn("failed to create source ID",
			slog.String("path", canonicalPath),
			slog.String("error", err.Error()),
		)
		return
	}

	// Normalize line endings (CRLF/CR → LF) for consistent offset calculations.
	// Windows clients may send CRLF, which would cause mismatches with
	// line-based operations if not normalized here at the storage layer.
	text = normalizeLineEndings(text)

	w.openCounter++
	w.open[uri] = &Document{
		URI:       uri,
		SourceID:  sourceID,
		Version:   version,
		Text:      text,
		OpenOrder: w.openCounter,
	}

	// Invalidate canonical-to-URI cache (new document may map to a canonical path)
	w.canonicalToURI = nil
}

// DocumentChanged handles a document content change.
// Ignores stale updates where version <= current version (unless version is 0/unknown).
func (w *Workspace) DocumentChanged(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if doc, ok := w.open[uri]; ok {
		// Ignore stale updates (version <= current) unless version is 0 (unknown).
		// This prevents out-of-order updates from overwriting newer content.
		if version != 0 && doc.Version != 0 && version <= doc.Version {
			w.logger.Debug("ignoring stale document change",
				slog.String("uri", uri),
				slog.Int("incoming_version", version),
				slog.Int("current_version", doc.Version),
			)
			return
		}
		doc.Version = version
		// Normalize line endings (CRLF/CR → LF) for consistent offset calculations.
		doc.Text = normalizeLineEndings(text)
		// Invalidate cached LineState - content changed, cache must be recomputed.
		// This is essential when version==0 (unknown), since version-based invalidation
		// would incorrectly consider the cache valid despite text changes.
		doc.lineState = nil
	}
}

// DocumentClosed handles a document being closed.
// notify is the notification function for clearing diagnostics; if nil,
// diagnostics are not cleared (useful in tests).
func (w *Workspace) DocumentClosed(notify Notifier, uri string) {
	w.mu.Lock()
	delete(w.open, uri)
	delete(w.snapshots, uri)

	// Invalidate canonical-to-URI cache (document removal changes mapping)
	w.canonicalToURI = nil

	// Get URIs published from this entry's analysis and remove tracking
	publishedFromEntry := w.publishedByEntry[uri]
	delete(w.publishedByEntry, uri)

	// Determine which URIs to clear: only those not published by any other entry
	urisToClear := make([]string, 0)
	for pubURI := range publishedFromEntry {
		stillPublished := false
		for _, otherPubs := range w.publishedByEntry {
			if _, ok := otherPubs[pubURI]; ok {
				stillPublished = true
				break
			}
		}
		if !stillPublished {
			urisToClear = append(urisToClear, pubURI)
		}
	}
	w.mu.Unlock()

	// Clear diagnostics for URIs only published by this entry
	for _, pubURI := range urisToClear {
		w.publishDiagnostics(notify, pubURI, nil)
	}

	// Cancel any pending analysis
	w.cancelPendingAnalysis(uri)

	// Clean up dependency tracking for this entry
	w.UpdateDependencies(uri, nil)
}

// ReanalyzeOpenDocuments triggers re-analysis of all open documents.
// This is called when workspace folders change, as module root selection
// may have changed for existing documents.
func (w *Workspace) ReanalyzeOpenDocuments(ctx *glsp.Context) {
	w.mu.RLock()
	uris := make([]string, 0, len(w.open))
	for uri := range w.open {
		uris = append(uris, uri)
	}
	w.mu.RUnlock()

	for _, uri := range uris {
		w.ScheduleAnalysis(ctx, uri)
	}
}

// ScheduleAnalysis schedules a debounced analysis for the given document.
func (w *Workspace) ScheduleAnalysis(glspCtx *glsp.Context, uri string) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	// Cancel existing entry
	if existing, ok := w.debounces[uri]; ok {
		existing.timer.Stop()
		existing.cancel()
	}

	// Create new cancellation context
	analyzeCtx, cancel := context.WithCancel(context.Background())

	// Create entry before timer so we can capture its pointer for identity check.
	// This pointer identity is used in the callback to ensure we only clean up
	// our own entry, not a newer one that may have been scheduled while we were
	// running analysis.
	entry := &debounceEntry{cancel: cancel}

	// Extract only the Notify function from the context.
	// This reduces coupling by capturing only what's needed, rather than
	// the entire glsp.Context object. The Notify function is bound to
	// the long-lived connection and handles concurrent writes internally.
	var notify Notifier
	if glspCtx != nil {
		// Wrap glsp.NotifyFunc to match our Notifier type signature
		notify = func(method string, params any) {
			glspCtx.Notify(method, params)
		}
	}

	// Schedule new analysis, capturing entry pointer for identity check
	entry.timer = time.AfterFunc(debounceDelay, func() {
		select {
		case <-analyzeCtx.Done():
			return
		default:
			w.AnalyzeAndPublish(notify, analyzeCtx, uri)
			// Clean up only if this is still our entry.
			// If a new ScheduleAnalysis call occurred while we were running,
			// a new entry will be in the map and we must not delete it.
			w.debounceMu.Lock()
			if w.debounces[uri] == entry {
				delete(w.debounces, uri)
			}
			w.debounceMu.Unlock()
		}
	})

	w.debounces[uri] = entry
}

// AnalyzeAndPublish analyzes a document and publishes diagnostics.
// analyzeCtx is a cancellable context; if cancelled, analysis aborts early.
// notify is the notification function for publishing diagnostics; if nil,
// diagnostics are computed but not published (useful in tests).
func (w *Workspace) AnalyzeAndPublish(notify Notifier, analyzeCtx context.Context, uri string) {
	w.mu.RLock()
	doc, ok := w.open[uri]
	if !ok {
		w.mu.RUnlock()
		return
	}

	// Collect overlay content from all open documents.
	// Use canonical SourceID as key to ensure symlinks and path variations
	// map to the same entry that the loader will use.
	overlays := make(map[string][]byte)
	for _, d := range w.open {
		overlays[d.SourceID.String()] = []byte(d.Text)
	}

	// Capture version before releasing lock
	entryVersion := doc.Version
	w.mu.RUnlock()

	// Find module root for this document (after releasing lock to avoid deadlock)
	path, err := URIToPath(uri)
	if err != nil {
		w.logger.Warn("failed to parse document URI for analysis",
			slog.String("uri", uri),
			slog.String("error", err.Error()),
		)
		return
	}

	// Canonicalize path to match workspace roots (which are now canonical).
	// This ensures findModuleRoot comparisons work under symlinks.
	canonicalPath := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		canonicalPath = filepath.Clean(resolved)
	}

	moduleRoot := w.findModuleRoot(canonicalPath)

	// Perform analysis with cancellable context.
	// Use canonical path to ensure consistent SourceID creation.
	snapshot, err := w.analyzer.Analyze(analyzeCtx, canonicalPath, overlays, moduleRoot)

	// Check if context was cancelled - abort silently
	if analyzeCtx.Err() != nil {
		w.logger.Debug("analysis cancelled", slog.String("uri", uri))
		return
	}

	// Version gate: skip publishing if document has been modified during analysis.
	// This prevents stale diagnostics from overwriting fresher results.
	w.mu.RLock()
	currentDoc := w.open[uri]
	isStale := currentDoc == nil || currentDoc.Version != entryVersion
	w.mu.RUnlock()

	if isStale {
		w.logger.Debug("skipping stale analysis results",
			slog.String("uri", uri),
			slog.Int("entry_version", entryVersion),
		)
		return
	}

	if err != nil {
		w.logger.Error("analysis failed",
			slog.String("uri", uri),
			slog.String("error", err.Error()),
		)
		// Still publish diagnostics from partial snapshot if available.
		// This ensures users see parse errors instead of silent failure.
		if snapshot != nil {
			snapshot.EntryVersion = entryVersion
			w.mu.Lock()
			w.snapshots[uri] = snapshot
			w.mu.Unlock()
			w.UpdateDependencies(uri, snapshot.ImportedPaths)
			w.publishSnapshotDiagnostics(notify, uri, snapshot)
		}
		return
	}

	snapshot.EntryVersion = entryVersion

	// Store snapshot
	w.mu.Lock()
	w.snapshots[uri] = snapshot
	w.mu.Unlock()

	// Update dependency tracking for file watching
	w.UpdateDependencies(uri, snapshot.ImportedPaths)

	// Publish diagnostics
	w.publishSnapshotDiagnostics(notify, uri, snapshot)
}

// publishSnapshotDiagnostics publishes diagnostics from a snapshot.
// This method computes the publication plan under lock, then releases the lock
// before sending notifications to avoid potential deadlock if Notify blocks.
// If notify is nil (e.g., in tests without a transport), this is a no-op.
//
// URI remapping: Diagnostics from the loader use canonical (symlink-resolved)
// paths, but clients identify documents by the URI they used to open them
// (which may be a symlink path). This method remaps diagnostic URIs to match
// the client's open document URIs, ensuring diagnostics appear in the editor.
//
// entryURI identifies the entry file being analyzed, used for per-entry
// diagnostic tracking to avoid cross-entry interference in multi-file sessions.
func (w *Workspace) publishSnapshotDiagnostics(notify Notifier, entryURI string, snapshot *Snapshot) {
	if notify == nil {
		return // No-op in test context without transport
	}

	// Phase 1: Compute publication plan under lock
	diagsByURI, staleURIs := w.computePublicationPlan(entryURI, snapshot)

	// Phase 2: Emit notifications outside the lock
	// Clear stale diagnostics
	for _, uri := range staleURIs {
		notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: []protocol.Diagnostic{},
		})
	}

	// Publish current diagnostics
	for uri, diags := range diagsByURI {
		notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
			URI:         uri,
			Diagnostics: diags,
		})
	}
}

// computePublicationPlan computes the publication plan for diagnostics.
// It returns the diagnostics grouped by URI (with symlink remapping applied)
// and the list of stale URIs that need to be cleared.
//
// entryURI identifies the entry file being analyzed. Per-entry tracking ensures
// that analyzing one entry doesn't clear diagnostics from another entry.
//
// This method acquires the workspace lock internally.
func (w *Workspace) computePublicationPlan(entryURI string, snapshot *Snapshot) (diagsByURI map[string][]protocol.Diagnostic, staleURIs []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Build map from canonical paths to open document URIs.
	// The loader resolves symlinks, so diagnostics have canonical paths.
	// We need to map them back to the URIs the client used to open documents.
	// Reuse cached map if available (invalidated by DocumentOpened/DocumentClosed).
	if w.canonicalToURI == nil {
		w.canonicalToURI = w.buildCanonicalToURIMap()
	}
	canonicalToDocURI := w.canonicalToURI

	// Collect current URIs with diagnostics
	currentURIs := make(map[string]struct{})

	// Group diagnostics by URI, remapping to open document URIs where applicable
	diagsByURI = make(map[string][]protocol.Diagnostic)

	for _, lspDiag := range snapshot.LSPDiagnostics {
		// Remap the diagnostic URI to the open document URI if available
		pubURI := w.remapToOpenDocURI(lspDiag.URI, canonicalToDocURI)

		// Also remap URIs in RelatedInformation
		diag := lspDiag.Diagnostic
		if len(diag.RelatedInformation) > 0 {
			remapped := make([]protocol.DiagnosticRelatedInformation, len(diag.RelatedInformation))
			for i, rel := range diag.RelatedInformation {
				remapped[i] = protocol.DiagnosticRelatedInformation{
					Location: protocol.Location{
						URI:   w.remapToOpenDocURI(rel.Location.URI, canonicalToDocURI),
						Range: rel.Location.Range,
					},
					Message: rel.Message,
				}
			}
			diag.RelatedInformation = remapped
		}

		diagsByURI[pubURI] = append(diagsByURI[pubURI], diag)
		currentURIs[pubURI] = struct{}{}
	}

	// Find stale URIs (published by THIS entry before but not now)
	// Per-entry tracking: only clear URIs from this entry's previous publication
	previousURIs := w.publishedByEntry[entryURI]
	staleURIs = make([]string, 0)
	for uri := range previousURIs {
		if _, ok := currentURIs[uri]; !ok {
			staleURIs = append(staleURIs, uri)
		}
	}

	// Update published URIs tracking for this entry only
	w.publishedByEntry[entryURI] = currentURIs

	return diagsByURI, staleURIs
}

// publishDiagnostics publishes diagnostics for a URI.
// This method does not require the workspace lock as it only sends a notification
// and does not access any workspace state. Calling notify under lock risks
// deadlock if the notification blocks on I/O.
// If notify is nil (e.g., in tests without a transport), this is a no-op.
func (w *Workspace) publishDiagnostics(notify Notifier, uri string, diagnostics []protocol.Diagnostic) {
	if notify == nil {
		return // No-op in test context without transport
	}

	if diagnostics == nil {
		diagnostics = []protocol.Diagnostic{}
	}

	notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         uri,
		Diagnostics: diagnostics,
	})
}

// FileChanged handles a watched file change notification.
// It triggers reanalysis of any open documents that import the changed file.
//
// The incoming URI is canonicalized before lookup to handle symlink and path
// variations between what VS Code reports and what we store internally.
func (w *Workspace) FileChanged(ctx *glsp.Context, uri string, changeType protocol.UInteger) {
	// Canonicalize URI to match how we store reverseDeps keys.
	// VS Code may report symlinked or case-different paths.
	// Resolve symlinks to match the loader's canonicalization (makeCanonicalPath).
	canonicalURI := uri
	if path, err := URIToPath(uri); err == nil {
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			path = filepath.Clean(resolved)
		} else {
			// Symlink resolution failed (deleted file, broken symlink, permissions).
			// Fall back to filesystem-independent canonicalization via Clean.
			// This ensures we still attempt lookup even when the file is gone.
			path = filepath.Clean(path)
		}
		if sourceID, err := location.SourceIDFromAbsolutePath(path); err == nil {
			canonicalURI = PathToURI(sourceID.String())
		}
	}

	w.mu.RLock()
	// If this file is a dependency of open documents, reanalyze them
	deps := make(map[string]struct{})
	for k := range w.reverseDeps[canonicalURI] {
		deps[k] = struct{}{}
	}
	w.mu.RUnlock()

	for entryURI := range deps {
		w.ScheduleAnalysis(ctx, entryURI)
	}
}

// UpdateDependencies updates the dependency tracking for an entry file.
// importedPaths contains the absolute paths of all imported files in the closure.
// This uses an atomic update algorithm: remove old edges, add new edges, store new set.
func (w *Workspace) UpdateDependencies(entryURI string, importedPaths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Convert imported paths to URIs
	importedURIs := make(map[string]struct{}, len(importedPaths))
	for _, path := range importedPaths {
		importedURIs[PathToURI(path)] = struct{}{}
	}

	// Step 1: Remove old edges from reverse deps
	if oldImports, ok := w.importsByEntry[entryURI]; ok {
		for importURI := range oldImports {
			if entries, exists := w.reverseDeps[importURI]; exists {
				delete(entries, entryURI)
				// Clean up empty reverse dep entries
				if len(entries) == 0 {
					delete(w.reverseDeps, importURI)
				}
			}
		}
	}

	// Step 2: Add new edges to reverse deps
	for importURI := range importedURIs {
		if w.reverseDeps[importURI] == nil {
			w.reverseDeps[importURI] = make(map[string]struct{})
		}
		w.reverseDeps[importURI][entryURI] = struct{}{}
	}

	// Step 3: Store new import set (or delete if empty)
	if len(importedURIs) == 0 {
		delete(w.importsByEntry, entryURI)
	} else {
		w.importsByEntry[entryURI] = importedURIs
	}

	w.logger.Debug("updated dependencies",
		slog.String("entry", entryURI),
		slog.Int("imports", len(importedURIs)),
	)
}

// cancelPendingAnalysis cancels any pending analysis for a URI.
func (w *Workspace) cancelPendingAnalysis(uri string) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	if entry, ok := w.debounces[uri]; ok {
		entry.timer.Stop()
		entry.cancel()
		delete(w.debounces, uri)
	}
}

// Shutdown cancels all pending analysis operations.
// This should be called during server shutdown to ensure clean termination.
func (w *Workspace) Shutdown() {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	for uri, entry := range w.debounces {
		entry.timer.Stop()
		entry.cancel()
		delete(w.debounces, uri)
	}
}

// findModuleRoot finds the module root for a file path.
//
// For multi-root workspaces, this selects the longest (deepest) matching
// workspace folder. For example, given roots ["/project", "/project/submodule"],
// a file at "/project/submodule/foo.yammm" will use "/project/submodule" as
// its module root.
//
// This method acquires its own lock to safely read w.roots and w.config.
// Callers must NOT hold w.mu when calling this method to avoid deadlock.
func (w *Workspace) findModuleRoot(path string) string {
	w.mu.RLock()
	defer w.mu.RUnlock()

	// If configured module root, use it
	if w.config.ModuleRoot != "" {
		return w.config.ModuleRoot
	}

	// Find the nearest (deepest) workspace folder ancestor.
	// Select the longest matching prefix to handle nested workspace folders.
	// Use path-boundary check to avoid misclassifying paths like /project2/foo
	// as being under /project (strings.HasPrefix would incorrectly match).
	// Use filepath.Separator for cross-platform compatibility (/ on Unix, \ on Windows).
	var nearest string
	for _, root := range w.roots {
		if (path == root || strings.HasPrefix(path, root+string(filepath.Separator))) && len(root) > len(nearest) {
			nearest = root
		}
	}
	if nearest != "" {
		return nearest
	}

	// Fall back to the file's directory
	return filepath.Dir(path)
}

// LatestSnapshot returns the latest snapshot for a URI.
func (w *Workspace) LatestSnapshot(uri string) *Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshots[uri]
}

// GetDocumentSnapshot returns an immutable snapshot of the document for a URI.
// The snapshot contains a copy of the document state at the time of the call,
// allowing safe access outside of locks without racing with DocumentChanged.
func (w *Workspace) GetDocumentSnapshot(uri string) *DocumentSnapshot {
	w.mu.RLock()
	doc := w.open[uri]
	if doc == nil {
		w.mu.RUnlock()
		return nil
	}

	// Check if we need to compute LineState (lazy computation)
	if doc.lineState == nil || doc.lineState.Version != doc.Version {
		// Upgrade to write lock to compute and cache LineState
		w.mu.RUnlock()
		w.mu.Lock()
		// Double-check after acquiring write lock (another goroutine may have computed it)
		doc = w.open[uri]
		if doc != nil && (doc.lineState == nil || doc.lineState.Version != doc.Version) {
			depths, inComment := ComputeBraceDepths(doc.Text)
			doc.lineState = &LineState{
				Version:        doc.Version,
				BraceDepth:     depths,
				InBlockComment: inComment,
			}
		}
		w.mu.Unlock()
		w.mu.RLock()
		doc = w.open[uri]
		if doc == nil {
			w.mu.RUnlock()
			return nil
		}
	}

	snapshot := &DocumentSnapshot{
		URI:       doc.URI,
		SourceID:  doc.SourceID,
		Version:   doc.Version,
		Text:      doc.Text,
		LineState: doc.lineState,
	}
	w.mu.RUnlock()
	return snapshot
}

// URIToPath converts a file:// URI to a filesystem path.
//
// On POSIX systems: file:///path/to/file → /path/to/file
// On Windows: file:///C:/path/to/file → C:\path\to\file
//
// UNC paths are not currently supported on Windows.
func URIToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("parse URI %q: %w", uri, err)
	}
	if u.Scheme != "file" {
		return "", fmt.Errorf("not a file URI: %s", uri)
	}

	path := u.Path

	// Windows: file:///C:/path → C:\path
	if runtime.GOOS == "windows" {
		// Remove leading slash before drive letter: /C:/foo → C:/foo
		if len(path) >= 3 && path[0] == '/' && isWindowsDriveLetter(path[1]) && path[2] == ':' {
			path = path[1:]
		}
		// Convert forward slashes to backslashes
		path = filepath.FromSlash(path)
	}

	return path, nil
}

// PathToURI converts a filesystem path to a file:// URI.
//
// On POSIX systems: /path/to/file → file:///path/to/file
// On Windows: C:\path\to\file → file:///C:/path/to/file
//
// UNC paths are not currently supported on Windows.
func PathToURI(path string) string {
	// Ensure absolute path
	if !filepath.IsAbs(path) {
		absPath, err := filepath.Abs(path)
		if err == nil {
			path = absPath
		}
	}

	// Normalize to forward slashes for URI
	path = filepath.ToSlash(path)

	// Windows: C:/path → /C:/path (add leading slash for URI format)
	if runtime.GOOS == "windows" && len(path) >= 2 && isWindowsDriveLetter(path[0]) && path[1] == ':' {
		path = "/" + path
	}

	// Use url.URL to properly escape the path
	u := url.URL{
		Scheme: "file",
		Path:   path,
	}
	return u.String()
}

// isWindowsDriveLetter reports whether c is a valid Windows drive letter (A-Z, a-z).
func isWindowsDriveLetter(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// RemapPathToURI maps a path to the client's document URI if the file is open.
// This ensures definitions point to the same URI the client used to open the
// document (important for symlink scenarios where the client opens a symlink
// but the loader resolves to the canonical path).
//
// Input normalization: accepts canonical path, file:// URI, or raw filesystem
// path. All forms are normalized to a canonical path for lookup.
//
// When multiple documents resolve to the same canonical path (e.g., user opened
// both a symlink and the real file), returns the URI of the first-opened
// document for determinism.
//
// If the file is not open, returns a file:// URI for the path.
//
// Uses a cached mapping for O(1) lookup; cache is lazily rebuilt after
// document open/close operations.
func (w *Workspace) RemapPathToURI(input string) string {
	// Normalize input to canonical path
	var rawPath string
	if path, err := URIToPath(input); err == nil {
		// Valid file:// URI - use extracted path
		rawPath = path
	} else if hasURIScheme(input) {
		// Non-file URI scheme (e.g., untitled:) - return as-is
		return input
	} else {
		// Raw filesystem path
		rawPath = input
	}

	// Clean and convert to forward slashes to match SourceID.String() format.
	// On Windows, filepath operations produce backslashes, but SourceID
	// uses forward slashes for cross-platform consistency.
	cleanedPath := filepath.ToSlash(filepath.Clean(rawPath))

	w.mu.Lock()
	defer w.mu.Unlock()

	// Rebuild cache if invalidated
	if w.canonicalToURI == nil {
		w.canonicalToURI = w.buildCanonicalToURIMap()
	}

	// Fast path: try direct lookup first. This succeeds when input is already
	// a canonical SourceID.String() path (common case from the schema loader),
	// avoiding the filesystem I/O of EvalSymlinks on hot paths like hover/definition.
	if docURI, ok := w.canonicalToURI[cleanedPath]; ok {
		return docURI
	}

	// Slow path: resolve symlinks and retry. This handles cases where input
	// is a symlink path but the map keys are resolved paths.
	if resolved, err := filepath.EvalSymlinks(rawPath); err == nil {
		canonicalPath := filepath.ToSlash(filepath.Clean(resolved))
		if canonicalPath != cleanedPath {
			if docURI, ok := w.canonicalToURI[canonicalPath]; ok {
				return docURI
			}
		}
		// File not open - return file:// URI for the canonical path
		return PathToURI(canonicalPath)
	}

	// EvalSymlinks failed (nonexistent file, broken symlink, etc.)
	// Return file:// URI for the cleaned path
	return PathToURI(cleanedPath)
}

// buildCanonicalToURIMap builds a mapping from canonical (symlink-resolved)
// paths to the URIs used by clients to open documents.
//
// This is needed because the schema loader resolves symlinks (via makeCanonicalPath),
// so diagnostics reference canonical paths. But clients identify documents by the
// URI they used to open them, which may be a symlink path. This mapping allows
// us to translate diagnostic URIs back to client-expected URIs.
//
// This method prefers Document.SourceID (set at open time via SourceIDFromAbsolutePath)
// over runtime EvalSymlinks, since SourceID is the canonical identity used by the
// loader and is already computed. This also works for new files that don't yet
// exist on disk, where EvalSymlinks would fail.
//
// Must be called with w.mu held.
func (w *Workspace) buildCanonicalToURIMap() map[string]string {
	canonicalToURI := make(map[string]string, len(w.open))
	openOrderByCanonical := make(map[string]int, len(w.open))

	for uri, doc := range w.open {
		var canonical string

		// Prefer SourceID which is already canonicalized at open time
		if !doc.SourceID.IsZero() {
			canonical = doc.SourceID.String()
		} else {
			// Fallback for non-file URIs or when SourceID unavailable
			path, err := URIToPath(uri)
			if err != nil {
				continue
			}

			// Resolve symlinks to get the canonical path (matching loader behavior).
			// filepath.EvalSymlinks also cleans the path.
			resolved, err := filepath.EvalSymlinks(path)
			if err != nil {
				// If symlink resolution fails (broken symlink, permissions, etc.),
				// fall back to the cleaned path. This ensures we still have a mapping
				// even when EvalSymlinks fails.
				canonical = filepath.Clean(path)
			} else {
				canonical = resolved
			}
			// Convert to forward slashes to match SourceID.String() format
			canonical = filepath.ToSlash(canonical)
		}

		// For determinism when multiple URIs resolve to the same canonical path,
		// prefer the document that was opened first (lowest OpenOrder).
		if existingOrder, exists := openOrderByCanonical[canonical]; !exists || doc.OpenOrder < existingOrder {
			canonicalToURI[canonical] = uri
			openOrderByCanonical[canonical] = doc.OpenOrder
		}
	}

	return canonicalToURI
}

// remapToOpenDocURI remaps a diagnostic URI to an open document URI if the
// underlying file matches an open buffer.
//
// If the diagnostic's path (after symlink resolution) matches an open document,
// returns the open document's URI. Otherwise, returns a valid file:// URI.
//
// This function tolerates both file:// URIs and raw filesystem paths as input,
// since RelatedInformation may come from various sources. It always returns
// a valid URI to maintain LSP protocol correctness.
func (w *Workspace) remapToOpenDocURI(diagURI string, canonicalToDocURI map[string]string) string {
	path, err := URIToPath(diagURI)
	if err != nil {
		// Not a valid file:// URI.
		// Check if it's a non-file URI scheme - preserve as-is.
		if hasURIScheme(diagURI) {
			return diagURI
		}
		// Raw filesystem path - use directly for lookup
		path = diagURI
	}

	// Convert to forward slashes to match map keys (SourceID.String() format).
	// On Windows, URIToPath produces backslashes but map keys use forward slashes.
	lookupPath := filepath.ToSlash(path)

	// The diagnostic URI is already using a canonical path (from the loader),
	// so we can look it up directly in the map.
	if docURI, ok := canonicalToDocURI[lookupPath]; ok {
		return docURI
	}

	// No match found.
	// If input was a raw path, convert to proper file:// URI for protocol correctness.
	// If input was already a file:// URI, return unchanged.
	if err != nil {
		return PathToURI(path)
	}
	return diagURI
}

// MarkdownDocumentOpened creates a MarkdownDocument with normalized text and version.
// Block extraction is deferred to AnalyzeMarkdownAndPublish.
func (w *Workspace) MarkdownDocumentOpened(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.markdownDocs[uri] = &MarkdownDocument{
		URI:     uri,
		Version: version,
		Text:    normalizeLineEndings(text),
	}
}

// MarkdownDocumentChanged updates text and version for a markdown document.
// Ignores stale updates (version <= current unless either is 0).
// Does NOT re-extract blocks — that is done atomically by AnalyzeMarkdownAndPublish.
func (w *Workspace) MarkdownDocumentChanged(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	md := w.markdownDocs[uri]
	if md == nil {
		return
	}

	if version != 0 && md.Version != 0 && version <= md.Version {
		w.logger.Debug("ignoring stale markdown document change",
			slog.String("uri", uri),
			slog.Int("incoming_version", version),
			slog.Int("current_version", md.Version),
		)
		return
	}
	md.Version = version
	md.Text = normalizeLineEndings(text)
}

// MarkdownDocumentClosed removes a markdown document and clears its diagnostics.
func (w *Workspace) MarkdownDocumentClosed(notify Notifier, uri string) {
	w.mu.Lock()
	delete(w.markdownDocs, uri)

	hadPublished := w.publishedByEntry[uri] != nil
	delete(w.publishedByEntry, uri)
	w.mu.Unlock()

	if hadPublished {
		w.publishDiagnostics(notify, uri, nil)
	}

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

	blocks := make([]CodeBlock, len(md.Blocks))
	copy(blocks, md.Blocks)

	snapshots := make([]*Snapshot, len(md.Snapshots))
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

// ScheduleMarkdownAnalysis schedules a debounced analysis for a markdown document.
func (w *Workspace) ScheduleMarkdownAnalysis(glspCtx *glsp.Context, uri string) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()

	if existing, ok := w.debounces[uri]; ok {
		existing.timer.Stop()
		existing.cancel()
	}

	analyzeCtx, cancel := context.WithCancel(context.Background())
	entry := &debounceEntry{cancel: cancel}

	var notify Notifier
	if glspCtx != nil {
		notify = func(method string, params any) {
			glspCtx.Notify(method, params)
		}
	}

	entry.timer = time.AfterFunc(debounceDelay, func() {
		select {
		case <-analyzeCtx.Done():
			return
		default:
			w.AnalyzeMarkdownAndPublish(notify, analyzeCtx, uri)
			w.debounceMu.Lock()
			if w.debounces[uri] == entry {
				delete(w.debounces, uri)
			}
			w.debounceMu.Unlock()
		}
	})

	w.debounces[uri] = entry
}

// AnalyzeMarkdownAndPublish analyzes a markdown document's code blocks and publishes diagnostics.
func (w *Workspace) AnalyzeMarkdownAndPublish(notify Notifier, analyzeCtx context.Context, uri string) {
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
	blocks := ExtractCodeBlocks(text)

	// Assign virtual SourceIDs
	path, err := URIToPath(uri)
	if err != nil {
		w.logger.Warn("failed to parse markdown URI", slog.String("uri", uri), slog.String("error", err.Error()))
		return
	}

	validBlocks := make([]CodeBlock, 0, len(blocks))
	for i, block := range blocks {
		id, err := VirtualSourceID(path, i)
		if err != nil {
			w.logger.Warn("failed to create virtual source ID",
				slog.String("uri", uri), slog.Int("block", i), slog.String("error", err.Error()))
			continue
		}
		block.SourceID = id
		validBlocks = append(validBlocks, block)
	}

	// Analyze each block
	snapshots := make([]*Snapshot, len(validBlocks))
	for i, block := range validBlocks {
		virtualPath := block.SourceID.String()
		overlays := map[string][]byte{
			virtualPath: []byte(block.Content),
		}
		snapshot, err := w.analyzer.Analyze(analyzeCtx, virtualPath, overlays, "", load.WithDisallowImports())
		if err != nil {
			w.logger.Warn("markdown block analysis failed",
				slog.String("uri", uri), slog.Int("block", i), slog.String("error", err.Error()))
		}
		snapshots[i] = snapshot
	}

	// Post-analysis cancellation check
	if analyzeCtx.Err() != nil {
		w.logger.Debug("markdown analysis cancelled", slog.String("uri", uri))
		return
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
		Blocks:    make([]CodeBlock, len(validBlocks)),
		Snapshots: make([]*Snapshot, len(snapshots)),
	}
	copy(snap.Blocks, validBlocks)
	copy(snap.Snapshots, snapshots)
	w.mu.Unlock()

	// Publish diagnostics (no lock held)
	w.publishMarkdownDiagnostics(notify, snap)
}

// publishMarkdownDiagnostics collects diagnostics from all block snapshots,
// remaps positions from block-local to markdown coordinates, and publishes.
func (w *Workspace) publishMarkdownDiagnostics(notify Notifier, snap *MarkdownDocumentSnapshot) {
	if notify == nil {
		return
	}

	var allDiagnostics []protocol.Diagnostic

	for i, snapshot := range snap.Snapshots {
		if snapshot == nil {
			continue
		}

		for _, uriDiag := range snapshot.LSPDiagnostics {
			diag := uriDiag.Diagnostic

			// Convert primary range from block-local to markdown coordinates
			startLine, startChar := snap.BlockPositionToMarkdown(i,
				int(diag.Range.Start.Line), int(diag.Range.Start.Character))
			endLine, endChar := snap.BlockPositionToMarkdown(i,
				int(diag.Range.End.Line), int(diag.Range.End.Character))

			diag.Range = protocol.Range{
				Start: protocol.Position{
					Line:      toUInteger(startLine),
					Character: toUInteger(startChar),
				},
				End: protocol.Position{
					Line:      toUInteger(endLine),
					Character: toUInteger(endChar),
				},
			}

			// Remap RelatedInformation URIs and ranges
			if len(diag.RelatedInformation) > 0 {
				block := snap.Blocks[i]
				expectedURI := PathToURI(block.SourceID.String())
				var remapped []protocol.DiagnosticRelatedInformation
				for _, rel := range diag.RelatedInformation {
					if rel.Location.URI != expectedURI {
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
									Line:      toUInteger(relStartLine),
									Character: toUInteger(relStartChar),
								},
								End: protocol.Position{
									Line:      toUInteger(relEndLine),
									Character: toUInteger(relEndChar),
								},
							},
						},
						Message: rel.Message,
					})
				}
				diag.RelatedInformation = remapped
			}

			allDiagnostics = append(allDiagnostics, diag)
		}
	}

	// Update publishedByEntry tracking
	w.mu.Lock()
	w.publishedByEntry[snap.URI] = map[string]struct{}{snap.URI: {}}
	w.mu.Unlock()

	if allDiagnostics == nil {
		allDiagnostics = []protocol.Diagnostic{}
	}
	notify(protocol.ServerTextDocumentPublishDiagnostics, protocol.PublishDiagnosticsParams{
		URI:         snap.URI,
		Diagnostics: allDiagnostics,
	})
}
