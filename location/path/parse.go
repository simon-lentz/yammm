package path

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Parse parses a path string into a Builder.
//
// The path must start with "$" and may contain:
//   - Property keys: $.name or $["complex key"]
//   - Array indices: $[0], $[42] (zero-based, non-negative)
//   - PK-based indices: $[id=123], $[name="Alice"], $[a="x",b=1]
//
// Parse does not validate PK field names or value types against a schema.
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
