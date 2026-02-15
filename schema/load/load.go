package load

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
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/internal/alias"
	"github.com/simon-lentz/yammm/schema/internal/complete"
	"github.com/simon-lentz/yammm/schema/internal/parse"
)

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
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
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
// On error, Schema is nil but diag.Result may contain useful diagnostics.
func Load(ctx context.Context, path string, opts ...Option) (*schema.Schema, diag.Result, error) {
	if ctx == nil {
		panic("load.Load: context must not be nil")
	}

	cfg := defaultConfig()
	applyOptions(cfg, opts)

	// Resolve the path to an absolute, symlink-resolved canonical path
	absPath, err := makeCanonicalPath(path)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("resolve path %q: %w", path, err)
	}

	// Read the file content
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("read %q: %w", absPath, err)
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
			return nil, diag.Result{}, fmt.Errorf("invalid module root %q: %w", cfg.moduleRoot, err)
		}
	}

	// Create loader and load the schema
	ldr, err := newLoader(cfg, moduleRoot)
	if err != nil {
		return nil, diag.Result{}, err
	}
	defer ldr.Close() // Release rootLoader resources when done

	s, result, err := ldr.loadFile(ctx, absPath, content)
	if err != nil || s == nil {
		return s, result, err
	}

	// Perform cross-schema inheritance cycle detection if there are imports.
	// This runs after all schemas are loaded when full cross-schema visibility is available.
	if ldr.registry.Len() > 1 {
		if cycleIssues := complete.DetectCrossSchemaInheritanceCycles(ldr.registry); len(cycleIssues) > 0 {
			for _, issue := range cycleIssues {
				ldr.collector.Collect(*issue)
			}
			return nil, ldr.collector.Result(), nil
		}
	}

	return s, result, err
}

// LoadString loads a schema from a string source.
//
// The sourceName is used as the display path in diagnostics. For
// consistent error messages, use a meaningful path-like name.
//
// Imports are not supported when loading from a string unless
// WithSourceRegistry is provided with pre-registered import sources.
//
// ctx must not be nil. Passing nil will panic.
// On error, Schema is nil but diag.Result may contain useful diagnostics.
func LoadString(ctx context.Context, sourceCode, sourceName string, opts ...Option) (*schema.Schema, diag.Result, error) {
	if ctx == nil {
		panic("load.LoadString: context must not be nil")
	}

	cfg := defaultConfig()
	applyOptions(cfg, opts)

	// Create a synthetic source ID (NewSourceID returns just SourceID, no error)
	sourceID := location.NewSourceID("string://" + sourceName)

	// Create loader and load from string
	ldr, err := newLoader(cfg, "")
	if err != nil {
		return nil, diag.Result{}, err
	}
	ldr.disallowImports = true // Imports not supported from string
	s, result, err := ldr.loadSource(ctx, sourceID, []byte(sourceCode))
	return s, result, err
}

