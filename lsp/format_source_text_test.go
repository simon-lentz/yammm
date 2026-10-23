package lsp

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
)

// TestHandleFormatting_FormatsADocumentHoldingRefusedText pins that a document
// whose one fault is a character the source rules refuse inside a comment or a
// literal, or an escape whose value is not UTF-8, is still formatted: the grammar matches it, and its diagnostic is the
// analysis's to publish.
func TestHandleFormatting_FormatsADocumentHoldingRefusedText(t *testing.T) {
	t.Parallel()
	for name, text := range map[string]string{
		"a NUL in a comment":             "schema \"s\"\n// a \x00 b\ntype T {\n  id String primary\n}\n",
		"a byte order mark in a comment": "schema \"s\"\n/* a \uFEFF b */\ntype T {\n  id String primary\n}\n",
		"a NUL in a string":              "schema \"s\"\ntype T {\n  id String primary\n  k Enum[\"a\x00\", \"b\"]\n}\n",
		"an escape that is not UTF-8":    "schema \"s\"\ntype T {\n  id String primary\n  k Enum[\"\\xff\", \"b\"]\n}\n",
		"a byte order mark in a message": "schema \"s\"\ntype T {\n  id String primary\n  ! \"m\uFEFF\" id != \"\"\n}\n",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			h := handleFormatting(&fakeResolver{
				docSnap: &docstate.Snapshot{URI: "file:///test.yammm", Version: 1, Text: text},
			}, testutil.DiscardLogger())
			var result []protocol.TextEdit
			err := callHandler(t, h, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: "file:///test.yammm"},
			}, &result)
			if err != nil {
				t.Fatal(err)
			}
			if len(result) != 1 {
				t.Fatalf("%d edits, want one for the badly indented document", len(result))
			}
			if !strings.Contains(result[0].NewText, "\tid String primary") {
				t.Errorf("the edit does not indent the property: %q", result[0].NewText)
			}
		})
	}
}
