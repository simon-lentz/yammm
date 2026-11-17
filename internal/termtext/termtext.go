// Package termtext escapes text a program writes to a terminal, so no byte of
// the text can act on the terminal: move the cursor, change colours, start a
// new line, or reorder how the text after it is drawn.
//
// A C0 control other than a tab shows as its Control Pictures glyph (U+2400
// to U+241F), DEL as U+2421, and a C1 control or a bidirectional mark,
// embedding, override or isolate as U+FFFD. A byte sequence that is not valid
// UTF-8 shows as U+FFFD. A tab is kept, so a terminal expands it as it
// expands the same tab elsewhere. Every other rune is kept.
//
// # Dependencies
//
//	internal/termtext  ──imports──▶  (standard library only)
package termtext

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Rune returns what a terminal is shown for c.
func Rune(c rune) rune {
	switch {
	case c == '\t':
		return c
	case c < 0x20:
		return 0x2400 + c
	case c == 0x7f:
		return 0x2421
	case c >= 0x80 && c < 0xa0, isBidiControl(c):
		return unicode.ReplacementChar
	default:
		return c
	}
}

// String returns s with each rune replaced by [Rune]'s, and each byte
// sequence that is not valid UTF-8 by U+FFFD. It returns s itself when no
// rune changes.
func String(s string) string {
	if !needsEscape(s) {
		return s
	}
	return strings.Map(Rune, s)
}

func needsEscape(s string) bool {
	for _, c := range s {
		if c == utf8.RuneError || Rune(c) != c {
			return true
		}
	}
	return false
}

// isBidiControl reports whether c is a bidirectional mark, embedding, override or
// isolate, which reorders how a terminal draws the text after it.
func isBidiControl(c rune) bool {
	return c == 0x61c || c == 0x200e || c == 0x200f || c >= 0x202a && c <= 0x202e || c >= 0x2066 && c <= 0x2069
}
