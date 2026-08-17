package csv

// adapterConfig holds CSV adapter configuration.
type adapterConfig struct {
	delimiter  rune
	hasHeader  bool
	typeColumn string // empty = no type column
	nullValue  string // string that represents nil
	listSep    string // list element separator
}

// Adapter parses CSV data into RawInstance values and serializes
// validated instances to CSV.
//
// Thread Safety: Adapter is safe for concurrent use after construction.
// Configuration is immutable; all context flows through parameters.
type Adapter struct {
	config adapterConfig
}

// Option configures Adapter behavior at construction time.
type Option func(*adapterConfig)

// New creates a new CSV adapter with the given options.
func New(opts ...Option) *Adapter {
	cfg := adapterConfig{
		delimiter: ',',
		hasHeader: true,
		nullValue: "",
		listSep:   "|",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Adapter{config: cfg}
}

// WithTypeColumn sets the column name used for type discrimination
// in [Adapter.ParseWithTypeColumn]. Required for that method; ignored
// by [Adapter.ParseTyped].
func WithTypeColumn(name string) Option {
	return func(c *adapterConfig) {
		c.typeColumn = name
	}
}
