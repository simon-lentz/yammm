package csv

import (
	"strings"

	"github.com/simon-lentz/yammm/adapter/internal/refusal"
)

// escapeListElem escapes the backslash and every occurrence of the separator's
// first byte, so [splitListElems] finds a separator only between elements.
// Escaping whole matches fails for "||": "a|" then the separator reads "a|||".
func escapeListElem(s, sep string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, sep[:1], `\`+sep[:1])
}

// splitListElems is the inverse of [escapeListElem]: it splits on the
// unescaped separator and unescapes each element. The separator is treated
// as an opaque string; an escape applies to the single byte after the
// backslash.
func splitListElems(s, sep string) []string {
	if s == "" {
		return nil
	}
	var (
		elems []string
		buf   strings.Builder
	)
	for i := 0; i < len(s); {
		if s[i] == '\\' && i+1 < len(s) {
			buf.WriteByte(s[i+1])
			i += 2
			continue
		}
		if strings.HasPrefix(s[i:], sep) {
			elems = append(elems, buf.String())
			buf.Reset()
			i += len(sep)
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	elems = append(elems, buf.String())
	return elems
}

// listSepError refuses a separator the parser cannot find again: one beginning
// with the backslash, which the splitter reads as an escape, and one holding a
// CR LF, which [encoding/csv]'s reader turns into LF. The separator is the
// adapter's own setting, so the refusal carries [ErrConfig] and never
// [ErrUnrepresentable]: no snapshot can make it go away.
func listSepError(sep string) error {
	switch {
	case strings.HasPrefix(sep, `\`):
		return refusal.New(ErrConfig, "csv adapter: list separator %q begins with the escape character \\, which the parser reads as an escape", sep)
	case strings.Contains(sep, "\r\n"):
		return refusal.New(ErrConfig, "csv adapter: list separator %q holds a CR LF, which encoding/csv reads back as LF, so the parser never finds it", sep)
	}
	return nil
}
