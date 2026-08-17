package schema

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

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

// readFile reads a file relative to the module root with sandboxed access.
// Returns ErrPathEscape if the path would escape the module root.
func (rl *rootLoader) readFile(relativePath string) ([]byte, location.SourceID, error) {
	f, err := rl.openFile(relativePath)
	if err != nil {
		return nil, location.SourceID{}, err
	}
	defer f.Close()

	content, err := io.ReadAll(f)
	if err != nil {
		return nil, location.SourceID{}, fmt.Errorf("read import %q: %w", relativePath, err)
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

	// Resolve the path to an absolute, symlink-resolved canonical path
	absPath, err := makeCanonicalPath(path)
	if err != nil {
		return fatalResult(fmt.Errorf("resolve path %q: %w", path, err), diag.Result{})
	}

	// Read the file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fatalResult(fmt.Errorf("read %q: %w", absPath, err), diag.Result{})
	}

	// Determine module root and canonicalize for consistent path comparison.
	// This is important because source file paths are canonicalized (symlinks resolved),
	// so module root must also be canonicalized for filepath.Rel to work correctly.
	moduleRoot := cfg.moduleRoot
	if moduleRoot == "" {
		moduleRoot = filepath.Dir(absPath)
	} else {
		var err error
		moduleRoot, err = makeCanonicalPath(moduleRoot)
		if err != nil {
			return fatalResult(fmt.Errorf("invalid module root %q: %w", cfg.moduleRoot, err), diag.Result{})
		}
	}

	// Create loader and load the schema
	ldr := newLoader(cfg, moduleRoot)
	defer ldr.Close() // Release rootLoader resources when done

	s, result, err := ldr.loadFile(ctx, absPath, content)
	if err != nil {
		return fatalResult(err, result)
	}
	if s == nil {
		return nil, result
	}

	// Perform cross-schema inheritance cycle detection if there are imports.
	// This runs after all schemas are loaded when full cross-schema visibility is available.
	if ldr.registry.Len() > 1 {
		if cycleIssues := detectCrossSchemaInheritanceCycles(ldr.registry); len(cycleIssues) > 0 {
			for _, issue := range cycleIssues {
				ldr.collector.Collect(*issue)
			}
			return nil, ldr.collector.Result()
		}
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
	cfg.disallowImports = true // Always disallow imports from string, even if user opts try to enable

	// Create a synthetic source ID (NewSourceID returns just SourceID, no error)
	sourceID := location.NewSourceID("string://" + sourceName)

	// Create loader and load from string
	ldr := newLoader(cfg, "")
	s, result, err := ldr.loadSource(ctx, sourceID, []byte(sourceCode))
	if err != nil {
		return fatalResult(err, result)
	}
	return s, result
}

// LoadSourcesWithEntry loads a schema from in-memory sources with an explicit entry point.
//
// The sources map keys are paths relative to moduleRoot, and values are
// the file contents. Unlike LoadSources, this function uses the provided
// entryPath as the entry point instead of selecting by sorted key order.
//
// This is useful when the caller knows which file should be the entry point,
// particularly in LSP scenarios where multiple documents may be open but only
// one is being analyzed.
//
// The entryPath must exist in the sources map (as either an absolute path
// or relative to moduleRoot). If entryPath is empty, falls back to sorted
// key order selection like LoadSources.
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

	// Canonicalize moduleRoot to absolute path if provided.
	// This ensures SourceIDFromAbsolutePath will work correctly.
	if moduleRoot != "" {
		var err error
		moduleRoot, err = makeCanonicalPath(moduleRoot)
		if err != nil {
			return fatalResult(fmt.Errorf("invalid module root %q: %w", moduleRoot, err), diag.Result{})
		}
	}

	// Create loader
	ldr := newLoader(cfg, moduleRoot)
	defer ldr.Close() // Release rootLoader resources when done

	// Pre-register all sources. SourceIDs are derived textually (no symlink
	// resolution of the joined path) so import lookups — which join the same
	// module root with the same keys — land on identical IDs regardless of
	// what exists on disk.
	for path, content := range sources {
		sourceID, err := inMemorySourceID(moduleRoot, path)
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
		// Fall back to lexicographic selection (same as LoadSources)
		for path := range sources {
			if selectedEntry == "" || path < selectedEntry {
				selectedEntry = path
			}
		}
	}

	// Derive the entry SourceID exactly like pre-registration did, so the
	// entry lookup hits the just-registered content.
	sourceID, err := inMemorySourceID(moduleRoot, selectedEntry)
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

	// Perform cross-schema inheritance cycle detection if there are imports.
	// This runs after all schemas are loaded when full cross-schema visibility is available.
	if ldr.registry.Len() > 1 {
		if cycleIssues := detectCrossSchemaInheritanceCycles(ldr.registry); len(cycleIssues) > 0 {
			for _, issue := range cycleIssues {
				ldr.collector.Collect(*issue)
			}
			return nil, ldr.collector.Result()
		}
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
func newLoader(cfg *loadConfig, moduleRoot string) *loader {
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
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_CYCLE,
			fmt.Sprintf("import cycle detected involving %s", sourceID)).Build())
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
	s.setSources(NewSources(l.sourceRegistry))
	s.setModuleRoot(l.moduleRoot)

	// Seal the schema to prevent further mutation
	s.seal()

	l.logger.Debug("schema loaded",
		"source", sourceID.String(),
		"name", s.Name(),
		"types", len(s.TypesSlice()),
		"imports", len(s.ImportsSlice()))

	// Register the schema
	if err := l.registry.Register(s); err != nil {
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_TYPE,
			fmt.Sprintf("register schema: %v", err)).Build())
		return nil, l.collector.Result(), nil
	}

	l.mu.Lock()
	l.loadedSchemas[sourceID] = s
	l.mu.Unlock()

	return s, l.collector.Result(), nil
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
	if l.moduleRoot == "" && len(m.Imports) > 0 {
		// Without a module root, we can only resolve relative imports
		// from file-based sources
		if !sourceID.IsFilePath() {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
				"cannot resolve imports without a module root").Build())
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
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
			fmt.Sprintf("cannot resolve import %q: %v", imp.Path, err)).
			WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
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
		if _, ok := errors.AsType[*pathEscapeError](err); ok { //nolint:errcheck // type check only, value unused
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_PATH_ESCAPE,
				fmt.Sprintf("import %q escapes module root", imp.Path)).
				WithSpan(imp.Span).
				WithDetail(diag.DetailKeyImportPath, imp.Path).Build())
			l.markImportFailed(imp)
			return nil
		}
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
			fmt.Sprintf("cannot read import %q: %v", imp.Path, err)).
			WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
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

	// Module-style import (just a path like "common/types")
	if l.moduleRoot == "" {
		return "", errors.New("module-style imports require a module root")
	}

	return importPath, nil
}

