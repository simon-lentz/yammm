package workspace

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/simon-lentz/yammm/lsp/internal/protocol"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsperr"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

// Config holds workspace configuration.
type Config struct {
	// ModuleRoot overrides the computed module root for import resolution.
	// What it overrides, in order: the directory of the nearest ancestor
	// holding a yammm.mod marker, then the workspace folder containing the
	// file, then the file's own directory.
	ModuleRoot string

	// DebounceDelay is the delay between a document change and the analysis
	// it triggers. Zero or negative means DefaultDebounceDelay. Tests use a
	// small value (e.g., 1ms) to make temporal behavior deterministic.
	DebounceDelay time.Duration
}

// NotifyFunc is a function that sends LSP notifications.
// This type decouples notification sending from transport details.
type NotifyFunc func(method string, params any)

// Workspace manages the state of open documents and analysis results.
//
// Lock ordering: debounceMu must never be acquired while holding mu.
// Methods that need both must release mu before acquiring debounceMu.
type Workspace struct {
	// mu protects: docs, snapshots, posEncoding, deps, mapper, roots.
	mu sync.RWMutex

	logger *slog.Logger
	config Config

	// notifyFn sends LSP notifications. Set once via SetNotifier after the
	// transport is available (RunStdio or test harness setup). Nil-safe:
	// workspace operations tolerate a nil notifier (diagnostics are silently
	// dropped). Protected by notifyMu for safe concurrent read access.
	notifyMu sync.RWMutex
	notifyFn NotifyFunc

	// Workspace roots (from workspaceFolders)
	roots []string

	// docs stores open .yammm document state (overlays, snapshots, line state).
	docs docstate.Overlay

	// markdownDocs stores open markdown document state keyed by URI.
	markdownDocs map[string]*markdownDocument

	// Latest analysis snapshots keyed by entry URI
	snapshots map[string]*analysis.Snapshot

	// Position encoding negotiated with client
	posEncoding lsputil.PositionEncoding

	// deps tracks forward and reverse import dependencies between entry files.
	deps depGraph

	// sched manages debounced analysis scheduling and background context.
	// Has its own debounceMu lock (must never be acquired while holding mu).
	sched *analysisScheduler

	// mapper manages canonical path mapping and per-entry publication tracking.
	mapper uriMapper

	// diagHash holds hash-based diagnostic deduplication state.
	// Separate from mu because publishDiagnostics is called outside the
	// workspace lock (to avoid deadlock on I/O).
	diagHash diagHashState

	// analysisCompletedHook, when non-nil, runs after an analysis completes
	// and before the version gate re-checks the document — on both the
	// .yammm path (analyzeAndPublish) and the markdown path
	// (AnalyzeMarkdownAndPublish). It exists so tests can deterministically
	// interleave a document change into the window the gate protects;
	// production leaves it nil. Protected by mu; invoked outside it.
	analysisCompletedHook func(uri string)
}

// NewWorkspace creates a new workspace.
// If logger is nil, slog.Default() is used.
func NewWorkspace(logger *slog.Logger, cfg Config) *Workspace {
	if logger == nil {
		logger = slog.Default()
	}

	// ModuleRoot is expected to be already canonicalized by Config.Validate()
	// in the server package. Clean it defensively for direct construction in tests.
	if cfg.ModuleRoot != "" {
		cfg.ModuleRoot = filepath.Clean(cfg.ModuleRoot)
	}

	bgCtx, bgCancel := context.WithCancel(context.Background())

	return &Workspace{
		logger:      logger.With(slog.String("component", "workspace")),
		config:      cfg,
		roots:       make([]string, 0),
		snapshots:   make(map[string]*analysis.Snapshot),
		posEncoding: lsputil.PositionEncodingUTF16,
		diagHash: diagHashState{
			hashes: make(map[string]uint64),
		},
		docs: docstate.Overlay{
			Open: make(map[string]*docstate.Document),
		},
		markdownDocs: make(map[string]*markdownDocument),
		deps: depGraph{
			importsByEntry: make(map[string]map[string]struct{}),
			reverseDeps:    make(map[string]map[string]struct{}),
		},
		sched: newAnalysisScheduler(logger, bgCtx, bgCancel, cfg.DebounceDelay),
		mapper: uriMapper{
			publishedByEntry: make(map[string]map[string]struct{}),
		},
	}
}

// SetNotifier sets the notification function used to push diagnostics
// to the client. Called once after the transport is available. Nil-safe:
// passing nil disables notifications (useful in tests).
func (w *Workspace) SetNotifier(fn NotifyFunc) {
	w.notifyMu.Lock()
	w.notifyFn = fn
	w.notifyMu.Unlock()
}

