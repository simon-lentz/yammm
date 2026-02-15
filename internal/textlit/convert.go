package textlit

import (
	"fmt"
	"strconv"
	"strings"
)

const (
	doubleQuote = `"`
	singleQuote = `'`
)

// ConvertString converts a parsed double or single quoted string to a Go string.
// Quoted inputs are unescaped via strconv.Unquote; unquoted inputs are returned
// unchanged. Invalid escape sequences return the original string alongside an
// error so callers can surface fatal diagnostics instead of silently accepting
// bad escapes.
func ConvertString(s string) (string, error) {
	original := s
	if strings.HasPrefix(s, singleQuote) && strings.HasSuffix(s, singleQuote) && len(s) >= 2 {
		inner := strings.TrimSuffix(strings.TrimPrefix(s, singleQuote), singleQuote)
		// Escape embedded double quotes for strconv.Unquote
		inner = strings.ReplaceAll(inner, `"`, `\"`)
		s = doubleQuote + inner + doubleQuote
	}
	if strings.HasPrefix(s, doubleQuote) {
		unquoted, err := strconv.Unquote(s)
		if err != nil {
			return original, fmt.Errorf("unquote %q: %w", original, err)
		}
		return unquoted, nil
	}
	return s, nil
}
