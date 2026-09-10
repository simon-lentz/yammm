package format

import (
	"fmt"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/parse"
)

// preserves is the formatter's postcondition: out carries in's non-trivia
// tokens and comment texts, whitespace and a trailing comma before "]" or "{"
// set aside. It lives beside the tests until TokenStream enforces it.
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

// TestPreserves_RefusesEachCorruptionClass pins the postcondition against one
// instance of every corruption the formatter has produced, and against the
// changes it is allowed to make.
func TestPreserves_RefusesEachCorruptionClass(t *testing.T) {
	t.Parallel()

	const src = "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n"
	if err := preserves(src, src); err != nil {
		t.Fatalf("identity must preserve: %v", err)
	}

	corruptions := map[string]string{
		"a variable split across lines":          "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$\n\tself.v != \"a\"\n}\n",
		"an enum value dropped":                  "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n",
		"a comment's text changed":               "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note,\n\tv Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n",
		"a block comment's blank line collapsed": "schema \"s\"\n\n/*\na\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n",
		"a literal's value changed":              "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"a\", \"B\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n",
		"the tail dropped":                       "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note\n\tv Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n",
		"code swallowed by a comment":            "schema \"s\"\n\n/*\na\n\nb\n*/\ntype T {\n\tid String primary // note v Enum[\"a\", \"b\"]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n",
	}
	for name, out := range corruptions {
		if err := preserves(src, out); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// A comma the formatter does not own, before ")".
	if err := preserves("schema \"s\"\ntype T {\n\tid String primary\n\t--> R (one) T\n}\n",
		"schema \"s\"\ntype T {\n\tid String primary\n\t--> R (one,) T\n}\n"); err == nil {
		t.Error("a comma added before ) was accepted")
	}

	// The formatter owns whitespace and a trailing comma before "]" or "{".
	owned := "schema \"s\"\n\n/*\n  a\n\n  b\n*/\ntype T {\n\tid   String primary   // note\n\tv Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t]\n\t! \"m\" $self.id != \"\"&&$self.v != \"a\"\n}\n"
	if err := preserves(src, owned); err != nil {
		t.Errorf("formatter-owned changes were refused: %v", err)
	}
}