// LoadSources loads a schema from in-memory sources.
//
// The sources map keys are paths relative to moduleRoot, and values are
// the file contents. The first entry (by sorted key order) is treated as the
// entry point schema. Use LoadSourcesWithEntry if you need to specify the
// entry point explicitly.
//
// This function is useful for testing and embedded schemas.
//
// ctx must not be nil. Passing nil will panic.
// On error, Schema is nil but diag.Result may contain useful diagnostics.
func LoadSources(ctx context.Context, sources map[string][]byte, moduleRoot string, opts ...Option) (*schema.Schema, diag.Result, error) {
	if ctx == nil {
		panic("load.LoadSources: context must not be nil")
	}

	if len(sources) == 0 {
		return nil, diag.Result{}, errors.New("no sources provided")
	}

	cfg := defaultConfig()
	applyOptions(cfg, opts)

	// Canonicalize moduleRoot to absolute path if provided.
	// This ensures SourceIDFromAbsolutePath will work correctly.
	if moduleRoot != "" {
		var err error
		moduleRoot, err = makeCanonicalPath(moduleRoot)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("invalid module root %q: %w", moduleRoot, err)
		}
	}

	// Create loader
	ldr, err := newLoader(cfg, moduleRoot)
	if err != nil {
		return nil, diag.Result{}, err
	}
	defer ldr.Close() // Release rootLoader resources when done

	// Pre-register all sources
	for path, content := range sources {
		var absPath string
		if filepath.IsAbs(path) {
			absPath = path
		} else {
			absPath = filepath.Join(moduleRoot, path)
		}

		// Canonicalize the path to match the canonicalized moduleRoot.
		// This ensures filepath.Rel works correctly during import resolution,
		// especially on systems with symlinks (e.g., macOS /var -> /private/var).
		absPath, err = makeCanonicalPath(absPath)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("canonicalize path %q: %w", path, err)
		}

		sourceID, err := location.SourceIDFromAbsolutePath(absPath)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("invalid path %q: %w", path, err)
		}

		if err := ldr.sourceRegistry.Register(sourceID, content); err != nil {
			return nil, diag.Result{}, fmt.Errorf("register source %q: %w", path, err)
		}

		ldr.sourceContent[sourceID] = content
	}

	// Find the entry point (first by sorted key order)
	var entryPath string
	for path := range sources {
		if entryPath == "" || path < entryPath {
			entryPath = path
		}
	}

	var entryAbsPath string
	if filepath.IsAbs(entryPath) {
		entryAbsPath = entryPath
	} else {
		entryAbsPath = filepath.Join(moduleRoot, entryPath)
	}

	// Canonicalize entry path to match sourceContent keys
	entryAbsPath, err = makeCanonicalPath(entryAbsPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("canonicalize entry path %q: %w", entryPath, err)
	}

	sourceID, err := location.SourceIDFromAbsolutePath(entryAbsPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("invalid entry path %q: %w", entryAbsPath, err)
	}
	content := ldr.sourceContent[sourceID]

	s, result, err := ldr.loadSource(ctx, sourceID, content)
	if err != nil || s == nil {
		return s, result, err
	}

	// Perform cross-schema inheritance cycle detection if there are imports.
	// This runs after all schemas are loaded when full cross-schema visibility is available.
	if ldr.registry.Len() > 1 {
		if cycleIssues := complete.DetectCrossSchemaInheritanceCycles(ldr.registry); len(cycleIssues) > 0 {
			for _, issue := range cycleIssues {
				ldr.collector.Collect(*issue)
			}
			return nil, ldr.collector.Result(), nil
		}
	}

	return s, result, err
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
// On error, Schema is nil but diag.Result may contain useful diagnostics.
func LoadSourcesWithEntry(ctx context.Context, sources map[string][]byte, entryPath string, moduleRoot string, opts ...Option) (*schema.Schema, diag.Result, error) {
	if ctx == nil {
		panic("load.LoadSourcesWithEntry: context must not be nil")
	}

	if len(sources) == 0 {
		return nil, diag.Result{}, errors.New("no sources provided")
	}

	cfg := defaultConfig()
	applyOptions(cfg, opts)

	// Canonicalize moduleRoot to absolute path if provided.
	// This ensures SourceIDFromAbsolutePath will work correctly.
	if moduleRoot != "" {
		var err error
		moduleRoot, err = makeCanonicalPath(moduleRoot)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("invalid module root %q: %w", moduleRoot, err)
		}
	}

	// Create loader
	ldr, err := newLoader(cfg, moduleRoot)
	if err != nil {
		return nil, diag.Result{}, err
	}
	defer ldr.Close() // Release rootLoader resources when done

	// Pre-register all sources
	for path, content := range sources {
		var absPath string
		if filepath.IsAbs(path) {
			absPath = path
		} else {
			absPath = filepath.Join(moduleRoot, path)
		}

		// Canonicalize the path to match the canonicalized moduleRoot.
		// This ensures filepath.Rel works correctly during import resolution,
		// especially on systems with symlinks (e.g., macOS /var -> /private/var).
		absPath, err = makeCanonicalPath(absPath)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("canonicalize path %q: %w", path, err)
		}

		sourceID, err := location.SourceIDFromAbsolutePath(absPath)
		if err != nil {
			return nil, diag.Result{}, fmt.Errorf("invalid path %q: %w", path, err)
		}

		if err := ldr.sourceRegistry.Register(sourceID, content); err != nil {
			return nil, diag.Result{}, fmt.Errorf("register source %q: %w", path, err)
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

	var entryAbsPath string
	if filepath.IsAbs(selectedEntry) {
		entryAbsPath = selectedEntry
	} else {
		entryAbsPath = filepath.Join(moduleRoot, selectedEntry)
	}

	// Canonicalize entry path to match sourceContent keys
	entryAbsPath, err = makeCanonicalPath(entryAbsPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("canonicalize entry path %q: %w", selectedEntry, err)
	}

	sourceID, err := location.SourceIDFromAbsolutePath(entryAbsPath)
	if err != nil {
		return nil, diag.Result{}, fmt.Errorf("invalid entry path %q: %w", entryAbsPath, err)
	}

	content, ok := ldr.sourceContent[sourceID]
	if !ok {
		return nil, diag.Result{}, fmt.Errorf("entry path %q not found in sources", selectedEntry)
	}

	s, result, err := ldr.loadSource(ctx, sourceID, content)
	if err != nil || s == nil {
		return s, result, err
	}

	// Perform cross-schema inheritance cycle detection if there are imports.
	// This runs after all schemas are loaded when full cross-schema visibility is available.
	if ldr.registry.Len() > 1 {
		if cycleIssues := complete.DetectCrossSchemaInheritanceCycles(ldr.registry); len(cycleIssues) > 0 {
			for _, issue := range cycleIssues {
				ldr.collector.Collect(*issue)
			}
			return nil, ldr.collector.Result(), nil
		}
	}

	return s, result, err
}

