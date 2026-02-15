package graph

import (
	"log/slog"
)

// GraphOption configures graph construction behavior.
type GraphOption func(*graphConfig)

// graphConfig holds internal configuration for a Graph.
type graphConfig struct {
	logger *slog.Logger
}

// WithLogger enables debug logging for graph operations.
//
// When set, the graph logs detailed information about:
//   - Instance additions (type, primary key)
//   - Edge resolution (source, target, relation)
//   - Forward reference handling
//   - Duplicate detection
//   - Check operations
//
// Pass nil to disable logging (the default).
func WithLogger(logger *slog.Logger) GraphOption {
	return func(cfg *graphConfig) {
		cfg.logger = logger
	}
}
