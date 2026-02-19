package load

import (
	"errors"
	"log/slog"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// ErrSourceStoreNotSupported is returned when WithSourceRegistry is called
// with a SourceStore implementation that is not *source.Registry.
//
// The current implementation requires *source.Registry for full functionality.
// Custom SourceStore implementations may be supported in future versions.
// Use source.NewRegistry() for compatibility.
var ErrSourceStoreNotSupported = errors.New("custom SourceStore implementation not supported; use *source.Registry")

// Option configures the behavior of Load functions.
type Option func(*config)

// config holds configuration for schema loading.
type config struct {
	registry        *schema.Registry
	moduleRoot      string
	issueLimit      int
	sourceRegistry  SourceStore
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
// For LoadString() and LoadSources(), the module root is inferred or provided directly.
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

// SourceStore provides source content and position information.
// This interface abstracts the source registry for testability and LSP integration.
// The interface is designed to be compatible with *source.Registry.
type SourceStore interface {
	// Register adds source content for a file. Implementations should handle
	// re-registration gracefully (e.g., return error or no-op if already registered).
	Register(sourceID location.SourceID, content []byte) error
	// PositionAt converts a byte offset to a position.
	// Returns a zero Position if the source or offset is invalid.
	// Use Position.IsZero() to check for "not found".
	PositionAt(sourceID location.SourceID, byteOffset int) location.Position
	// RuneToByteOffset converts a rune offset to a byte offset.
	// Returns (offset, false) if the source or rune offset is invalid.
	RuneToByteOffset(sourceID location.SourceID, runeOffset int) (int, bool)
}

// WithSourceRegistry provides a custom source registry for position tracking.
// If not provided, a new source registry is created for the load operation.
//
// IMPORTANT: Currently only *source.Registry is supported. Passing a custom
// SourceStore implementation will cause Load/LoadSources/LoadString to return
// ErrSourceStoreNotSupported. This limitation exists because the internal
// implementation requires source.Registry-specific functionality.
//
// For compatibility, use source.NewRegistry() to create the store.
func WithSourceRegistry(store SourceStore) Option {
	return func(c *config) {
		c.sourceRegistry = store
	}
}

// WithDisallowImports prevents import declarations from being processed.
// When enabled, any import statements in the source produce an
// E_IMPORT_NOT_ALLOWED diagnostic. Used by LoadString (unconditionally)
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
