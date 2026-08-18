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
