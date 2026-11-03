package markdown

import (
	"strings"
	"testing"
)

// TestMarshal_InvariantFenceShowsTheDeclarationAsWritten pins the declaration
// source an invariant's fence holds. A lone CR or a CR LF in it is the line end
// it is in the source. A comment the span starts with is the declaration's doc
// comment, stripped whether it says anything or not. A continuation line keeps
// its indent beyond the declaration's own.
func TestMarshal_InvariantFenceShowsTheDeclarationAsWritten(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, decl, want string }{
		{"a lone CR", "\t! \"bounded\" n > 0\r&& n < 10\n", "    ! \"bounded\" n > 0\n    && n < 10\n"},
		{"a CR LF", "\t! \"bounded\" n > 0\r\n\t\t&& n < 10\r\n", "    ! \"bounded\" n > 0\n    \t&& n < 10\n"},
		{"an empty doc comment", "\t/* */\n\t! \"bounded\" n > 0\n", "    ```yammm\n    ! \"bounded\" n > 0\n"},
		{"a blank doc comment", "\t/*\n\t*/ ! \"bounded\" n > 0\n", "    ```yammm\n    ! \"bounded\" n > 0\n"},
		{"a deeper continuation", "\t! \"bounded\" n > 0\n\t\t\t&& n < 10\n\t\t&& n != 5\n", "    ! \"bounded\" n > 0\n    \t\t&& n < 10\n    \t&& n != 5\n"},
		{"a shallower continuation", "\t\t! \"bounded\" n > 0\n\t&& n < 10\n", "    ! \"bounded\" n > 0\n    && n < 10\n"},
		{"a doc comment and CR LF", "\t/* doc */\r\n\t! \"bounded\" n > 0\r\n", "    ```yammm\n    ! \"bounded\" n > 0\n"},
		{"a doc comment indented otherwise", "\t/* doc */\n  ! \"bounded\" n > 0\n    && n < 10\n", "    ! \"bounded\" n > 0\n      && n < 10\n"},
		{"lone CRs before the declaration", "\r\t! \"bounded\" n > 0\r\t\t&& n < 10\r", "    ! \"bounded\" n > 0\n    \t&& n < 10\n"},
		{"a line comment a lone CR ends", "\t/* doc */ // note\r\t! \"bounded\" n > 0\r", "    ```yammm\n    ! \"bounded\" n > 0\n"},
		{"a line comment after the doc comment", "\t/* doc */ // note\n\t! \"bounded\" n > 0\n", "    ```yammm\n    ! \"bounded\" n > 0\n"},
		{"a declaration after its doc comment's end", "\t/* doc\n*/  ! \"bounded\" n > 0\n    && n < 10\n", "    ! \"bounded\" n > 0\n    && n < 10\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := marshalString(t, "schema \"s\"\ntype A {\n\tid String primary\n\tn Integer\n"+tt.decl+"}\n")
			if !strings.Contains(doc, tt.want) {
				t.Errorf("document does not hold %q:\n%s", tt.want, doc)
			}
			if strings.Contains(doc, "\r") || strings.Contains(doc, "/*") {
				t.Errorf("the fence holds a CR or a comment:\n%q", doc)
			}
		})
	}
}
