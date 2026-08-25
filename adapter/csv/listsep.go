package csv

import "strings"

// escapeListElem renders one list element or edge segment so the separator
// can be split unambiguously: the backslash escapes itself and the
// separator. Escaping the separator alone is not injective — `a\` + sep +
// `b` would collide with the element `a\` `b` — so both are escaped, and
// [splitListElems] is the one inverse.
func escapeListElem(s, sep string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, sep, `\`+sep)
}

// splitListElems is the inverse of [escapeListElem]: it splits on the
// unescaped separator and unescapes each element. The separator is treated
// as an opaque string; an escape applies to the single character after the
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
