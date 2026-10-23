package lsputil

import (
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
)

const testSignature = "\xef\xbb\xbf"

// markedLines is a mark, then "aé😀b" on line 1 and "cd" on line 2. With the
// mark, a is at byte 3, é at 4, 😀 at 6, b at 10, the line feed at 11, c at 12.
const markedLines = testSignature + "aé\U0001F600b\ncd\n"

func byteSpan(id location.SourceID, line, startByte, endByte int) location.Span {
	return location.Span{
		Source: id,
		Start:  location.NewPosition(line, 1, startByte),
		End:    location.NewPosition(line, 1, endByte),
	}
}

func mustRegister(t *testing.T, register func(location.SourceID, []byte) error, id location.SourceID, content string) {
	t.Helper()
	if err := register(id, []byte(content)); err != nil {
		t.Fatalf("register %s: %v", id, err)
	}
}

// characters returns the start and end character of a span on one line.
func characters(t *testing.T, texts *Texts, span location.Span, enc PositionEncoding) [2]int {
	t.Helper()
	start, end, ok := SpanToLSPRange(texts, span, enc)
	if !ok {
		t.Fatalf("SpanToLSPRange(%v) is not ok", span)
	}
	if start[0] != span.Start.Line-1 || end[0] != span.End.Line-1 {
		t.Fatalf("SpanToLSPRange(%v) lines = %d, %d", span, start[0], end[0])
	}
	return [2]int{start[1], end[1]}
}

func TestTexts_AFileIsCountedFromAfterItsMark(t *testing.T) {
	t.Parallel()

	sent := location.MustNewSourceID("test://sent.yammm")
	file := location.MustNewSourceID("test://file.yammm")
	texts := NewTexts()
	mustRegister(t, texts.Register, sent, markedLines)
	mustRegister(t, texts.RegisterFile, file, markedLines)

	tests := []struct {
		name               string
		id                 location.SourceID
		enc                PositionEncoding
		line               int
		startByte, endByte int
		want               [2]int
	}{
		// The client sent the mark, so it is one unit, or three bytes, of line 1.
		{"sent, utf-16, b", sent, PositionEncodingUTF16, 1, 10, 11, [2]int{5, 6}},
		{"sent, utf-8, b", sent, PositionEncodingUTF8, 1, 10, 11, [2]int{10, 11}},
		{"sent, utf-16, the mark itself", sent, PositionEncodingUTF16, 1, 0, 3, [2]int{0, 1}},
		{"sent, utf-8, the mark itself", sent, PositionEncodingUTF8, 1, 0, 3, [2]int{0, 3}},
		{"sent, utf-16, line 2", sent, PositionEncodingUTF16, 2, 13, 14, [2]int{1, 2}},
		{"sent, utf-8, line 2", sent, PositionEncodingUTF8, 2, 13, 14, [2]int{1, 2}},
		// An editor's text of the file starts at a.
		{"file, utf-16, a", file, PositionEncodingUTF16, 1, 3, 4, [2]int{0, 1}},
		{"file, utf-8, a", file, PositionEncodingUTF8, 1, 3, 4, [2]int{0, 1}},
		{"file, utf-16, b", file, PositionEncodingUTF16, 1, 10, 11, [2]int{4, 5}},
		{"file, utf-8, b", file, PositionEncodingUTF8, 1, 10, 11, [2]int{7, 8}},
		{"file, utf-16, line 2", file, PositionEncodingUTF16, 2, 13, 14, [2]int{1, 2}},
		{"file, utf-8, line 2", file, PositionEncodingUTF8, 2, 13, 14, [2]int{1, 2}},
		// A span over the mark has no character of its own: it is the line's start.
		{"file, utf-16, the mark itself", file, PositionEncodingUTF16, 1, 0, 3, [2]int{0, 0}},
		{"file, utf-8, the mark itself", file, PositionEncodingUTF8, 1, 0, 3, [2]int{0, 0}},
		{"file, utf-8, from the mark to a", file, PositionEncodingUTF8, 1, 0, 4, [2]int{0, 1}},
		{"file, utf-8, an empty span at the first byte", file, PositionEncodingUTF8, 1, 0, 0, [2]int{0, 0}},
		{"file, utf-16, an empty span at the first byte", file, PositionEncodingUTF16, 1, 0, 0, [2]int{0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := characters(t, texts, byteSpan(tt.id, tt.line, tt.startByte, tt.endByte), tt.enc); got != tt.want {
				t.Errorf("characters = %v, want %v", got, tt.want)
			}

			// The same position reaches the same byte on the way in. A span
			// that starts in a file's mark is left out: character 0 of a
			// file is the byte after the mark.
			if tt.startByte == 0 && tt.id == file {
				return
			}
			got, ok := ByteOffsetFromLSP(texts, tt.id, tt.line-1, tt.want[0], tt.enc)
			if !ok || got != tt.startByte {
				t.Errorf("ByteOffsetFromLSP(%d, %d) = %d, %v, want %d", tt.line-1, tt.want[0], got, ok, tt.startByte)
			}
		})
	}
}

