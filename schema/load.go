package schema

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/text/unicode/norm"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// errorToFatalIssue converts a Go error to a Fatal diagnostic issue.
// Context errors use E_CONTEXT_CANCELLED; all others use E_LOAD_IO_FAILURE.
func errorToFatalIssue(err error) diag.Issue {
	code := diag.E_LOAD_IO_FAILURE
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		code = diag.E_CONTEXT_CANCELLED
	}
	return diag.NewIssue(diag.Fatal, code, err.Error()).Build()
}

// fatalResult creates a diag.Result containing a single Fatal diagnostic from an error,
// optionally merged with a partial result that may contain earlier diagnostics.
func fatalResult(err error, partial diag.Result) (*Schema, diag.Result) {
	c := diag.NewCollectorUnlimited()
	c.Merge(partial)
	c.Collect(errorToFatalIssue(err))
	return nil, c.Result()
}

// issueResult creates a diag.Result carrying a single non-Fatal issue and a
// nil Schema. It is the shape a load takes when it fails on user content
// before any source is parsed — a malformed module-root marker, today — where
// [fatalResult]'s Fatal severity would contradict HasFatal's promise of I/O
// or cancellation.
func issueResult(issue diag.Issue) diag.Result {
	c := diag.NewCollectorUnlimited()
	c.Collect(issue)
	return c.Result()
}

// rootLoader provides sandboxed file access for imports using os.Root.
// This uses kernel-level file access controls rather than string-based
// path validation, eliminating TOCTOU race conditions.
type rootLoader struct {
	root     *os.Root
	rootPath string // Canonical absolute path for SourceID construction
}

// newRootLoader creates a rootLoader for sandboxed import file access.
func newRootLoader(moduleRoot string) (*rootLoader, error) {
	root, err := os.OpenRoot(moduleRoot)
	if err != nil {
		return nil, fmt.Errorf("open module root %q: %w", moduleRoot, err)
	}
	// Get the canonical path for consistent SourceID construction
	canonicalRoot, err := makeCanonicalPath(moduleRoot)
	if err != nil {
		_ = root.Close() // best-effort cleanup; primary error is canonicalization failure
		return nil, fmt.Errorf("canonicalize module root %q: %w", moduleRoot, err)
	}
	return &rootLoader{root: root, rootPath: canonicalRoot}, nil
}

// openFile opens a file relative to the module root with sandboxed access.
// Returns ErrPathEscape if the path would escape the module root.
func (rl *rootLoader) openFile(relativePath string) (*os.File, error) {
	// Clean the path to normalize separators and remove . and ..
	cleanPath := filepath.Clean(relativePath)
	f, err := rl.root.Open(cleanPath)
	if err != nil {
		return nil, rl.handleOpenError(err, relativePath)
	}
	return f, nil
}

// maxSchemaSourceBytes bounds one schema source, entry or import. The largest
// real schema is tens of kilobytes; the bound stops a stray large file from
// being read whole before anything can refuse it.
const maxSchemaSourceBytes = 16 << 20

// readAtMost reads r up to limit bytes and reports whether more remained. It
// reads one byte past the limit so a source exactly at it is legal and the
// first byte beyond it is detectable without reading the rest.
func readAtMost(r io.Reader, limit int64) (content []byte, tooLong bool, err error) {
	content, err = io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, false, fmt.Errorf("read: %w", err)
	}
	if int64(len(content)) > limit {
		return nil, true, nil
	}
	return content, false, nil
}

// readSource reads one schema source up to maxSchemaSourceBytes, naming what
// it read in any error.
func readSource(r io.Reader, what string) ([]byte, error) {
	content, tooLong, err := readAtMost(r, maxSchemaSourceBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", what, err)
	}
	if tooLong {
		return nil, fmt.Errorf("%s is larger than %d bytes, the most a schema source may hold", what, maxSchemaSourceBytes)
	}
	return content, nil
}

// readSourceFile opens and reads one schema file within the source bound.
func readSourceFile(absPath string) ([]byte, error) {
	f, err := os.Open(absPath)
	if err != nil {
		return nil, fmt.Errorf("read %q: %w", absPath, err)
	}
	defer f.Close()
	return readSource(f, fmt.Sprintf("schema %q", absPath))
}

// readFile reads a file relative to the module root with sandboxed access.
// Returns ErrPathEscape if the path would escape the module root.
func (rl *rootLoader) readFile(relativePath string) ([]byte, location.SourceID, error) {
	// Stat before Open: opening a FIFO blocks until a writer appears, and
	// neither this call nor io.ReadAll takes a context to cancel it.
	if info, statErr := rl.root.Stat(filepath.Clean(relativePath)); statErr == nil && !info.Mode().IsRegular() {
		return nil, location.SourceID{}, fmt.Errorf("import %q is not a regular file", relativePath)
	}

	f, err := rl.openFile(relativePath)
	if err != nil {
		return nil, location.SourceID{}, err
	}
	defer f.Close()

	content, err := readSource(f, fmt.Sprintf("import %q", relativePath))
	if err != nil {
		return nil, location.SourceID{}, err
	}

	// Construct SourceID from the canonical path
	cleanPath := filepath.Clean(relativePath)
	absPath := filepath.Join(rl.rootPath, cleanPath)
	sourceID, err := location.SourceIDFromAbsolutePath(absPath)
	if err != nil {
		return nil, location.SourceID{}, fmt.Errorf("create source ID for %q: %w", relativePath, err)
	}

	return content, sourceID, nil
}

// handleOpenError converts os.Root errors to appropriate domain errors.
func (rl *rootLoader) handleOpenError(err error, requestedPath string) error {
	// os.Root returns "path escapes from parent" when path tries to escape the root.
	// We check for both fs.ErrInvalid and the specific error message.
	if errors.Is(err, fs.ErrInvalid) {
		return &pathEscapeError{path: requestedPath}
	}

	// Check for the specific "path escapes from parent" error message
	// that os.Root returns on escape attempts
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		if pathErr.Err != nil && strings.Contains(pathErr.Err.Error(), "escapes") {
			return &pathEscapeError{path: requestedPath}
		}
	}

	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("import file %q not found", requestedPath)
	}
	return fmt.Errorf("open import file %q: %w", requestedPath, err)
}

// Close releases the underlying os.Root handle.
func (rl *rootLoader) Close() error {
	if err := rl.root.Close(); err != nil {
		return fmt.Errorf("close module root: %w", err)
	}
	return nil
}

// pathEscapeError indicates an import path attempted to escape the module root.
type pathEscapeError struct {
	path string
}

func (e *pathEscapeError) Error() string {
	return fmt.Sprintf("import path %q escapes module root", e.path)
}

