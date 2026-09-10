package format

import (
	"errors"
	"fmt"
	"strings"

	"github.com/simon-lentz/yammm/internal/parse"
)

// ErrNotPreserved reports that formatting would have changed the source's
// tokens or comments. It is a defect in the formatter, never in the input, and
// [TokenStream] returns no output with it.
var ErrNotPreserved = errors.New("format: output does not preserve the source")

// preserves is the formatter's postcondition: out carries in's non-trivia
// tokens and comment texts, whitespace and a trailing comma before "]" or "{"
// set aside. Those are the only things a formatter may change.
func preserves(in, out string) error {
	return preservesTokens(parse.Lex(lineEndingReplacer.Replace(in)), out)
}

// preservesTokens is preserves over a source already lexed.
func preservesTokens(in []parse.Token, out string) error {
	a := significant(in)
	b := significant(parse.Lex(out))
	n := min(len(a), len(b))
	for i := range n {
		if !sameToken(a[i], b[i]) {
			return fmt.Errorf("token %d: source %s %q, output %s %q", i, a[i].Kind, a[i].Value, b[i].Kind, b[i].Value)
		}
	}
	if len(a) != len(b) {
		return fmt.Errorf("source has %d significant tokens, output %d", len(a), len(b))
	}
	return nil
}

// significant drops whitespace and every comma the formatter may add or remove.
func significant(toks []parse.Token) []parse.Token {
	out := make([]parse.Token, 0, len(toks))
	for i, tok := range toks {
		if tok.Kind == kindWS {
			continue
		}
		if tok.Kind == kindCOMMA && closesList(toks[i+1:]) {
			continue
		}
		out = append(out, tok)
	}
	return out
}

// closesList reports whether the next token that is not trivia closes a list.
func closesList(rest []parse.Token) bool {
	for _, tok := range rest {
		switch tok.Kind {
		case kindWS, kindSLComment, kindDocComment:
			continue
		case kindRBRACK, kindLBRACE:
			return true
		default:
			return false
		}
	}
	return false
}

func sameToken(a, b parse.Token) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case kindSLComment:
		return strings.TrimRight(a.Value, " \t") == strings.TrimRight(b.Value, " \t")
	case kindDocComment:
		return commentLines(a.Value) == commentLines(b.Value)
	default:
		return a.Value == b.Value
	}
}

func commentLines(text string) string {
	ls := strings.Split(text, "\n")
	for i, l := range ls {
		ls[i] = strings.TrimSpace(l)
	}
	return strings.Join(ls, "\n")
}
