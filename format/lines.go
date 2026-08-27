package format

import "strings"

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

// line is one source line and its class.
type line struct {
	text  string
	class lineClass
}

// classifyText splits text into lines and classifies each one. It is the one
// place line structure is decided; every pass takes its output.
func classifyText(text string) []line {
	raw := strings.Split(text, "\n")
	ls := make([]line, len(raw))
	inDocComment := false

	for i, text := range raw {
		trimmed := strings.TrimSpace(text)
		ls[i].text = text

		switch {
		case inDocComment:
			ls[i].class = lineComment
			if strings.Contains(trimmed, "*/") {
				inDocComment = false
			}
		case trimmed == "":
			ls[i].class = lineBlank
		case strings.HasPrefix(trimmed, "/*"):
			ls[i].class = lineComment
			if !strings.Contains(trimmed, "*/") {
				inDocComment = true
			}
		case strings.HasPrefix(trimmed, "//"):
			ls[i].class = lineComment
		default:
			ls[i].class = lineContent
		}
	}
	return ls
}

// joinLines is the inverse of classifyText over the text.
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
	return line{text: text, class: lineContent}
}

// textsOf returns the lines' texts, for a pass that reads a construct's text.
func textsOf(ls []line) []string {
	out := make([]string, len(ls))
	for i, ln := range ls {
		out[i] = ln.text
	}
	return out
}
