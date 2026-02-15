package instance

import (
	"log/slog"

	"github.com/simon-lentz/yammm/internal/value"
)

// ValidatorOption configures the Validator.
type ValidatorOption func(*validatorConfig)

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

// WithLogger sets the logger for debug output during validation.
// If not set, no logging is performed.
func WithLogger(logger *slog.Logger) ValidatorOption {
	return func(c *validatorConfig) {
		c.logger = logger
	}
}

// WithStrictPropertyNames controls property name matching.
//
// When true, property names must match exactly (case-sensitive).
// When false (default), property names are matched case-insensitively.
func WithStrictPropertyNames(strict bool) ValidatorOption {
	return func(c *validatorConfig) {
		c.strictPropertyNames = strict
	}
}

// WithAllowUnknownFields controls handling of unknown fields.
//
// When true, unknown fields in the input are silently ignored.
// When false (default), unknown fields produce a diagnostic error.
func WithAllowUnknownFields(allow bool) ValidatorOption {
	return func(c *validatorConfig) {
		c.allowUnknownFields = allow
	}
}

// WithMaxIssuesPerInstance sets the maximum number of issues to collect
// per instance before stopping validation of that instance.
// Default is 100.
func WithMaxIssuesPerInstance(max int) ValidatorOption {
	return func(c *validatorConfig) {
		if max > 0 {
			c.maxIssuesPerInstance = max
		}
	}
}

// WithValueRegistry sets a custom value registry for type classification.
// This enables recognition of custom Go types (e.g., `type MyInt int64`)
// during constraint checking.
//
// Note: The registry affects type classification in CheckValue, allowing
// custom types to pass type checks. However, CoerceValue converts values
// to canonical Go types (int64, float64, etc.), so the original custom
// type information is not preserved in validated instances.
//
// A zero-value Registry uses built-in type detection only.
func WithValueRegistry(reg value.Registry) ValidatorOption {
	return func(c *validatorConfig) {
		c.valueRegistry = reg
	}
}

// RecommendedValidatorOptions returns the recommended default options
// for new projects. These options prioritize correctness and early error
// detection over permissiveness.
//
// Includes:
//   - WithStrictPropertyNames(true): Require exact case matching for property names
//
// Use this as a starting point and relax specific options as needed for your use case.
func RecommendedValidatorOptions() []ValidatorOption {
	return []ValidatorOption{
		WithStrictPropertyNames(true),
		WithAllowUnknownFields(false),
	}
}

// applyOptions applies the given options to a config.
func applyOptions(opts []ValidatorOption) *validatorConfig {
	cfg := defaultConfig()
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