// candidateImportSourceIDs returns the SourceIDs readImportFile could produce
// for the given relative path, without performing any file I/O. Used by the
// cross-Load short-circuit in loadImport to detect already-registered imports
// before paying the read cost.
//
// The returned list mirrors readImportFile's candidate order (with-.yammm
// first, then without) across the two code paths readImportFile exercises:
// the in-memory source-content lookup uses l.moduleRoot verbatim, while
// rootLoader.readFile uses the canonicalized rootPath. Both forms are
// emitted so a Registry populated by either path will hit on the short-
// circuit. Paths that fail canonicalization are silently dropped; they
// would fail again in readImportFile with a uniform diagnostic.
func (l *loader) candidateImportSourceIDs(relativePath string) []location.SourceID {
	candidates := []string{relativePath}
	if !strings.HasSuffix(relativePath, ".yammm") {
		candidates = []string{relativePath + ".yammm", relativePath}
	}

	ids := make([]location.SourceID, 0, 4)
	seen := make(map[location.SourceID]struct{}, 4)
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
		if id, err := inMemorySourceID(l.moduleRoot, candidate); err == nil {
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
	// Try with .yammm extension first, then without
	candidates := []string{relativePath}
	if !strings.HasSuffix(relativePath, ".yammm") {
		candidates = []string{relativePath + ".yammm", relativePath}
	}

	// First check if we have it in in-memory sources (for Sources). The
	// candidate SourceID derivation matches pre-registration's exactly.
	for _, candidate := range candidates {
		testID, err := inMemorySourceID(l.moduleRoot, candidate)
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
// import candidate resolved against the module root.
//
// Root-relative keys join the (already canonical) module root textually —
// the joined path is never resolved against the filesystem — so
// pre-registration in LoadSources/LoadSourcesWithEntry, entry selection,
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
func inMemorySourceID(moduleRoot, path string) (location.SourceID, error) {
	var absPath string
	if !filepath.IsAbs(path) && moduleRoot != "" {
		absPath = filepath.Join(moduleRoot, path)
	} else {
		abs, err := makeCanonicalPath(path)
		if err != nil {
			return location.SourceID{}, err
		}
		absPath = abs
	}
	id, err := location.SourceIDFromAbsolutePath(absPath)
	if err != nil {
		return location.SourceID{}, fmt.Errorf("derive source ID for %q: %w", path, err)
	}
	return id, nil
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
