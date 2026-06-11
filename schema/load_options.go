package schema

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/source"
)

// LoadOption configures the behavior of Load functions.
type LoadOption func(*loadConfig)

// loadConfig holds configuration for schema loading.
type loadConfig struct {
	registry        *Registry
	moduleRoot      string
	issueLimit      int
	sourceRegistry  *source.Registry
	logger          *slog.Logger
	disallowImports bool
	sourcesOnly     bool
}

// defaultLoadConfig returns a loadConfig with sensible defaults.
func defaultLoadConfig() *loadConfig {
	return &loadConfig{
		issueLimit: 100,
	}
}

// applyLoadOptions applies all options to the loadConfig.
func applyLoadOptions(cfg *loadConfig, opts []LoadOption) {
	for _, opt := range opts {
		opt(cfg)
	}
}

// WithRegistry provides a schema registry for cross-schema type resolution.
// Schemas loaded via imports will be registered automatically. If nil, a
// fresh Registry is created for the Load operation (the default, safe for
// any usage pattern).
//
// Shared-Registry semantics (post-v0.3.0). Passing the same *Registry to
// multiple Load calls is safe and efficient:
//
//   - Overlapping transitive imports short-circuit via the registry cache:
//     when loadImport encounters a SourceID already registered in r, the
//     existing *Schema pointer is reused and the import is NOT re-parsed.
//     This is where cross-Load schema caching pays off.
//   - Same-content re-registration is a no-op (see Registry.Register for
//     the idempotence contract); divergent-content re-registration still
//     errors with a full hash-diff diagnostic.
//   - The root schema returned by each Load call is always a fresh compile.
//     registry.LookupBySourceID(rootID) returns the first Load's pointer on
//     repeat calls; only imports benefit from the cache. This asymmetry is
//     intentional.
//   - Cross-Load sharing fires whenever an import resolves to a SourceID
//     already in r — including across loads with different WithModuleRoot
//     values whose root+key combinations land on the same canonical path
//     (nested roots reaching one shared file, for example). A cache-reused
//     import keeps the ModuleRoot of the load that compiled it; see
//     [Schema.ModuleRoot]. For LoadString, the synthetic
//     "string://<sourceName>" SourceID scheme means two LoadString calls
//     sharing a Registry must use distinct sourceName values unless
//     re-registering byte-identical content.
func WithRegistry(r *Registry) LoadOption {
	return func(c *loadConfig) {
		c.registry = r
	}
}

// WithModuleRoot sets the root directory for module-style imports.
// This option is only meaningful for Load() which operates on filesystem paths.
// For LoadString() and LoadSources(), the module root is inferred or provided directly.
func WithModuleRoot(root string) LoadOption {
	return func(c *loadConfig) {
		c.moduleRoot = root
	}
}

// WithIssueLimit sets the maximum number of diagnostic issues to collect.
// When the limit is reached, loading continues but additional issues are dropped.
// Set to 0 for unlimited. Default is 100.
func WithIssueLimit(limit int) LoadOption {
	return func(c *loadConfig) {
		c.issueLimit = limit
	}
}

// withSourceRegistry provides a source registry for position tracking.
// If not provided, a new source registry is created for the load operation.
//
// Unexported: the parameter type lives in internal/source, so no caller
// outside the module can construct a meaningful argument — consumers read
// the loaded schema's source closure via [Schema.Sources] instead. Tests
// reach this seam through the package's test-only exports.
func withSourceRegistry(reg *source.Registry) LoadOption {
	return func(c *loadConfig) {
		c.sourceRegistry = reg
	}
}

// WithSourcesOnly restricts import resolution to the pre-registered
// in-memory sources: an import that misses the registered set fails with
// E_IMPORT_RESOLVE instead of falling back to a filesystem read under the
// module root. Use it when loading embedded sources (e.g. a generated
// package's SerializedModel via LoadSourcesWithEntry) to make the load
// hermetic: source bytes come only from the provided map, a missing or
// mis-keyed source surfaces as an error rather than being silently
// satisfied by an on-disk file, and on-disk state cannot change how keys
// resolve — SourceIDs derive textually from the module root and key, and
// the module-root sandbox is never opened. Meaningful for
// LoadSources/LoadSourcesWithEntry; a plain Load resolves its entry from
// disk by definition (its imports would still be restricted).
func WithSourcesOnly() LoadOption {
	return func(c *loadConfig) {
		c.sourcesOnly = true
	}
}

// WithDisallowImports prevents import declarations from being processed.
// When enabled, any import statements in the source produce an
// E_IMPORT_NOT_ALLOWED diagnostic. Used by LoadString (unconditionally)
// and by the LSP markdown analysis path (isolated blocks).
func WithDisallowImports() LoadOption {
	return func(c *loadConfig) {
		c.disallowImports = true
	}
}

// WithLogger provides a structured logger for load operation diagnostics.
// If not provided, logging is disabled.
func WithLogger(logger *slog.Logger) LoadOption {
	return func(c *loadConfig) {
		c.logger = logger
	}
}
