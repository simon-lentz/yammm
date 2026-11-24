package path

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Path is an instance path's text, such as "$.Person[id=42]". A Path is kept
// as written: a path read from a document is that document's text, whether or
// not this package would spell it so, and [Path.Key], [Path.Index] and
// [Path.PK] append to the text as it is.
//
// The zero Path is the empty text, not the root; start a path from [Root].
type Path string

// Root is the path of an instance itself.
const Root Path = "$"

// Index appends an array index segment to the path. It panics on a negative
// index, which the grammar does not spell.
//
//	path.Root.Index(0).String() // returns "$[0]"
func (p Path) Index(i int) Path {
	if i < 0 {
		panic(fmt.Sprintf("path.Path.Index: negative index %d; the grammar spells none", i))
	}
	return p + Path("["+strconv.Itoa(i)+"]")
}

// Key appends an object key segment to the path.
//
// For identifier-safe keys (e.g., "name", "firstName"), dot notation is used: .key
// For other keys, bracket notation with escaping is used: ["key with spaces"]
//
// Example:
//
//	path.Root.Key("person").String()       // returns "$.person"
//	path.Root.Key("with spaces").String()  // returns `$["with spaces"]`
func (p Path) Key(key string) Path {
	return p + Path(formatKey(key))
}

// PK appends a primary key-based index segment: one bracket pair holding a
// field=value entry per field, comma-separated, in the order given, such as
// [region="us",studentId=12345]. With no fields it appends nothing. The package
// documentation states how each value is written.
// The fields are not checked against a schema. It panics on a field name that
// is not an identifier, and on a NaN or infinite float value: the grammar
// spells neither.
//
//	path.Root.Key("Person").PK(path.PKField{Name: "id", Value: 42}) // $.Person[id=42]
func (p Path) PK(fields ...PKField) Path {
	if len(fields) == 0 {
		return p
	}

	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range fields {
		if !isIdentifierSafe(f.Name) {
			panic(fmt.Sprintf("path.Path.PK: field name %q is not an identifier; the grammar spells none", f.Name))
		}
		if !isSpellableFloat(f.Value) {
			panic(fmt.Sprintf("path.Path.PK: field %q holds %v, which the grammar does not spell", f.Name, f.Value))
		}
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(f.Name)
		sb.WriteByte('=')
		sb.WriteString(formatPKValue(f.Value))
	}
	sb.WriteByte(']')
	return p + Path(sb.String())
}

// String returns the path's text as it is held.
func (p Path) String() string {
	return string(p)
}

// formatKey returns the appropriate segment format for a key.
// Identifier-safe keys use ".key", others use `["escaped"]`.
func formatKey(key string) string {
	if isIdentifierSafe(key) {
		return "." + key
	}
	return `["` + escapeString(key) + `"]`
}

// formatPKValue formats a primary key value for path output. Strings are
// quoted; integers and booleans are unquoted; a float is unquoted, with ".0"
// appended when it has no fraction, which the grammar reads as a float64; any
// other value is written as the quoted, escaped text of its fmt.Sprint form,
// which the grammar reads as a string.
func formatPKValue(v any) string {
	switch val := v.(type) {
	case string:
		return `"` + escapeString(val) + `"`
	case bool:
		if val {
			return "true"
		}
		return "false"
	case int:
		return strconv.Itoa(val)
	case int8:
		return strconv.FormatInt(int64(val), 10)
	case int16:
		return strconv.FormatInt(int64(val), 10)
	case int32:
		return strconv.FormatInt(int64(val), 10)
	case int64:
		return strconv.FormatInt(val, 10)
	case uint:
		return strconv.FormatUint(uint64(val), 10)
	case uint8:
		return strconv.FormatUint(uint64(val), 10)
	case uint16:
		return strconv.FormatUint(uint64(val), 10)
	case uint32:
		return strconv.FormatUint(uint64(val), 10)
	case uint64:
		return strconv.FormatUint(val, 10)
	case float32:
		s := strconv.FormatFloat(float64(val), 'f', -1, 32)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	case float64:
		s := strconv.FormatFloat(val, 'f', -1, 64)
		if !strings.Contains(s, ".") {
			s += ".0"
		}
		return s
	default:
		return `"` + escapeString(fmt.Sprint(v)) + `"`
	}
}

// isSpellableFloat reports whether v is a value the grammar spells. A NaN or an
// infinity would be written "NaN.0" or "+Inf.0", which the grammar does not
// spell; every other value formatPKValue writes does.
func isSpellableFloat(v any) bool {
	switch f := v.(type) {
	case float32:
		return !math.IsNaN(float64(f)) && !math.IsInf(float64(f), 0)
	case float64:
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	default:
		return true
	}
}

// isIdentifierSafe reports whether key can be used with dot notation.
//
// A key is identifier-safe if it:
//   - Is non-empty
//   - Starts with a letter (a-z, A-Z) or underscore
//   - Contains only letters, digits (0-9), and underscores
func isIdentifierSafe(key string) bool {
	if len(key) == 0 {
		return false
	}

	for i, r := range key {
		if i == 0 {
			if !isLetter(r) && r != '_' {
				return false
			}
		} else {
			if !isLetter(r) && !isDigit(r) && r != '_' {
				return false
			}
		}
	}
	return true
}

// isLetter reports whether r is an ASCII letter (a-z, A-Z).
func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

// isDigit reports whether r is an ASCII digit (0-9).
func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

// escapeString returns s with JSON escape sequences applied per RFC 8259.
// Escapes: \\ \" \n \r \t \b \f and control characters. Invalid UTF-8 is
// written as U+FFFD, the rune a JSON string decoder reads it as.
func escapeString(s string) string {
	if utf8.ValidString(s) && !strings.ContainsFunc(s, func(r rune) bool {
		return r == '\\' || r == '"' || r < 0x20
	}) {
		return s
	}

	var sb strings.Builder
	sb.Grow(len(s) + 8)
	for _, r := range s {
		switch r {
		case '\\':
			sb.WriteString(`\\`)
		case '"':
			sb.WriteString(`\"`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				// Control character: use \uXXXX
				fmt.Fprintf(&sb, `\u%04x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}
