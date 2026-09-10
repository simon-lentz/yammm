package format

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestTokenStream_SyntaxErrorCarriesTheDiagnostic pins what a caller receives
// for a source that does not parse: a *SyntaxError holding the parser's
// positioned E_SYNTAX issue, whose message keeps the "parse failed: " form the
// language server logs.
func TestTokenStream_SyntaxErrorCarriesTheDiagnostic(t *testing.T) {
	t.Parallel()

	out, err := TokenStream("schema \"test\"\ntype Person {\n\n")
	if out != "" {
		t.Errorf("a source that does not parse produced output %q", out)
	}
	syntaxErr, ok := errors.AsType[*SyntaxError](err)
	if !ok {
		t.Fatalf("error %v (%T) is not a *SyntaxError", err, err)
	}
	iss := syntaxErr.Issue
	if iss.Code() != diag.E_SYNTAX {
		t.Errorf("code = %s, want %s", iss.Code(), diag.E_SYNTAX)
	}
	if !iss.HasSpan() || iss.Span().Start.Line != 4 {
		t.Errorf("span = %v (present %v), want line 4", iss.Span(), iss.HasSpan())
	}
	if want := "parse failed: " + iss.Message(); err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
	if errors.Is(err, ErrNotPreserved) {
		t.Error("a syntax error reads as a refusal")
	}
}
