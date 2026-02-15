package path

import (
	"fmt"
	"strconv"
	"strings"
)

// Builder constructs canonical instance paths for diagnostics.
//
// A Builder is immutable; each method returns a new Builder with the
// appended segment. This enables safe concurrent use and simple sharing
// of path prefixes across validation branches.
//
// The zero value represents the root path ($); use [Root] for clarity and convention.
type Builder struct {
	segments []string
}

// Root returns a new Builder representing the root of an instance path ("$").
func Root() Builder {
	return Builder{}
}

// Index appends an array index segment to the path.
//
// Example:
//
//	path.Root().Index(0).String() // returns "$[0]"
func (b Builder) Index(i int) Builder {
	return b.append("[" + strconv.Itoa(i) + "]")
}

// Key appends an object key segment to the path.
//
// For identifier-safe keys (e.g., "name", "firstName"), dot notation is used: .key
// For other keys, bracket notation with escaping is used: ["key with spaces"]
//
// Example:
//
//	path.Root().Key("person").String()       // returns "$.person"
//	path.Root().Key("with spaces").String()  // returns `$["with spaces"]`
func (b Builder) Key(key string) Builder {
	return b.append(formatKey(key))
}

// PK appends a primary key-based index segment to the path.
//
// PK indices identify elements semantically by their primary key values
// rather than by array position. The value types are preserved in the output:
//
//   - String values are quoted: [name="Alice"]
//   - Integer values are unquoted: [id=123]
//   - Boolean values are unquoted: [active=true]
//   - Multiple fields produce composite keys: [region="us",studentId=12345]
//
// Note: The Builder does not validate that the provided PKField values
// match any particular schema. Callers are responsible for providing
// the correct and complete set of PK fields for the target type.
// Incomplete PKs will produce valid path syntax but may not uniquely
// identify an instance.
//
// Example:
//
//	path.Root().Key("Person").PK(path.PKField{Name: "id", Value: 42}).String()
//	// returns "$.Person[id=42]"
//
//	path.Root().Key("Enrollment").PK(
//	    path.PKField{Name: "region", Value: "us"},
//	    path.PKField{Name: "studentId", Value: 12345},
//	).String()
//	// returns `$.Enrollment[region="us",studentId=12345]`
func (b Builder) PK(fields ...PKField) Builder {
	if len(fields) == 0 {
		return b
	}

	var sb strings.Builder
	sb.WriteByte('[')
	for i, f := range fields {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(f.Name)
		sb.WriteByte('=')
		sb.WriteString(formatPKValue(f.Value))
	}
	sb.WriteByte(']')
	return b.append(sb.String())
}

// String returns the canonical path string.
//
// The path always starts with "$" (the root symbol), followed by all segments.
func (b Builder) String() string {
	if len(b.segments) == 0 {
		return "$"
	}
	var sb strings.Builder
	sb.WriteByte('$')
	for _, seg := range b.segments {
		sb.WriteString(seg)
	}
	return sb.String()
}

// IsRoot reports whether this builder represents the root path ("$").
func (b Builder) IsRoot() bool {
	return len(b.segments) == 0
}

// Len returns the number of segments in the path.
// The root path has length 0.
func (b Builder) Len() int {
	return len(b.segments)
}

// Parent returns a new Builder representing the parent path.
// If the builder is at the root, it returns the root.
func (b Builder) Parent() Builder {
	if len(b.segments) == 0 {
		return b
	}
	child := Builder{segments: make([]string, len(b.segments)-1)}
	copy(child.segments, b.segments[:len(b.segments)-1])
	return child
}

// Last returns the last segment of the path as a string.
// Returns an empty string if the builder is at the root.
func (b Builder) Last() string {
	if len(b.segments) == 0 {
		return ""
	}
	return b.segments[len(b.segments)-1]
}

// append is an internal helper that creates a new Builder with an added segment.
func (b Builder) append(segment string) Builder {
	child := Builder{segments: make([]string, len(b.segments), len(b.segments)+1)}
	copy(child.segments, b.segments)
	child.segments = append(child.segments, segment)
	return child
}

// formatKey returns the appropriate segment format for a key.
// Identifier-safe keys use ".key", others use `["escaped"]`.
func formatKey(key string) string {
	if isIdentifierSafe(key) {
		return "." + key
	}
	return `["` + escapeString(key) + `"]`
}

// formatPKValue formats a primary key value for path output.
// Strings are quoted, integers and booleans are unquoted.
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
		// Fallback: quote as string
		return fmt.Sprintf(`"%v"`, v)
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
// Escapes: \\ \" \n \r \t \b \f and control characters.
func escapeString(s string) string {
	// Fast path: check if escaping is needed
	needsEscape := false
	for _, r := range s {
		if r == '\\' || r == '"' || r == '\n' || r == '\r' || r == '\t' || r == '\b' || r == '\f' || r < 0x20 {
			needsEscape = true
			break
		}
	}
	if !needsEscape {
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
				sb.WriteString(fmt.Sprintf(`\u%04x`, r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}