// resolvedImport holds the resolved identity and schema for an import.
type resolvedImport struct {
	sourceID location.SourceID
	schema   *schema.Schema
	decl     *parse.ImportDecl // original declaration for diagnostics
}

// loader handles the schema loading process.
type loader struct {
	cfg             *config
	moduleRoot      string
	rootLoader      *rootLoader // sandboxed file access for imports (nil if no moduleRoot)
	registry        *schema.Registry
	sourceRegistry  *source.Registry
	collector       *diag.Collector
	logger          *slog.Logger
	disallowImports bool

	// Tracking state
	mu              sync.Mutex
	sourceContent   map[location.SourceID][]byte
	loadedSchemas   map[location.SourceID]*schema.Schema
	loadingSchemas  map[location.SourceID]bool // For cycle detection
	resolvedImports map[string]resolvedImport  // alias -> resolved import for current schema
}

// registryAdapter adapts *schema.Registry to the complete.Registry interface.
type registryAdapter struct {
	r *schema.Registry
}

// LookupBySourceID implements the complete.Registry interface.
func (a *registryAdapter) LookupBySourceID(id location.SourceID) (*schema.Schema, bool) {
	s, status := a.r.LookupBySourceID(id)
	return s, status.Found()
}

// newLoader creates a new loader with the given configuration.
// Returns ErrSourceStoreNotSupported if a custom SourceStore implementation is provided
// that is not *source.Registry.
func newLoader(cfg *config, moduleRoot string) (*loader, error) {
	registry := cfg.registry
	if registry == nil {
		registry = schema.NewRegistry()
	}

	// Validate and use provided source registry if it's a *source.Registry.
	// Custom SourceStore implementations are not supported - fail fast with
	// ErrSourceStoreNotSupported rather than silently falling back.
	var sourceReg *source.Registry
	if cfg.sourceRegistry != nil {
		sr, ok := cfg.sourceRegistry.(*source.Registry)
		if !ok {
			return nil, ErrSourceStoreNotSupported
		}
		sourceReg = sr
	}
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
		sourceContent:   make(map[location.SourceID][]byte),
		loadedSchemas:   make(map[location.SourceID]*schema.Schema),
		loadingSchemas:  make(map[location.SourceID]bool),
		resolvedImports: make(map[string]resolvedImport),
	}, nil
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
func (l *loader) loadFile(ctx context.Context, absPath string, content []byte) (*schema.Schema, diag.Result, error) {
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
func (l *loader) loadSource(ctx context.Context, sourceID location.SourceID, content []byte) (*schema.Schema, diag.Result, error) {
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

	// Save parent's resolvedImports and create fresh map for this invocation.
	// This stack-based approach ensures each schema load has its own isolated
	// import resolution map, while preserving the parent's map across recursive calls.
	parentResolvedImports := l.resolvedImports
	l.resolvedImports = make(map[string]resolvedImport)
	defer func() {
		l.resolvedImports = parentResolvedImports
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
	parser := parse.NewParser(sourceID, l.collector, l.sourceRegistry, l.sourceRegistry)
	model := parser.Parse(content)

	if model == nil {
		return nil, l.collector.Result(), nil
	}

	// Validate imports and check for duplicates
	if !l.validateImports(sourceID, model) {
		return nil, l.collector.Result(), nil
	}

	// Load imported schemas first
	ok, err := l.loadImports(ctx, sourceID, model)
	if err != nil {
		return nil, l.collector.Result(), err // propagate cancellation
	}
	if !ok {
		return nil, l.collector.Result(), nil // content failure
	}

	// Build resolved imports map for completion
	resolvedImports := make(complete.ResolvedImports, len(l.resolvedImports))
	for alias, resolved := range l.resolvedImports {
		resolvedImports[alias] = resolved.sourceID
	}

	// Complete the schema (resolve types, validate, etc.)
	s := complete.Complete(model, sourceID, l.collector, &registryAdapter{l.registry}, resolvedImports)

	if s == nil {
		return nil, l.collector.Result(), nil
	}

	// Wire resolved schema references (SourceID already set during completion)
	for _, imp := range s.ImportsSlice() {
		if resolved, ok := l.resolvedImports[imp.Alias()]; ok {
			imp.SetSchema(resolved.schema)
		}
	}

	// Seal all imports to prevent further mutation
	for _, imp := range s.ImportsSlice() {
		imp.Seal()
	}

	// Schema must be nil if any errors exist.
	// Check BEFORE registration to avoid registering schemas we'll discard.
	if l.collector.HasErrors() {
		return nil, l.collector.Result(), nil
	}

	// Attach sources for diagnostics rendering
	s.SetSources(schema.NewSources(l.sourceRegistry))

	// Seal the schema to prevent further mutation
	s.Seal()

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

// validateImports checks for import issues.
func (l *loader) validateImports(sourceID location.SourceID, model *parse.Model) bool {
	if l.disallowImports && len(model.Imports) > 0 {
		// Per spec: single E_IMPORT_NOT_ALLOWED issue with import_count detail,
		// positioned at the first import declaration.
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_NOT_ALLOWED,
			"import declarations are not supported in LoadString(); use Load() or LoadSources() for schemas with imports").
			WithSpan(model.Imports[0].Span).
			WithDetail(diag.DetailKeyImportCount, strconv.Itoa(len(model.Imports))).Build())
		return false
	}

	// Check for duplicate imports (same path or alias)
	seenPaths := make(map[string]*parse.ImportDecl)
	seenAliases := make(map[string]*parse.ImportDecl)

	for _, imp := range model.Imports {
		// Check for duplicate path
		if existing, ok := seenPaths[imp.Path]; ok {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
				fmt.Sprintf("duplicate import of %q", imp.Path)).
				WithSpan(imp.Span).
				WithRelated(location.RelatedInfo{
					Span:    existing.Span,
					Message: "first imported here",
				}).Build())
			return false
		}
		seenPaths[imp.Path] = imp

		// Check for duplicate alias
		if existing, ok := seenAliases[imp.Alias]; ok {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
				fmt.Sprintf("duplicate import alias %q", imp.Alias)).
				WithSpan(imp.Span).
				WithRelated(location.RelatedInfo{
					Span:    existing.Span,
					Message: "alias first used here",
				}).Build())
			return false
		}
		seenAliases[imp.Alias] = imp

		// Check for reserved keyword alias
		if alias.IsReservedKeyword(imp.Alias) {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INVALID_ALIAS,
				fmt.Sprintf("import alias %q is a reserved keyword", imp.Alias)).
				WithSpan(imp.Span).Build())
			return false
		}
	}

	// Check for alias collision with local type names and datatype aliases
	localNames := make(map[string]location.Span)
	for _, t := range model.Types {
		localNames[t.Name] = t.Span
	}
	for _, dt := range model.DataTypes {
		localNames[dt.Name] = dt.Span
	}

	for _, imp := range model.Imports {
		if existingSpan, ok := localNames[imp.Alias]; ok {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_ALIAS_COLLISION,
				fmt.Sprintf("import alias %q collides with local type or datatype", imp.Alias)).
				WithSpan(imp.Span).
				WithRelated(location.RelatedInfo{
					Span:    existingSpan,
					Message: "defined here",
				}).Build())
			return false
		}
	}

	_ = sourceID // Unused but may be needed for future enhancements
	return true
}

