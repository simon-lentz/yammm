package path

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/schema"
)

// Parse parses a path string into a Builder.
//
// The path must start with "$" and may contain:
//   - Property keys: $.name or $["complex key"]
//   - Array indices: $[0], $[42] (zero-based, non-negative)
//   - PK-based indices: $[id=123], $[name="Alice"], $[a="x",b=1]
//
// Parse does not validate PK field names or value types against a schema.
// Use [ParseWithSchema] for schema-validated parsing.
//
// Returns an error if the path syntax is invalid.
func Parse(s string) (Builder, error) {
	if s == "" {
		return Builder{}, errors.New("empty path")
	}
	if s[0] != '$' {
		return Builder{}, errors.New("path must start with '$'")
	}

	// Root path
	if s == "$" {
		return Root(), nil
	}

	b := Root()
	pos := 1 // Skip the '$'

	for pos < len(s) {
		switch s[pos] {
		case '.':
			// Dot notation: .key
			pos++
			if pos >= len(s) {
				return Builder{}, errors.New("unexpected end after '.'")
			}
			key, newPos, err := parseIdentifier(s, pos)
			if err != nil {
				return Builder{}, err
			}
			b = b.Key(key)
			pos = newPos

		case '[':
			// Bracket notation: ["key"], [0], [id=123], [a="x",b=1]
			pos++
			if pos >= len(s) {
				return Builder{}, errors.New("unexpected end after '['")
			}

			// Check what kind of bracket content
			switch {
			case s[pos] == '"':
				// String key: ["key"]
				str, newPos, err := parseQuotedString(s, pos)
				if err != nil {
					return Builder{}, err
				}
				pos = newPos
				if pos >= len(s) || s[pos] != ']' {
					return Builder{}, errors.New("expected ']' after string key")
				}
				pos++
				b = b.Key(str)
			case isDigit(rune(s[pos])) || s[pos] == '-':
				// Could be array index [0] or start of PK [id=123]
				// Look ahead to see if there's an '=' sign
				if containsBeforeClose(s[pos:], '=') {
					// PK-based index
					fields, newPos, err := parsePKFields(s, pos)
					if err != nil {
						return Builder{}, err
					}
					b = b.PK(fields...)
					pos = newPos
				} else {
					// Array index
					idx, newPos, err := parseInteger(s, pos)
					if err != nil {
						return Builder{}, err
					}
					if idx < 0 {
						return Builder{}, fmt.Errorf("negative array index %d not allowed", idx)
					}
					pos = newPos
					if pos >= len(s) || s[pos] != ']' {
						return Builder{}, errors.New("expected ']' after array index")
					}
					pos++
					b = b.Index(idx)
				}
			case isLetter(rune(s[pos])) || s[pos] == '_':
				// PK-based index: [id=123] or [name="Alice"]
				fields, newPos, err := parsePKFields(s, pos)
				if err != nil {
					return Builder{}, err
				}
				b = b.PK(fields...)
				pos = newPos
			default:
				return Builder{}, fmt.Errorf("unexpected character '%c' in bracket", s[pos])
			}

		default:
			return Builder{}, fmt.Errorf("unexpected character '%c' at position %d", s[pos], pos)
		}
	}

	return b, nil
}

// parseIdentifier parses an identifier starting at pos.
// Returns the identifier, the new position, and any error.
func parseIdentifier(s string, pos int) (string, int, error) {
	start := pos
	for pos < len(s) {
		r, size := utf8.DecodeRuneInString(s[pos:])
		if pos == start {
			if !isLetter(r) && r != '_' {
				return "", pos, fmt.Errorf("identifier must start with letter or underscore, got '%c'", r)
			}
		} else {
			if !isLetter(r) && !isDigit(r) && r != '_' {
				break
			}
		}
		pos += size
	}
	if pos == start {
		return "", pos, errors.New("empty identifier")
	}
	return s[start:pos], pos, nil
}

