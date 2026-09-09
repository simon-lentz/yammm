package format

import (
	"strings"

	"github.com/simon-lentz/yammm/internal/parse"
)

// lineClass classifies a source line. The three text passes read this
// classification and never re-derive it from the text: a pass that decides
// on its own whether a line is a comment is how a doc-comment continuation
// line came to be padded as a property.
type lineClass int

const (
	lineBlank   lineClass = iota // empty or whitespace-only
	lineComment                  // a // line, or any line of a /* */ block
	lineContent                  // declaration tokens, possibly with a trailing comment
)

// span is a half-open byte range within one line's text.
type span struct {
	start int
	end   int
}

// lexical is what the lexer knows about one line: where a trailing comment
// begins, and where the string and regex literals are. Phases read it instead
// of re-deriving it, which is how a comma inside a literal came to be read as
// a value separator.
type lexical struct {
	commentAt int    // byte offset of the trailing comment, or noComment
	literals  []span // literal extents, in source order
}

// noComment is commentAt's value on a line with no trailing comment.
const noComment = -1

// line is one source line, its class, and the lexer's view of it.
type line struct {
	text  string
	class lineClass
	lex   lexical
}

// codeEnd returns the offset at which this line's code stops: the trailing
// comment's start, or the end of the text.
func (ln line) codeEnd() int {
	if ln.lex.commentAt >= 0 && ln.lex.commentAt <= len(ln.text) {
		return ln.lex.commentAt
	}
	return len(ln.text)
}

// hasComment reports whether the line carries a trailing comment.
func (ln line) hasComment() bool { return ln.lex.commentAt >= 0 }

// mask returns the line's text with every literal and any trailing comment
// blanked out, byte for byte. A scan over the mask sees code alone and every
// offset it finds still indexes the real text, which is what lets one lexical
// view serve rules that were each scanning for themselves.
func (ln line) mask() string {
	if len(ln.lex.literals) == 0 && !ln.hasComment() {
		return ln.text
	}
	b := []byte(ln.text)
	blank := func(from, to int) {
		from = max(from, 0)
		to = min(to, len(b))
		for i := from; i < to; i++ {
			if b[i] != '\t' {
				b[i] = ' '
			}
		}
	}
	for _, lit := range ln.lex.literals {
		blank(lit.start, lit.end)
	}
	if ln.hasComment() {
		blank(ln.lex.commentAt, len(b))
	}
	return string(b)
}

// declaresWith reports whether the line's code is the declaration keyword kw
// followed immediately by a string literal. It is what tells `import "./x"`
// from a property legally named import: both start with the same word, and only
// the declaration takes a literal there.
func (ln line) declaresWith(kw string) bool {
	m := ln.mask()
	code := strings.TrimLeft(m[:min(ln.codeEnd(), len(m))], "\t ")
	if !strings.HasPrefix(code, kw) {
		return false
	}
	after := len(m) - len(code) + len(kw)
	for _, lit := range ln.lex.literals {
		if lit.start >= after && strings.TrimSpace(m[after:lit.start]) == "" {
			return true
		}
	}
	return false
}

// trimmedCode returns the line's code with surrounding whitespace removed, so a
// rule reading how a line ends is not answered by its comment.
func (ln line) trimmedCode() string {
	m := ln.mask()
	return strings.TrimSpace(m[:min(ln.codeEnd(), len(m))])
}

// hasAnnotationFrom reports an annotation sigil in the line's code at or after
// off. It reads the mask, so an "@" inside a string or a comment is not one.
func (ln line) hasAnnotationFrom(off int) bool {
	return strings.ContainsRune(ln.maskedSub(off, len(ln.text)), '@')
}

// maskedSub returns the mask of ln[start:end], for a rule that scans one region
// of a line and then slices the real text at the offsets it found.
func (ln line) maskedSub(start, end int) string {
	m := ln.mask()
	start = max(start, 0)
	end = min(end, len(m))
	if start >= end {
		return ""
	}
	return m[start:end]
}

// classifyLexed splits text into lines and records the lexer's view of each.
// It lexes, because a caller entering at an exported phase has no token stream
// to inherit; the pipeline builds the same record during phase 1's emission and
// pays no lex at all.
func classifyLexed(text string) []line {
	raw := strings.Split(text, "\n")
	starts := make([]int, len(raw))
	off := 0
	for i, r := range raw {
		starts[i] = off
		off += len(r) + 1
	}

	lexes := make([]lexical, len(raw))
	for i := range lexes {
		lexes[i] = lexical{commentAt: noComment}
	}
	for _, tok := range parse.Lex(text) {
		var kind chunkKind
		switch tok.Kind {
		case kindSTRING, kindREGEXP:
			kind = chunkLiteral
		case kindSLComment, kindDocComment:
			kind = chunkComment
		default:
			continue
		}
		markToken(lexes, starts, raw, tok.Start, tok.End, kind)
	}

	ls := make([]line, len(raw))
	for i, r := range raw {
		ls[i] = line{text: r, class: classOf(r, lexes[i]), lex: lexes[i]}
	}
	return ls
}

// markToken records one token's extent against every line it covers, so a
// block comment spanning lines marks each of them.
func markToken(lexes []lexical, starts []int, raw []string, start, end int, kind chunkKind) {
	for i := range raw {
		lineStart := starts[i]
		lineEnd := lineStart + len(raw[i])
		if end <= lineStart || start >= lineEnd+1 {
			continue
		}
		from := max(start-lineStart, 0)
		to := min(end-lineStart, len(raw[i]))
		if to <= from {
			continue
		}
		if kind == chunkLiteral {
			lexes[i].literals = append(lexes[i].literals, span{start: from, end: to})
			continue
		}
		if lexes[i].commentAt == noComment || from < lexes[i].commentAt {
			lexes[i].commentAt = from
		}
	}
}

// joinLines is the inverse of the split over the text.
func joinLines(ls []line) string {
	var b strings.Builder
	for i, ln := range ls {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(ln.text)
	}
	return b.String()
}

// contentLine wraps rewritten text a pass emits in place of declaration
// lines; a pass rewrites content only, so the class is fixed.
func contentLine(text string) line {
	return line{text: text, class: lineContent, lex: lexical{commentAt: noComment}}
}

// textsOf returns the lines' texts, for a pass that reads a construct's text.
func textsOf(ls []line) []string {
	out := make([]string, len(ls))
	for i, ln := range ls {
		out[i] = ln.text
	}
	return out
}
