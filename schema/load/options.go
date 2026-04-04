package load

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/schema"
)

// Option configures the behavior of Load functions.
type Option func(*config)

// config holds configuration for schema loading.
type config struct {
	registry        *schema.Registry
	moduleRoot      string
	issueLimit      int
	sourceRegistry  *source.Registry
	logger          *slog.Logger
	disallowImports bool
}

// defaultConfig returns a config with sensible defaults.
func defaultConfig() *config {
	return &config{
		issueLimit: 100,
	}
}

// applyOptions applies all options to the config.
func applyOptions(cfg *config, opts []Option) {
	for _, opt := range opts {
		opt(cfg)
	}
}

// WithRegistry provides a schema registry for cross-schema type resolution.
// Schemas loaded via imports will be registered automatically.
// If nil, a new registry is created for the load operation.
func WithRegistry(r *schema.Registry) Option {
	return func(c *config) {
		c.registry = r
	}
}

// WithModuleRoot sets the root directory for module-style imports.
// This option is only meaningful for Load() which operates on filesystem paths.
// For String() and Sources(), the module root is inferred or provided directly.
func WithModuleRoot(root string) Option {
	return func(c *config) {
		c.moduleRoot = root
	}
}

// WithIssueLimit sets the maximum number of diagnostic issues to collect.
// When the limit is reached, loading continues but additional issues are dropped.
// Set to 0 for unlimited. Default is 100.
func WithIssueLimit(limit int) Option {
	return func(c *config) {
		c.issueLimit = limit
	}
}

// WithSourceRegistry provides a source registry for position tracking.
// If not provided, a new source registry is created for the load operation.
func WithSourceRegistry(reg *source.Registry) Option {
	return func(c *config) {
		c.sourceRegistry = reg
	}
}

// WithDisallowImports prevents import declarations from being processed.
// When enabled, any import statements in the source produce an
// E_IMPORT_NOT_ALLOWED diagnostic. Used by String (unconditionally)
// and by the LSP markdown analysis path (isolated blocks).
func WithDisallowImports() Option {
	return func(c *config) {
		c.disallowImports = true
	}
}

// WithLogger provides a structured logger for load operation diagnostics.
// If not provided, logging is disabled.
func WithLogger(logger *slog.Logger) Option {
	return func(c *config) {
		c.logger = logger
	}
}
