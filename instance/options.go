package instance

// Option configures the Validator.
type Option func(*validatorConfig)

// validatorConfig holds validator configuration.
type validatorConfig struct {
	maxIssuesPerInstance int
}

// defaultConfig returns the default validator configuration.
func defaultConfig() *validatorConfig {
	return &validatorConfig{maxIssuesPerInstance: 100}
}

// WithIssueLimit sets the maximum number of diagnostic issues one instance
// stores, with 0 for unlimited and 100 by default. Reaching the limit stops no
// validation: the collector keeps the most severe issues seen.
// [github.com/simon-lentz/yammm/diag.Result.DroppedCount] and
// [github.com/simon-lentz/yammm/diag.Result.LimitReached] count the drops exactly,
// on a batch result as well. A cancelled row is the exception, as
// [Validator.Validate] states.
func WithIssueLimit(limit int) Option {
	return func(c *validatorConfig) {
		c.maxIssuesPerInstance = max(limit, 0)
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
