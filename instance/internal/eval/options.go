package eval

import "log/slog"

// Option configures the Evaluator.
type Option func(*evalConfig)

// evalConfig holds evaluator configuration.
type evalConfig struct {
	logger *slog.Logger
}

// WithLogger sets the logger for debug output during evaluation.
// If not set, no logging is performed.
func WithLogger(logger *slog.Logger) Option {
	return func(c *evalConfig) {
		c.logger = logger
	}
}

// applyOptions applies the given options to a config.
func applyOptions(opts []Option) *evalConfig {
	cfg := &evalConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
