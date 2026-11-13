package parse

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// syntaxIssue parses one invariant and returns the message and the covered
// text of its first issue, which must be an E_SYNTAX.
func syntaxIssue(t *testing.T, expression string) (message, covered string) {
	t.Helper()
	full := invariantSource(expression)
	_, issues := Parse([]byte(full), location.NewSourceID("s.yammm"))
	if len(issues) == 0 || issues[0].Code() != diag.E_SYNTAX {
		t.Fatalf("%q: %s; want an E_SYNTAX first", expression, messages(issues))
	}
	sp := issues[0].Span()
	return issues[0].Message(), full[sp.Start.Byte:sp.End.Byte]
}

// The expression language has no caret operator.
func TestParse_CaretIsNoOperator(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]string{
		"n ^ n":       `unexpected token "^" in a type body`,
		"true ^ true": `unexpected token "^" in a type body`,
		"^n":          `unexpected token "^" in an expression`,
	} {
		if got, covered := syntaxIssue(t, src); got != want || covered != "^" {
			t.Errorf("%q: %q at %q, want %q at the caret", src, got, covered, want)
		}
	}
}

// A datatype keyword is no operand, on the right of a match as anywhere else.
// A name that is no keyword reads a member.
func TestParse_DatatypeKeywordIsNoOperand(t *testing.T) {
	t.Parallel()
	keywords := []string{
		"String", "Integer", "Float", "Boolean", "Timestamp", "Date", "UUID",
		"Enum", "Pattern", "Vector", "List",
	}
	if len(keywords) != len(datatypeKeywords) {
		t.Fatalf("the test names %d keywords, the parser holds %d", len(keywords), len(datatypeKeywords))
	}
	for _, kw := range keywords {
		for _, src := range []string{kw, "n =~ " + kw, "n !~ " + kw, "n == " + kw, "[" + kw + "] -> Len == 1", kw + " -> TypeOf == \"x\"", kw + "[0] == nil"} {
			want := `unexpected keyword "` + kw + `" in an expression`
			if got, covered := syntaxIssue(t, src); got != want || covered != kw {
				t.Errorf("%q: %q at %q, want %q at the keyword", src, got, covered, want)
			}
		}
	}
	file, issues := Parse([]byte(invariantSource("n == Alias")), location.NewSourceID("s.yammm"))
	if len(issues) != 0 {
		t.Fatalf("a name that is no keyword: %s", messages(issues))
	}
	if got, want := sexprString(file.Types[0].Invariants[0].Expr), `(== (p "n") (p "Alias"))`; got != want {
		t.Errorf("n == Alias parsed as %s, want %s", got, want)
	}
}

// A bracket holds one index expression: none, two, and one with a trailing
// comma are each refused where the bracket departs from that.
func TestParse_BracketHoldsOneIndex(t *testing.T) {
	t.Parallel()
	for src, want := range map[string]struct{ message, covered string }{
		"xs[] == nil":     {"expected an index before ']'", "]"},
		"xs[0, 1] == nil": {"expected ']' after one index", ","},
		"xs[0,] == nil":   {"expected ']' after one index", ","},
		"xs[,] == nil":    {`unexpected token "," in an expression`, ","},
	} {
		if got, covered := syntaxIssue(t, src); got != want.message || covered != want.covered {
			t.Errorf("%q: %q at %q, want %q at %q", src, got, covered, want.message, want.covered)
		}
	}
	for src, want := range map[string]string{
		"xs[0] == nil":         `(== (@ (p "xs") 0) <nil>)`,
		"xs[0][1] == nil":      `(== (@ (@ (p "xs") 0) 1) <nil>)`,
		"xs[n - 1] == nil":     `(== (@ (p "xs") (- (p "n") 1)) <nil>)`,
		"xs[[1] -> Len] == 1":  `(== (@ (p "xs") (Len ([] 1) args[] params[] <absent>)) 1)`,
		"xs[f1 ? {0 : 1}] > 0": `(> (@ (p "xs") (? (p "f1") 0 1)) 0)`,
	} {
		file, issues := Parse([]byte(invariantSource(src)), location.NewSourceID("s.yammm"))
		if len(issues) != 0 {
			t.Errorf("%q: %s", src, messages(issues))
			continue
		}
		if got := sexprString(file.Types[0].Invariants[0].Expr); got != want {
			t.Errorf("%q parsed as %s, want %s", src, got, want)
		}
	}
}

// Any word after -> names a function, a keyword among them. The catalogue
// decides whether the name is one, so the parser refuses only what is no word.
func TestParse_AnyWordNamesAFunction(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"Len", "len", "in", "p", "nil", "true", "false", "type", "part", "primary", "String", "List", "worksAt", "WORKS_AT",
	} {
		file, issues := Parse([]byte(invariantSource("n -> "+name+" == nil")), location.NewSourceID("s.yammm"))
		if len(issues) != 0 {
			t.Errorf("n -> %s: %s", name, messages(issues))
			continue
		}
		want := `(== (` + name + ` (p "n") args[] params[] <absent>) <nil>)`
		if got := sexprString(file.Types[0].Invariants[0].Expr); got != want {
			t.Errorf("n -> %s parsed as %s, want %s", name, got, want)
		}
	}
	for src, covered := range map[string]string{"n -> _ == nil": "_", "n -> 5 == nil": "5", "n -> \"Len\" == nil": `"Len"`, "n -> (Len) == nil": "("} {
		if got, at := syntaxIssue(t, src); got != "expected function name after '->'" || at != covered {
			t.Errorf("%q: %q at %q, want the function-name message at %q", src, got, at, covered)
		}
	}
}