// setAnalysisCompletedHook installs the test-only hook that runs after an
// analysis completes and before the version gate re-checks the document,
// on both the .yammm and markdown paths.
func (w *Workspace) setAnalysisCompletedHook(h func(uri string)) {
	w.mu.Lock()
	w.analysisCompletedHook = h
	w.mu.Unlock()
}

// analysisCompletedHookFn snapshots the installed hook under lock; callers
// invoke the returned hook outside the lock, since it may mutate the
// workspace.
func (w *Workspace) analysisCompletedHookFn() func(uri string) {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.analysisCompletedHook
}

// OpenDocument stores document state and triggers immediate analysis.
// Routes to yammm or markdown handling based on URI extension.
// Unsupported file types are silently ignored (debug logged).
func (w *Workspace) OpenDocument(uri string, version int, text string) {
	switch {
	case lsputil.IsYammmURI(uri):
		w.documentOpened(uri, version, text)
		w.analyzeAndPublish(w.sched.backgroundContext(), uri)
	case lsputil.IsMarkdownURI(uri):
		w.markdownDocumentOpened(uri, version, text)
		w.AnalyzeMarkdownAndPublish(w.sched.backgroundContext(), uri)
	default:
		w.logger.Debug("ignoring open for unsupported file type", slog.String("uri", uri))
	}
}

// ChangeDocument updates document text and schedules debounced analysis.
// The text parameter is the full document content (full-sync mode).
// Protocol-level content change extraction is handled at the server boundary.
func (w *Workspace) ChangeDocument(uri string, version int, text string) {
	switch {
	case lsputil.IsYammmURI(uri):
		w.documentChanged(uri, version, text)
		w.scheduleAnalysis(uri)
	case lsputil.IsMarkdownURI(uri):
		w.markdownDocumentChanged(uri, version, text)
		w.scheduleMarkdownAnalysis(uri)
	default:
		w.logger.Debug("ignoring change for unsupported file type", slog.String("uri", uri))
	}
}

// CloseDocument cleans up document state and clears diagnostics.
func (w *Workspace) CloseDocument(uri string) {
	switch {
	case lsputil.IsYammmURI(uri):
		w.documentClosed(uri)
	case lsputil.IsMarkdownURI(uri):
		w.markdownDocumentClosed(uri)
	default:
		w.logger.Debug("ignoring close for unsupported file type", slog.String("uri", uri))
	}
}

// AddRoot adds a workspace root.
func (w *Workspace) AddRoot(uri string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	path, err := lsputil.URIToPath(uri)
	if err != nil {
		w.logger.Warn(
			"failed to parse workspace root URI",
			slog.String("uri", uri),
			slog.Any("error", err),
		)
		return
	}

	canonicalPath := lsputil.CanonicalPath(path)

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

	path, err := lsputil.URIToPath(uri)
	if err != nil {
		w.logger.Warn(
			"failed to parse workspace root URI for removal",
			slog.String("uri", uri),
			slog.Any("error", err),
		)
		return
	}

	canonicalPath := lsputil.CanonicalPath(path)

	lenBefore := len(w.roots)
	w.roots = slices.DeleteFunc(w.roots, func(root string) bool {
		return root == canonicalPath
	})
	removed := len(w.roots) < lenBefore

	if removed {
		w.logger.Debug("removed workspace root", slog.String("path", canonicalPath))
	} else {
		w.logger.Debug("workspace root not found for removal", slog.String("path", canonicalPath))
	}
}

// SetPositionEncoding sets the position encoding to use.
func (w *Workspace) SetPositionEncoding(enc lsputil.PositionEncoding) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.posEncoding = enc
}

// PositionEncoding returns the negotiated position encoding.
func (w *Workspace) PositionEncoding() lsputil.PositionEncoding {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.posEncoding
}

// documentOpened handles a document being opened.
// Resolves symlinks to compute the canonical SourceID before storing.
func (w *Workspace) documentOpened(uri string, version int, text string) {
	path, err := lsputil.URIToPath(uri)
	if err != nil {
		w.logger.Warn(
			"failed to parse document URI",
			slog.String("uri", uri),
			slog.Any("error", err),
		)
		return
	}

	canonicalPath := lsputil.CanonicalPath(path)

	sourceID, err := location.SourceIDFromAbsolutePath(canonicalPath)
	if err != nil {
		w.logger.Warn(
			"failed to create source ID",
			slog.String("path", canonicalPath),
			slog.Any("error", err),
		)
		return
	}

	w.mu.Lock()
	defer w.mu.Unlock()

	w.docs.OpenDocument(uri, sourceID, version, text)

	// Invalidate canonical-to-URI cache (new document may map to a canonical path)
	w.mapper.invalidateCache()
}

