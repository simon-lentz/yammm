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
// Schemas loaded via imports will be registered automatically.
// If nil, a new registry is created for the load operation.
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

// WithSourceRegistry provides a source registry for position tracking.
// If not provided, a new source registry is created for the load operation.
func WithSourceRegistry(reg *source.Registry) LoadOption {
	return func(c *loadConfig) {
		c.sourceRegistry = reg
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
