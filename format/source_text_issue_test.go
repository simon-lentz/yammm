package format

import (
	"errors"
	"testing"
)

// TestTokenStream_ReportsTheFirstGrammarFailure pins which diagnostic a
// refusal carries when a source holds text the source rules refuse before two
// constructs the grammar does not match: the first of the two, and not the
// text's.
func TestTokenStream_ReportsTheFirstGrammarFailure(t *testing.T) {
	t.Parallel()
	src := "schema \"s\"\n// a \x00 b\ntype T {\n\tid String primary primary\n}\ntype U {\n\tn Integer[0x10, 5]\n}\n"
	_, err := TokenStream(src)
	syntaxErr, ok := errors.AsType[*SyntaxError](err)
	if !ok {
		t.Fatalf("error %v (%T) is not a *SyntaxError", err, err)
	}
	if got, want := syntaxErr.Issue.Message(), `unexpected token "}" in a type body`; got != want {
		t.Errorf("the refusal carries %q, want %q", got, want)
	}
	if line := syntaxErr.Issue.Span().Start.Line; line != 5 {
		t.Errorf("the refusal is at line %d, want 5", line)
	}
}
