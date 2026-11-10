package markdown

import (
	"strings"
	"testing"
)

// GitHub reads a document as parse5 does with scripting off: after an open
// <select> it drops every later start tag, and a comment opened inside an open
// noscript element takes the rest of the document. Its rendering of a committed
// file read each input below so, and lost every heading and table after it.
// Each is closed after the comment by a line that reads as nothing.
func TestMarshal_ClosesWhatGitHubReadsAsOpen(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ doc, closer string }{
		{"<select>", "\n</select>\n"},
		{"<select>\n<option>a", "\n</select>\n"},
		{"<noscript>\n<!--\n</noscript>", "\n<!-- -->\n"},
	} {
		out := marshalString(t, "schema \"s\"\n/* "+tt.doc+" */\ntype A {\n  id String primary\n  --> TO (one) B\n}\ntype B {\n  id String primary\n}\n")
		if !strings.Contains(out, tt.doc+tt.closer) {
			t.Errorf("doc %q is not followed by %q:\n%s", tt.doc, tt.closer, out)
		}
	}
}
