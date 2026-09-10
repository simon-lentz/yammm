package lsp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/format"
	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/testutil"
	"github.com/simon-lentz/yammm/schema"
)

// TestFormattingHandler_LogsARefusalApartFromAParseError pins the two ways the
// formatter declines: a refusal is a defect worth a warning, a parse error is
// the normal state of a buffer mid-edit.
func TestFormattingHandler_LogsARefusalApartFromAParseError(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name  string
		err   error
		level slog.Level
		msg   string
	}{
		{"refusal", fmt.Errorf("%w: token 3", format.ErrNotPreserved), slog.LevelWarn, "formatting refused"},
		{"parse error", errors.New("parse failed: unexpected end"), slog.LevelDebug, "formatting skipped due to parse error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var logs bytes.Buffer
			logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
			h := formattingHandler(&fakeResolver{
				docSnap: &docstate.Snapshot{URI: "file:///t.yammm", Version: 1, Text: "schema \"t\"\n"},
			}, logger, func(string) (string, error) { return "", tt.err })

			var result []protocol.TextEdit
			if err := callHandler(t, h, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: "file:///t.yammm"},
			}, &result); err != nil {
				t.Fatalf("the request failed: %v", err)
			}
			if len(result) != 0 {
				t.Errorf("got %d edits, want none", len(result))
			}
			if want := "level=" + tt.level.String() + " msg=\"" + tt.msg; !strings.Contains(logs.String(), want) {
				t.Errorf("the log lacks %q:\n%s", want, logs.String())
			}
		})
	}
}

// TestHandleFormatting_NeverEditsWhatASchemaMeans formats a document the
// formatter drops an enum value from: the editor gets no edit, or one that
// means what the document meant.
func TestHandleFormatting_NeverEditsWhatASchemaMeans(t *testing.T) {
	t.Parallel()

	src, err := os.ReadFile(filepath.Join("..", "format", "testdata", "roundtrip", "enum_value_on_opening_line.yammm"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	before, ok := lspSchemaHash(string(src))
	if !ok {
		t.Fatal("the document must load for its meaning to be compared")
	}

	h := handleFormatting(&fakeResolver{
		docSnap: &docstate.Snapshot{URI: "file:///x.yammm", Version: 1, Text: string(src)},
	}, testutil.DiscardLogger())
	var result []protocol.TextEdit
	if err := callHandler(t, h, &protocol.DocumentFormattingParams{
		TextDocument: protocol.TextDocumentIdentifier{URI: "file:///x.yammm"},
	}, &result); err != nil {
		t.Fatalf("the request failed: %v", err)
	}
	if len(result) == 0 {
		return
	}
	after, ok := lspSchemaHash(result[0].NewText)
	if !ok {
		t.Fatalf("the edit does not load:\n%s", result[0].NewText)
	}
	if after != before {
		t.Errorf("the edit changes what the schema means (%s -> %s):\n%s", before, after, result[0].NewText)
	}
}

// lspSchemaHash loads src and returns its structural hash, reporting whether it
// loaded.
func lspSchemaHash(src string) (string, bool) {
	s, result := schema.LoadString(context.Background(), src, "x.yammm")
	if result.Err() != nil || s == nil {
		return "", false
	}
	return schema.StructuralHash(s), true
}
