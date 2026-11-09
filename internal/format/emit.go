package format

import "strings"

// chunkKind tells the emitter what a written run of bytes is, so the lexical
// record is produced with the text instead of being re-derived from it.
type chunkKind int

const (
	chunkPlain   chunkKind = iota // separators, identifiers, punctuation
	chunkLiteral                  // a STRING or REGEXP token
	chunkComment                  // a SL_COMMENT or DOC_COMMENT token
)

// emitter accumulates phase 1's output and the lexer's view of each line it
// completes. It exists because the token stream is the only place that knows a
// comma is inside a literal, and phase 1 is the last place that holds it.
type emitter struct {
	lineStart bool
	cur       strings.Builder // the line being built
	curLex    lexical
	done      []line
}

func newEmitter() *emitter {
	return &emitter{lineStart: true, curLex: lexical{commentAt: noComment}}
}

// write appends text, closing a line record at every newline it contains.
func (e *emitter) write(text string, kind chunkKind) {
	if text == "" {
		return
	}
	for {
		nl := strings.IndexByte(text, '\n')
		if nl < 0 {
			e.append(text, kind)
			break
		}
		e.append(text[:nl], kind)
		// A blank line inside a block comment is comment text, not a section break.
		if kind == chunkComment && e.curLex.commentAt == noComment {
			e.curLex.commentAt = e.cur.Len()
		}
		e.closeLine()
		text = text[nl+1:]
	}
}

// append adds one newline-free run to the current line and records what it is.
func (e *emitter) append(text string, kind chunkKind) {
	if text == "" {
		return
	}
	start := e.cur.Len()
	e.cur.WriteString(text)
	switch kind {
	case chunkLiteral:
		e.curLex.literals = append(e.curLex.literals, span{start: start, end: start + len(text)})
	case chunkComment:
		// A doc comment's later lines start at column 0; only the first run on
		// a line opens that line's comment.
		if e.curLex.commentAt == noComment {
			e.curLex.commentAt = start
		}
	case chunkPlain:
	}
	e.lineStart = updateLineStart(e.lineStart, text)
}

// closeLine files the current line and starts the next.
func (e *emitter) closeLine() {
	text := e.cur.String()
	e.done = append(e.done, line{text: text, class: classOf(text, e.curLex), lex: e.curLex})
	e.cur.Reset()
	e.curLex = lexical{commentAt: noComment}
	e.lineStart = true
}

// lines closes the final line and returns the classified result.
func (e *emitter) lines() []line {
	e.closeLine()
	return e.done
}

// classOf decides a line's class from what was written, not from its text: a
// line is a comment when its comment starts at or before its first non-space
// byte, which no text test can settle for a value that merely looks like one.
func classOf(text string, lex lexical) lineClass {
	if lex.commentAt >= 0 && strings.TrimSpace(text[:lex.commentAt]) == "" {
		return lineComment
	}
	if strings.TrimSpace(text) == "" {
		return lineBlank
	}
	return lineContent
}