// documentChanged handles a document content change.
func (w *Workspace) documentChanged(uri string, version int, text string) {
	w.mu.Lock()
	defer w.mu.Unlock()

	w.docs.ChangeDocument(uri, version, text, w.logger)
}

// documentClosed handles a document being closed.
func (w *Workspace) documentClosed(uri string) {
	w.mu.Lock()
	w.docs.RemoveDocument(uri)
	delete(w.snapshots, uri)

	w.mapper.invalidateCache()

	publishedFromEntry := w.mapper.clearEntryPublications(uri)

	urisToClear := make([]string, 0)
	for pubURI := range publishedFromEntry {
		if !w.mapper.uriStillPublishedByOthers(pubURI) {
			urisToClear = append(urisToClear, pubURI)
		}
	}

	w.deps.updateDependencies(uri, nil, w.logger)
	w.mu.Unlock()

	for _, pubURI := range urisToClear {
		w.publishDiagnostics(pubURI, nil, nil)
		w.clearDiagHash(pubURI)
	}

	w.cancelPendingAnalysis(uri)
}

// ReanalyzeOpenDocuments triggers re-analysis of all open documents.
func (w *Workspace) ReanalyzeOpenDocuments() {
	w.mu.RLock()
	uris := w.docs.AllOpenURIs()
	w.mu.RUnlock()

	for _, uri := range uris {
		w.scheduleAnalysis(uri)
	}
}

// scheduleAnalysis schedules a debounced analysis for the given document.
func (w *Workspace) scheduleAnalysis(uri string) {
	w.sched.schedule(uri, w.analyzeAndPublish)
}

// analyzeAndPublish analyzes a document and publishes diagnostics.
func (w *Workspace) analyzeAndPublish(analyzeCtx context.Context, uri string) {
	// Read Version and overlays directly from docs.Open under RLock.
	// This intentionally bypasses GetSnapshot to avoid copying LineState
	// and Text into a Snapshot that would be immediately discarded — we
	// only need the version number and the overlay map here.
	w.mu.RLock()
	doc, ok := w.docs.Open[uri]
	if !ok {
		w.mu.RUnlock()
		return
	}

	overlays := w.docs.CollectOverlays()
	entryVersion := doc.Version
	w.mu.RUnlock()

	path, err := lsputil.URIToPath(uri)
	if err != nil {
		w.logger.Warn(
			"failed to parse document URI for analysis",
			slog.String("uri", uri),
			slog.Any("error", fmt.Errorf("%w: %w", lsperr.ErrInvalidURI, err)),
		)
		return
	}

	canonicalPath := lsputil.CanonicalPath(path)

	// The root is resolved before the analyzer is called: the analyzer takes
	// a root as an argument and never discovers one. A discovery failure is a
	// document diagnostic rather than a dropped analysis — the editor says
	// what the CLI says, because schema.ModuleRootIssue builds it for both.
	var snapshot *analysis.Snapshot
	moduleRoot, rootErr := w.FindModuleRoot(canonicalPath)
	if rootErr != nil {
		result := diag.NewCollectorUnlimited()
		result.Collect(schema.ModuleRootIssue(rootErr))
		snapshot, err = w.sched.analyzer.SnapshotForResult(canonicalPath, result.Result(), w.PositionEncoding())
	} else {
		snapshot, err = w.sched.analyzer.Analyze(analyzeCtx, canonicalPath, overlays, moduleRoot, w.PositionEncoding())
	}

	if analyzeCtx.Err() != nil {
		w.logger.Debug(
			"analysis cancelled",
			slog.String("uri", uri),
			slog.Any("error", analyzeCtx.Err()),
		)
		return
	}

	if hook := w.analysisCompletedHookFn(); hook != nil {
		hook(uri)
	}

	w.mu.RLock()
	currentDoc := w.docs.Open[uri]
	isStale := currentDoc == nil || currentDoc.Version != entryVersion
	w.mu.RUnlock()

	if isStale {
		w.logger.Debug(
			"skipping stale analysis results",
			slog.String("uri", uri),
			slog.Int("entry_version", entryVersion),
		)
		return
	}

	if err != nil {
		w.logger.Error(
			"analysis failed",
			slog.String("uri", uri),
			slog.Any("error", err),
		)
		if snapshot != nil {
			snapshot.EntryVersion = entryVersion
			w.mu.Lock()
			w.snapshots[uri] = snapshot
			w.mu.Unlock()
			w.updateDeps(uri, snapshot.ImportedPaths)
			w.publishSnapshotDiagnostics(uri, snapshot)
		}
		return
	}

	snapshot.EntryVersion = entryVersion

	w.mu.Lock()
	w.snapshots[uri] = snapshot
	w.mu.Unlock()

	w.updateDeps(uri, snapshot.ImportedPaths)

	w.publishSnapshotDiagnostics(uri, snapshot)
}

