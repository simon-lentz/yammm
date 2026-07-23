package docstate

import "strings"

// BraceScanner tracks brace nesting depth across lines, correctly skipping
// braces inside line comments (//), block comments (/* */), and string literals.
//
// This is the single implementation of the comment/string/brace scanning state
// machine used by ComputeBraceDepths, isInsideTypeBodyDirect, and depthAtColumn.
type BraceScanner struct {
	Depth          int
	InBlockComment bool
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
// string literal ("..." / '...'), a // line comment, or a /* */ block comment.
// It shares BraceScanner's lexer rules so annotation tooling recognises exactly
// the string/comment regions the brace scanner skips. startInBlockComment
// carries the block-comment state at the start of the line
// (LineState.InBlockComment of the preceding line); pass false for a standalone
// line.
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
