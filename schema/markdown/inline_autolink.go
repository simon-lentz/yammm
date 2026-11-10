package markdown

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// extendedAutolinkLength returns the length of the www link or URL rest that
// cmark-gfm's autolink extension reads at s[i], or 0. The extension takes the
// link's bytes as they are, brackets and backticks among them.
func extendedAutolinkLength(s string, i int) int {
	switch s[i] {
	case 'w':
		return wwwAutolinkLength(s, i)
	case ':':
		return urlAutolinkLength(s, i)
	}
	return 0
}

// wwwAutolinkLength is the extension's www_match: "www." after the text's
// start, white space or one of "*_~(", a domain with a dot, and the bytes up
// to white space or "<", less the trailing ones autolinkDelim drops.
func wwwAutolinkLength(s string, i int) int {
	if i > 0 && !strings.ContainsRune("*_~(", rune(s[i-1])) && !isCmarkSpace(s[i-1]) {
		return 0
	}
	data := s[i:]
	if !strings.HasPrefix(data, "www.") {
		return 0
	}
	end := checkDomain(data, false)
	if end == 0 {
		return 0
	}
	return autolinkDelim(data, linkRunEnd(data, end))
}

// urlAutolinkLength is the extension's url_match at the ":" of s[i]: the
// ASCII letters before it spell a scheme isSafeAutolink takes, and the length
// is the rest's, from the ":" up to white space or "<", less what
// autolinkDelim drops.
func urlAutolinkLength(s string, i int) int {
	data := s[i:]
	if len(data) < 4 || data[1] != '/' || data[2] != '/' {
		return 0
	}
	from := i
	for from > 0 && isASCIILetter(s[from-1]) {
		from--
	}
	if !isSafeAutolink(s[from:]) {
		return 0
	}
	domain := checkDomain(data[3:], true)
	if domain == 0 {
		return 0
	}
	return autolinkDelim(data, linkRunEnd(data, 3+domain))
}

// isSafeAutolink is the extension's sd_autolink_issafe: link starts with one
// of its three schemes, in any case, and a host character follows it.
func isSafeAutolink(link string) bool {
	for _, scheme := range [...]string{"http://", "https://", "ftp://"} {
		if len(link) > len(scheme) && strings.EqualFold(link[:len(scheme)], scheme) && isHostChar(link[len(scheme):]) {
			return true
		}
	}
	return false
}

// checkDomain is the extension's check_domain: it returns where its read of
// host characters, "-", "." and "_" from data[1] stopped, or 0 for an
// underscore in the last two of at most eleven parts or, unless allowShort,
// for no dot.
func checkDomain(data string, allowShort bool) int {
	i, dots, under1, under2 := 1, 0, 0, 0
scan:
	for ; i < len(data)-1; i++ {
		if data[i] == '\\' && i < len(data)-2 {
			i++
		}
		switch {
		case data[i] == '_':
			under2++
		case data[i] == '.':
			under1, under2 = under2, 0
			dots++
		case data[i] != '-' && !isHostChar(data[i:]):
			break scan
		}
	}
	if (under1 > 0 || under2 > 0) && dots <= 10 {
		return 0
	}
	if !allowShort && dots == 0 {
		return 0
	}
	return i
}

// linkRunEnd returns where the link that reads data from end on stops: at
// white space or "<".
func linkRunEnd(data string, end int) int {
	for end < len(data) && !isCmarkSpace(data[end]) && data[end] != '<' {
		end++
	}
	return end
}

// autolinkDelim is the extension's autolink_delim over data[:end], which
// holds no "<": a ")" with more ")" than "(" before it, one of ?!.,:*_~'" or
// an entity-like "&name;" is dropped from the link's end, repeatedly.
func autolinkDelim(data string, end int) int {
	opening := strings.Count(data[:end], "(")
	closing := strings.Count(data[:end], ")")
	for end > 0 {
		switch data[end-1] {
		case ')':
			if closing <= opening {
				return end
			}
			closing--
			end--
		case '?', '!', '.', ',', ':', '*', '_', '~', '\'', '"':
			end--
		case ';':
			at := end - 2
			for at > 0 && isASCIILetter(data[at]) {
				at--
			}
			if at < end-2 && data[at] == '&' {
				end = at
			} else {
				end--
			}
		default:
			return end
		}
	}
	return end
}

// isHostChar is the extension's is_valid_hostchar: s starts with a valid
// UTF-8 character that is neither white space nor punctuation as cmark-gfm
// classes them.
func isHostChar(s string) bool {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError && n <= 1 {
		return false
	}
	return !isCmarkUnicodeSpace(r) && !isCmarkPunct(r)
}

// isCmarkSpace is cmark_isspace: tab, LF, CR and space.
func isCmarkSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// isCmarkUnicodeSpace is cmark_utf8proc_is_space.
func isCmarkUnicodeSpace(r rune) bool {
	switch r {
	case '\t', '\n', '\f', '\r', ' ', 0xA0, 0x1680, 0x202F, 0x205F, 0x3000:
		return true
	}
	return r >= 0x2000 && r <= 0x200A
}

// isCmarkPunct is cmark_utf8proc_is_punctuation: ASCII punctuation, which is
// ASCII's P and S categories, and the Unicode P categories beyond ASCII.
func isCmarkPunct(r rune) bool {
	return unicode.IsPunct(r) || r < utf8.RuneSelf && unicode.IsSymbol(r)
}