// Load loads a schema from a file path.
//
// The path must be an absolute or relative path to a .yammm file.
// Imports are resolved relative to the file's directory or the module root
// if WithModuleRoot is provided.
//
// ctx must not be nil. Passing nil will panic.
// A non-OK result with a nil Schema indicates failure. Check result.HasFatal() for I/O or cancellation errors.
func Load(ctx context.Context, path string, opts ...LoadOption) (*Schema, diag.Result) {
	if ctx == nil {
		panic("load.Load: context must not be nil")
	}

	cfg := defaultLoadConfig()
	applyLoadOptions(cfg, opts)
	if err := rejectSyntheticRoot(cfg); err != nil {
		return fatalResult(err, diag.Result{})
	}

	// Resolve the path to an absolute, symlink-resolved canonical path
	absPath, err := makeCanonicalPath(path)
	if err != nil {
		return fatalResult(fmt.Errorf("resolve path %q: %w", path, err), diag.Result{})
	}

	// Read the file content. Stat first: os.ReadFile on a FIFO blocks until a
	// writer appears, and the read takes no context to cancel it.
	if info, statErr := os.Stat(absPath); statErr == nil && !info.Mode().IsRegular() {
		return fatalResult(fmt.Errorf("schema path %q is not a regular file", path), diag.Result{})
	}
	content, err := readSourceFile(absPath)
	if err != nil {
		return fatalResult(err, diag.Result{})
	}

	// Determine module root and canonicalize for consistent path comparison.
	// This is important because source file paths are canonicalized (symlinks resolved),
	// so module root must also be canonicalized for filepath.Rel to work correctly.
	//
	// The ladder: an explicit WithModuleRoot wins outright and no marker is
	// read at all — a caller can always override a marker it did not put
	// there. Otherwise the nearest ancestor holding a yammm.mod supplies the
	// root, and failing that the entry file's own directory does.
	moduleRoot := cfg.moduleRoot
	rootOrigin := diag.ModuleRootExplicit
	if moduleRoot == "" {
		entryDir := filepath.Dir(absPath)
		discovered, found, err := FindModuleRoot(entryDir)
		switch {
		case err != nil:
			// A marker the author wrote and the loader could not honour fails
			// the load. Falling through to the entry directory would be the
			// silently-ignored marker this mechanism exists to remove.
			return nil, issueResult(ModuleRootIssue(err))
		case found:
			moduleRoot, rootOrigin = discovered, diag.ModuleRootDiscovered
		default:
			moduleRoot, rootOrigin = entryDir, diag.ModuleRootDefault
		}
	} else {
		var err error
		moduleRoot, err = makeCanonicalPath(moduleRoot)
		if err != nil {
			return fatalResult(fmt.Errorf("invalid module root %q: %w", cfg.moduleRoot, err), diag.Result{})
		}
	}

	// Create loader and load the schema
	ldr := newLoader(cfg, moduleRoot, "", rootOrigin)
	defer ldr.Close() // Release rootLoader resources when done

	s, result, err := ldr.loadFile(ctx, absPath, content)
	if err != nil {
		return fatalResult(err, result)
	}
	if s == nil {
		return nil, result
	}

	return s, result
}

// LoadString loads a schema from a string source.
//
// The sourceName is used as the display path in diagnostics. For
// consistent error messages, use a meaningful path-like name.
//
// Imports are not supported when loading from a string.
// The loader always disallows imports for string sources, regardless of other options.
// The rejection (a single E_IMPORT_NOT_ALLOWED at the first import
// declaration) does not suppress the source's other diagnostics: analysis
// continues with the rejected aliases deferred, and the rejected imports
// are never probed or resolved.
//
// ctx must not be nil. Passing nil will panic.
// A non-OK result with a nil Schema indicates failure. Check result.HasFatal() for I/O or cancellation errors.
func LoadString(ctx context.Context, sourceCode, sourceName string, opts ...LoadOption) (*Schema, diag.Result) {
	if ctx == nil {
		panic("load.String: context must not be nil")
	}

	cfg := defaultLoadConfig()
	applyLoadOptions(cfg, opts)
	if err := rejectSyntheticRoot(cfg); err != nil {
		return fatalResult(err, diag.Result{})
	}
	cfg.disallowImports = true // Always disallow imports from string, even if user opts try to enable

	// Create a synthetic source ID (NewSourceID returns just SourceID, no error)
	sourceID := location.NewSourceID("string://" + sourceName)

	// Create loader and load from string. A string source has no directory,
	// so it has no root to default to — the origin is "none", not "default".
	ldr := newLoader(cfg, "", "", diag.ModuleRootNone)
	s, result, err := ldr.loadSource(ctx, sourceID, []byte(sourceCode))
	if err != nil {
		return fatalResult(err, result)
	}
	return s, result
}

// LoadSourcesWithEntry loads a schema from in-memory sources with an explicit entry point.
//
// The sources map keys are paths relative to moduleRoot, and values are
// the file contents. The provided entryPath selects the entry point; an empty
// entryPath falls back to sorted key order.
//
// This is useful when the caller knows which file should be the entry point,
// particularly in LSP scenarios where multiple documents may be open but only
// one is being analyzed.
//
// The entryPath must exist in the sources map (as either an absolute path
// or relative to moduleRoot). Under [WithSyntheticRoot] an absolute key is an
// error, so the entry must be given in its relative form there. If entryPath is
// empty, the lexicographically smallest key is selected.
//
// ctx must not be nil. Passing nil will panic.
// A non-OK result with a nil Schema indicates failure. Check result.HasFatal() for I/O or cancellation errors.
func LoadSourcesWithEntry(ctx context.Context, sources map[string][]byte, entryPath string, moduleRoot string, opts ...LoadOption) (*Schema, diag.Result) {
	if ctx == nil {
		panic("load.SourcesWithEntry: context must not be nil")
	}

	if len(sources) == 0 {
		return fatalResult(errors.New("no sources provided"), diag.Result{})
	}

	cfg := defaultLoadConfig()
	applyLoadOptions(cfg, opts)

	syntheticRoot, err := normalizeSyntheticRoot(cfg, moduleRoot)
	if err != nil {
		return fatalResult(err, diag.Result{})
	}

	// Canonicalize moduleRoot to absolute path if provided.
	// This ensures SourceIDFromAbsolutePath will work correctly.
	if moduleRoot != "" {
		canonical, err := makeCanonicalPath(moduleRoot)
		if err != nil {
			return fatalResult(fmt.Errorf("invalid module root %q: %w", moduleRoot, err), diag.Result{})
		}
		moduleRoot = canonical
	}

	// Create loader. The moduleRoot argument is "explicit" whatever produced
	// it: a root the caller discovered before calling arrives here as a plain
	// argument, and the loader has no way to know its provenance.
	rootOrigin := diag.ModuleRootNone
	switch {
	case syntheticRoot != "":
		rootOrigin = diag.ModuleRootSynthetic
	case moduleRoot != "":
		rootOrigin = diag.ModuleRootExplicit
	}
	ldr := newLoader(cfg, moduleRoot, syntheticRoot, rootOrigin)
	defer ldr.Close() // Release rootLoader resources when done

	// Pre-register all sources. SourceIDs are derived textually (no symlink
	// resolution of the joined path) so import lookups — which join the same
	// module root with the same keys — land on identical IDs regardless of
	// what exists on disk.
	for path, content := range sources {
		sourceID, err := inMemorySourceID(syntheticRoot, moduleRoot, path)
		if err != nil {
			return fatalResult(fmt.Errorf("invalid path %q: %w", path, err), diag.Result{})
		}

		if err := ldr.sourceRegistry.Register(sourceID, content); err != nil {
			return fatalResult(fmt.Errorf("register source %q: %w", path, err), diag.Result{})
		}

		ldr.sourceContent[sourceID] = content
	}

	// Determine the entry point
	var selectedEntry string
	if entryPath != "" {
		// Use the provided entry path
		selectedEntry = entryPath
	} else {
		// Fall back to lexicographic selection. A found flag, not the empty
		// string, marks "nothing chosen yet": an empty key is a legal entry.
		var found bool
		for path := range sources {
			if !found || path < selectedEntry {
				selectedEntry, found = path, true
			}
		}
	}

	// Derive the entry SourceID exactly like pre-registration did, so the
	// entry lookup hits the just-registered content.
	sourceID, err := inMemorySourceID(syntheticRoot, moduleRoot, selectedEntry)
	if err != nil {
		return fatalResult(fmt.Errorf("invalid entry path %q: %w", selectedEntry, err), diag.Result{})
	}

	content, ok := ldr.sourceContent[sourceID]
	if !ok {
		return fatalResult(fmt.Errorf("entry path %q not found in sources", selectedEntry), diag.Result{})
	}

	s, result, err := ldr.loadSource(ctx, sourceID, content)
	if err != nil {
		return fatalResult(err, result)
	}
	if s == nil {
		return nil, result
	}

	return s, result
}

