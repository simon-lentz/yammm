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

// WithIssueLimit sets the maximum number of failures the error holds for one
// instance, with 0 for unlimited and 100 by default. Reaching the limit stops no
// validation: the error keeps the most severe failures seen, and its
// [github.com/simon-lentz/yammm/diag.Result.DroppedCount] and
// [github.com/simon-lentz/yammm/diag.Result.LimitReached] count the drops
// exactly, over a batch as well, save a cancelled row's, as [Validator.Validate]
// states.
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
