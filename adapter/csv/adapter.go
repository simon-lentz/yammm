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

// WithDelimiter sets the field delimiter. Default is comma (',').
// Use '\t' for TSV files.
func WithDelimiter(r rune) Option {
	return func(c *adapterConfig) {
		c.delimiter = r
	}
}

// WithHeader configures whether input CSV has a header row.
// Default is true. When false, column indices are used as property names
// ("0", "1", "2", ...).
func WithHeader(has bool) Option {
	return func(c *adapterConfig) {
		c.hasHeader = has
	}
}

// WithTypeColumn sets the column name used for type discrimination
// in [Adapter.ParseWithTypeColumn]. Required for that method; ignored
// by [Adapter.ParseTyped] and [Adapter.ParseOne].
func WithTypeColumn(name string) Option {
	return func(c *adapterConfig) {
		c.typeColumn = name
	}
}

// WithNullValue sets the string that represents a null value.
// Default is the empty string "". Cells matching this value exactly
// produce nil in the property map.
func WithNullValue(s string) Option {
	return func(c *adapterConfig) {
		c.nullValue = s
	}
}

// WithListSeparator sets the separator for list property values.
// Default is "|". A cell "a|b|c" with a List<String> constraint
// produces []any{"a", "b", "c"}.
func WithListSeparator(sep string) Option {
	return func(c *adapterConfig) {
		c.listSep = sep
	}
}
