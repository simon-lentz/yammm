package format_test

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/format"
)

// An invariant holds no type position, so the word Enum before a bracket in one
// is a member or a function name and the bracket an index. Wrapping it as an
// Enum's value list writes a trailing comma, which an index does not take.
func TestSource_AnInvariantLineIsNotWrappedAsAnEnum(t *testing.T) {
	t.Parallel()
	message := `"` + strings.Repeat("x", 90) + `"`
	for name, expression := range map[string]string{
		"a member named Enum":   `$self. Enum[0] == "n"`,
		"a function named Enum": `name -> Enum[0] == "n"`,
		"the bracket left open": "$self. Enum[\n\t\t0] == \"n\"",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\n\ntype T {\n\tname String\n\t! " + message + " " + expression + "\n}\n"
			once, err := format.Source([]byte(src))
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			twice, err := format.Source(once)
			if err != nil {
				t.Fatalf("the output does not format again: %v\n%s", err, once)
			}
			if string(twice) != string(once) {
				t.Errorf("the output is no fixed point:\n%s\nthen:\n%s", once, twice)
			}
		})
	}
}

// A line an invariant's expression continues on holds no declaration, wherever
// it is indented: no rule for an Enum or an extends clause reads it.
func TestSource_AnInvariantContinuationLineIsNoDeclaration(t *testing.T) {
	t.Parallel()
	long := `"` + strings.Repeat("x", 90) + `"`
	for name, member := range map[string]string{
		"at the indent of its first line": "\t! \"m\" name == \"a\" ||\n\t$self. Enum[0] == " + long,
		"after a message alone":           "\t! \"m\"\n\t$self. Enum[0] == " + long,
		"at column zero":                  "\t! \"m\" name == \"a\" ||\n$self. Enum[0] == " + long,
		"the bracket left open":           "\t! \"m\" name == \"a\" ||\n\t$self. Enum[\n\t0] == \"n\"",
		"an extends clause's words":       "\t! \"m\" name == \"a\" ||\n\ttype -> Default( extends , name, " + long + ") -> Map {\n\t$0 }",
		"an open extends clause's words":  "\t! \"m\" name == \"a\" ||\n\ttype == extends ||\n\tname == \"b\" ||\n\tname -> Map {\n\t$0 }",
		"before a comment":                "\t! \"m\" name == \"a\" ||\n\t$self. Enum[0] == " + long + " // why",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			src := "schema \"s\"\n\ntype T {\n\tname String\n" + member + "\n}\n"
			once, err := format.Source([]byte(src))
			if err != nil {
				t.Fatalf("format: %v", err)
			}
			if strings.Join(strings.Fields(string(once)), " ") != strings.Join(strings.Fields(src), " ") {
				t.Errorf("the output holds other tokens than the source:\n%s", once)
			}
			twice, err := format.Source(once)
			if err != nil {
				t.Fatalf("the output does not format again: %v\n%s", err, once)
			}
			if string(twice) != string(once) {
				t.Errorf("the output is no fixed point:\n%s\nthen:\n%s", once, twice)
			}
		})
	}
}