// parseQuotedString parses a quoted string starting at pos (which should be at the opening quote).
// Returns the unescaped string, the position after the closing quote, and any error.
func parseQuotedString(s string, pos int) (string, int, error) {
	if pos >= len(s) || s[pos] != '"' {
		return "", pos, errors.New("expected '\"'")
	}
	pos++ // Skip opening quote

	var sb strings.Builder
	for pos < len(s) {
		if s[pos] == '"' {
			pos++ // Skip closing quote
			return sb.String(), pos, nil
		}
		if s[pos] == '\\' {
			if pos+1 >= len(s) {
				return "", pos, errors.New("unexpected end in escape sequence")
			}
			pos++
			switch s[pos] {
			case '"':
				sb.WriteByte('"')
			case '\\':
				sb.WriteByte('\\')
			case 'n':
				sb.WriteByte('\n')
			case 'r':
				sb.WriteByte('\r')
			case 't':
				sb.WriteByte('\t')
			case 'b':
				sb.WriteByte('\b') // U+0008 backspace
			case 'f':
				sb.WriteByte('\f') // U+000C form feed
			case 'u':
				// Unicode escape: \uXXXX
				if pos+4 >= len(s) {
					return "", pos, errors.New("incomplete unicode escape")
				}
				hex := s[pos+1 : pos+5]
				codepoint, err := strconv.ParseUint(hex, 16, 16)
				if err != nil {
					return "", pos, fmt.Errorf("invalid unicode escape: %s", hex)
				}
				sb.WriteRune(rune(codepoint))
				pos += 4
			default:
				return "", pos, fmt.Errorf("unknown escape sequence: \\%c", s[pos])
			}
			pos++
		} else {
			r, size := utf8.DecodeRuneInString(s[pos:])
			sb.WriteRune(r)
			pos += size
		}
	}
	return "", pos, errors.New("unterminated string")
}

// parseInteger parses an integer starting at pos.
func parseInteger(s string, pos int) (int, int, error) {
	start := pos
	if pos < len(s) && s[pos] == '-' {
		pos++
	}
	for pos < len(s) && isDigit(rune(s[pos])) {
		pos++
	}
	if pos == start || (pos == start+1 && s[start] == '-') {
		return 0, pos, errors.New("expected integer")
	}
	val, err := strconv.Atoi(s[start:pos])
	if err != nil {
		return 0, pos, fmt.Errorf("invalid integer: %w", err)
	}
	return val, pos, nil
}

// parsePKFields parses comma-separated PK fields like "id=123" or "a=\"x\",b=1".
// pos should be at the start of the first field name.
// Returns the fields, the position after the closing ']', and any error.
func parsePKFields(s string, pos int) ([]PKField, int, error) {
	var fields []PKField

	for {
		// Parse field name (identifier)
		name, newPos, err := parseIdentifier(s, pos)
		if err != nil {
			return nil, pos, fmt.Errorf("PK field name: %w", err)
		}
		pos = newPos

		// Expect '='
		if pos >= len(s) || s[pos] != '=' {
			return nil, pos, errors.New("expected '=' after PK field name")
		}
		pos++

		// Parse value
		var value any
		if pos >= len(s) {
			return nil, pos, errors.New("expected PK value")
		}

		switch {
		case s[pos] == '"':
			// String value
			str, newPos, err := parseQuotedString(s, pos)
			if err != nil {
				return nil, pos, fmt.Errorf("PK string value: %w", err)
			}
			value = str
			pos = newPos
		case s[pos] == 't' && pos+4 <= len(s) && s[pos:pos+4] == "true":
			// Boolean true
			value = true
			pos += 4
		case s[pos] == 'f' && pos+5 <= len(s) && s[pos:pos+5] == "false":
			// Boolean false
			value = false
			pos += 5
		case isDigit(rune(s[pos])) || s[pos] == '-' || s[pos] == '.':
			// Numeric value (could be int or float)
			numStr, newPos, err := parseNumber(s, pos)
			if err != nil {
				return nil, pos, fmt.Errorf("PK numeric value: %w", err)
			}
			pos = newPos

			// Determine if int or float
			if strings.Contains(numStr, ".") || strings.Contains(numStr, "e") || strings.Contains(numStr, "E") {
				f, err := strconv.ParseFloat(numStr, 64)
				if err != nil {
					return nil, pos, fmt.Errorf("invalid float: %w", err)
				}
				value = f
			} else {
				i, err := strconv.ParseInt(numStr, 10, 64)
				if err != nil {
					return nil, pos, fmt.Errorf("invalid integer: %w", err)
				}
				value = i
			}
		default:
			return nil, pos, fmt.Errorf("unexpected character '%c' in PK value", s[pos])
		}

		fields = append(fields, PKField{Name: name, Value: value})

		// Check for more fields or end
		if pos >= len(s) {
			return nil, pos, errors.New("expected ']' or ','")
		}
		if s[pos] == ']' {
			pos++
			break
		}
		if s[pos] == ',' {
			pos++
			continue
		}
		return nil, pos, fmt.Errorf("expected ']' or ',' after PK value, got '%c'", s[pos])
	}

	return fields, pos, nil
}