func TestTexts_CharacterZeroOfAMarkedFileIsTheByteAfterTheMark(t *testing.T) {
	t.Parallel()

	file := location.MustNewSourceID("test://file.yammm")
	texts := NewTexts()
	mustRegister(t, texts.RegisterFile, file, markedLines)

	for _, enc := range []PositionEncoding{PositionEncodingUTF16, PositionEncodingUTF8} {
		if got, ok := ByteOffsetFromLSP(texts, file, 0, 0, enc); !ok || got != len(testSignature) {
			t.Errorf("%s: ByteOffsetFromLSP(0, 0) = %d, %v, want %d", enc, got, ok, len(testSignature))
		}
		// The position's column is the schema's own: it counts the mark.
		want := location.NewPosition(1, 2, len(testSignature))
		if got, ok := PositionFromLSP(texts, file, 0, 0, enc); !ok || got != want {
			t.Errorf("%s: PositionFromLSP(0, 0) = %v, %v, want %v", enc, got, ok, want)
		}
	}
}

func TestTexts_OnlyTheFirstMarkOfAFileIsDecodedAway(t *testing.T) {
	t.Parallel()

	file := location.MustNewSourceID("test://twice.yammm")
	texts := NewTexts()
	mustRegister(t, texts.RegisterFile, file, testSignature+testSignature+"a\n")

	a := byteSpan(file, 1, 6, 7)
	if got, want := characters(t, texts, a, PositionEncodingUTF16), [2]int{1, 2}; got != want {
		t.Errorf("utf-16: characters = %v, want %v", got, want)
	}
	if got, want := characters(t, texts, a, PositionEncodingUTF8), [2]int{3, 4}; got != want {
		t.Errorf("utf-8: characters = %v, want %v", got, want)
	}
}

func TestTexts_AFileThatDoesNotStartWithAMarkIsCountedFromItsFirstByte(t *testing.T) {
	t.Parallel()

	for name, content := range map[string]string{
		"no mark":                       "ab\n",
		"a mark past the first byte":    "a" + testSignature + "b\n",
		"a mark at the start of line 2": "a\n" + testSignature + "b\n",
	} {
		file := location.MustNewSourceID("test://" + strings.ReplaceAll(name, " ", "-") + ".yammm")
		texts := NewTexts()
		mustRegister(t, texts.RegisterFile, file, content)

		if got, want := characters(t, texts, byteSpan(file, 1, 1, 2), PositionEncodingUTF8), [2]int{1, 2}; got != want {
			t.Errorf("%s: characters = %v, want %v", name, got, want)
		}
	}
}

func TestTexts_ASourceKeepsTheOriginItWasFirstRegisteredWith(t *testing.T) {
	t.Parallel()

	sentFirst := location.MustNewSourceID("test://sent-first.yammm")
	fileFirst := location.MustNewSourceID("test://file-first.yammm")
	texts := NewTexts()
	mustRegister(t, texts.Register, sentFirst, markedLines)
	mustRegister(t, texts.RegisterFile, sentFirst, markedLines)
	mustRegister(t, texts.RegisterFile, fileFirst, markedLines)
	mustRegister(t, texts.Register, fileFirst, markedLines)

	if got, want := characters(t, texts, byteSpan(sentFirst, 1, 3, 4), PositionEncodingUTF16), [2]int{1, 2}; got != want {
		t.Errorf("text the client sent counts its mark: characters = %v, want %v", got, want)
	}
	if got, want := characters(t, texts, byteSpan(fileFirst, 1, 3, 4), PositionEncodingUTF16), [2]int{0, 1}; got != want {
		t.Errorf("a file does not count its mark: characters = %v, want %v", got, want)
	}
}

func TestTexts_RegisterFileRefusesOtherContentForARegisteredSource(t *testing.T) {
	t.Parallel()

	id := location.MustNewSourceID("test://changed.yammm")
	texts := NewTexts()
	mustRegister(t, texts.Register, id, "a\n")

	if err := texts.RegisterFile(id, []byte(testSignature+"a\n")); err == nil {
		t.Fatal("RegisterFile took other content for a registered source")
	}
	if content, ok := texts.ContentBySource(id); !ok || string(content) != "a\n" {
		t.Errorf("ContentBySource = %q, %v, want the first content", content, ok)
	}
	// The refused content's mark is not recorded.
	if got, want := characters(t, texts, byteSpan(id, 1, 1, 2), PositionEncodingUTF8), [2]int{1, 2}; got != want {
		t.Errorf("characters = %v, want %v", got, want)
	}
}

func TestTexts_ReadsAnswerFromTheRegisteredContent(t *testing.T) {
	t.Parallel()

	id := location.MustNewSourceID("test://reads.yammm")
	texts := NewTexts()
	if texts.Has(id) || texts.Len() != 0 {
		t.Fatalf("a new Texts holds a source: Has = %v, Len = %d", texts.Has(id), texts.Len())
	}
	mustRegister(t, texts.RegisterFile, id, markedLines)

	if !texts.Has(id) || texts.Len() != 1 {
		t.Errorf("Has = %v, Len = %d, want true, 1", texts.Has(id), texts.Len())
	}
	// The content's own line start counts the mark.
	if start, ok := texts.LineStartByte(id, 1); !ok || start != 0 {
		t.Errorf("LineStartByte(1) = %d, %v, want 0", start, ok)
	}
	if start, ok := texts.LineStartByte(id, 2); !ok || start != 12 {
		t.Errorf("LineStartByte(2) = %d, %v, want 12", start, ok)
	}
	if got, want := texts.PositionAt(id, 12), location.NewPosition(2, 1, 12); got != want {
		t.Errorf("PositionAt(12) = %v, want %v", got, want)
	}
}