// importBinding records one import alias's resolution within a single schema
// load: a success carries the resolved sourceID and loaded schema; a failure
// has failed=true (unresolvable path, escape, registration failure, or a
// nested compile failure). A compile failure keeps its sourceID — the file
// resolved, then failed to compile — so validateResolvedImports can still
// detect two aliases that name the same file; a path-resolution failure has a
// zero sourceID.
type importBinding struct {
	decl     *importDecl // original declaration for diagnostics
	failed   bool
	sourceID location.SourceID
	schema   *Schema // resolved schema; nil for failures
}

// loader handles the schema loading process.
type loader struct {
	cfg             *loadConfig
	moduleRoot      string
	rootOrigin      string      // where moduleRoot came from; one of diag's ModuleRoot* origin values
	syntheticRoot   string      // normalized WithSyntheticRoot value; empty for every other load
	rootLoader      *rootLoader // sandboxed file access for imports (nil if no moduleRoot)
	registry        *Registry
	sourceRegistry  *source.Registry
	collector       *diag.Collector
	logger          *slog.Logger
	disallowImports bool

	// Tracking state
	mu             sync.Mutex
	sourceContent  map[location.SourceID][]byte
	loadedSchemas  map[location.SourceID]*Schema
	loadingSchemas map[location.SourceID]bool     // For cycle detection
	imports        map[string]importBinding       // alias -> binding (resolved or failed) for current schema
	failedCompiles map[location.SourceID]struct{} // sources whose nested compile failed (memo, per Load call)
	closureSeen    map[*Schema]struct{}           // schemas whose cached closures are already registered
}

// registryAdapter adapts *Registry to the completionRegistry interface.
type registryAdapter struct {
	r *Registry
}

// LookupBySourceID implements the completionRegistry interface.
func (a *registryAdapter) LookupBySourceID(id location.SourceID) (*Schema, bool) {
	s, ok := a.r.LookupBySourceID(id)
	return s, ok
}

// newLoader creates a new loader with the given configuration.
func newLoader(cfg *loadConfig, moduleRoot, syntheticRoot, rootOrigin string) *loader {
	registry := cfg.registry
	if registry == nil {
		registry = NewRegistry()
	}

	sourceReg := cfg.sourceRegistry
	if sourceReg == nil {
		sourceReg = source.NewRegistry()
	}

	// Use provided logger or create a discard logger (zero overhead when unused)
	logger := cfg.logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}

	return &loader{
		cfg:             cfg,
		moduleRoot:      moduleRoot,
		rootOrigin:      rootOrigin,
		syntheticRoot:   syntheticRoot,
		registry:        registry,
		sourceRegistry:  sourceReg,
		collector:       diag.NewCollector(cfg.issueLimit),
		logger:          logger,
		disallowImports: cfg.disallowImports,
		sourceContent:   make(map[location.SourceID][]byte),
		loadedSchemas:   make(map[location.SourceID]*Schema),
		loadingSchemas:  make(map[location.SourceID]bool),
		imports:         make(map[string]importBinding),
		failedCompiles:  make(map[location.SourceID]struct{}),
		closureSeen:     make(map[*Schema]struct{}),
	}
}

// hasImportRoot reports whether the load carries a root that module-style
// import paths resolve against: a filesystem module root, or a synthetic root
// standing in for one. Both import guards read this predicate, so they cannot
// disagree about what counts as having a root.
func (l *loader) hasImportRoot() bool {
	return l.moduleRoot != "" || l.syntheticRoot != ""
}

// ensureRootLoader creates the rootLoader if not already created.
// This is called lazily when imports are loaded from the filesystem.
func (l *loader) ensureRootLoader() error {
	if l.rootLoader != nil {
		return nil
	}
	if l.moduleRoot == "" {
		return nil // No module root means no sandboxing needed
	}
	rl, err := newRootLoader(l.moduleRoot)
	if err != nil {
		return err
	}
	l.rootLoader = rl
	return nil
}

// Close releases any resources held by the loader.
func (l *loader) Close() error {
	if l.rootLoader != nil {
		return l.rootLoader.Close()
	}
	return nil
}

// loadFile loads a schema from a file path.
func (l *loader) loadFile(ctx context.Context, absPath string, content []byte) (*Schema, diag.Result, error) {
	sourceID, err := location.SourceIDFromAbsolutePath(absPath)
	if err != nil {
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
			fmt.Sprintf("invalid source path %q: %v", absPath, err)).Build())
		return nil, l.collector.Result(), nil
	}

	// Register the source content
	if err := l.sourceRegistry.Register(sourceID, content); err != nil {
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
			fmt.Sprintf("register source %q: %v", absPath, err)).Build())
		return nil, l.collector.Result(), nil
	}

	l.sourceContent[sourceID] = content

	return l.loadSource(ctx, sourceID, content)
}