// FileChanged handles a watched file change notification.
func (w *Workspace) FileChanged(uri string, changeType protocol.UInteger) {
	// A marker event moves the root for every document beneath it, and no
	// document declares a dependency on it, so the reverse-dependency map
	// below would find nothing. Handled before the source-identity conversion
	// because a marker is not a source. Nothing is stored: this is a refresh,
	// not a cache.
	if path, err := lsputil.URIToPath(uri); err == nil && filepath.Base(path) == schema.ModuleRootMarker {
		w.ReanalyzeOpenDocuments()
		return
	}

	canonicalURI := uri
	if path, err := lsputil.URIToPath(uri); err == nil {
		path = lsputil.CanonicalPath(path)
		if sourceID, err := location.SourceIDFromAbsolutePath(path); err == nil {
			canonicalURI = lsputil.PathToURI(sourceID.String())
		}
	}

	w.mu.RLock()
	deps := w.deps.reverseDependents(canonicalURI)
	w.mu.RUnlock()

	for entryURI := range deps {
		w.scheduleAnalysis(entryURI)
	}
}

// updateDeps updates the dependency tracking for an entry file.
func (w *Workspace) updateDeps(entryURI string, importedPaths []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.deps.updateDependencies(entryURI, importedPaths, w.logger)
}

// cancelPendingAnalysis cancels any pending analysis for a URI.
func (w *Workspace) cancelPendingAnalysis(uri string) {
	w.sched.cancelPending(uri)
}

// Shutdown cancels all pending analysis operations.
// Exported because Server.Close() calls this from the lsp package, and tests
// that construct Workspace directly (without Server) need cleanup access.
// Production code outside the workspace package should prefer Server.Close(),
// which calls Shutdown() as step 1 of its ordered shutdown sequence.
func (w *Workspace) Shutdown() {
	w.sched.shutdown()
}

// FindModuleRoot finds the module root for a file path, over four tiers: the
// configured root, then the directory of the nearest ancestor holding a
// module-root marker, then the nearest workspace folder containing the file,
// then the file's own directory.
//
// The marker beats the workspace folder because the marker is a file the
// author committed and the folder is wherever the editor happened to be
// opened. The folder tier survives for a workspace carrying no marker, and
// goes dead the moment one is added.
//
// An error means a marker exists and cannot be honoured. It is returned rather
// than swallowed: falling through to the folder would be the silently ignored
// marker that discovery exists to remove.
//
// This method acquires its own lock to safely read w.roots and w.config.
// Callers must NOT hold w.mu when calling this method to avoid deadlock.
func (w *Workspace) FindModuleRoot(path string) (string, error) {
	// The configured root and a copy of the folder list are read under the
	// lock; the marker walk is filesystem I/O and runs outside it, on every
	// debounced analysis.
	w.mu.RLock()
	configured := w.config.ModuleRoot
	roots := slices.Clone(w.roots)
	w.mu.RUnlock()

	if configured != "" {
		return configured, nil
	}

	// No cache: the editor cannot see a marker being created or deleted, and
	// a cached miss would be the ignored marker again. A marker event
	// re-analyzes instead (see FileChanged).
	switch root, found, err := schema.FindModuleRoot(filepath.Dir(path)); {
	case err != nil:
		return "", fmt.Errorf("discover module root for %q: %w", path, err)
	case found:
		return root, nil
	}

	var nearest string
	for _, root := range roots {
		if (path == root || strings.HasPrefix(path, root+string(filepath.Separator))) && len(root) > len(nearest) {
			nearest = root
		}
	}
	if nearest != "" {
		return nearest, nil
	}

	return filepath.Dir(path), nil
}

// LatestSnapshot returns the latest snapshot for a URI.
func (w *Workspace) LatestSnapshot(uri string) *analysis.Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.snapshots[uri]
}

// GetDocumentSnapshot returns an immutable snapshot of the document for a URI.
func (w *Workspace) GetDocumentSnapshot(uri string) *docstate.Snapshot {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.docs.GetSnapshot(uri)
}

// RemapPathToURI maps a path to the client's document URI if the file is open.
// Uses a two-phase locking strategy: tries RLock first (common case — cache
// populated), upgrades to Lock only when the cache needs rebuilding.
func (w *Workspace) RemapPathToURI(input string) string {
	// Fast path: cache already populated, read lock suffices.
	w.mu.RLock()
	if w.mapper.canonicalToURI != nil {
		result := w.mapper.remapPathToURI(input, w.docs.Open)
		w.mu.RUnlock()
		return result
	}
	w.mu.RUnlock()

	// Slow path: cache invalidated, need write lock to rebuild.
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mapper.remapPathToURI(input, w.docs.Open)
}
