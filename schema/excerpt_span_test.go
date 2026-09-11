package schema_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// sourceBytes serves one source's content for excerpt rendering.
type sourceBytes struct {
	id      location.SourceID
	content []byte
}

func (s sourceBytes) Content(span location.Span) ([]byte, bool) {
	return s.content, span.Source == s.id
}

// TestExcerpt_MarksTheParsersSpan renders a loaded schema's diagnostic with an
// excerpt. The parser counts a span's columns in runes and the renderer turns
// them into terminal columns, so the marks must sit under the text the parser
// spanned, past a tab, a wide rune and a combining mark.
func TestExcerpt_MarksTheParsersSpan(t *testing.T) {
	t.Parallel()

	// Each line is line 5 of its schema. The invariant's span runs to the end
	// of its line, which holds 28 columns after the tab.
	rows := []struct {
		name  string
		line  string
		code  diag.Code
		marks string
	}{
		{"a tab-indented property of an unknown type", "\tname Strin", diag.E_UNKNOWN_TYPE, "\t" + strings.Repeat("^", 10)},
		{"an invariant holding wide runes and a combining mark", "\t! \"\u6f22\u5b57 cafe\u0301\" nme -> Len > 0", diag.E_UNKNOWN_PROPERTY, "\t" + strings.Repeat("^", 28)},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			content := "schema \"x\"\n\ntype A {\n\tid String primary\n" + row.line + "\n}\n"
			dir := t.TempDir()
			p := filepath.Join(dir, "x.yammm")
			writeHostPathFile(t, p, content)

			_, res := schema.Load(t.Context(), p, schema.WithModuleRoot(dir))
			var issue diag.Issue
			found := false
			for is := range res.Issues() {
				if is.Code() == row.code {
					issue, found = is, true
					break
				}
			}
			if !found || !issue.HasSpan() {
				t.Fatalf("no spanned %s in %v", row.code, issueCodes(res))
			}

			c := diag.NewCollector(0)
			c.Collect(issue)
			out := diag.NewRenderer(
				diag.WithSourceProvider(sourceBytes{id: issue.Span().Source, content: []byte(content)}),
				diag.WithExcerpts(true),
			).FormatResult(c.Result())
			if want := "\n  |\n5 | " + row.line + "\n  | " + row.marks; !strings.HasSuffix(out, want) {
				t.Errorf("span %v renders\n%q\nwant suffix\n%q", issue.Span(), out, want)
			}
		})
	}
}