// loadSource loads a schema from source content.
func (l *loader) loadSource(ctx context.Context, sourceID location.SourceID, content []byte) (*Schema, diag.Result, error) {
	// Check if already loaded
	l.mu.Lock()
	if s, ok := l.loadedSchemas[sourceID]; ok {
		l.mu.Unlock()
		return s, l.collector.Result(), nil
	}

	// Check for cycle
	if l.loadingSchemas[sourceID] {
		l.mu.Unlock()
		root, origin := l.loaderRoot()
		l.collector.Collect(importCycleIssue(root, origin, sourceID))
		return nil, l.collector.Result(), nil
	}

	l.loadingSchemas[sourceID] = true
	l.mu.Unlock()

	// Ensure loadingSchemas marker is always cleaned up on exit.
	// This prevents persistent false "import cycle" errors after any failure.
	defer func() {
		l.mu.Lock()
		delete(l.loadingSchemas, sourceID)
		l.mu.Unlock()
	}()

	// Check for cancellation before starting expensive work (/19)
	// Per, cancellation is returned as error, not collected as diagnostic.
	if err := ctx.Err(); err != nil {
		return nil, l.collector.Result(), fmt.Errorf("load cancelled: %w", err)
	}

	// The collector is shared across the whole Load call, so "any errors
	// collected" conflates this schema's failures with failures collected
	// earlier (a sibling import that failed before this one was attempted).
	// Snapshot the error count at entry: this schema is judged on the
	// errors IT contributes — its own parse/completion findings and those
	// of imports loaded on its behalf — so a clean import loaded after a
	// broken sibling still compiles, registers, and draws no false
	// E_UPSTREAM_FAIL. For the entry schema the snapshot is zero, which
	// preserves the public all-or-nothing contract exactly.
	gate := newErrorGate(l.collector)

	// Save the parent's per-schema import bindings and create a fresh map for
	// this invocation. This stack-based approach gives each schema load its own
	// isolated resolution state while preserving the parent's across recursive
	// calls — a nested load leaking its aliases into the parent would corrupt
	// the parent's bindings.
	parentImports := l.imports
	l.imports = make(map[string]importBinding)
	defer func() {
		l.imports = parentImports
	}()

	l.logger.Debug("loading schema", "source", sourceID.String())

	// Register source content if not already registered
	if _, ok := l.sourceContent[sourceID]; !ok {
		if err := l.sourceRegistry.Register(sourceID, content); err != nil {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
				fmt.Sprintf("register source %s: %v", sourceID, err)).Build())
			return nil, l.collector.Result(), nil
		}
		l.sourceContent[sourceID] = content
	}

	// Parse the schema
	parser := newParser(sourceID, l.collector)
	m := parser.Parse(content)

	if m == nil {
		return nil, l.collector.Result(), nil
	}

	// When the load context structurally disallows imports, the rejection
	// is collected once (at the first declaration) and every declared alias
	// is marked failed: categorically rejected imports are never probed —
	// no resolution, no read, no E_IMPORT_RESOLVE noise — but analysis
	// still proceeds so the source's other findings surface alongside the
	// rejection. Duplicate aliases collapse to one failed binding
	// (keep-first); the completer's duplicate-alias check reports the
	// extra declarations. Otherwise imports load normally; content
	// failures are collected at their declarations and analysis continues
	// with the failed aliases deferred. Either way the error-delta gate
	// below keeps the returned schema nil whenever anything failed.
	if l.rejectDisallowedImports(m) {
		l.markUnboundImportsFailed(m.Imports)
	} else if err := l.loadImports(ctx, sourceID, m); err != nil {
		return nil, l.collector.Result(), err // propagate cancellation
	}

	// Build the completion map from the loader's bindings: a successful import
	// carries its resolved SourceID; a failed one becomes a deferred entry,
	// telling the completer "the loader saw and reported this alias" so
	// references through it defer rather than re-blaming. An alias absent from
	// the completion map was never bound — a loader bug, which the completer
	// reports as such.
	resolvedImports := make(resolvedImportMap, len(l.imports))
	for alias, binding := range l.imports {
		if binding.failed {
			resolvedImports[alias] = importResolution{deferred: true}
		} else {
			resolvedImports[alias] = importResolution{sourceID: binding.sourceID}
		}
	}

	// Complete the schema (resolve types, validate, etc.)
	s := completeModel(m, sourceID, l.collector, &registryAdapter{l.registry}, resolvedImports)

	if s == nil {
		return nil, l.collector.Result(), nil
	}

	// Wire resolved schema references (SourceID already set during completion).
	// The Import's own ResolvedSourceID is the single authority for whether to
	// wire — exactly as [Builder.wireImports] uses it. A completer-resolved
	// import (non-zero SourceID) carries a schema; a deferred import — a failed
	// load, or an alias the loader resolved but the completer then rejected (e.g.
	// colliding with a local name) — has a zero SourceID and stays schema-less.
	// Wiring the loader's still-resolved schema onto a deferred Import would give
	// it a non-nil Schema contradicting its zero ResolvedSourceID, so the
	// zero-SourceID check — not the loader binding's failed flag — is what gates
	// this (the binding is then known non-failed, so its schema is real).
	//
	// The schema pointer comes from the loader binding rather than a
	// registry-by-SourceID lookup (the shape Builder.wireImports uses): the
	// binding is the loader's authoritative within-load record, whereas s and its
	// freshly-loaded imports are not registered until after this loop (see
	// l.registry.Register below), so a registry lookup here would couple wiring to
	// registration order.
	for _, imp := range s.ImportsSlice() {
		if imp.ResolvedSourceID().IsZero() {
			continue
		}
		if binding, ok := l.imports[imp.Alias()]; ok {
			imp.setSchema(binding.schema)
		}
	}

	// Seal all imports to prevent further mutation
	for _, imp := range s.ImportsSlice() {
		imp.seal()
	}

	// Schema must be nil if this schema's load contributed any errors —
	// its own findings or those of imports loaded on its behalf (the
	// error-delta against the entry snapshot; errors collected before this
	// schema began belong to siblings and do not poison it).
	// Check BEFORE registration to avoid registering schemas we'll discard.
	if gate.tripped() {
		return nil, l.collector.Result(), nil
	}

	// Attach sources for diagnostics rendering and record the load's
	// module root (the basis for module-root-relative source keys, e.g.
	// gogen's embedded SerializedModel).
	//
	// A synthetic root IS the root this load resolved module-style imports
	// against — hasImportRoot already treats the two as one — so it is what
	// the accessor reports. Recording "" here would deny the root the loader
	// used and leave every module-root-relative key deriving from a fallback.
	s.setSources(NewSources(l.sourceRegistry))
	if l.syntheticRoot != "" {
		s.setModuleRoot(l.syntheticRoot)
	} else {
		s.setModuleRoot(l.moduleRoot)
	}

	// Seal the schema to prevent further mutation
	s.seal()

	l.logger.Debug("schema loaded",
		"source", sourceID.String(),
		"name", s.Name(),
		"types", len(s.TypesSlice()),
		"imports", len(s.ImportsSlice()))

	// Register the schema
	if err := l.registry.Register(s); err != nil {
		l.collector.Collect(l.registerFailureIssue(s, err))
		return nil, l.collector.Result(), nil
	}

	// Two loads sharing a registry can both complete one import in the window
	// between the cache miss and this call; Register keeps the first and
	// returns nil for an identical second. Adopt the registered pointer so
	// every reader of this load sees one object per SourceID.
	if canonical, ok := l.registry.LookupBySourceID(sourceID); ok {
		s = canonical
	}

	l.mu.Lock()
	l.loadedSchemas[sourceID] = s
	l.mu.Unlock()

	return s, l.collector.Result(), nil
}

