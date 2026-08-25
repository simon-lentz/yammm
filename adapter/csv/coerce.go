package csv

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/simon-lentz/yammm/schema"
)

// coerceStringValue converts a raw CSV string to a typed Go value
// based on the schema constraint for that property.
//
// Returns (nil, nil) for null values (matching the configured null string).
// Returns (value, nil) on success.
// Returns ("", error) when the string cannot be parsed as the target type.
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
		v, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as Integer: %w", raw, err)
		}
		return v, nil

	case schema.KindFloat:
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as Float: %w", raw, err)
		}
		return v, nil

	case schema.KindBoolean:
		v, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("cannot parse %q as Boolean: %w", raw, err)
		}
		return v, nil

	case schema.KindDate:
		if _, err := time.Parse("2006-01-02", raw); err != nil {
			return nil, fmt.Errorf("cannot parse %q as Date (expected YYYY-MM-DD): %w", raw, err)
		}
		return raw, nil

	case schema.KindTimestamp:
		// Mirror the validator's rule exactly: a declared layout is the
		// only accepted form; RFC 3339 applies to the default layout alone.
		if tc, ok := c.(schema.TimestampConstraint); ok && tc.Format() != "" {
			if _, err := time.Parse(tc.Format(), raw); err != nil {
				return nil, fmt.Errorf("cannot parse %q as Timestamp (expected layout %s): %w", raw, tc.Format(), err)
			}
			return raw, nil
		}
		if _, err := time.Parse(time.RFC3339, raw); err != nil {
			if _, err2 := time.Parse(time.RFC3339Nano, raw); err2 != nil {
				return nil, fmt.Errorf("cannot parse %q as Timestamp (expected RFC 3339): %w", raw, err)
			}
		}
		return raw, nil

	case schema.KindVector:
		return a.parseVectorValue(raw)

	case schema.KindList:
		lc, ok := c.(schema.ListConstraint)
		if !ok {
			return raw, nil
		}
		return a.parseListValue(raw, lc)

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
		v, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
		if err != nil {
			return nil, fmt.Errorf("vector element %d: cannot parse %q as Float: %w", i, part, err)
		}
		result[i] = v
	}
	return result, nil
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// stripBOM returns a reader that strips a UTF-8 BOM if present.
// If no BOM is found, the original bytes are preserved.
func stripBOM(r io.Reader) io.Reader {
	buf := make([]byte, 3)
	n, err := io.ReadFull(r, buf)
	if err != nil || n < 3 {
		// Short read or error — return what we got plus the rest.
		return io.MultiReader(bytes.NewReader(buf[:n]), r)
	}
	if bytes.Equal(buf, utf8BOM) {
		return r // BOM consumed, return remaining reader.
	}
	// No BOM — prepend the 3 bytes back.
	return io.MultiReader(bytes.NewReader(buf), r)
}
