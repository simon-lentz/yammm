// Package jsonstring finds the text in a JSON string that encoding/json does
// not read as written. encoding/json reads each byte of an invalid UTF-8
// sequence, and each \u escape that names half of no UTF-16 surrogate pair, as
// U+FFFD, so two strings that differ only in such text read as one string. The
// module's readers refuse such text: each finds a lone escape with
// [LoneSurrogate], and a reader that names the byte it refuses finds it with
// [InvalidUTF8].
//
// Each function takes the body of a string, the bytes between its quotes, that
// a JSON decoder has already read: every \u in it is followed by four hex
// digits.
//
// # Dependencies
//
//	internal/jsonstring  ──imports──▶  (standard library only)
package jsonstring

import (
	"unicode"
	"unicode/utf16"
	"unicode/utf8"
)

// InvalidUTF8 returns the index in body of the first byte that is not part of
// a valid UTF-8 sequence, or -1.
func InvalidUTF8(body []byte) int {
	if utf8.Valid(body) {
		return -1
	}
	for i := 0; i < len(body); {
		if body[i] < utf8.RuneSelf {
			i++
			continue
		}
		r, size := utf8.DecodeRune(body[i:])
		if r == utf8.RuneError && size == 1 {
			return i
		}
		i += size
	}
	return -1
}

// LoneSurrogate returns the index in body of the first \uXXXX escape naming a
// UTF-16 surrogate that is not half of a pair, or -1.
func LoneSurrogate(body []byte) int {
	for i := 0; i < len(body); i++ {
		if body[i] != '\\' {
			continue
		}
		i++
		if body[i] != 'u' {
			continue
		}
		r := escapedRune(body[i+1:])
		start := i - 1
		i += 4
		if !utf16.IsSurrogate(r) {
			continue
		}
		if i+6 < len(body) && body[i+1] == '\\' && body[i+2] == 'u' && utf16.DecodeRune(r, escapedRune(body[i+3:])) != unicode.ReplacementChar {
			i += 6
			continue
		}
		return start
	}
	return -1
}

// escapedRune reads the four hex digits a \u escape holds.
func escapedRune(digits []byte) rune {
	var r rune
	for _, d := range digits[:4] {
		switch {
		case d <= '9':
			r = r<<4 | rune(d-'0')
		case d <= 'F':
			r = r<<4 | rune(d-'A'+10)
		default:
			r = r<<4 | rune(d-'a'+10)
		}
	}
	return r
}
