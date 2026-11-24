package markdown

import "strings"

// maxBackticks is the longest backtick run that opens a code span on GitHub.
const maxBackticks = 80

// The closers a search for the end of raw HTML looks for.
const (
	commentCloser = iota
	instructionCloser
	declarationCloser
	cdataCloser
	closerCount
)

var closerText = [closerCount]string{"-->", "?>", ">", "]]>"}

// inlineScanner finds where the code spans and the raw HTML of one text end,
// as cmark-gfm, GitHub's parser, reads them in inline Markdown. Every search
// starts at or after the one before it began, and the scanner keeps what a
// search learned, so all searches together read the text a bounded number of
// times, which read counts.
type inlineScanner struct {
	src string
	// lastRun[n] is where the last run of n backticks a search has passed
	// starts, and searchedRuns records that one search reached the end of src:
	// a later search for a length whose last run is behind it stops at once.
	lastRun      [maxBackticks + 1]int
	searchedRuns bool
	// absent[c] records that a search found no closer c in the rest of src.
	absent [closerCount]bool
	// quoteAfter[q][i] is where the first quote q at or after src[i] is, or
	// len(src); nil until an attribute value quoted with q is read.
	quoteAfter [2][]int
	// read is how many bytes of src the searches have read.
	read int
}

// htmlTag names the element of an HTML tag read in inline Markdown. The name
// is empty for a comment, a processing instruction, a declaration, a CDATA
// section and an autolink.
type htmlTag struct {
	name string
	end  bool // an end tag
}

// codeSpanEnd returns where the code span that a run of n backticks ending at
// from opens ends: after the next run of exactly n backticks. It returns -1
// when the run opens none.
func (sc *inlineScanner) codeSpanEnd(from, n int) int {
	if n > maxBackticks || (sc.searchedRuns && sc.lastRun[n] < from) {
		return -1
	}
	for at := from; ; {
		i := strings.IndexByte(sc.src[at:], '`')
		if i < 0 {
			sc.read += len(sc.src) - at
			sc.searchedRuns = true
			return -1
		}
		start := at + i
		m := runLength(sc.src, start, '`')
		sc.read += start + m - at
		at = start + m
		if m <= maxBackticks {
			sc.lastRun[m] = start
		}
		if m == n {
			return at
		}
	}
}

// htmlEnd returns where the autolink or the raw HTML that starts at src[i], a
// "<", ends, and the tag it is; it returns -1 when inline Markdown reads the
// "<" as text. An autolink is tried first, then a start tag, an end tag, a
// comment, a processing instruction, a declaration and a CDATA section.
func (sc *inlineScanner) htmlEnd(i int) (int, htmlTag) {
	rest := sc.src[i+1:]
	if n := autolinkLength(rest); n > 0 {
		return i + 1 + n, htmlTag{}
	}
	switch {
	case rest == "":
	case rest[0] == '/':
		name := tagNameLength(rest[1:])
		if at := 1 + name + spaceLength(rest[1+name:]); name > 0 && at < len(rest) && rest[at] == '>' {
			return i + 1 + at + 1, htmlTag{name: rest[1 : 1+name], end: true}
		}
	case rest[0] == '?':
		return sc.through(i+2, instructionCloser), htmlTag{}
	case rest[0] == '!' && sc.absent[commentCloser]:
		// cmark-gfm reads no "<!" form after a comment it found no end of.
	case strings.HasPrefix(rest, "!-->"), strings.HasPrefix(rest, "!--->"):
		return i + 1 + strings.IndexByte(rest, '>') + 1, htmlTag{}
	case strings.HasPrefix(rest, "!--"):
		return sc.through(i+4, commentCloser), htmlTag{}
	case strings.HasPrefix(rest, "![CDATA["):
		return sc.through(i+1+len("![CDATA["), cdataCloser), htmlTag{}
	case rest[0] == '!':
		if name := declarationNameLength(rest); name > 0 {
			return sc.through(i+1+name, declarationCloser), htmlTag{}
		}
	default:
		if name := tagNameLength(rest); name > 0 {
			if end := sc.startTagEnd(i + 1 + name); end > 0 {
				return end, htmlTag{name: rest[:name]}
			}
		}
	}
	return -1, htmlTag{}
}

// declarationNameLength returns the length of the "!" and the upper-case
// letters a declaration starts with after its "<", or 0 when s starts none: the
// letters are followed by white space.
func declarationNameLength(s string) int {
	if s == "" || s[0] != '!' {
		return 0
	}
	n := 1
	for n < len(s) && 'A' <= s[n] && s[n] <= 'Z' {
		n++
	}
	if n == 1 || n == len(s) || !isHTMLSpace(s[n]) {
		return 0
	}
	return n
}

// through returns where the first closer c at or after from ends, or -1 when
// the rest of src holds none.
func (sc *inlineScanner) through(from, c int) int {
	if sc.absent[c] {
		return -1
	}
	i := strings.Index(sc.src[from:], closerText[c])
	if i < 0 {
		sc.read += len(sc.src) - from
		sc.absent[c] = true
		return -1
	}
	sc.read += i + len(closerText[c])
	return from + i + len(closerText[c])
}

