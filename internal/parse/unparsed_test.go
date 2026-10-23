package parse

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
)

// TestParse_UnparsedHoldsTheGrammarFailuresAlone pins File.Unparsed: an issue
// for a construct that did not parse or an escape the language does not have
// outside an invariant expression is in it, and an issue for text the source rules refuse inside a token, for
// an escape whose value is not UTF-8 or for a constraint is not. Every issue
// in it is an E_SYNTAX of the issue list, in the list's order.
func TestParse_UnparsedHoldsTheGrammarFailuresAlone(t *testing.T) {
	t.Parallel()
	rows := []struct {
		name, src string
		want      []string
	}{
		{"clean", checkSource(`n Integer`), nil},
		{"a NUL in a comment", "schema \"s\"\n// a \x00 b\ntype T {\n\tid String primary\n}\n", nil},
		{"an invalid byte in a string", checkSource("e Enum[\"a\xff\", \"b\"]"), nil},
		{"a byte order mark in a regex", checkSource("! \"m\" id =~ /a\uFEFF/"), nil},
		{"an escape that is not UTF-8", checkSource(`e Enum["\xff", "b"]`), nil},
		{"an escape that is not UTF-8 in a schema name", "schema \"g\\xff\"\ntype T {\n\tid String primary\n}\n", nil},
		{"inverted bounds", checkSource(`n Integer[5, 1]`), nil},
		{"an escape with malformed digits in an enum", checkSource(`e Enum["\xZZ", "b"]`), []string{"invalid enum value: unquote string: invalid syntax"}},
		{"an escape with malformed digits in a pattern", checkSource(`p Pattern["\xZZ"]`), []string{"invalid pattern: unquote string: invalid syntax"}},
		{"an escape with malformed digits in a format", checkSource(`t Timestamp["\xZZ"]`), []string{"invalid timestamp format: unquote string: invalid syntax"}},
		{"an escape with malformed digits in a message", checkSource(`! "\xZZ" id != ""`), []string{"invalid invariant message: unquote string: invalid syntax"}},
		{"an escape with malformed digits in a schema name", "schema \"\\xZZ\"\ntype T {\n\tid String primary\n}\n", []string{"invalid schema name: unquote string: invalid syntax"}},
		{"an escape with malformed digits in an import path", "schema \"s\"\nimport \"\\xZZ\"\ntype T {\n\tid String primary\n}\n", []string{"invalid import path: unquote string: invalid syntax"}},
		{"a surrogate escape", checkSource(`e Enum["\uD800", "b", "c"]`), []string{"invalid enum value: unquote string: invalid syntax"}},
		{"an unexpected token", checkSource(`n Integer primary primary`), []string{`unexpected token "}" in a type body`}},
		{"a NUL outside every token", "schema \"s\"\ntype T {\n\tid String primary\n}\n\x00\n", []string{`unexpected token "\x00" in a declaration`}},
		{"an unterminated type body", "schema \"s\"\ntype T {\n\tid String primary\n", []string{"unexpected end of input in a type body"}},
		{"a malformed number", checkSource(`n Integer[0x10, 5]`), []string{InvalidNumberMessage("0x10")}},
		{"no schema header", "type T {\n\tid String primary\n}\n", []string{`unexpected token "type" in the schema header`}},
		{"three failures in three declarations", "schema \"s\"\ntype A {\n\tid String primary\n\te Enum[\"\\xZZ\", \"b\"]\n}\ntype B {\n\tid String primary primary\n}\ntype C {\n\tn Integer[0x10, 5]\n\tt Timestamp[\"\\xZZ\"]\n}\n", []string{
			"invalid enum value: unquote string: invalid syntax",
			`unexpected token "}" in a type body`,
			InvalidNumberMessage("0x10"),
			"invalid timestamp format: unquote string: invalid syntax",
		}},
		{"a refused comment beside an unexpected token", "schema \"s\"\n// a \x00 b\ntype T {\n\tid String primary primary\n}\n", []string{`unexpected token "}" in a type body`}},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			f, issues := Parse([]byte(row.src), location.NewSourceID("s.yammm"))
			var got []string
			at := 0
			for _, u := range f.Unparsed {
				got = append(got, u.Message())
				if u.Code() != diag.E_SYNTAX {
					t.Errorf("Unparsed holds a %s", u.Code())
				}
				found := slices.IndexFunc(issues[at:], func(iss diag.Issue) bool {
					return iss.Message() == u.Message() && iss.Span() == u.Span()
				})
				if found < 0 {
					t.Fatalf("Unparsed issue %q is not in the issue list from index %d on: %s", u.Message(), at, messages(issues))
				}
				at += found + 1
			}
			if !slices.Equal(got, row.want) {
				t.Errorf("Unparsed = %q, want %q; issues: %s", got, row.want, messages(issues))
			}
		})
	}
}

// TestParse_ExpressionsHoldsEveryInvariantExpression pins that an invariant
// left out of the tree for its message still gives File.Expressions its
// expression's extent, in source order.
func TestParse_ExpressionsHoldsEveryInvariantExpression(t *testing.T) {
	t.Parallel()
	src := "schema \"s\"\ntype T {\n\tid String primary\n" +
		"\t! \"a\" id != \"\"\n" +
		"\t! \"\\xff\" id  ==  \"b\"\n" +
		"\t! \"c\xff\" id  =~  /d/\n" +
		"\t! \"\\xZZ\" id  !=  \"e\"\n}\n"
	f, _ := Parse([]byte(src), location.NewSourceID("s.yammm"))
	var got []string
	for _, span := range f.Expressions {
		got = append(got, src[span.Start.Byte:span.End.Byte])
	}
	want := []string{`id != ""`, `id  ==  "b"`, `id  =~  /d/`, `id  !=  "e"`}
	if !slices.Equal(got, want) {
		t.Errorf("Expressions cover %q, want %q", got, want)
	}
	if n := len(f.Types[0].Invariants); n != 1 {
		t.Errorf("the tree holds %d invariants, want the one whose message has a value", n)
	}
}
