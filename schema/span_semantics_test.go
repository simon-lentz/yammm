// Substrate contract for the spans this package hands to every consumer.
//
// These tests state yammm's own rules — offsets are bytes, columns are runes,
// ends are exclusive, and the lexical stream elides nothing — rather than any
// property of the parser generator underneath. The file they replaced pinned
// ANTLR's rune-based indexing and the conversion that bridged it to byte
// offsets; there is no conversion now, because the parser reads bytes.
//
// A failure here means a span no longer slices back to the text it names, which
// is the invariant the LSP's position conversion and every diagnostic renderer
// rest on.
package schema_test

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// multibyteSource exercises every width UTF-8 has: ASCII, two-byte Greek,
// three-byte kanji and a four-byte emoji. Identifiers stay ASCII because the
// grammar admits no others; the wide runes live where the language allows
// them — the schema name, a comment, and string values.
const multibyteSource = "schema \"日本語\"\n" +
	"// αβγ comment 🎉\n" +
	"type Doc = String[1, 10]\n" +
	"type Record {\n" +
	"\tid String primary\n" +
	"\tstatus Enum[\"日\", \"🎉\"]\n" +
	"}\n"

func TestSpans_TokenOffsetsAreBytes(t *testing.T) {
	t.Parallel()
	for _, tok := range parse.Lex(multibyteSource) {
		if got := multibyteSource[tok.Start:tok.End]; got != tok.Value {
			t.Errorf("token %s at [%d,%d) slices to %q, want %q",
				tok.Kind, tok.Start, tok.End, got, tok.Value)
		}
	}
}

// Every token after the wide runes must sit at a byte offset the rune index
// does not reach, or the test above would pass on a byte-blind lexer too.
func TestSpans_ByteOffsetsAreNotRuneIndices(t *testing.T) {
	t.Parallel()
	toks := parse.Lex(multibyteSource)
	last := toks[len(toks)-1]

	runes := utf8.RuneCountInString(multibyteSource[:last.Start])
	if runes >= last.Start {
		t.Errorf("last token starts at byte %d and rune %d; the fixture does not "+
			"distinguish the two, so this file proves nothing", last.Start, runes)
	}
}

func TestSpans_EndIsExclusive(t *testing.T) {
	t.Parallel()
	file, issues := parse.Parse([]byte(multibyteSource), location.NewSourceID("m.yammm"))
	if len(issues) != 0 {
		t.Fatalf("fixture does not parse cleanly: %v", issues)
	}

	span := file.NameSpan
	width := span.End.Byte - span.Start.Byte
	if want := len(`"日本語"`); width != want {
		t.Errorf("schema-name span width = %d bytes, want %d — End must be exclusive", width, want)
	}
	if got := multibyteSource[span.Start.Byte:span.End.Byte]; got != `"日本語"` {
		t.Errorf("schema-name span slices to %q, want the quoted name", got)
	}
}

// Offsets count bytes and columns count runes, which is what internal/source's
// own line mapper does — so an editor and a diagnostic agree on where a span is.
// The second enum value is the subject because a three-byte rune precedes it on
// its own line, which is the only shape where the two counts disagree.
func TestSpans_ColumnsCountRunes(t *testing.T) {
	t.Parallel()
	file, issues := parse.Parse([]byte(multibyteSource), location.NewSourceID("m.yammm"))
	if len(issues) != 0 {
		t.Fatalf("fixture does not parse cleanly: %v", issues)
	}

	var status *parse.Property
	for _, decl := range file.Types {
		for _, prop := range decl.Properties {
			if prop.Name == "status" {
				status = prop
			}
		}
	}
	if status == nil || status.Constraint == nil || len(status.Constraint.EnumLits) != 2 {
		t.Fatal("the two-value enum property did not parse")
	}

	span := status.Constraint.EnumLits[1].Span
	line := lineOf(multibyteSource, span.Start.Line)
	byteInLine := strings.Index(line, `"🎉"`)
	wantColumn := utf8.RuneCountInString(line[:byteInLine]) + 1

	if span.Start.Column != wantColumn {
		t.Errorf("column = %d, want %d (runes, not bytes)", span.Start.Column, wantColumn)
	}
	if wantColumn == byteInLine+1 {
		t.Errorf("rune column %d equals the byte column; the fixture does not "+
			"distinguish the two, so this test proves nothing", wantColumn)
	}
}

// The lexical stream elides nothing: a formatter reads whitespace and comments
// from it, which the node tree deliberately does not carry.
func TestSpans_LexElidesNothing(t *testing.T) {
	t.Parallel()
	seen := make(map[string]bool)
	for _, tok := range parse.Lex(multibyteSource) {
		seen[tok.Kind] = true
	}
	for _, kind := range []string{"WS", "SL_COMMENT", "STRING", "UC_WORD", "LC_WORD"} {
		if !seen[kind] {
			t.Errorf("Lex elided %s; the un-elided stream must carry it", kind)
		}
	}
	if seen["EOF"] {
		t.Error("Lex returned an EOF token; the stream ends without one")
	}
}

// A line comment stops before its newline, so the newline belongs to the
// whitespace that follows and a formatter can re-indent without eating it.
func TestSpans_LineCommentExcludesItsNewline(t *testing.T) {
	t.Parallel()
	var found bool
	for _, tok := range parse.Lex(multibyteSource) {
		if tok.Kind != "SL_COMMENT" {
			continue
		}
		found = true
		if strings.ContainsAny(tok.Value, "\r\n") {
			t.Errorf("SL_COMMENT %q contains a line break", tok.Value)
		}
	}
	if !found {
		t.Fatal("fixture carries no line comment")
	}
}

// Every diagnostic a load reports carries a byte offset that slices back into
// the source, which is what the LSP converts to an editor range.
func TestSpans_DiagnosticSpansSliceBackToSource(t *testing.T) {
	t.Parallel()
	src := multibyteSource + "type Broken {\n\tx Vector[0]\n}\n"
	_, res := schema.LoadString(context.Background(), src, "m.yammm")

	var checked int
	for iss := range res.Issues() {
		span := iss.Span()
		if span.IsZero() {
			continue
		}
		if !span.Start.HasByte() {
			t.Errorf("%s span has no byte offset: %v", iss.Code(), span)
			continue
		}
		if span.Start.Byte < 0 || span.End.Byte > len(src) || span.End.Byte < span.Start.Byte {
			t.Errorf("%s span [%d,%d) is out of range for a %d-byte source",
				iss.Code(), span.Start.Byte, span.End.Byte, len(src))
			continue
		}
		if !utf8.ValidString(src[span.Start.Byte:span.End.Byte]) {
			t.Errorf("%s span [%d,%d) splits a rune", iss.Code(), span.Start.Byte, span.End.Byte)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no span-bearing diagnostic to check")
	}
}

// lineOf returns the 1-based line, without its terminator.
func lineOf(src string, line int) string {
	lines := strings.Split(src, "\n")
	if line < 1 || line > len(lines) {
		return ""
	}
	return lines[line-1]
}
