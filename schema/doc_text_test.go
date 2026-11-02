package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// TestLoad_DocTextLosesTheIndentationItsContinuationLinesShare pins the text
// docs/SPEC.md's Comments section states: the content between the delimiters,
// trimmed, and the spaces and tabs every continuation line holding text
// shares, compared byte for byte, removed from each continuation line.
func TestLoad_DocTextLosesTheIndentationItsContinuationLinesShare(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, comment, want string
	}{
		{"one line", "/*   Only.   */", "Only."},
		{"aligned continuation", "/* First.\n       Second. */", "First.\nSecond."},
		{"a paragraph after a blank line", "/* First.\n\n       Para.\n         Deeper. */", "First.\n\nPara.\n  Deeper."},
		{"a blank line shorter than the indent", "/* First.\n  \n     Text. */", "First.\n\nText."},
		{"a blank line longer than the indent", "/* First.\n       \n     Text. */", "First.\n  \nText."},
		{"a tab and spaces share nothing", "/* First.\n\tTabbed.\n    Spaced. */", "First.\n\tTabbed.\n    Spaced."},
		{"a shared tab", "/* First.\n\t\tTwo.\n\tOne. */", "First.\n\tTwo.\nOne."},
		{"a first line indented after the delimiter", "/*\n    Body.\n    More.\n*/", "Body.\nMore."},
		{"CR LF line ends", "/* First.\r\n     Second. */", "First.\r\nSecond."},
		{"CR line ends", "/* First.\r\r     Second.\r     Third. */", "First.\r\rSecond.\rThird."},
		{"a no-break space is text", "/* \u00a0First.\u00a0 */", "\u00a0First.\u00a0"},
		{"a form feed is text", "/* A.\n  \fX.\n  \fY. */", "A.\n\fX.\n\fY."},
		{"indents that differ past their first byte", "/* A.\n\t  X.\n\t\tY. */", "A.\n  X.\n\tY."},
		{"a delimiter alone on its line and a deeper line", "/*\n    Call it as:\n\n        t.Drive()\n*/", "Call it as:\n\n    t.Drive()"},
		{"a delimiter alone on its line and a deeper first line", "/*\n      Deeper.\n    Next.\n*/", "  Deeper.\nNext."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			src := c.comment + "\nschema \"s\"\n\n" + c.comment + "\ntype T {\n    " + c.comment + "\n    id String primary\n}\n"
			s, res := schema.LoadString(t.Context(), src, "s.yammm")
			if res.HasErrors() {
				t.Fatalf("load: %v", res.Err())
			}
			typ, ok := s.Type("T")
			if !ok {
				t.Fatal("type T missing")
			}
			id, ok := typ.Property("id")
			if !ok {
				t.Fatal("property id missing")
			}
			for what, got := range map[string]string{"schema": s.Documentation(), "type": typ.Documentation(), "property": id.Documentation()} {
				if got != c.want {
					t.Errorf("%s doc = %q, want %q", what, got, c.want)
				}
			}
		})
	}
}
