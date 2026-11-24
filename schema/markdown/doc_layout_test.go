package markdown

import (
	"strings"
	"testing"

	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/text"
)

// TestMarshal_DocLayoutMakesNoCodeBlock pins, in the emitted document read by
// a CommonMark parser, that the indentation a schema writes a doc comment
// with is no indented code block: a paragraph after a blank line in the
// comment is a paragraph, in a type's section, in the title's doc and under a
// relation's bullet.
func TestMarshal_DocLayoutMakesNoCodeBlock(t *testing.T) {
	t.Parallel()

	doc := marshalString(t, `/* Schema summary.

       Schema detail. */
schema "s"

    /* Type summary.

       Type detail. */
    type T {
        id String primary
        /* Relation summary.

           Relation detail. */
        --> TO (one) U
    }

    type U {
        id String primary
    }
`)
	src := []byte(doc)
	root := newParser().Parser().Parse(text.NewReader(src))
	var paragraphs []string
	err := ast.Walk(root, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeBlock:
			var body strings.Builder
			for i := range n.Lines().Len() {
				seg := n.Lines().At(i)
				body.Write(seg.Value(src))
			}
			t.Errorf("an indented code block holds %q", body.String())
		case ast.KindParagraph:
			var body strings.Builder
			for i := range n.Lines().Len() {
				seg := n.Lines().At(i)
				body.Write(seg.Value(src))
			}
			paragraphs = append(paragraphs, strings.TrimSpace(body.String()))
		}
		return ast.WalkContinue, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Schema detail.", "Type detail.", "Relation detail."} {
		found := false
		for _, p := range paragraphs {
			found = found || p == want
		}
		if !found {
			t.Errorf("no paragraph reads %q; paragraphs: %q", want, paragraphs)
		}
	}
}