// parseNumber parses a JSON number (integer or float) starting at pos.
func parseNumber(s string, pos int) (string, int, error) {
	start := pos

	// Optional negative sign
	if pos < len(s) && s[pos] == '-' {
		pos++
	}

	// Integer part
	switch {
	case pos < len(s) && s[pos] == '0':
		pos++
	case pos < len(s) && isDigit(rune(s[pos])):
		for pos < len(s) && isDigit(rune(s[pos])) {
			pos++
		}
	default:
		return "", start, errors.New("expected digit in number")
	}

	// Fractional part
	if pos < len(s) && s[pos] == '.' {
		pos++
		if pos >= len(s) || !isDigit(rune(s[pos])) {
			return "", start, errors.New("expected digit after decimal point")
		}
		for pos < len(s) && isDigit(rune(s[pos])) {
			pos++
		}
	}

	// Exponent part
	if pos < len(s) && (s[pos] == 'e' || s[pos] == 'E') {
		pos++
		if pos < len(s) && (s[pos] == '+' || s[pos] == '-') {
			pos++
		}
		if pos >= len(s) || !isDigit(rune(s[pos])) {
			return "", start, errors.New("expected digit in exponent")
		}
		for pos < len(s) && isDigit(rune(s[pos])) {
			pos++
		}
	}

	return s[start:pos], pos, nil
}

// containsBeforeClose checks if char appears before the first ']' in s.
// Used to distinguish array indices [0] from PK indices [id=123].
//
// Note: PK syntax requires no whitespace around '='. Input like "[id = 123]"
// will be parsed as an array index (and likely fail numeric parsing).
// This is intentional: Builder.String() always emits canonical form without
// spaces, and round-trip fidelity requires consistent syntax.
func containsBeforeClose(s string, char byte) bool {
	for i := range len(s) {
		if s[i] == ']' {
			return false
		}
		if s[i] == char {
			return true
		}
	}
	return false
}

// ParseWithSchema parses a path string and validates PK segments against the schema.
//
// Unlike [Parse], this function validates:
//   - PK field names exist as primary key properties on the referenced type
//   - PK value types match schema-declared constraint types
//
// The path format is the same as [Parse]. After the root ($), the first key
// segment is interpreted as a type name. PK segments following a type name
// are validated against that type's primary key properties.
//
// Type matching rules for PK values:
//   - Integer schema type: accepts int64 from parser, rejects quoted string
//   - Float schema type: accepts float64 from parser
//   - Boolean schema type: accepts bool from parser, rejects quoted string
//   - String/UUID/Timestamp/Date schema types: accept string from parser
//
// Returns an error if:
//   - Path syntax is invalid (same errors as [Parse])
//   - Type name is not found in schema
//   - PK field name is not a primary key on the type
//   - PK value type doesn't match schema constraint type
func ParseWithSchema(s string, sch *schema.Schema) (Builder, error) {
	// First parse without schema validation
	b, err := Parse(s)
	if err != nil {
		return Builder{}, err
	}

	// Validate PK segments against schema
	return validatePathAgainstSchema(b, s, sch)
}

// validatePathAgainstSchema validates that PK segments in the path match schema types.
func validatePathAgainstSchema(b Builder, original string, sch *schema.Schema) (Builder, error) {
	if sch == nil {
		return Builder{}, errors.New("schema is nil")
	}

	// Re-parse to extract segment info for validation
	segments, err := extractSegments(original)
	if err != nil {
		return Builder{}, err
	}

	// Track type context as we traverse
	var currentType *schema.Type

	for i, seg := range segments {
		switch seg.kind {
		case segmentKey:
			// First key after root is the type name
			if i == 0 || currentType == nil {
				typ, ok := sch.Type(seg.key)
				if !ok {
					return Builder{}, fmt.Errorf("type %q not found in schema", seg.key)
				}
				currentType = typ
			} else {
				// Subsequent keys are relation names - find target type
				rel, ok := currentType.Relation(seg.key)
				if ok {
					targetID := rel.TargetID()
					typ, ok := sch.Type(targetID.Name())
					if ok {
						currentType = typ
					} else {
						// Cross-schema type, can't validate further
						currentType = nil
					}
				} else {
					// Could be a property access, not a relation
					currentType = nil
				}
			}

		case segmentPK:
			// Validate PK fields against current type
			if currentType == nil {
				// No type context, can't validate
				continue
			}

			if err := validatePKFields(seg.pkFields, currentType); err != nil {
				return Builder{}, err
			}

		case segmentIndex:
			// Array index - no type validation needed
			continue
		}
	}

	return b, nil
}

