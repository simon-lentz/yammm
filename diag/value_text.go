package diag

import (
	"strconv"
	"unicode/utf8"
)

// maxValueText bounds how much of a value's text a message repeats: a string
// or a number literal can run to megabytes, and a message can be written once
// per value.
const maxValueText = 32

// QuoteValue renders the text of a data value, or of a name the data writes,
// for a message. The text is quoted as [strconv.Quote] quotes it, so a byte
// that is no part of a character is spelled as an escape and the result is
// valid UTF-8. Text longer than 32 bytes is cut to its longest prefix of whole
// characters within that bound, and its length in bytes follows the quoted head.
func QuoteValue(s string) string {
	head, cut := valueHead(s)
	if !cut {
		return strconv.Quote(head)
	}
	return strconv.Quote(head) + "… (" + strconv.Itoa(len(s)) + " bytes)"
}

// ValueText renders text a message shows unquoted, such as a number literal,
// a key's JSON or a type identity: cut as [QuoteValue] cuts it. Text that is
// not valid UTF-8 is rendered by QuoteValue, so the result is valid UTF-8.
func ValueText(s string) string {
	if !utf8.ValidString(s) {
		return QuoteValue(s)
	}
	head, cut := valueHead(s)
	if !cut {
		return head
	}
	return head + "… (" + strconv.Itoa(len(s)) + " bytes)"
}

// valueHead returns s whole when it is no longer than maxValueText bytes, and
// otherwise its longest prefix of whole characters within that bound, with
// true. A byte that is no part of a character counts as one character.
func valueHead(s string) (head string, cut bool) {
	if len(s) <= maxValueText {
		return s, false
	}
	end := 0
	for {
		_, size := utf8.DecodeRuneInString(s[end:])
		if end+size > maxValueText {
			return s[:end], true
		}
		end += size
	}
}
