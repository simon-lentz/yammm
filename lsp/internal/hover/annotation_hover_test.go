package hover

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
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

// TestAtPosition_AnnotationHover_MultibytePrefix pins that the LSP char is
// converted to a byte column: with multi-byte runes before @index, treating the
// UTF-16 char as a byte offset would land the scan left of the annotation. The
// three Ω runes are one UTF-16 unit but two bytes each; char 7 is the 'i' of
// @index (byte 10).
func TestAtPosition_AnnotationHover_MultibytePrefix(t *testing.T) {
	t.Parallel()
	doc := &docstate.Snapshot{Text: "Ω Ω Ω @index"}
	h, err := AtPosition(nil, doc, 0, 7, lsputil.PositionEncodingUTF16, discardLogger())
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	if h == nil {
		t.Fatal("expected annotation hover for @index after a multi-byte prefix, got nil")
	}
	if !strings.Contains(h.Contents.Value, "@index") {
		t.Errorf("hover content should mention @index; got %q", h.Contents.Value)
	}
	// Each Ω is 2 bytes but 1 UTF-16 code unit, so "@index" spans bytes 9..15 and
	// UTF-16 columns 6..12. Emitting byte columns shifts the client's highlight
	// three characters right, off the token — every other hover range converts
	// outbound via SpanToLSPRange, and this one must too.
	if h.Range == nil {
		t.Fatal("annotation hover should carry a range")
	}
	if got, want := h.Range.Start.Character, protocol.UInteger(6); got != want {
		t.Errorf("range start character = %d, want %d (UTF-16 units, not bytes)", got, want)
	}
	if got, want := h.Range.End.Character, protocol.UInteger(12); got != want {
		t.Errorf("range end character = %d, want %d (UTF-16 units, not bytes)", got, want)
	}
}

// TestAtPosition_AnnotationHover_InStringNoHover pins that an @name inside a
// string literal is not treated as an annotation.
func TestAtPosition_AnnotationHover_InStringNoHover(t *testing.T) {
	t.Parallel()
	doc := &docstate.Snapshot{Text: "schema \"m\"\ntype T {\n\temail Pattern[\"user@index.com\"]\n}\n"}
	h, err := AtPosition(nil, doc, 2, 22, lsputil.PositionEncodingUTF16, discardLogger())
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	if h != nil {
		t.Errorf("no annotation hover expected inside a string literal, got %q", h.Contents.Value)
	}
}

// TestAtPosition_AnnotationHover_InCommentNoHover pins that an @name inside a
// block comment is not treated as an annotation.
func TestAtPosition_AnnotationHover_InCommentNoHover(t *testing.T) {
	t.Parallel()
	doc := &docstate.Snapshot{Text: "schema \"m\"\ntype T {\n\t/* see @vector */\n}\n"}
	h, err := AtPosition(nil, doc, 2, 10, lsputil.PositionEncodingUTF16, discardLogger())
	if err != nil {
		t.Fatalf("hover error: %v", err)
	}
	if h != nil {
		t.Errorf("no annotation hover expected inside a comment, got %q", h.Contents.Value)
	}
}
