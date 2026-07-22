package hover

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestAtPosition_PropertyAnnotationHover(t *testing.T) {
	t.Parallel()
	// Parse-independent: no snapshot. Cursor on "index" of a property @index.
	doc := &docstate.Snapshot{Text: "schema \"m\"\ntype T {\n\tstate String @index\n}\n"}
	h, err := AtPosition(nil, doc, 2, 16, lsputil.PositionEncodingUTF16, discardLogger())
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	if h == nil {
		t.Fatal("expected annotation hover for @index, got nil")
	}
	if !strings.Contains(h.Contents.Value, "@index") {
		t.Errorf("hover content should mention @index; got %q", h.Contents.Value)
	}
}

func TestAtPosition_TypeAnnotationHover(t *testing.T) {
	t.Parallel()
	// Cursor on "index" of a type-level @@index member.
	doc := &docstate.Snapshot{Text: "schema \"m\"\ntype T {\n\tid String primary\n\t@@index(id)\n}\n"}
	h, err := AtPosition(nil, doc, 3, 5, lsputil.PositionEncodingUTF16, discardLogger())
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	if h == nil {
		t.Fatal("expected annotation hover for @@index, got nil")
	}
	if !strings.Contains(h.Contents.Value, "@@index") {
		t.Errorf("hover content should mention @@index; got %q", h.Contents.Value)
	}
}
