package csv

import "strings"

// listSeparator separates list elements, vector elements and edge-column
// segments, on the write side and the parse side alike.
const listSeparator = "|"

// escapeListElem escapes the backslash and every separator, so [splitListElems]
// finds a separator only between elements.
func escapeListElem(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	return strings.ReplaceAll(s, listSeparator, `\`+listSeparator)
}

// splitListElems is the inverse of [escapeListElem]: it splits on the unescaped
// separator and unescapes each element. An escape applies to the single byte
// after the backslash.
func splitListElems(s string) []string {
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
		if s[i] == listSeparator[0] {
			elems = append(elems, buf.String())
			buf.Reset()
			i++
			continue
		}
		buf.WriteByte(s[i])
		i++
	}
	elems = append(elems, buf.String())
	return elems
}
