package parse

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

// Every syntax diagnostic names where the parser was when it failed. The
// phrases are a closed user-facing vocabulary spread across two parsers — the
// declaration grammar composes four of them through syntaxErr, the Pratt
// parser writes the rest — and nothing but this test holds them to one shape.
//
// The shape is "in <article> <construct>". An earlier end-of-input message
// read "in type body" beside its own sibling's "in a type body", which is the
// kind of drift a closed vocabulary exists to prevent.

var locationPhrase = regexp.MustCompile(`\bin (a|an|the) [a-z ]+$`)

func TestSyntaxMessages_LocationVocabulary(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want string
	}{{
		name: "schema header",
		src:  "schema x",
		want: `unexpected token "x" in the schema header`,
	}, {
		name: "import declaration",
		src:  "schema \"s\"\nimport 3\n",
		want: `unexpected token "3" in an import declaration`,
	}, {
		name: "declaration",
		src:  "schema \"s\"\ntype 3 {}\n",
		want: `unexpected token "3" in a declaration`,
	}, {
		name: "type body",
		src:  "schema \"s\"\ntype A { ! }\n",
		want: `unexpected token "}" in a type body`,
	}, {
		name: "type body at end of input",
		src:  "schema \"s\"\ntype A {",
		want: `unexpected end of input in a type body`,
	}, {
		name: "expression, unexpected token",
		src:  "schema \"s\"\ntype A {\n\tid String primary\n\t! \"m\" )\n}\n",
		want: `unexpected token ")" in an expression`,
	}, {
		name: "expression, reserved keyword as operand",
		src:  "schema \"s\"\ntype A {\n\tid String primary\n\t! \"m\" as > 1\n}\n",
		want: `unexpected keyword "as" in an expression`,
	}, {
		name: "expression, bare in",
		src:  "schema \"s\"\ntype A {\n\tid String primary\n\t! \"m\" in\n}\n",
		want: `unexpected 'in' in an expression`,
	}, {
		name: "expression list",
		src:  "schema \"s\"\ntype A {\n\tid String primary\n\t! \"m\" id -> concat(1 2)\n}\n",
		want: `expected ',' or closing delimiter in an expression list`,
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, issues := Parse([]byte(tt.src), location.NewSourceID(tt.name))
			var got []string
			for _, iss := range issues {
				got = append(got, iss.Message())
			}
			if !slices.Contains(got, tt.want) {
				t.Errorf("no diagnostic reads %q\ngot: %s", tt.want, strings.Join(got, "\n     "))
			}
			if !locationPhrase.MatchString(tt.want) {
				t.Errorf("%q does not end in an articled location phrase", tt.want)
			}
		})
	}
}
