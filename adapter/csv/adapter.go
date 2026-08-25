package csv

import "github.com/simon-lentz/yammm/schema"

// adapterConfig holds CSV adapter configuration.
type adapterConfig struct {
	delimiter  rune
	hasHeader  bool
	typeColumn string // empty = no type column
	nullValue  string // string that represents nil
	listSep    string // list element separator
	schema     *schema.Schema
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

// WithListSeparator sets the separator for list elements, vector elements
// and edge-column segments, on the write side and the parse side alike.
// The default is "|". A value containing the separator survives either way:
// both sides escape through one shared helper pair.
func WithListSeparator(sep string) Option {
	return func(c *adapterConfig) {
		if sep != "" {
			c.listSep = sep
		}
	}
}

// WithSchema gives the parser the import closure it needs to coerce
// foreign-key components in edge columns: the component's constraint lives
// on the association's target type, which the per-call [*schema.Type]
// cannot reach. Without it, FK components stay strings — sufficient for
// string-keyed targets, and the validator reports the rest.
func WithSchema(s *schema.Schema) Option {
	return func(c *adapterConfig) {
		c.schema = s
	}
}
