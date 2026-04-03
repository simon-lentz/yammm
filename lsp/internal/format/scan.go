package format

// quoteAwareScanner iterates over characters in a string, skipping content
// inside double-quoted string literals. The yammm DSL uses only double quotes
// for strings. Backslash escapes inside strings are handled correctly.
type quoteAwareScanner struct {
	s        string
	pos      int
	inString bool
}

// newQuoteAwareScanner returns a scanner positioned before the first character.
func newQuoteAwareScanner(s string) quoteAwareScanner {
	return quoteAwareScanner{s: s}
}

// newQuoteAwareScannerFrom returns a scanner starting at byte offset start.
func newQuoteAwareScannerFrom(s string, start int) quoteAwareScanner {
	return quoteAwareScanner{s: s, pos: start}
}

// next advances to the next non-string character.
// Returns the byte index and character, or (-1, 0) when exhausted.
func (sc *quoteAwareScanner) next() (int, byte) {
	for sc.pos < len(sc.s) {
		i := sc.pos
		ch := sc.s[i]
		sc.pos++

		if sc.inString {
			if ch == '\\' && sc.pos < len(sc.s) {
				sc.pos++ // skip escaped character
				continue
			}
			if ch == '"' {
				sc.inString = false
			}
			continue
		}

		if ch == '"' {
			sc.inString = true
			continue
		}

		return i, ch
	}
	return -1, 0
}

// Done reports the scanner's state after iteration has exhausted the input.
// inString is true if the scanner ended inside an unclosed string literal.
// pos is the byte offset one past the last character examined.
func (sc *quoteAwareScanner) Done() (pos int, inString bool) {
	return sc.pos, sc.inString
}

// isInsideStringAt reports whether byte position pos in s falls inside a
// double-quoted string literal. It walks the quote state machine from the
// start of s up to (but not including) pos.
func isInsideStringAt(s string, pos int) bool {
	inStr := false
	for i := 0; i < pos && i < len(s); i++ {
		ch := s[i]
		if inStr {
			if ch == '\\' {
				i++ // skip escaped character
				continue
			}
			if ch == '"' {
				inStr = false
			}
		} else if ch == '"' {
			inStr = true
		}
	}
	return inStr
}
