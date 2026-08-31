package graph

import (
	"log/slog"
)

// Option configures graph construction behavior.
type Option func(*graphConfig)

// graphConfig holds internal configuration for a Graph.
type graphConfig struct {
	logger *slog.Logger
}

// WithLogger provides a structured logger for graph operation diagnostics.
// If not provided, logging is disabled. Symmetric with
// [github.com/simon-lentz/yammm/schema.WithLogger].
func WithLogger(logger *slog.Logger) Option {
	return func(c *graphConfig) {
		c.logger = logger
	}
}