// startTagEnd returns where the start tag whose name ends at src[at] ends,
// after its ">", or -1 when src holds no whole start tag there: attributes,
// each after white space and with an optional value that is unquoted or quoted
// with either quote, then optional white space and an optional "/". Outside a
// quoted value it reads no further than the next "<".
func (sc *inlineScanner) startTagEnd(at int) int {
	s, start := sc.src, at
	defer func() { sc.read += max(at, start) - start }()
	for {
		sp := spaceLength(s[at:])
		at += sp
		if sp == 0 || at == len(s) || !isAttributeNameStart(s[at]) {
			break
		}
		for at < len(s) && isAttributeNameChar(s[at]) {
			at++
		}
		eq := at + spaceLength(s[at:])
		if eq == len(s) || s[eq] != '=' {
			continue
		}
		at = sc.attributeValueEnd(eq + 1 + spaceLength(s[eq+1:]))
		if at < 0 {
			return -1
		}
	}
	if at < len(s) && s[at] == '/' {
		at++
	}
	if at < len(s) && s[at] == '>' {
		return at + 1
	}
	return -1
}

// attributeValueEnd returns where the attribute value that starts at src[at]
// ends, or -1 when none starts there.
func (sc *inlineScanner) attributeValueEnd(at int) int {
	s := sc.src
	if at == len(s) {
		return -1
	}
	if q := strings.IndexByte(`"'`, s[at]); q >= 0 {
		if sc.quoteAfter[q] == nil {
			sc.quoteAfter[q] = positionsAfter(s, s[at])
			sc.read += len(s)
		}
		end := sc.quoteAfter[q][at+1]
		if end == len(s) {
			return -1
		}
		return end + 1
	}
	end := at
	for end < len(s) && !isHTMLSpace(s[end]) && strings.IndexByte("\"'=<>`", s[end]) < 0 {
		end++
	}
	if end == at {
		return -1
	}
	return end
}

// positionsAfter returns, for each offset of s and for its end, where the
// first c at or after it is, or len(s).
func positionsAfter(s string, c byte) []int {
	after := make([]int, len(s)+1)
	next := len(s)
	after[len(s)] = next
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == c {
			next = i
		}
		after[i] = next
	}
	return after
}

// autolinkLength returns the length of the autolink s holds after its "<",
// through the ">", or 0 when s starts none: a scheme of 2 to 32 characters, a
// colon and no white space, or an email address.
func autolinkLength(s string) int {
	if n := uriAutolinkLength(s); n > 0 {
		return n
	}
	return emailAutolinkLength(s)
}

func uriAutolinkLength(s string) int {
	if s == "" || !isASCIILetter(s[0]) {
		return 0
	}
	at := 1
	for at < len(s) && at < 32 && (isASCIIAlnum(rune(s[at])) || strings.IndexByte(".+-", s[at]) >= 0) {
		at++
	}
	if at < 2 || at == len(s) || s[at] != ':' {
		return 0
	}
	for at++; at < len(s); at++ {
		switch c := s[at]; {
		case c == '>':
			return at + 1
		case c <= ' ' || c == '<':
			return 0
		}
	}
	return 0
}

func emailAutolinkLength(s string) int {
	at := 0
	for at < len(s) && (isASCIIAlnum(rune(s[at])) || strings.IndexByte(".!#$%&'*+/=?^_`{|}~-", s[at]) >= 0) {
		at++
	}
	if at == 0 || at == len(s) || s[at] != '@' {
		return 0
	}
	for {
		at++
		label := at
		for at < len(s) && (isASCIIAlnum(rune(s[at])) || s[at] == '-') {
			at++
		}
		if n := at - label; n == 0 || n > 63 || s[label] == '-' || s[at-1] == '-' || at == len(s) {
			return 0
		}
		switch s[at] {
		case '>':
			return at + 1
		case '.':
		default:
			return 0
		}
	}
}

// tagNameLength returns the length of the tag name s starts with: an ASCII
// letter, then ASCII letters, digits and hyphens.
func tagNameLength(s string) int {
	if s == "" || !isASCIILetter(s[0]) {
		return 0
	}
	n := 1
	for n < len(s) && (isASCIIAlnum(rune(s[n])) || s[n] == '-') {
		n++
	}
	return n
}

// spaceLength returns the length of the white space s starts with.
func spaceLength(s string) int {
	n := 0
	for n < len(s) && isHTMLSpace(s[n]) {
		n++
	}
	return n
}

// runLength returns the length of the run of c that starts at s[i].
func runLength(s string, i int, c byte) int {
	n := 0
	for i+n < len(s) && s[i+n] == c {
		n++
	}
	return n
}

func isHTMLSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func isASCIILetter(c byte) bool {
	return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
}

func isAttributeNameStart(c byte) bool {
	return isASCIILetter(c) || c == '_' || c == ':'
}

func isAttributeNameChar(c byte) bool {
	return isASCIIAlnum(rune(c)) || strings.IndexByte("_:.-", c) >= 0
}

// isASCIIPunct reports whether c is a character a backslash escapes in
// Markdown.
func isASCIIPunct(c byte) bool {
	return '!' <= c && c <= '/' || ':' <= c && c <= '@' || '[' <= c && c <= '`' || '{' <= c && c <= '~'
}
