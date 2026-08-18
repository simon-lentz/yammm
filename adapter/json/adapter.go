package json

// adapterConfig holds JSON adapter configuration. It is currently empty:
// construction-time options were removed with the unexercised option surface,
// and [Option] stays as the extension point.
type adapterConfig struct{}

// Adapter parses JSON data into RawInstance values.
//
// Thread Safety: Adapter is safe for concurrent Parse* calls after construction.
// No shared mutable state exists; all context flows through parameters.
type Adapter struct {
	config adapterConfig
}

// Option configures Adapter behavior at construction time.
type Option func(*adapterConfig)

// New creates a new JSON adapter with the given options.
func New(opts ...Option) *Adapter {
	cfg := adapterConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Adapter{config: cfg}
}
