package format

import (
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/parse"
)

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

// preserveSrc is formatted already, so a rewrite that returns it unchanged is
// the identity and any other rewrite changes the text.
const preserveSrc = "schema \"s\"\n\ntype T {\n\tid String primary // note\n\tv  Enum[\"a\", \"b\"]\n}\n"

// TestTokenStream_RefusesARewriteThatDoesNotPreserve drives the postcondition
// through rewrites that break it, so it stays asserted once no live formatter
// defect is left to trip it.
func TestTokenStream_RefusesARewriteThatDoesNotPreserve(t *testing.T) {
	t.Parallel()

	for name, phases := range map[string]func([]line) string{
		"a value dropped":   func(ls []line) string { return strings.Replace(joinLines(ls), `"a", `, "", 1) },
		"a comment changed": func(ls []line) string { return strings.Replace(joinLines(ls), "// note", "// note,", 1) },
		"a type appended":   func(ls []line) string { return joinLines(ls) + "\ntype U {\n\tid String primary\n}\n" },
		"the body cut":      func(ls []line) string { return "schema \"s\"\n" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := tokenStream(preserveSrc, phases)
			if !errors.Is(err, ErrNotPreserved) {
				t.Fatalf("err = %v, want ErrNotPreserved", err)
			}
			if out != "" {
				t.Errorf("a refusal returned output %q", out)
			}
		})
	}
}

// TestTokenStream_AcceptsARewriteOfWhatItOwns pins the other direction: moving
// whitespace and adding a trailing comma before "]" is what formatting is.
func TestTokenStream_AcceptsARewriteOfWhatItOwns(t *testing.T) {
	t.Parallel()

	const owned = "schema \"s\"\n\ntype T {\n\tid String primary   // note\n\tv Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t]\n}\n"
	out, err := tokenStream(preserveSrc, func([]line) string { return owned })
	if err != nil {
		t.Fatalf("a rewrite of whitespace and an owned comma was refused: %v", err)
	}
	if out != owned {
		t.Errorf("out = %q, want the rewrite unchanged", out)
	}
}

// TestLexicalLines_TokensAreTheWholeSource pins that the tokens the
// postcondition compares against are the whole source. A parse that stopped
// early would let a dropped tail agree with itself.
func TestLexicalLines_TokensAreTheWholeSource(t *testing.T) {
	t.Parallel()

	checked := 0
	for _, path := range agreementCorpus(t) {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		normalized := lineEndingReplacer.Replace(string(src))
		_, tokens, err := lexicalLines(normalized)
		if err != nil {
			continue
		}
		checked++
		if !slices.Equal(tokens, parse.Lex(normalized)) {
			t.Errorf("%s: the parse's tokens are not the source's", path)
		}
	}
	if checked < 30 {
		t.Fatalf("checked %d fixtures; the corpus walk is not reaching testdata", checked)
	}
}
