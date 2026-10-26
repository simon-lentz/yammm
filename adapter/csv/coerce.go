package csv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/schema"
)

// coerceStringValue converts a raw CSV string to a typed Go value
// based on the schema constraint for that property.
//
// Returns (value, nil) on success, and (nil, error) when the string cannot be
// parsed as the target type. It never returns nil for success: an empty string
// is either the kind's empty rendering ("" or []) or an error.
//
// Date and Timestamp values are validated but kept as strings in the output,
// matching the JSON adapter's behavior. Temporal coercion to driver types
// happens downstream in write adapters (e.g., adapter/neo4j).
func (a *Adapter) coerceStringValue(raw string, c schema.Constraint) (any, error) {
	// Unwrap alias chains.
	c = schema.ResolveAlias(c)

	//exhaustive:enforce
	switch c.Kind() {
	case schema.KindString, schema.KindUUID, schema.KindEnum, schema.KindPattern:
		return raw, nil

	case schema.KindInteger:
		return integerFromCell(raw)

	case schema.KindFloat:
		// Returned through a nil check: a failed float64 in an any is not nil.
		v, err := floatFromCell(raw)
		if err != nil {
			return nil, err
		}
		return v, nil

	case schema.KindBoolean:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %s as Boolean: %w", diag.QuoteValue(raw), parseCause(err))
		}
		return v, nil

	case schema.KindDate:
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return nil, fmt.Errorf("cannot parse %s as Date (expected YYYY-MM-DD): %w", diag.QuoteValue(raw), parseCause(err))
		}
		return raw, nil

	case schema.KindTimestamp:
		// Mirror the validator's rule exactly: a declared layout is the
		// only accepted form; RFC 3339 applies to the default layout alone.
		if tc, ok := c.(schema.TimestampConstraint); ok && tc.Format() != "" {
			if _, err := time.Parse(tc.Format(), raw); err != nil {
				return nil, fmt.Errorf("cannot parse %s as Timestamp (expected layout %s): %w", diag.QuoteValue(raw), tc.Format(), parseCause(err))
			}
			return raw, nil
		}
		// RFC3339 reads a fractional second too: time.Parse accepts one after
		// the seconds field whether or not the layout carries it.
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			return nil, fmt.Errorf("cannot parse %s as Timestamp (expected RFC 3339): %w", diag.QuoteValue(raw), parseCause(err))
		}
		return raw, nil

	case schema.KindVector:
		// Returned through a nil check: a nil []any in an any is not nil.
		v, err := a.parseVectorValue(raw)
		if err != nil {
			return nil, err
		}
		return v, nil

	case schema.KindList:
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			return nil, fmt.Errorf("list constraint of unexpected form %T", c)
		}
		v, err := a.parseListValue(raw, lc)
		if err != nil {
			return nil, err
		}
		return v, nil

	case schema.KindAlias:
		// Unreachable in a completed schema: c is alias-resolved above. Listed to
		// satisfy the exhaustiveness guard; matches the pass-through default.
		return raw, nil

	default:
		// KindAlias should be unwrapped above; unknown kinds pass through.
		return raw, nil
	}
}

// parseListValue splits a string by the configured list separator
// and coerces each element using the list element constraint.
func (a *Adapter) parseListValue(raw string, lc schema.ListConstraint) ([]any, error) {
	if raw == "" {
		return []any{}, nil
	}

	parts := splitListElems(raw, a.config.listSep)
	elem := schema.ResolveAlias(lc.Element())

	result := make([]any, len(parts))
	for i, part := range parts {
		val, err := a.coerceStringValue(part, elem)
		if err != nil {
			return nil, fmt.Errorf("list element %d: %w", i, err)
		}
		result[i] = val
	}
	return result, nil
}

// parseVectorValue parses a separator-delimited string of floats.
// The separator is the adapter's configured list separator (default "|").
func (a *Adapter) parseVectorValue(raw string) ([]any, error) {
	if raw == "" {
		return []any{}, nil
	}

	parts := splitListElems(raw, a.config.listSep)
	result := make([]any, len(parts))
	for i, part := range parts {
		v, err := floatFromCell(part)
		if err != nil {
			return nil, fmt.Errorf("vector element %d: %w", i, err)
		}
		result[i] = v
	}
	return result, nil
}