// loadImports loads all imported schemas.
func (l *loader) loadImports(ctx context.Context, sourceID location.SourceID, model *parse.Model) (bool, error) {
	if l.moduleRoot == "" && len(model.Imports) > 0 {
		// Without a module root, we can only resolve relative imports
		// from file-based sources
		if !sourceID.IsFilePath() {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
				"cannot resolve imports without a module root").Build())
			return false, nil
		}
	}

	for _, imp := range model.Imports {
		// Check for cancellation before each import (/19)
		// Per, cancellation is returned as error, not collected as diagnostic.
		if err := ctx.Err(); err != nil {
			return false, fmt.Errorf("load cancelled: %w", err)
		}
		ok, err := l.loadImport(ctx, sourceID, imp)
		if err != nil {
			return false, err // propagate cancellation
		}
		if !ok {
			return false, nil // content failure
		}
	}

	// Check for duplicate imports by resolved SourceID (not raw path)
	// Two different import paths may resolve to the same canonical file
	if !l.validateResolvedImports() {
		return false, nil
	}

	return true, nil
}

// validateResolvedImports checks for duplicate resolved SourceIDs.
// Two different import paths that resolve to the same canonical file are an error.
func (l *loader) validateResolvedImports() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	seenSourceIDs := make(map[location.SourceID]resolvedImport)
	for _, resolved := range l.resolvedImports {
		if existing, ok := seenSourceIDs[resolved.sourceID]; ok {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_DUPLICATE_IMPORT,
				fmt.Sprintf("schema %q imported multiple times", resolved.sourceID.String())).
				WithSpan(resolved.decl.Span).
				WithDetail(diag.DetailKeyImportPath, resolved.sourceID.String()).
				WithDetail(diag.DetailKeyFirstAlias, existing.decl.Alias).
				WithDetail(diag.DetailKeyFirstLine, strconv.Itoa(existing.decl.Span.Start.Line)).
				WithDetail(diag.DetailKeyDuplicateAlias, resolved.decl.Alias).
				WithDetail(diag.DetailKeyDuplicateLine, strconv.Itoa(resolved.decl.Span.Start.Line)).
				WithRelated(location.RelatedInfo{
					Span:    existing.decl.Span,
					Message: fmt.Sprintf("first imported here as %q", existing.decl.Alias),
				}).Build())
			return false
		}
		seenSourceIDs[resolved.sourceID] = resolved
	}
	return true
}