// registerFailureIssue renders a Registry.Register refusal at the schema
// declaration it is about, naming the source that already holds the name so a
// reader can find the other half of the clash. Only DuplicateName is an
// authoring mistake — a registry holds one schema per name, whichever loads
// registered them — and it draws E_DUPLICATE_SCHEMA; the other kinds mean the
// loader built something it should not have.
func (l *loader) registerFailureIssue(s *Schema, err error) diag.Issue {
	regErr, ok := errors.AsType[*RegistryError](err)
	if !ok {
		return diag.NewIssue(diag.Error, diag.E_INTERNAL,
			"register schema: "+err.Error()).WithSpan(s.Span()).Build()
	}

	switch regErr.Kind {
	case DuplicateName:
		existing, found := l.registry.LookupByName(s.Name())
		held := "another source in this closure"
		if found {
			held = existing.SourceID().String()
		}
		b := diag.NewIssue(diag.Error, diag.E_DUPLICATE_SCHEMA,
			fmt.Sprintf("schema name %q is already registered by %s; a registry holds "+
				"one schema per name", s.Name(), held)).
			WithSpan(s.Span()).
			WithDetail(diag.DetailKeySchemaName, s.Name())
		if found {
			b = b.WithRelated(location.RelatedInfo{
				Span:    existing.Span(),
				Message: fmt.Sprintf("%q is declared here", existing.Name()),
			})
		}
		return b.Build()
	case DuplicateSourceID:
		return diag.NewIssue(diag.Error, diag.E_INTERNAL,
			"one source registered twice with divergent content: "+regErr.Message).
			WithSpan(s.Span()).Build()
	case InvalidSourceID, InvalidName:
		return diag.NewIssue(diag.Error, diag.E_INTERNAL,
			"the loader built an unregisterable schema: "+regErr.Message).
			WithSpan(s.Span()).Build()
	default:
		return diag.NewIssue(diag.Error, diag.E_INTERNAL,
			"register schema: "+regErr.Message).WithSpan(s.Span()).Build()
	}
}

// rejectDisallowedImports reports whether the load context structurally
// disallows imports (string sources, isolated analysis blocks) and the source
// declares any: it collects the single E_IMPORT_NOT_ALLOWED rejection and
// returns true, so the caller marks the declared aliases deferred and skips
// loading. It returns false when imports may load normally. All other
// declaration-level validation — duplicate aliases, reserved keywords,
// collisions with local names — is owned by the completer, the one layer both
// front doors (parsed sources and the Builder) share; resolution-level
// validation (duplicate resolved SourceIDs) is owned by the resolver, in
// validateResolvedImports.
func (l *loader) rejectDisallowedImports(m *model) bool {
	if l.disallowImports && len(m.Imports) > 0 {
		// Per spec: single E_IMPORT_NOT_ALLOWED issue with import_count detail,
		// positioned at the first import declaration.
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_NOT_ALLOWED,
			"import declarations are not allowed in this context").
			WithSpan(m.Imports[0].Span).
			WithDetail(diag.DetailKeyImportCount, strconv.Itoa(len(m.Imports))).Build())
		return true
	}
	return false
}

// loadImports loads all imported schemas. Content failures (unresolvable
// paths, escapes, failed compiles) are collected as diagnostics at their
// declarations and recorded per-alias — every independent failure is
// reported in one pass — so the error return carries only cancellation.
func (l *loader) loadImports(ctx context.Context, sourceID location.SourceID, m *model) error {
	if !l.hasImportRoot() && len(m.Imports) > 0 {
		// Without a root, we can only resolve relative imports
		// from file-based sources
		if !sourceID.IsFilePath() {
			root, origin := l.loaderRoot()
			l.collector.Collect(importResolveIssue(root, origin,
				"cannot resolve imports without a module root", nil))
			// Nothing can be probed; mark every alias failed so completion
			// defers references through them instead of re-reporting the
			// same root cause per declaration.
			l.markUnboundImportsFailed(m.Imports)
			return nil
		}
	}

	for _, imp := range m.Imports {
		// Check for cancellation before each import (/19)
		// Per, cancellation is returned as error, not collected as diagnostic.
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("load cancelled: %w", err)
		}
		// An alias binds once (keep-first): a later declaration of an
		// already-bound alias is skipped entirely — not loaded, not resolved,
		// not tracked. The completer's duplicate-alias diagnostic is the sole
		// report for it. A broken path on a skipped declaration is
		// intentionally not probed: its root cause is the duplicate alias,
		// not the path.
		if l.aliasBound(imp.Alias) {
			continue
		}
		if err := l.loadImport(ctx, sourceID, imp); err != nil {
			return err // propagate cancellation
		}
	}

	// Check for duplicate imports by resolved SourceID (not raw path):
	// two different import paths may resolve to the same canonical file.
	// Runs even when some imports failed — it validates the ones that did
	// resolve.
	l.validateResolvedImports(m.Imports)

	return nil
}

// aliasBound reports whether an import alias already has a binding (resolved or
// failed) for the current schema. loadImport routes each attempted declaration
// into exactly one binding, and aliasBound gates re-attempts of an already-bound
// alias before they reach loadImport (keep-first).
func (l *loader) aliasBound(alias string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.imports[alias]
	return ok
}

// markImportFailed records a declaration whose import failed before resolving
// to a file (unresolvable path, escape, or registration failure) — so it has no
// SourceID. The alias stays bound, keeping later declarations of the same alias
// inert and surfacing to the completer as a deferred entry. A compile failure,
// which does have a resolved SourceID, uses [loader.reportFailedCompile].
func (l *loader) markImportFailed(imp *importDecl) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.markImportFailedLocked(imp)
}

// markImportFailedLocked records imp's alias as a failed (no-SourceID) binding.
// Caller must hold l.mu; see [loader.markImportFailed] for the contract.
func (l *loader) markImportFailedLocked(imp *importDecl) {
	l.imports[imp.Alias] = importBinding{decl: imp, failed: true}
}

// markUnboundImportsFailed marks every not-yet-bound alias failed (keep-first:
// an already-bound alias is left as-is). Used when imports are categorically
// rejected (disallowed) or unresolvable as a group (no module root), so the
// completer defers references through them instead of re-reporting per site.
// The check-and-set runs under a single lock for the whole batch rather than a
// lock cycle per import.
func (l *loader) markUnboundImportsFailed(imports []*importDecl) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, imp := range imports {
		if _, bound := l.imports[imp.Alias]; !bound {
			l.markImportFailedLocked(imp)
		}
	}
}

