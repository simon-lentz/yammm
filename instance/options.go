package instance

import "log/slog"

// Option configures the Validator.
type Option func(*validatorConfig)

// validatorConfig holds validator configuration.
type validatorConfig struct {
	logger               *slog.Logger
	strictPropertyNames  bool
	allowUnknownFields   bool
	maxIssuesPerInstance int
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

// WithIssueLimit sets the maximum number of diagnostic issues one instance
// stores. Validation always completes: past the limit the collector keeps the
// most severe issues seen, and [github.com/simon-lentz/yammm/diag.Result.DroppedCount]
// and [github.com/simon-lentz/yammm/diag.Result.LimitReached] report the rest
// exactly, on the batch result too. Set to 0 for unlimited. Default is 100,
// matching [github.com/simon-lentz/yammm/schema.WithIssueLimit].
func WithIssueLimit(limit int) Option {
	return func(c *validatorConfig) {
		c.maxIssuesPerInstance = max(limit, 0)
	}
}

// WithLogger provides a structured logger for validation diagnostics: a
// debug record when a property name is normalized, and — at Debug — the
// evaluator's per-node trace of every invariant it evaluates, one record per
// s-expression plus the operation's start and end. If not provided, logging
// is disabled. Symmetric with [github.com/simon-lentz/yammm/schema.WithLogger].
func WithLogger(logger *slog.Logger) Option {
	return func(c *validatorConfig) {
		c.logger = logger
	}
}

// RecommendedOptions returns the recommended default options
// for new projects. These options prioritize correctness and early error
// detection over permissiveness.
//
// Includes:
//   - WithStrictPropertyNames(true): require exact case matching for property names
//   - WithAllowUnknownFields(false): report unknown fields
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