// integerFromCell reads an Integer cell as the JSON adapter reads the literal a
// document would hold there: a JSON integer literal, read exactly, and refused
// outside int64. A float literal is never an Integer, whole or not, and a
// spelling JSON has no literal for ("+5", "007") is refused.
func integerFromCell(raw string) (any, error) {
	if !numberLiteral(raw) || !immutable.IsIntegerLiteral(json.Number(raw)) {
		return nil, fmt.Errorf("cannot parse %s as Integer: not a JSON integer literal", diag.QuoteValue(raw))
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("cannot parse %s as Integer: %w", diag.QuoteValue(raw), parseCause(err))
	}
	return v, nil
}

// floatFromCell reads a Float cell, or a Vector element, as the JSON adapter reads
// the literal: any JSON number literal, read to its nearest float64 with the
// sign of a zero kept, and refused when no float64 is finite.
func floatFromCell(raw string) (float64, error) {
	if !numberLiteral(raw) {
		return 0, fmt.Errorf("cannot parse %s as Float: not a JSON number literal", diag.QuoteValue(raw))
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("cannot parse %s as Float: %w", diag.QuoteValue(raw), parseCause(err))
	}
	return v, nil
}

// numberLiteral reports whether s is a JSON number literal (RFC 8259, section
// 6): -? (0 | [1-9][0-9]*) (.[0-9]+)? ([eE][+-]?[0-9]+)?, with nothing around
// it.
func numberLiteral(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		i = digitsFrom(s, i)
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		j := digitsFrom(s, i+1)
		if j == i+1 {
			return false
		}
		i = j
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		j := digitsFrom(s, i)
		if j == i {
			return false
		}
		i = j
	}
	return i == len(s)
}

// digitsFrom returns the index past the run of ASCII digits starting at i.
func digitsFrom(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// stripBOM returns a reader that skips a leading UTF-8 BOM and returns r's
// first error from every Read after it, and the [stickyReader] that keeps it.
func stripBOM(r io.Reader) (io.Reader, *stickyReader) {
	sticky := &stickyReader{r: r}
	head := make([]byte, len(utf8BOM))
	n, err := io.ReadFull(sticky, head) // on an error, sticky returns it from the Read past head
	head = head[:n]
	if err == nil && bytes.Equal(head, utf8BOM) {
		head = head[:0]
	}
	return io.MultiReader(bytes.NewReader(head), sticky), sticky
}

// stickyReader returns the first error its reader returns from every later
// Read. [encoding/csv.Reader] drops a read error that meets a quote fault and reads on,
// so a reader that fails once would otherwise lose its failure; the error that
// ends the head's read would be lost the same way.
type stickyReader struct {
	r   io.Reader
	err error
}

func (s *stickyReader) Read(p []byte) (int, error) {
	if s.err != nil {
		return 0, s.err
	}
	n, err := s.r.Read(p)
	s.err = err
	return n, err //nolint:wrapcheck // an io.Reader passes io.EOF through as it is: callers compare it by identity
}

// failure returns the error r returned, where [readFailure] holds of it.
func (s *stickyReader) failure() error {
	if s.err == nil || !readFailure(s.err) {
		return nil
	}
	return s.err
}

// parseCause returns what a parse of a cell failed on without the cell's text:
// strconv and time repeat the whole text they were given, and the message
// beside the cause already shows its head. A number's cause is its sentinel,
// [strconv.ErrSyntax] or [strconv.ErrRange]. Of time's messages only extra
// text repeats the cell, so its text is cut as the cell is.
func parseCause(err error) error {
	if ne, ok := errors.AsType[*strconv.NumError](err); ok {
		return ne.Err
	}
	if pe, ok := errors.AsType[*time.ParseError](err); ok {
		if strings.HasPrefix(pe.Message, ": extra text: ") {
			return errors.New("extra text: " + diag.QuoteValue(pe.ValueElem))
		}
		if pe.Message != "" {
			return errors.New(strings.TrimPrefix(pe.Message, ": "))
		}
		return fmt.Errorf("%s does not match %q", diag.QuoteValue(pe.ValueElem), pe.LayoutElem)
	}
	return err
}
