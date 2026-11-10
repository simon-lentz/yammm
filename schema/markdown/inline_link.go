package markdown

import "strings"

// maxLinkParens is how many "(" a bare link destination may hold open at once
// in cmark-gfm.
const maxLinkParens = 32

// linkTail is where the parts of an inline link's "(destination title)" tail
// are. A line break in the destination or the title is part of its value; any
// other is white space.
type linkTail struct {
	destFrom, destTo, titleFrom, titleTo, end int
}

// text reports whether src[i] is in the tail's destination or title.
func (t linkTail) text(i int) bool {
	return t.destFrom <= i && i < t.destTo || t.titleFrom <= i && i < t.titleTo
}

// parenRun indexes the parentheses of one run of text with no white space, in
// which bare link destinations are read: each byte of the text is indexed at
// most once, however many destinations start in its run.
type parenRun struct {
	from, to int
	opens    []parenOpen
	stack    []int
	next     int
	// close and deep are, for a destination that starts at from, the first
	// ")" that closes nothing and the "(" one level too deep; -1 when none.
	close, deep int
}

// parenOpen is one "(" of a parenRun: where its ")" is, and where the "(" one
// level too deep for a destination that starts after it is; -1 when none.
type parenOpen struct {
	at, close, deep int
}

// index reads the run of src that starts at from up to its first white space.
func (r *parenRun) index(src string, from int, read *int) {
	to := from
	for to < len(src) && !isLinkSpace(src[to]) {
		to++
	}
	*read += to - from
	if r.opens == nil {
		// Made once, to hold every "(" from the first run on.
		n := strings.Count(src[from:], "(")
		r.opens, r.stack = make([]parenOpen, 0, n), make([]int, 0, n)
	}
	r.from, r.to, r.opens, r.stack, r.next, r.close, r.deep = from, to, r.opens[:0], r.stack[:0], 0, -1, -1
	for p := from; p < to; p++ {
		switch src[p] {
		case '\\':
			if p+1 < len(src) && isASCIIPunct(src[p+1]) {
				p++
			}
		case '(':
			r.stack = append(r.stack, len(r.opens))
			r.opens = append(r.opens, parenOpen{at: p, close: -1, deep: -1})
			h := len(r.stack)
			if h > maxLinkParens+1 {
				if o := &r.opens[r.stack[h-maxLinkParens-2]]; o.deep < 0 {
					o.deep = p
				}
			}
			if h == maxLinkParens+1 && r.close < 0 && r.deep < 0 {
				r.deep = p
			}
		case ')':
			if len(r.stack) == 0 {
				if r.close < 0 && r.deep < 0 {
					r.close = p
				}
				continue
			}
			r.opens[r.stack[len(r.stack)-1]].close = p
			r.stack = r.stack[:len(r.stack)-1]
		}
	}
}

// bareDestinationEnd returns where the bare destination that starts at
// src[from] ends, or -1 when cmark-gfm reads none there. With no white space
// after the tail's "(" at src[open], the destination ends at that
// parenthesis' ")"; after white space, at the first ")" that closes nothing.
func (f *lineFolder) bareDestinationEnd(open, from int) int {
	r := &f.parens
	var end, deep int
	if !strings.ContainsAny(f.src[open+1:from], " \t\n\r") {
		if open < r.from || open >= r.to {
			r.index(f.src, open, &f.read)
		}
		// The run holds this "(": a "]" is before it, so no escape takes it.
		for r.opens[r.next].at < open {
			r.next++
		}
		end, deep = r.opens[r.next].close, r.opens[r.next].deep
	} else {
		r.index(f.src, from, &f.read)
		end, deep = r.close, r.deep
	}
	switch {
	case deep >= 0:
		return -1
	case end >= 0:
		return end
	case r.to == len(f.src):
		return -1
	}
	return r.to
}

// linkTailAt reads the inline link tail that opens at src[i], a "(" right
// after a link text's "]", as cmark-gfm reads it in one paragraph, so no
// white space or title in it holds a blank line. It returns false when the
// text there is no tail.
func (f *lineFolder) linkTailAt(i int) (linkTail, bool) {
	s := f.src
	var t linkTail
	t.destFrom = f.linkSpaceEnd(i + 1)
	if t.destFrom < 0 || t.destFrom == len(s) {
		return t, false
	}
	if s[t.destFrom] == '<' {
		t.destTo = f.pointyDestinationEnd(t.destFrom)
	} else {
		t.destTo = f.bareDestinationEnd(i, t.destFrom)
	}
	if t.destTo < 0 {
		return t, false
	}
	// Destinations that end at the same byte share what follows them.
	if t.destTo != f.lastDestTo {
		f.lastDestTo = t.destTo
		f.lastTitleFrom, f.lastTitleTo, f.lastEnd = f.linkTitleAndEnd(t.destTo)
	}
	t.titleFrom, t.titleTo, t.end = f.lastTitleFrom, f.lastTitleTo, f.lastEnd
	return t, t.end > 0
}

