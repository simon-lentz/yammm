package csv

import (
	"encoding/csv"
	"errors"
	"io"
	"strings"

	"github.com/simon-lentz/yammm/adapter/internal/refusal"
	"github.com/simon-lentz/yammm/schema"
)

// adapterConfig holds CSV adapter configuration.
type adapterConfig struct {
	delimiter  rune
	typeColumn string // empty = no type column
	listSep    string // "" only outside New; the zero delimiter refuses before any list
	schema     *schema.Schema
	strict     bool // match names exactly, as a strict validator does
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
		listSep:   "|",
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return &Adapter{config: cfg}
}

// WithDelimiter sets the field delimiter for the parse side and the write side
// alike. The default is ','; a TSV file takes '\t', and keeps [encoding/csv]'s
// quoting. A delimiter [encoding/csv] refuses — 0, '"', '\r', '\n',
// U+FFFD or an invalid rune — is refused before a parse reads or a write
// requests a writer, as a refused list separator is: as an Error
// [E_CSV_CONFIG] diagnostic, or as an error marked [ErrConfig], each carrying
// [encoding/csv]'s own refusal.
func WithDelimiter(r rune) Option {
	return func(c *adapterConfig) {
		c.delimiter = r
	}
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
// The default is "|", and an empty separator is ignored, so the separator
// already set is kept. An element holding any part of the separator splits back unchanged, at
// every depth of a nested list. A separator that begins
// with a backslash, the escape character, or holds a CR LF, which
// [encoding/csv] reads back as LF, is refused before a parse reads or a write
// requests a writer: as an Error [E_CSV_CONFIG] diagnostic, or as an error
// marked [ErrConfig].
func WithListSeparator(sep string) Option {
	return func(c *adapterConfig) {
		if sep != "" {
			c.listSep = sep
		}
	}
}

// WithStrictPropertyNames matches column names exactly, as
// [instance.WithStrictPropertyNames] makes the validator match keys; the
// default folds them, as the validator's default does. Give the parser the
// validator's setting: a column the parser resolves and the validator does not
// is reported as an unknown field.
func WithStrictPropertyNames(strict bool) Option {
	return func(c *adapterConfig) {
		c.strict = strict
	}
}

// WithSchema gives the parser the import closure, and with it each
// association's target type, which the per-call [*schema.Type] cannot reach.
// The parser needs the target's primary keys to read an empty foreign-key
// segment: with them, it is the key's empty value where the key's kind has one
// and is otherwise absent, and an empty column naming no key of the target is
// skipped. Without it every empty foreign-key segment is absent. A non-empty
// segment keeps its text either way, except that with it a Date or Timestamp
// key that does not parse draws E_CSV_COERCE. s must be the schema that owns the
// [*schema.Type] the parse methods are given: a target is resolved in s by the
// association's identity, and another schema's type of that name is another type.
func WithSchema(s *schema.Schema) Option {
	return func(c *adapterConfig) {
		c.schema = s
	}
}

// configError refuses a setting this adapter cannot use, before a parse reads a
// byte and before a write requests a writer: a list separator the parser could
// not find again, or a delimiter [encoding/csv] refuses. It carries [ErrConfig];
// a parse reports the same text under [E_CSV_CONFIG]. A setting is knowable
// before any input or output, so both sides ask here and nowhere later.
func (a *Adapter) configError() error {
	if err := listSepError(a.config.listSep); err != nil {
		return err
	}
	return delimiterError(a.config.delimiter)
}

// delimiterError refuses a delimiter [encoding/csv] refuses, asked of the
// package itself so this adapter never restates its rule, and carrying its text.
func delimiterError(delim rune) error {
	probe := csv.NewReader(strings.NewReader(""))
	probe.Comma = delim
	if _, err := probe.Read(); !errors.Is(err, io.EOF) {
		return refusal.New(ErrConfig, "csv adapter: delimiter %q: %s", delim, err)
	}
	return nil
}
