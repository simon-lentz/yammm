package immutable

import (
	"encoding/json"
	"strings"
)

// IsIntegerLiteral reports whether n is spelled as an integer: it carries no
// float indicator ('.', 'e' or 'E'). It judges the spelling alone, not the
// syntax or the range. It is the lexical rule every reader of a data document
// applies: an Integer value is an integer literal, and a decimal or exponent
// literal is a float even when its value is whole.
func IsIntegerLiteral(n json.Number) bool {
	return !strings.ContainsAny(string(n), ".eE")
}

// IsNumberLiteral reports whether n is a JSON number literal (RFC 8259,
// section 6), -? (0 | [1-9][0-9]*) (.[0-9]+)? ([eE][+-]?[0-9]+)?, with nothing
// around it: the text encoding/json decodes as a number and writes as one. A
// json.Number holding any other text, which only a Go caller builds, is no
// number to any reader or writer of the module.
func IsNumberLiteral(n json.Number) bool {
	s := string(n)
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
