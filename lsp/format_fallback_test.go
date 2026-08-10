package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// A document that does not parse is left alone: the handler returns no edits
// rather than rewriting text the formatter could not read.
//
// This is the path a user is on for most of an editing session — a schema is
// unparseable between almost every pair of keystrokes — and it had no test
// before the formatter's input shape changed.
func TestHandleFormatting_UnparseableDocumentReturnsNoEdits(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct{ name, text string }{
		{"unclosed type body", "schema \"test\"\n\ntype Person {\n\tname String\n"},
		{"half-typed pipeline", "schema \"test\"\n\ntype Person {\n\t! \"m\" x->\n}\n"},
		{"no schema header", "type Person {\n\tid String primary\n}\n"},
		{"stray delimiter", "schema \"test\"\n\ntype Person {\n\tname String]\n}\n"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := handleFormatting(&fakeResolver{
				docSnap: &docstate.Snapshot{
					URI:     "file:///broken.yammm",
					Version: 1,
					Text:    tt.text,
				},
			}, testutil.DiscardLogger())

			var result []protocol.TextEdit
			if err := callHandler(t, h, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: "file:///broken.yammm"},
			}, &result); err != nil {
				t.Fatalf("formatting an unparseable document must not fail the request: %v", err)
			}
			if len(result) != 0 {
				t.Errorf("got %d edits, want none: %+v", len(result), result)
			}
		})
	}
}