// reportFailedCompile records an alias as failed-with-SourceID and collects the
// importer's own E_UPSTREAM_FAIL at its declaration: each importer of a broken
// source reports its own failure, while the broken source's own diagnostics
// were collected once (by the first attempt — see the failedCompiles memo).
// The retained sourceID lets validateResolvedImports still see that two aliases
// name the same (broken) file. The caller must NOT hold l.mu.
func (l *loader) reportFailedCompile(imp *importDecl, sourceID location.SourceID) {
	l.mu.Lock()
	l.imports[imp.Alias] = importBinding{decl: imp, failed: true, sourceID: sourceID}
	l.mu.Unlock()
	l.collector.Collect(diag.NewIssue(diag.Error, diag.E_UPSTREAM_FAIL,
		fmt.Sprintf("import %q failed to compile", imp.Path)).
		WithSpan(imp.Span).
		WithDetail(diag.DetailKeyImportPath, imp.Path).
		WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
}

// validateResolvedImports checks for two declarations that resolve to the same
// canonical file. Both successful imports and compile-failed ones participate
// (a compile failure retains its resolved SourceID), so a file imported twice
// is reported as a duplicate even when that file is itself broken; only a
// path-resolution failure (no SourceID) is skipped. Declarations are visited in
// source order — the alias-keyed binding map is consulted, never iterated — so
// the blamed declaration, its span, and the first/duplicate detail pair are
// deterministically the later declaration's, independent of map iteration
// order. All duplicates are reported; findings are diagnostics, so there is
// nothing to return.
func (l *loader) validateResolvedImports(imports []*importDecl) {
	l.mu.Lock()
	defer l.mu.Unlock()

	seenSourceIDs := make(map[location.SourceID]importBinding)
	for _, imp := range imports {
		binding, found := l.imports[imp.Alias]
		if !found || binding.decl != imp || binding.sourceID.IsZero() {
			// Not bound to this declaration (a duplicate alias's later
			// declaration maps to the kept first binding — the completer's
			// duplicate-alias check is its report), or a path-resolution
			// failure with no SourceID to compare.
			continue
		}
		if existing, dup := seenSourceIDs[binding.sourceID]; dup {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
				fmt.Sprintf("schema %q imported multiple times", binding.sourceID.String())).
				WithSpan(binding.decl.Span).
				WithDetail(diag.DetailKeyImportPath, binding.sourceID.String()).
				WithDetail(diag.DetailKeyFirstAlias, existing.decl.Alias).
				WithDetail(diag.DetailKeyFirstLine, strconv.Itoa(existing.decl.Span.Start.Line)).
				WithDetail(diag.DetailKeyDuplicateAlias, binding.decl.Alias).
				WithDetail(diag.DetailKeyDuplicateLine, strconv.Itoa(binding.decl.Span.Start.Line)).
				WithRelated(location.RelatedInfo{
					Span:    existing.decl.Span,
					Message: fmt.Sprintf("first imported here as %q", existing.decl.Alias),
				}).Build())
			continue
		}
		seenSourceIDs[binding.sourceID] = binding
	}
}

// bindIfKnown binds imp to an already-known schema for sourceID without reading
// or compiling it: a prior compile failure in this load, a within-Load cache hit
// (l.loadedSchemas), or a shared-registry hit. It returns true when it handled
// imp — bound, or (for a prior compile failure) reported imp's own
// E_UPSTREAM_FAIL — and false when sourceID is not yet known, leaving imp for the
// caller to read and load.
//
// The failed-compile memo is why a diamond over a broken import does not
// re-parse it per importer: the broken source's own diagnostics are collected
// once (by the first attempt that recursively loaded it), while every importer
// still reports its own E_UPSTREAM_FAIL here.
//
// Acquires l.mu internally; the caller must NOT hold it.
func (l *loader) bindIfKnown(imp *importDecl, sourceID location.SourceID) bool {
	l.mu.Lock()
	if _, failed := l.failedCompiles[sourceID]; failed {
		l.mu.Unlock() // reportFailedCompile takes l.mu itself
		l.reportFailedCompile(imp, sourceID)
		return true
	}
	loaded, ok := l.loadedSchemas[sourceID]
	if !ok {
		loaded, ok = l.registry.LookupBySourceID(sourceID)
		if !ok {
			l.mu.Unlock()
			return false
		}
		l.loadedSchemas[sourceID] = loaded
	}
	l.registerCachedClosureSources(loaded)
	l.imports[imp.Alias] = importBinding{sourceID: sourceID, schema: loaded, decl: imp}
	l.mu.Unlock()
	return true
}

// loadImport loads a single imported schema. A content failure is
// collected at the declaration and the alias recorded as failed; the
// error return carries only cancellation.
func (l *loader) loadImport(ctx context.Context, sourceID location.SourceID, imp *importDecl) error {
	l.logger.Debug("loading import", "path", imp.Path, "alias", imp.Alias)

	// Resolve the import path to a relative path (relative to module root)
	relativePath, err := l.resolveImportToRelative(sourceID, imp.Path)
	if err != nil {
		root, origin := l.loaderRoot()
		l.collector.Collect(importResolveIssue(root, origin,
			fmt.Sprintf("cannot resolve import %q: %v", imp.Path, err), imp))
		l.markImportFailed(imp)
		return nil
	}

	// Cross-Load short-circuit: derive the candidate SourceIDs without
	// reading the import file, then check the within-Load cache and the
	// shared Registry. If either holds the schema, reuse the existing pointer
	// and skip the read + parse + compile + register pipeline entirely.
	// ensureRootLoader canonicalizes l.moduleRoot into l.rootLoader.rootPath
	// without touching the import file itself; any error is absorbed here
	// and re-surfaced uniformly by readImportFile on the fall-through path.
	// A sources-only load never reads from disk, so the module-root sandbox
	// is never opened for it.
	if !l.cfg.sourcesOnly {
		_ = l.ensureRootLoader() //nolint:errcheck // readImportFile re-runs ensureRootLoader and surfaces the error uniformly
	}
	for _, cand := range l.candidateImportSourceIDs(relativePath) {
		if l.bindIfKnown(imp, cand) {
			return nil
		}
	}

	// Read the import using rootLoader (sandboxed) or in-memory sources
	content, importSourceID, err := l.readImportFile(relativePath, imp)
	if err != nil {
		root, origin := l.loaderRoot()
		if _, ok := errors.AsType[*pathEscapeError](err); ok { //nolint:errcheck // type check only, value unused
			l.collector.Collect(pathEscapeIssue(root, origin, imp))
			l.markImportFailed(imp)
			return nil
		}
		l.collector.Collect(importResolveIssue(root, origin,
			fmt.Sprintf("cannot read import %q: %v", imp.Path, err), imp))
		l.markImportFailed(imp)
		return nil
	}

	// Post-read within-Load belt-and-braces check. The pre-read candidate
	// SourceIDs above cover the typical path-resolution cases, but retaining
	// this check preserves the existing within-Load cache invariant (and
	// survives any future path-resolution edge case that the candidate
	// derivation above doesn't anticipate). The failed-compile memo gets the
	// same treatment for the same reason.
	if l.bindIfKnown(imp, importSourceID) {
		return nil
	}

	// Register the source if not already registered
	if _, exists := l.sourceContent[importSourceID]; !exists {
		if err := l.sourceRegistry.Register(importSourceID, content); err != nil {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
				fmt.Sprintf("register import source: %v", err)).Build())
			l.markImportFailed(imp)
			return nil
		}
		l.sourceContent[importSourceID] = content
	}

	// Recursively load the imported schema
	s, _, err := l.loadSource(ctx, importSourceID, content)
	if err != nil {
		return err // propagate cancellation
	}
	if s == nil {
		l.mu.Lock()
		l.failedCompiles[importSourceID] = struct{}{}
		l.mu.Unlock()
		l.reportFailedCompile(imp, importSourceID)
		return nil
	}

	// Store the resolved import information for later wiring to the schema's Import objects
	l.mu.Lock()
	l.imports[imp.Alias] = importBinding{
		sourceID: importSourceID,
		schema:   s,
		decl:     imp,
	}
	l.mu.Unlock()

	return nil
}

