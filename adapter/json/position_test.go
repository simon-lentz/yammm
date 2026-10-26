package json

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// positionCorpus holds every shape the two converters must agree on: an empty
// document, both line breaks, multibyte runes before a position, a line that
// ends the content without a break, a lone "\r", and malformed UTF-8 — a
// truncated sequence, a stray continuation byte, a surrogate, one of each
// straight after a line break, and one inside a document the parser reads. A
// decoder offset reaches a malformed sequence, so the two must agree there too.
var positionCorpus = map[string]string{
	"empty":                   "",
	"one line no break":       `{"Person": [{"name": "ada"}]}`,
	"lf":                      "{\n  \"Person\": [\n    {\"name\": \"ada\"}\n  ]\n}\n",
	"crlf":                    "{\r\n  \"Person\": [\r\n    {\"name\": \"ada\"}\r\n  ]\r\n}\r\n",
	"bare cr":                 "{\r  \"Person\": []\r}\r",
	"multibyte":               "{\n  \"Kafé\": [\n    {\"naïve\": \"élan\"}\n  ]\n}\n",
	"multibyte in value":      "{\"a\": \"日本語のテキスト\", \"b\": 1}",
	"blank lines":             "{\n\n\n  \"A\": []\n\n}\n",
	"trailing break":          "{}\n",
	"astral":                  "{\"e\": \"\U0001F600\U0001F600x\"}",
	"truncated rune":          "\xe2\x82",
	"stray continuation":      "a\x80b",
	"surrogate":               "\xed\xa0\x80",
	"malformed at line start": "a\n\x80b",
	"malformed in a document": "{\"A\": [\x80\x80]}",
}

// TestPositionTable_AgreesWithSourceRegistry holds the adapter's converter to
// the one the schema loader reads positions through, offset by offset over
// every document in the corpus, the out-of-range offsets included.
func TestPositionTable_AgreesWithSourceRegistry(t *testing.T) {
	for name, text := range positionCorpus {
		t.Run(name, func(t *testing.T) {
			content := []byte(text)
			id := location.MustNewSourceID("test://position/" + name)
			reg := source.NewRegistry()
			if err := reg.Register(id, content); err != nil {
				t.Fatalf("register: %v", err)
			}
			table := newPositionTable(content)

			for off := -2; off <= len(content)+2; off++ {
				want := reg.PositionAt(id, off)
				got := table.positionAt(off)
				if got != want {
					t.Errorf("offset %d: got %+v, want %+v", off, got, want)
				}
			}
		})
	}
}

// TestPositionTable_CountsRunesNotBytes pins the column rule the agreement test
// cannot state on its own: the two converters would agree while both counted
// bytes.
func TestPositionTable_CountsRunesNotBytes(t *testing.T) {
	table := newPositionTable([]byte("élan x\nsecond"))

	if got := table.positionAt(6); got.Line != 1 || got.Column != 6 {
		t.Errorf("the \"x\" at byte 6, the sixth rune: got %d:%d, want 1:6", got.Line, got.Column)
	}
	if got := table.positionAt(8); got.Line != 2 || got.Column != 1 {
		t.Errorf("line 2 starts at byte 8: got %d:%d, want 2:1", got.Line, got.Column)
	}
}

// TestPositionTable_OffsetInsideARuneReportsThatRune pins the floor rule, which
// no offset the decoder reports can reach but the registry's contract states.
func TestPositionTable_OffsetInsideARuneReportsThatRune(t *testing.T) {
	table := newPositionTable([]byte("aé"))

	if got := table.positionAt(2); got.Column != 2 {
		t.Errorf("second byte of the two-byte rune: got column %d, want 2", got.Column)
	}
}

// TestPositionTable_CountsEveryByteItDecodes holds the cost measure to its
// definition: a line's checkpoints decode the line once, and each position
// adds the bytes it decodes past its checkpoint.
func TestPositionTable_CountsEveryByteItDecodes(t *testing.T) {
	table := newPositionTable([]byte(strings.Repeat("a", 1000)))

	steps := []struct{ offset, counted int }{
		{100, 100},               // no checkpoint: the walk from the line start
		{600, 100 + 1000 + 88},   // the checkpoints, then 512 to 600
		{700, 100 + 1000 + 276},  // the checkpoints are kept: 512 to 700
		{1000, 100 + 1000 + 508}, // the end of input, from the checkpoint at 768
	}
	for _, s := range steps {
		if got := table.positionAt(s.offset); got.Column != s.offset+1 {
			t.Fatalf("offset %d: column %d, want %d", s.offset, got.Column, s.offset+1)
		}
		if table.counted != s.counted {
			t.Errorf("after offset %d: counted %d, want %d", s.offset, table.counted, s.counted)
		}
	}
}

// TestParseObject_PositionsOnOneLineCostTheDocumentLinearly pins that locating
// a diagnostic does not rescan its line from the start. A minified document is
// one line holding a repeated member per eight bytes, so each issue's position
// is asked for at a growing offset. The bytes the parse's own table decodes are
// bounded per byte of the document at both sizes; counting from the line start
// decodes the line's length per position. The table decodes the line at least
// once to place its checkpoints, so a parse that reads positions from another
// table, or a table that counts nothing, falls below the lower bound.
func TestParseObject_PositionsOnOneLineCostTheDocumentLinearly(t *testing.T) {
	t.Parallel()
	// A position decodes at most markStride bytes past its checkpoint, and a
	// line's checkpoints are found once: 17 bytes a byte here.
	const perByte = 64
	for _, repeats := range []int{2_000, 16_000} {
		var b strings.Builder
		b.WriteString(`{"Person": [{"a": 0`)
		for range repeats {
			b.WriteString(`, "a": 0`)
		}
		b.WriteString(`}]}`)
		doc := b.String()
		table := newPositionTable([]byte(doc))
		_, result := New().parseObject(t.Context(), location.MustNewSourceID("test://data/minified.json"), []byte(doc), table)
		decoded := table.counted
		fresh := newPositionTable([]byte(doc))
		issues := 0
		for issue := range result.Issues() {
			issues++
			start := issue.Span().Start
			if got := fresh.positionAt(start.Byte); got != start {
				t.Fatalf("%d repeats: issue %d at %v, a fresh table gives %v", repeats, issues, start, got)
			}
		}
		if issues != repeats {
			t.Fatalf("%d repeats: %d issues", repeats, issues)
		}
		t.Logf("%d bytes, %d decoded", len(doc), decoded)
		if decoded < len(doc) || decoded > perByte*len(doc) {
			t.Errorf("%d repeats: the parse's positions decoded %d bytes of a %d-byte document, want at least the document and at most %d a byte", repeats, decoded, len(doc), perByte)
		}
	}
}