// segmentKind identifies the kind of path segment.
type segmentKind int

const (
	segmentKey segmentKind = iota
	segmentIndex
	segmentPK
)

// pathSegment represents a parsed path segment for validation.
type pathSegment struct {
	kind     segmentKind
	key      string    // for segmentKey
	index    int       // for segmentIndex
	pkFields []PKField // for segmentPK
}

// extractSegments parses a path string into segments for validation.
func extractSegments(s string) ([]pathSegment, error) {
	if s == "" || s[0] != '$' {
		return nil, errors.New("invalid path")
	}

	var segments []pathSegment
	pos := 1 // Skip '$'

	for pos < len(s) {
		switch s[pos] {
		case '.':
			pos++
			key, newPos, err := parseIdentifier(s, pos)
			if err != nil {
				return nil, err
			}
			segments = append(segments, pathSegment{kind: segmentKey, key: key})
			pos = newPos

		case '[':
			pos++
			if pos >= len(s) {
				return nil, errors.New("unexpected end after '['")
			}

			switch {
			case s[pos] == '"':
				// Quoted key
				str, newPos, err := parseQuotedString(s, pos)
				if err != nil {
					return nil, err
				}
				pos = newPos
				if pos >= len(s) || s[pos] != ']' {
					return nil, errors.New("expected ']'")
				}
				pos++
				segments = append(segments, pathSegment{kind: segmentKey, key: str})

			case isDigit(rune(s[pos])) || s[pos] == '-':
				if containsBeforeClose(s[pos:], '=') {
					// PK segment
					fields, newPos, err := parsePKFields(s, pos)
					if err != nil {
						return nil, err
					}
					segments = append(segments, pathSegment{kind: segmentPK, pkFields: fields})
					pos = newPos
				} else {
					// Array index
					idx, newPos, err := parseInteger(s, pos)
					if err != nil {
						return nil, err
					}
					pos = newPos
					if pos >= len(s) || s[pos] != ']' {
						return nil, errors.New("expected ']'")
					}
					pos++
					segments = append(segments, pathSegment{kind: segmentIndex, index: idx})
				}

			case isLetter(rune(s[pos])) || s[pos] == '_':
				// PK segment
				fields, newPos, err := parsePKFields(s, pos)
				if err != nil {
					return nil, err
				}
				segments = append(segments, pathSegment{kind: segmentPK, pkFields: fields})
				pos = newPos

			default:
				return nil, fmt.Errorf("unexpected character '%c'", s[pos])
			}

		default:
			return nil, fmt.Errorf("unexpected character '%c'", s[pos])
		}
	}

	return segments, nil
}

// validatePKFields validates that PK fields match the type's primary key properties.
func validatePKFields(fields []PKField, typ *schema.Type) error {
	// Build map of PK properties
	pkProps := make(map[string]*schema.Property)
	for prop := range typ.PrimaryKeys() {
		pkProps[prop.Name()] = prop
	}

	// Validate each field
	for _, field := range fields {
		prop, ok := pkProps[field.Name]
		if !ok {
			return fmt.Errorf("field %q is not a primary key on type %q", field.Name, typ.Name())
		}

		if err := validatePKValueType(field.Value, prop.Constraint()); err != nil {
			return fmt.Errorf("PK field %q: %w", field.Name, err)
		}
	}

	return nil
}

// validatePKValueType validates that a PK value matches the expected constraint type.
// Only types permitted as primary keys (String, UUID, Date, Timestamp) reach here;
// schema validation rejects all other types before instance paths are parsed.
func validatePKValueType(value any, constraint schema.Constraint) error {
	if constraint == nil {
		return nil // Can't validate without constraint
	}

	// Resolve alias constraints to their underlying type
	kind := resolveConstraintKind(constraint)

	switch kind {
	case schema.KindString, schema.KindUUID, schema.KindTimestamp, schema.KindDate:
		// All allowed PK types are string-representable.
		if _, ok := value.(string); !ok {
			return fmt.Errorf("expected string, got %T", value)
		}

	default:
		return fmt.Errorf("type %s is not allowed as a primary key", kind)
	}

	return nil
}

// resolveConstraintKind unwraps alias constraints to get the underlying kind.
func resolveConstraintKind(c schema.Constraint) schema.ConstraintKind {
	// Handle alias constraints by checking for Resolved method
	if alias, ok := c.(schema.AliasConstraint); ok {
		if resolved := alias.Resolved(); resolved != nil {
			return resolveConstraintKind(resolved)
		}
	}
	return c.Kind()
}
