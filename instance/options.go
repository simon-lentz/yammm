package instance

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/value"
)

// Option configures the Validator.
type Option func(*validatorConfig)

// validatorConfig holds validator configuration.
type validatorConfig struct {
	logger               *slog.Logger
	strictPropertyNames  bool
	allowUnknownFields   bool
	maxIssuesPerInstance int
	valueRegistry        value.Registry
}

// defaultConfig returns the default validator configuration.
func defaultConfig() *validatorConfig {
	return &validatorConfig{
		strictPropertyNames:  false,
		allowUnknownFields:   false,
		maxIssuesPerInstance: 100,
	}
}

// WithStrictPropertyNames controls property name matching.
//
// When true, property names must match exactly (case-sensitive).
// When false (default), property names are matched case-insensitively.
func WithStrictPropertyNames(strict bool) Option {
	return func(c *validatorConfig) {
		c.strictPropertyNames = strict
	}
}

// WithAllowUnknownFields controls handling of unknown fields.
//
// When true, unknown fields in the input are silently ignored.
// When false (default), unknown fields produce a diagnostic error.
func WithAllowUnknownFields(allow bool) Option {
	return func(c *validatorConfig) {
		c.allowUnknownFields = allow
	}
}

// RecommendedOptions returns the recommended default options
// for new projects. These options prioritize correctness and early error
// detection over permissiveness.
//
// Includes:
//   - WithStrictPropertyNames(true): Require exact case matching for property names
//
// Use this as a starting point and relax specific options as needed for your use case.
func RecommendedOptions() []Option {
	return []Option{
		WithStrictPropertyNames(true),
		WithAllowUnknownFields(false),
	}
}

// applyOptions applies the given options to a config.
func applyOptions(opts []Option) *validatorConfig {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