// loadImport loads a single imported schema.
func (l *loader) loadImport(ctx context.Context, sourceID location.SourceID, imp *parse.ImportDecl) (bool, error) {
	l.logger.Debug("loading import", "path", imp.Path, "alias", imp.Alias)

	// Resolve the import path to a relative path (relative to module root)
	relativePath, err := l.resolveImportToRelative(sourceID, imp.Path)
	if err != nil {
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
			fmt.Sprintf("cannot resolve import %q: %v", imp.Path, err)).
			WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
		return false, nil
	}

	// Read the import using rootLoader (sandboxed) or in-memory sources
	content, importSourceID, err := l.readImportFile(relativePath, imp)
	if err != nil {
		var escapeErr *pathEscapeError
		if errors.As(err, &escapeErr) {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_PATH_ESCAPE,
				fmt.Sprintf("import %q escapes module root", imp.Path)).
				WithSpan(imp.Span).
				WithDetail(diag.DetailKeyImportPath, imp.Path).Build())
			return false, nil
		}
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_IMPORT_RESOLVE,
			fmt.Sprintf("cannot read import %q: %v", imp.Path, err)).
			WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
		return false, nil
	}

	// Check if already loaded
	l.mu.Lock()
	if loadedSchema, ok := l.loadedSchemas[importSourceID]; ok {
		// Store the resolved import information even for already-loaded schemas
		l.resolvedImports[imp.Alias] = resolvedImport{
			sourceID: importSourceID,
			schema:   loadedSchema,
			decl:     imp,
		}
		l.mu.Unlock()
		return true, nil
	}
	l.mu.Unlock()

	// Register the source if not already registered
	if _, exists := l.sourceContent[importSourceID]; !exists {
		if err := l.sourceRegistry.Register(importSourceID, content); err != nil {
			l.collector.Collect(diag.NewIssue(diag.Error, diag.E_INTERNAL,
				fmt.Sprintf("register import source: %v", err)).Build())
			return false, nil
		}
		l.sourceContent[importSourceID] = content
	}

	// Recursively load the imported schema
	s, _, err := l.loadSource(ctx, importSourceID, content)
	if err != nil {
		return false, err // propagate cancellation
	}
	if s == nil {
		l.collector.Collect(diag.NewIssue(diag.Error, diag.E_UPSTREAM_FAIL,
			fmt.Sprintf("import %q failed to compile", imp.Path)).
			WithSpan(imp.Span).
			WithDetail(diag.DetailKeyImportPath, imp.Path).
			WithDetail(diag.DetailKeyAlias, imp.Alias).Build())
		return false, nil
	}

	// Store the resolved import information for later wiring to the schema's Import objects
	l.mu.Lock()
	l.resolvedImports[imp.Alias] = resolvedImport{
		sourceID: importSourceID,
		schema:   s,
		decl:     imp,
	}
	l.mu.Unlock()

	return true, nil
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

// readImportFile reads an import file using sandboxed access via rootLoader.
// Falls back to in-memory sources if available.
func (l *loader) readImportFile(relativePath string, imp *parse.ImportDecl) ([]byte, location.SourceID, error) {
	// Try with .yammm extension first, then without
	candidates := []string{relativePath}
	if !strings.HasSuffix(relativePath, ".yammm") {
		candidates = []string{relativePath + ".yammm", relativePath}
	}

	// First check if we have it in in-memory sources (for LoadSources)
	for _, candidate := range candidates {
		var absPath string
		if l.moduleRoot != "" {
			absPath = filepath.Join(l.moduleRoot, candidate)
		} else {
			absPath = candidate
		}
		testID, err := location.SourceIDFromAbsolutePath(absPath)
		if err != nil {
			continue
		}
		if content, ok := l.sourceContent[testID]; ok {
			return content, testID, nil
		}
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
		var escapeErr *pathEscapeError
		if errors.As(err, &escapeErr) {
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