// resolveImportToRelative resolves an import path to a path relative to the module root.
func (l *loader) resolveImportToRelative(sourceID location.SourceID, importPath string) (string, error) {
	// Relative import (./foo or ../bar)
	if strings.HasPrefix(importPath, "./") || strings.HasPrefix(importPath, "../") {
		// Get the source file's directory
		cp, ok := sourceID.CanonicalPath()
		if !ok {
			return "", errors.New("relative imports require a file-based source")
		}
		sourceDir := filepath.Dir(cp.String())

		// Compute the target path
		targetPath := filepath.Join(sourceDir, importPath)

		// Make it relative to module root
		if l.moduleRoot != "" {
			rel, err := filepath.Rel(l.moduleRoot, targetPath)
			if err != nil {
				return "", fmt.Errorf("compute relative path: %w", err)
			}
			return rel, nil
		}

		// No module root - return absolute path for legacy compatibility
		return targetPath, nil
	}

	// Module-style import (just a path like "common/types"). A synthetic root
	// stands in for the module root here; the relative branch above cannot,
	// because it needs the importing source's canonical path.
	if !l.hasImportRoot() {
		return "", errors.New("module-style imports require a module root")
	}

	return importPath, nil
}

// importCandidates returns the file names an import path may resolve to.
//
// An import written without the extension resolves ONLY to "<path>.yammm". The
// bare path was a second candidate, so `import "./parts"` would compile a file
// literally named "parts" of any content type — against the extension rule
// docs/SPEC.md states. One function serves both derivation sites because they
// must agree: readImportFile does the read, candidateImportSourceIDs derives
// the identities the cross-Load short-circuit looks up, and a short-circuit that
// admitted a candidate the reader refuses would bind an import without ever
// reaching the tightened read.
func importCandidates(relativePath string) []string {
	if strings.HasSuffix(relativePath, ".yammm") {
		return []string{relativePath}
	}
	return []string{relativePath + ".yammm"}
}

// candidateImportSourceIDs returns the SourceIDs readImportFile could produce
// for the given relative path, without performing any file I/O. Used by the
// cross-Load short-circuit in loadImport to detect already-registered imports
// before paying the read cost.
//
// It derives its candidates from [importCandidates], the same function
// readImportFile reads through, and emits an identity for each of the two code
// paths readImportFile exercises: the in-memory source-content lookup uses
// l.moduleRoot verbatim, while rootLoader.readFile uses the canonicalized
// rootPath. Both forms are emitted so a Registry populated by either path hits
// on the short-circuit. Paths that fail canonicalization are silently dropped;
// they would fail again in readImportFile with a uniform diagnostic.
func (l *loader) candidateImportSourceIDs(relativePath string) []location.SourceID {
	candidates := importCandidates(relativePath)

	ids := make([]location.SourceID, 0, 2*len(candidates))
	seen := make(map[location.SourceID]struct{}, 2*len(candidates))
	appendUnique := func(id location.SourceID) {
		if _, dup := seen[id]; dup {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	// Mirror readImportFile's in-memory lookup: l.moduleRoot + candidate,
	// derived textually.
	for _, candidate := range candidates {
		if id, err := inMemorySourceID(l.syntheticRoot, l.moduleRoot, candidate); err == nil {
			appendUnique(id)
		}
	}

	// Mirror rootLoader.readFile's path computation: canonical rootPath +
	// filepath.Clean(candidate). rootLoader canonicalizes the module root
	// once via makeCanonicalPath (symlink-resolved); candidate SourceIDs
	// derived here must use that same canonical root to match the SourceIDs
	// previously registered via rootLoader.readFile.
	if l.rootLoader != nil {
		for _, candidate := range candidates {
			absPath := filepath.Join(l.rootLoader.rootPath, filepath.Clean(candidate))
			if id, err := location.SourceIDFromAbsolutePath(absPath); err == nil {
				appendUnique(id)
			}
		}
	}

	return ids
}

// registerCachedClosureSources copies the source content of a
// registry-cached schema and its transitive imports into this load's
// source registries, so the current load's Sources() carries the full
// import closure even when the cross-Load short-circuit skipped the
// read+parse pipeline. Without it a cache-hit import is absent from
// Sources(), breaking consumers that need the closure's content —
// diagnostics rendering across imports, and gogen's embedded
// SerializedModel (whose round-trip check would see a single source
// that still declares imports). A registration that collides with
// different pre-registered bytes under the same SourceID is surfaced
// as E_INTERNAL: silently keeping both would hand consumers a
// Sources() view that disagrees with the schema the import was
// compiled from. Each schema is visited at most once per load (the
// closureSeen memo) — diamond-shaped import graphs have exponentially
// many import paths but only linearly many schemas. Callers hold l.mu.
func (l *loader) registerCachedClosureSources(s *Schema) {
	if _, ok := l.closureSeen[s]; ok {
		return
	}
	l.closureSeen[s] = struct{}{}
	srcs := s.Sources()
	if srcs == nil {
		return
	}
	id := s.SourceID()
	if _, ok := l.sourceContent[id]; !ok {
		if content, ok := srcs.ContentBySource(id); ok {
			if err := l.sourceRegistry.Register(id, content); err != nil {
				l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
					fmt.Sprintf("cached import source %s conflicts with pre-registered content: %v", id, err)).Build())
				return
			}
			l.sourceContent[id] = content
		}
	}
	for _, imp := range s.ImportsSlice() {
		if sub := imp.Schema(); sub != nil {
			l.registerCachedClosureSources(sub)
		}
	}
}

// readImportFile reads an import file using sandboxed access via rootLoader.
// Falls back to in-memory sources if available.
func (l *loader) readImportFile(relativePath string, imp *importDecl) ([]byte, location.SourceID, error) {
	candidates := importCandidates(relativePath)

	// First check if we have it in in-memory sources (for Sources). The
	// candidate SourceID derivation matches pre-registration's exactly.
	for _, candidate := range candidates {
		testID, err := inMemorySourceID(l.syntheticRoot, l.moduleRoot, candidate)
		if err != nil {
			continue
		}
		if content, ok := l.sourceContent[testID]; ok {
			return content, testID, nil
		}
	}

	// Under WithSourcesOnly the in-memory set is the whole universe:
	// a miss is an error, never a filesystem read.
	if l.cfg.sourcesOnly {
		return nil, location.SourceID{}, fmt.Errorf("import file %q not found in pre-registered sources", relativePath)
	}

	// Use rootLoader for sandboxed file access
	if err := l.ensureRootLoader(); err != nil {
		return nil, location.SourceID{}, fmt.Errorf("initialize sandboxed loader: %w", err)
	}

	if l.rootLoader == nil {
		// No module root and not in sourceContent - try direct file access (legacy)
		for _, candidate := range candidates {
			content, err := os.ReadFile(candidate)
			if err == nil {
				sourceID, err := location.SourceIDFromAbsolutePath(candidate)
				if err != nil {
					return nil, location.SourceID{}, fmt.Errorf("create source ID for %q: %w", candidate, err)
				}
				return content, sourceID, nil
			}
		}
		return nil, location.SourceID{}, fmt.Errorf("import file not found: %s", relativePath)
	}

	// Try each candidate with rootLoader.
	// We try each candidate and keep track of the last error.
	// For path escape errors, we return immediately since they are security-relevant.
	var lastErr error
	for _, candidate := range candidates {
		content, sourceID, err := l.rootLoader.readFile(candidate)
		if err == nil {
			return content, sourceID, nil
		}

		// Check if this is a path escape error - return immediately
		if _, ok := errors.AsType[*pathEscapeError](err); ok { //nolint:errcheck // type check only, value unused
			return nil, location.SourceID{}, err
		}

		lastErr = err
	}

	// Return the last error, or a generic "not found" if we had no errors
	if lastErr != nil {
		return nil, location.SourceID{}, lastErr
	}
	return nil, location.SourceID{}, fmt.Errorf("import file %q not found", imp.Path)
}

