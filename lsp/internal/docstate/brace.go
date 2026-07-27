package docstate

import "strings"

// BraceScanner tracks brace nesting depth across lines, correctly skipping
// braces inside line comments (//), block comments (/* */), string literals, and
// regex literals (/.../).
//
// This is the single implementation of the comment/string/brace scanning state
// machine used by ComputeBraceDepths, isInsideTypeBodyDirect, and depthAtColumn.
type BraceScanner struct {
	Depth          int
	InBlockComment bool
}

// regexpEnd returns the index one past the closing '/' of a REGEXP literal
// beginning at line[start], and whether the literal closes on this line.
//
// It mirrors the grammar's REGEXP token — an opening '/', then a run of escape
// pairs and bytes that are neither '/' nor a newline, then a closing '/' — which
// by construction cannot span lines. yammm's lexer resolves the resulting
// ambiguity with the SLASH division operator by maximal munch (SLASH is one
// byte, REGEXP at least two), so reading a slash pair as a literal is what the
// parser does with the same text; a slash with no partner on the line stays
// division.
//
// Callers must already have ruled out the comment openers "//" and "/*", which
// win over REGEXP.
func regexpEnd(line string, start int) (int, bool) {
	for i := start + 1; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // An escaped byte cannot close the literal.
		case '/':
			return i + 1, true
		}
	}
	return 0, false
}

// ScanLine processes a single line up to maxCol bytes and updates the scanner's
// Depth and InBlockComment state. Returns the depth after scanning.
//
// Two-character tokens (block comment delimiters /*, */, and line comment //) are
// detected by peeking one byte past maxCol when the first byte falls within the
// boundary. This ensures correct state transitions when maxCol falls mid-token.
// Callers that pass a cursor position as maxCol should be aware that the scanner
// may consume a delimiter that straddles the boundary.
func (bs *BraceScanner) ScanLine(line string, maxCol int) int {
	if maxCol > len(line) {
		maxCol = len(line)
	}

	j := 0
	for j < maxCol {
		// Handle block comment continuation
		if bs.InBlockComment {
			if j+1 < len(line) && line[j] == '*' && line[j+1] == '/' {
				j += 2
				bs.InBlockComment = false
				continue
			}
			j++
			continue
		}

		ch := line[j]

		// Skip line comments (rest of line)
		if j+1 < len(line) && line[j] == '/' && line[j+1] == '/' {
			break
		}

		// Start block comment
		if j+1 < len(line) && line[j] == '/' && line[j+1] == '*' {
			bs.InBlockComment = true
			j += 2
			continue
		}

		// Skip regex literals. A brace inside an invariant's /.../ pattern is
		// pattern syntax, not nesting: counting it corrupts the depth for the
		// whole rest of the document. An end past maxCol correctly consumes the
		// remainder — every byte up to the boundary is inside the literal.
		if ch == '/' {
			if end, ok := regexpEnd(line, j); ok {
				j = end
				continue
			}
		}

		// Skip string literals
		if ch == '"' || ch == '\'' {
			quote := ch
			j++
			for j < maxCol {
				if line[j] == '\\' && j+1 < maxCol {
					j += 2 // skip escape sequence
					continue
				}
				if line[j] == quote {
					j++
					break
				}
				j++
			}
			continue
		}

		// Count braces
		switch ch {
		case '{':
			bs.Depth++
		case '}':
			bs.Depth--
		}
		j++
	}

	return bs.Depth
}

// InStringOrComment reports whether the byte at index pos on line falls inside a
// string literal ("..." / '...'), a regex literal (/.../), a // line comment, or
// a /* */ block comment. It shares BraceScanner's lexer rules so annotation
// tooling recognises exactly the literal and comment regions the brace scanner
// skips. startInBlockComment carries the block-comment state at the start of the
// line (LineState.InBlockComment of the preceding line); pass false for a
// standalone line.
//
// Regex literals are part of that vocabulary because '@' is ordinary inside one:
// without them, the '@' of an email pattern in an invariant
// (`! "bad" email =~ /^[a-z]+@example[.]com$/`) reads as an annotation sigil and
// hijacks both hover and completion for the rest of the line.
//
// Intended for classifying a content byte such as the '@' of an annotation; a
// pos landing exactly on a multi-byte delimiter boundary (the '/' of a closing
// "*/", a quote) is not specially handled, which such content bytes never do.
func InStringOrComment(line string, pos int, startInBlockComment bool) bool {
	inBlock := startInBlockComment
	for i := 0; i < len(line); i++ {
		if i == pos {
			return inBlock
		}
		switch {
		case inBlock:
			if line[i] == '*' && i+1 < len(line) && line[i+1] == '/' {
				inBlock = false
				i++ // consume the '/'
			}
		case line[i] == '/' && i+1 < len(line) && line[i+1] == '/':
			return pos > i // a line comment runs to end of line
		case line[i] == '/' && i+1 < len(line) && line[i+1] == '*':
			inBlock = true
			i++ // consume the '*'
		case line[i] == '/':
			end, ok := regexpEnd(line, i)
			if !ok {
				break // A slash with no partner on the line is division.
			}
			if pos > i && pos < end {
				return true
			}
			i = end - 1 // the loop's i++ resumes just past the literal
		case line[i] == '"' || line[i] == '\'':
			quote := line[i]
			for i++; i < len(line); i++ {
				if i == pos {
					return true
				}
				if line[i] == '\\' && i+1 < len(line) {
					i++ // skip the escaped byte
					if i == pos {
						return true
					}
					continue
				}
				if line[i] == quote {
					break
				}
			}
		}
	}
	return inBlock
}

// ComputeBraceDepths computes the brace nesting depth and block comment state
// at the end of each line. This is used for completion context detection (isInsideTypeBody).
// The function properly skips braces inside comments and string literals.
//
// Normalizes line endings defensively via NormalizeLineEndings. Most call sites
// (Overlay.OpenDocument, ChangeDocument) already normalize upstream, but this
// function is public and may receive raw editor text (e.g., in tests or future callers).
//
// Returns two parallel slices:
//   - depths[i] = brace nesting depth at end of line i
//   - inComment[i] = true if line i ends inside a block comment
func ComputeBraceDepths(text string) (depths []int, inComment []bool) {
	text = NormalizeLineEndings(text)
	lines := strings.Split(text, "\n")
	depths = make([]int, len(lines))
	inComment = make([]bool, len(lines))

	var bs BraceScanner
	for i, line := range lines {
		bs.ScanLine(line, len(line))
		depths[i] = bs.Depth
		inComment[i] = bs.InBlockComment
	}

	return depths, inComment
}
