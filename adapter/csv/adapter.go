package csv

import (
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/adapter/internal/refusal"
	"github.com/simon-lentz/yammm/schema"
)

// adapterConfig holds CSV adapter configuration.
type adapterConfig struct {
	delimiter  rune
	typeColumn string // empty = no type column
	schema     schema.Schema
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
// starts: as an Error [E_CSV_CONFIG] diagnostic, or as an error marked
// [ErrConfig], each naming the delimiter and quoting [encoding/csv]'s refusal
// text.
func WithDelimiter(r rune) Option {
	return func(c *adapterConfig) {
		c.delimiter = r
	}
}

// WithTypeColumn sets the column name used for type discrimination
// in [Adapter.ParseWithTypeColumn]. That method needs a name: with none, or
// with one holding a CR LF, which [encoding/csv] reads back as LF so that no
// header holds it, its result carries an Error [E_CSV_CONFIG] and no byte is
// read. [Adapter.ParseTyped] and [Adapter.MarshalSnapshot] do not read the option.
func WithTypeColumn(name string) Option {
	return func(c *adapterConfig) {
		c.typeColumn = name
	}
}

// WithSchema gives the parser the import closure, and with it each
// association's target type, which the per-call [schema.Type] cannot reach.
// The parser needs the target's primary keys to read an empty foreign-key
// segment: with them, it is the key's empty value where the key's kind has one
// and is otherwise absent, and an empty column naming no key of the target is
// skipped. Without it every empty foreign-key segment is absent. A non-empty
// segment keeps its text either way, except that with it a Date or Timestamp
// key that does not parse draws E_CSV_COERCE and is an [instance.Refused]. s must be the schema that owns the
// [schema.Type] the parse methods are given: a target is resolved in s by the
// association's identity, and another schema's type of that name is another type.
func WithSchema(s schema.Schema) Option {
	return func(c *adapterConfig) {
		c.schema = s
	}
}

// configError refuses a delimiter [encoding/csv] refuses, before a parse reads a
// byte and before a write starts. It carries [ErrConfig]; a parse reports the
// same text under [E_CSV_CONFIG]. A setting is knowable before any input or
// output, so both sides ask here and nowhere later.
func (a *Adapter) configError() error {
	return delimiterError(a.config.delimiter)
}

// delimiterError refuses a delimiter [encoding/csv] refuses, asked of the
// package itself so this adapter never restates its rule, and quoting its text.
// The text is all a caller can have: encoding/csv does not export that error.
func delimiterError(delim rune) error {
	probe := csv.NewReader(strings.NewReader(""))
	probe.Comma = delim
	if _, err := probe.Read(); !errors.Is(err, io.EOF) {
		return refusal.New(ErrConfig, "csv adapter: delimiter %s: %s", runeText(delim), err)
	}
	return nil
}

// runeText names r in a message. %q writes U+FFFD for every value that is not
// a Unicode scalar value, so such a value is written by its number instead.
func runeText(r rune) string {
	switch {
	case utf8.ValidRune(r):
		return strconv.QuoteRune(r)
	case r < 0:
		return fmt.Sprintf("%d (not a Unicode scalar value)", r)
	default:
		return fmt.Sprintf("%U (not a Unicode scalar value)", r)
	}
}