// linkTitleAndEnd reads the white space, the title and the ")" after a link
// destination that ends at src[from]. It returns 0 for end when they are no
// tail's end.
func (f *lineFolder) linkTitleAndEnd(from int) (titleFrom, titleTo, end int) {
	titleFrom = f.linkSpaceEnd(from)
	if titleFrom < 0 {
		return 0, 0, 0
	}
	titleTo = titleFrom
	if titleFrom > from {
		titleTo = f.linkTitleEnd(titleFrom)
	}
	end = f.linkSpaceEnd(titleTo)
	if end < 0 || end == len(f.src) || f.src[end] != ')' {
		return 0, 0, 0
	}
	return titleFrom, titleTo, end + 1
}

// pointyDestinationEnd returns where the destination in angle brackets that
// starts at src[from] ends, or -1 when it has none. A backslash takes the
// byte after it into the destination, a line break too.
func (f *lineFolder) pointyDestinationEnd(from int) int {
	s := f.src
	p := from + 1
	for p < len(s) && s[p] != '>' && s[p] != '<' && s[p] != '\n' {
		if s[p] != '\\' {
			p++
			continue
		}
		if p+1 < len(s) && s[p+1] == '\n' && blankLineAfter(s, p+1) {
			break
		}
		p += 2
	}
	f.read += min(p+1, len(s)) - from
	if p+1 >= len(s) || s[p] != '>' {
		return -1
	}
	return p + 1
}

// linkTitleEnd returns where the link title that starts at src[from] ends, or
// from when none does. A closing quote or ")" with a backslash before it may
// also be text, and cmark-gfm takes the longest title it can.
func (f *lineFolder) linkTitleEnd(from int) int {
	s := f.src
	if from == len(s) {
		return from
	}
	closer := s[from]
	switch closer {
	case '"', '\'':
	case '(':
		closer = ')'
	default:
		return from
	}
	end := from
	p := from + 1
	for ; p < len(s); p++ {
		c := s[p]
		if c == '\n' && blankLineAfter(s, p) {
			break
		}
		if c != closer && (closer != ')' || c != '(') {
			continue
		}
		if c == closer {
			end = p + 1
		}
		if s[p-1] != '\\' {
			break
		}
	}
	f.read += min(p+1, len(s)) - from
	return end
}

// linkSpaceEnd returns where the white space that starts at src[from] ends,
// or -1 when it holds a blank line.
func (f *lineFolder) linkSpaceEnd(from int) int {
	s := f.src
	p := from
	for p < len(s) && (isLinkSpace(s[p]) || s[p] == '\v' || s[p] == '\f') {
		if s[p] == '\n' && blankLineAfter(s, p) {
			f.read += p - from
			return -1
		}
		p++
	}
	f.read += p - from
	return p
}

// isLinkSpace reports whether c is white space to cmark-gfm's cmark_isspace,
// which ends a bare link destination.
func isLinkSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r'
}

// blankLineAfter reports whether the line after the line break at s[i] holds
// only spaces and tabs and is followed by another line.
func blankLineAfter(s string, i int) bool {
	return strings.HasPrefix(strings.TrimLeft(s[i+1:], " \t"), "\n")
}

// writeLinkTail writes an inline link tail on one line, with the next line's
// leading white space dropped as a paragraph drops it. A line break in a
// destination or title is a character reference, a lone backslash before it
// doubled, and any other line break is a space.
func (f *lineFolder) writeLinkTail(from int, t linkTail) {
	s := f.src
	for p := from; p < t.end; p++ {
		if s[p] != '\n' {
			f.out = append(f.out, s[p])
			continue
		}
		if t.text(p) {
			if endsInOddBackslashRun(f.out) {
				f.out = append(f.out, '\\')
			}
			f.out = append(f.out, "&#10;"...)
		} else {
			f.out = append(f.out, ' ')
		}
		for p+1 < t.end && (s[p+1] == ' ' || s[p+1] == '\t') {
			p++
		}
	}
}