// inMemorySourceID derives the SourceID for an in-memory source key or an
// import candidate resolved against the load's root.
//
// A synthetic root (see [WithSyntheticRoot]) takes precedence over the module
// root and produces a synthetic identity: the normalized key joined to the root
// with a single "/". This branch runs first because a synthetic root is
// meaningful with an empty module root, and the module-root branch below would
// otherwise re-derive a filesystem path from the working directory.
//
// Root-relative keys join the (already canonical) module root textually —
// the joined path is never resolved against the filesystem — so
// pre-registration in LoadSourcesWithEntry, entry selection,
// the in-memory lookup in readImportFile, and the sandboxed disk reads in
// rootLoader.readFile all derive byte-identical SourceIDs for the same
// root-relative path regardless of disk state (including symlinked
// directories under the root).
//
// Absolute keys (and the legacy empty-root relative form) are instead
// canonicalized like entry paths — best-effort symlink resolution — so they
// land in the same path regime as the canonicalized module root:
// relative-import resolution computes filepath.Rel between the importing
// file's directory and the root, and the two must agree for the result to
// stay inside the sandbox (e.g. a /var/... overlay key against a
// /private/var/... root would otherwise escape).
func inMemorySourceID(syntheticRoot, moduleRoot, key string) (location.SourceID, error) {
	if syntheticRoot != "" {
		normalized, err := syntheticSourceKey(key)
		if err != nil {
			return location.SourceID{}, err
		}
		// NewSourceID bypasses validation, which is right here: the root was
		// validated once at load, and no key can make a validated root look
		// absolute. The joined string is deliberately not cleaned.
		return location.NewSourceID(syntheticRoot + "/" + normalized), nil
	}

	var absPath string
	if !filepath.IsAbs(key) && moduleRoot != "" {
		absPath = filepath.Join(moduleRoot, key)
	} else {
		abs, err := makeCanonicalPath(key)
		if err != nil {
			return location.SourceID{}, err
		}
		absPath = abs
	}
	id, err := location.SourceIDFromAbsolutePath(absPath)
	if err != nil {
		return location.SourceID{}, fmt.Errorf("derive source ID for %q: %w", key, err)
	}
	return id, nil
}

// syntheticSourceKey normalizes an in-memory source key for joining to a
// synthetic root, so the three sites that derive an identity from one key —
// pre-registration, candidateImportSourceIDs, and readImportFile — reach the
// same string. A disagreement between them is a silent hermetic-load miss under
// WithSourcesOnly, not a compile error.
//
// The joined identity is never cleaned: path.Clean collapses "//" to "/", so
// cleaning "embedded://app/x.yammm" would eat the scheme separator. The key is
// therefore cleaned here instead. A cleaned key keeps a leading "..", so a
// source outside the root — a layout sourceKey in adapter/gogen calls legal —
// yields a ".."-bearing identity that is still stable and still distinct.
func syntheticSourceKey(key string) (string, error) {
	slashed := filepath.ToSlash(key)
	// ValidateSyntheticSourceID is the absoluteness predicate rather than
	// filepath.IsAbs: it rejects the Unix, UNC, and Windows-volume forms on
	// every platform, and yammm ships six. The empty key is left to the
	// cleans-to-"." check below, which names it better.
	if slashed != "" {
		if err := location.ValidateSyntheticSourceID(slashed); err != nil {
			return "", fmt.Errorf("source key %q must be relative to the synthetic root: %w", key, err)
		}
	}
	cleaned := path.Clean(slashed) // "" cleans to "."
	if cleaned == "." {
		return "", fmt.Errorf("source key %q resolves to the synthetic root itself", key)
	}
	// NFC completes the three normalizations location.SourceIDFromAbsolutePath
	// applies for a file-backed key; location.NewSourceID applies none.
	return norm.NFC.String(cleaned), nil
}

// normalizeSyntheticRoot validates cfg's synthetic root against the load it was
// given to and returns the value the derivation sites use. It returns "" when
// the option was not passed.
//
// The trailing-slash trim runs before validation so two spellings of one root
// give one identity, and so a bare "/" reaches ValidateSyntheticSourceID as the
// empty string rather than as an absolute path. Both companion checks refuse
// rather than degrade: without WithSourcesOnly an import miss falls back to a
// sandboxed disk read and mixes a file-backed identity into the same closure,
// and a non-empty module root names the same concept twice when the load can
// honor only one.
func normalizeSyntheticRoot(cfg *loadConfig, moduleRoot string) (string, error) {
	if !cfg.syntheticRootSet {
		return "", nil
	}
	root := strings.TrimRight(cfg.syntheticRoot, "/")
	if err := location.ValidateSyntheticSourceID(root); err != nil {
		return "", fmt.Errorf("invalid synthetic root %q: %w", cfg.syntheticRoot, err)
	}
	if !cfg.sourcesOnly {
		return "", fmt.Errorf("synthetic root %q requires WithSourcesOnly: without it an unresolved import "+
			"reads from disk and mints a file-backed identity into the same closure", cfg.syntheticRoot)
	}
	if moduleRoot != "" {
		return "", fmt.Errorf("synthetic root %q cannot be combined with module root %q: "+
			"pass an empty module root, which the synthetic root stands in for", cfg.syntheticRoot, moduleRoot)
	}
	return root, nil
}

// rejectSyntheticRoot refuses WithSyntheticRoot on the load functions it cannot
// serve. Load resolves its entry from disk and always has a module root;
// LoadString mints its own "string://" identity and disallows imports. On both
// the option could only ever be a silent no-op, which is the worst of the three
// outcomes.
func rejectSyntheticRoot(cfg *loadConfig) error {
	if !cfg.syntheticRootSet {
		return nil
	}
	return errors.New("WithSyntheticRoot applies to LoadSourcesWithEntry only")
}

// makeCanonicalPath converts a path to absolute, cleaned, symlink-resolved form.
// This is used for trusted entry-point paths (not imports), where we need a
// canonical path for SourceID construction.
//
// If filepath.EvalSymlinks fails (e.g., the path doesn't exist yet, or permission
// issues in LSP scenarios), the function silently falls back to returning the
// cleaned absolute path without symlink resolution. This allows the loader to
// proceed with non-existent paths for better error reporting downstream.
func makeCanonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("abs path: %w", err)
	}
	cleaned := filepath.Clean(abs)

	// Attempt to resolve symlinks
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved, nil
	}
	return cleaned, nil
}
