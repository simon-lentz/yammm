package lsp

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

func TestByteOffsetFromLSP_UTF16_ASCII(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://ascii.yammm")
	// Line 1: "hello\n" (bytes 0-5, 6 total including newline)
	// Line 2: "world\n" (bytes 6-11)
	content := []byte("hello\nworld\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		line     int // 0-based LSP line
		char     int // 0-based UTF-16 code unit offset
		wantByte int
	}{
		{"start of file", 0, 0, 0},
		{"middle of line 1", 0, 2, 2},
		{"end of line 1 content", 0, 5, 5},
		{"start of line 2", 1, 0, 6},
		{"middle of line 2", 1, 2, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, tt.line, tt.char, PositionEncodingUTF16)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(line=%d, char=%d) = %d; want %d",
					tt.line, tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_UTF16_BMP(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://bmp.yammm")
	// "héllo" = h(1) + é(2) + l(1) + l(1) + o(1) = 6 bytes
	// UTF-16: h(1) + é(1) + l(1) + l(1) + o(1) = 5 code units
	content := []byte("héllo\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		char     int // UTF-16 code unit offset
		wantByte int
	}{
		{"before h", 0, 0},
		{"after h (before é)", 1, 1},
		{"after é (before first l)", 2, 3}, // é is 2 bytes
		{"after first l", 3, 4},
		{"after second l", 4, 5},
		{"after o", 5, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char, PositionEncodingUTF16)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(char=%d) = %d; want %d",
					tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_UTF16_Surrogate(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://emoji.yammm")
	// "a😀b" = a(1) + 😀(4) + b(1) = 6 bytes
	// UTF-16: a(1) + 😀(2 surrogates) + b(1) = 4 code units
	content := []byte("a😀b\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		char     int // UTF-16 code unit offset
		wantByte int
	}{
		{"before a", 0, 0},
		{"after a (at emoji)", 1, 1},
		{"mid-surrogate (second half of emoji)", 2, 1}, // Floor to start of emoji
		{"after emoji (at b)", 3, 5},
		{"after b", 4, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char, PositionEncodingUTF16)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(char=%d) = %d; want %d",
					tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_UTF16_CJK(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://cjk.yammm")
	// "日本語" = 9 bytes (3 per char), 3 UTF-16 code units (all BMP)
	content := []byte("日本語\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		char     int
		wantByte int
	}{
		{"at 日", 0, 0},
		{"at 本", 1, 3},
		{"at 語", 2, 6},
		{"after 語", 3, 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char, PositionEncodingUTF16)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(char=%d) = %d; want %d",
					tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_UTF8_Encoding(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8.yammm")
	content := []byte("héllo\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// With UTF-8 encoding, character offset IS byte offset from line start
	tests := []struct {
		name     string
		char     int
		wantByte int
	}{
		{"offset 0", 0, 0},
		{"offset 1", 1, 1},
		{"offset 2", 2, 2},
		{"offset 3", 3, 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char, PositionEncodingUTF8)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(char=%d, UTF8) = %d; want %d",
					tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_MultiLine(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://multiline.yammm")
	// Line 1: "type 日本 {\n" = 11 bytes (type=4, space=1, 日=3, 本=3, space=1, {=1, \n=1)
	// Wait, let me recalculate:
	// "type " = 5 bytes
	// "日" = 3 bytes
	// "本" = 3 bytes
	// " {" = 2 bytes
	// "\n" = 1 byte
	// Total line 1 = 14 bytes (0-13)
	// Line 2: "  name\n" = 7 bytes (14-20)
	content := []byte("type 日本 {\n  name\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		line     int
		char     int
		wantByte int
	}{
		{"line 1, start", 0, 0, 0},
		{"line 1, at 日", 0, 5, 5},     // After "type "
		{"line 1, at 本", 0, 6, 8},     // After "type 日"
		{"line 1, after 本", 0, 7, 11}, // After "type 日本"
		{"line 2, start", 1, 0, 14},   // Start of "  name"
		{"line 2, at 'n'", 1, 2, 16},  // After "  "
		{"line 2, at 'a'", 1, 3, 17},  // After "  n"
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, tt.line, tt.char, PositionEncodingUTF16)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(line=%d, char=%d) = %d; want %d",
					tt.line, tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_InvalidLine(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://invalid.yammm")
	content := []byte("hello\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Invalid line should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 10, 0, PositionEncodingUTF16)
	if ok {
		t.Error("ByteOffsetFromLSP(line=10) should return ok=false for invalid line")
	}
}

func TestByteOffsetFromLSP_UnknownSource(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	// Unknown source should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 0, 5, PositionEncodingUTF16)
	if ok {
		t.Error("ByteOffsetFromLSP(unknown source) should return ok=false")
	}
}

func TestByteOffsetFromLSP_DefaultEncoding(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://default.yammm")
	// "a😀" = a(1) + 😀(4) = 5 bytes
	// UTF-16: a(1) + 😀(2) = 3 code units
	content := []byte("a😀\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Unknown encoding should default to UTF-16
	got, ok := ByteOffsetFromLSP(sources, sourceID, 0, 3, PositionEncoding("unknown"))
	if !ok {
		t.Fatal("ByteOffsetFromLSP returned ok=false")
	}
	// char 3 in UTF-16 = after emoji = byte 5
	if got != 5 {
		t.Errorf("ByteOffsetFromLSP(unknown encoding, char=3) = %d; want 5", got)
	}
}

func TestUtf16CharToByteOffset_Negative(t *testing.T) {
	t.Parallel()

	content := []byte("hello")
	got := utf16CharToByteOffset(content, 0, -1)
	if got != 0 {
		t.Errorf("utf16CharToByteOffset(charOffset=-1) = %d; want 0", got)
	}

	got = utf16CharToByteOffset(content, 0, 0)
	if got != 0 {
		t.Errorf("utf16CharToByteOffset(charOffset=0) = %d; want 0", got)
	}
}

func TestUtf16CharToByteOffset_StopsAtNewline(t *testing.T) {
	t.Parallel()

	content := []byte("ab\ncd")
	// Should stop at newline character
	got := utf16CharToByteOffset(content, 0, 10)
	// Should stop at byte 2 (the newline)
	if got != 2 {
		t.Errorf("utf16CharToByteOffset(past newline) = %d; want 2", got)
	}
}

func TestUtf16CharToByteOffset_InvalidUTF8(t *testing.T) {
	t.Parallel()

	// Invalid UTF-8 sequence: continuation byte without lead byte
	content := []byte{0x80, 0x81, 'a', 'b'}
	got := utf16CharToByteOffset(content, 0, 2)
	// Each invalid byte should be counted as 1 UTF-16 unit
	// So char 2 should be at byte 2
	if got != 2 {
		t.Errorf("utf16CharToByteOffset(invalid UTF-8) = %d; want 2", got)
	}
}

func TestSpanToLSPRange_ZeroSpan(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	var span location.Span // zero span

	_, _, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if ok {
		t.Error("SpanToLSPRange(zero span) = ok; want !ok")
	}
}

func TestSpanToLSPRange_UnknownStart(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	span := location.Span{
		Source: sourceID,
		// Start is unknown (zero Position)
	}

	_, _, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if ok {
		t.Error("SpanToLSPRange(unknown start) = ok; want !ok")
	}
}

func TestSpanToLSPRange_Valid(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 10, Column: 5, Byte: 100},
		End:    location.Position{Line: 10, Column: 15, Byte: 110},
	}

	// When content is not registered, falls back to rune column conversion
	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange() = !ok; want ok")
	}

	// Extract values (fixed-size arrays, safe to index)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Line: 10 (1-based) → 9 (0-based)
	if startLine != 9 {
		t.Errorf("start[0] (line) = %d; want 9", startLine)
	}
	// Column: 5 (1-based) → 4 (0-based) - fallback to rune column
	if startChar != 4 {
		t.Errorf("start[1] (char) = %d; want 4", startChar)
	}

	if endLine != 9 {
		t.Errorf("end[0] (line) = %d; want 9", endLine)
	}
	// Column: 15 (1-based) → 14 (0-based) - fallback to rune column
	if endChar != 14 {
		t.Errorf("end[1] (char) = %d; want 14", endChar)
	}
}

func TestSpanToLSPRange_PointSpan(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	// Point span: only start is set
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 5, Column: 10, Byte: 50},
		// End is zero
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange(point span) = !ok; want ok")
	}

	// Extract values (fixed-size arrays, safe to index)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Start: line 5 → 4, column 10 → 9 (fallback to rune column)
	if startLine != 4 || startChar != 9 {
		t.Errorf("start = [%d, %d]; want [4, 9]", startLine, startChar)
	}

	// End should equal start for point span
	if endLine != startLine || endChar != startChar {
		t.Errorf("end = [%d, %d]; want same as start [%d, %d] for point span", endLine, endChar, startLine, startChar)
	}
}

func TestSpanToLSPRange_NegativeLine(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	// Line 1 should become 0, not negative
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},
	}

	start, _, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange() = !ok; want ok")
	}

	// Extract values (fixed-size array, safe to index)
	startLine, startChar := start[0], start[1]

	if startLine != 0 {
		t.Errorf("start[0] = %d; want 0 for line 1", startLine)
	}
	if startChar != 0 {
		t.Errorf("start[1] = %d; want 0 for column 1", startChar)
	}
}

// --- LSP-005: SpanToLSPRange with unknown byte offset ---

func TestSpanToLSPRange_UnknownByte(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	// Register content so hasContent is true
	content := []byte("type MyType {\n  name String\n}\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Create span with Byte: -1 (unknown byte offset, as created by computeTypeNameSpan)
	// Position represents "MyType" starting at column 6 on line 1
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 6, Byte: -1},
		End:    location.Position{Line: 1, Column: 12, Byte: -1},
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange() = !ok; want ok")
	}

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// When Byte is -1, should fall back to Column-1 (rune column)
	// Line: 1 (1-based) → 0 (0-based)
	if startLine != 0 {
		t.Errorf("start line = %d; want 0", startLine)
	}
	// Column: 6 (1-based) → 5 (0-based)
	if startChar != 5 {
		t.Errorf("start char = %d; want 5 (Column-1 fallback)", startChar)
	}
	if endLine != 0 {
		t.Errorf("end line = %d; want 0", endLine)
	}
	// Column: 12 (1-based) → 11 (0-based)
	if endChar != 11 {
		t.Errorf("end char = %d; want 11 (Column-1 fallback)", endChar)
	}
}

func TestSpanToLSPRange_MixedByteKnowledge(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://mixed.yammm")
	content := []byte("héllo world\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Start has known byte, End has unknown byte
	// "héllo" = h(1) + é(2) + l(1) + l(1) + o(1) = 6 bytes
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},  // Known byte
		End:    location.Position{Line: 1, Column: 6, Byte: -1}, // Unknown byte
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	if !ok {
		t.Fatal("SpanToLSPRange() = !ok; want ok")
	}

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Start should use UTF-16 conversion (byte 0 → char 0)
	if startLine != 0 || startChar != 0 {
		t.Errorf("start = [%d, %d]; want [0, 0]", startLine, startChar)
	}

	// End should fall back to Column-1 (column 6 → char 5)
	if endLine != 0 {
		t.Errorf("end line = %d; want 0", endLine)
	}
	if endChar != 5 {
		t.Errorf("end char = %d; want 5 (Column-1 fallback for unknown byte)", endChar)
	}
}

// --- LSP-008: UTF-8 mode clamps to end-of-line ---

func TestByteOffsetFromLSP_UTF8_ClampsToEOL(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8eol.yammm")
	// Line 1: "abc\n" (bytes 0-3, newline at 3)
	// Line 2: "def\n" (bytes 4-7)
	content := []byte("abc\ndef\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	tests := []struct {
		name     string
		line     int
		char     int
		wantByte int
	}{
		{"within line 1", 0, 2, 2},
		{"at end of line 1 content", 0, 3, 3}, // At newline position
		{"past end of line 1", 0, 10, 3},      // Should clamp to newline, not cross to line 2
		{"within line 2", 1, 2, 6},
		{"past end of line 2", 1, 10, 7}, // Should clamp to newline at position 7
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, ok := ByteOffsetFromLSP(sources, sourceID, tt.line, tt.char, PositionEncodingUTF8)
			if !ok {
				t.Fatal("ByteOffsetFromLSP returned ok=false")
			}
			if got != tt.wantByte {
				t.Errorf("ByteOffsetFromLSP(line=%d, char=%d, UTF8) = %d; want %d",
					tt.line, tt.char, got, tt.wantByte)
			}
		})
	}
}

func TestByteOffsetFromLSP_UTF8_LastLineNoNewline(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://noeol.yammm")
	// Last line has no trailing newline
	content := []byte("abc\ndef")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Line 2 (0-based line 1) has no newline, should clamp to content end
	got, ok := ByteOffsetFromLSP(sources, sourceID, 1, 100, PositionEncodingUTF8)
	if !ok {
		t.Fatal("ByteOffsetFromLSP returned ok=false")
	}
	// Content length is 7, so should clamp to 7
	if got != 7 {
		t.Errorf("ByteOffsetFromLSP(line=1, char=100, UTF8, no trailing newline) = %d; want 7", got)
	}
}

func TestClampToLineEnd(t *testing.T) {
	t.Parallel()

	content := []byte("abc\ndef\n")

	tests := []struct {
		name      string
		lineStart int
		offset    int
		want      int
	}{
		{"negative offset", 0, -5, 0},        // Clamps to lineStart (0)
		{"offset before lineStart", 4, 2, 4}, // Clamps to lineStart (4) - conceptual correctness
		{"within first line", 0, 2, 2},
		{"at newline", 0, 3, 3},
		{"past newline on line 1", 0, 5, 3}, // Should clamp to newline at 3
		{"within second line", 4, 5, 5},
		{"at second newline", 4, 7, 7},
		{"past second newline", 4, 10, 7}, // Should clamp to newline at 7
		{"past content end", 4, 100, 7},   // Should clamp to line end (newline at 7), not content length
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clampToLineEnd(content, tt.lineStart, tt.offset)
			if got != tt.want {
				t.Errorf("clampToLineEnd(lineStart=%d, offset=%d) = %d; want %d",
					tt.lineStart, tt.offset, got, tt.want)
			}
		})
	}
}

func TestClampToLineEnd_NoNewline(t *testing.T) {
	t.Parallel()

	// Content with no trailing newline
	content := []byte("abcdef")

	tests := []struct {
		name      string
		lineStart int
		offset    int
		want      int
	}{
		{"within content", 0, 3, 3},
		{"at content end", 0, 6, 6},
		{"past content end", 0, 100, 6},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := clampToLineEnd(content, tt.lineStart, tt.offset)
			if got != tt.want {
				t.Errorf("clampToLineEnd(offset=%d) = %d; want %d",
					tt.offset, got, tt.want)
			}
		})
	}
}

// --- SpanToLSPRange with UTF-8 encoding ---

func TestSpanToLSPRange_UTF8_ASCII(t *testing.T) {
	// Tests SpanToLSPRange with UTF-8 encoding and ASCII content.
	// In UTF-8 mode, character offset IS byte offset from line start.
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8ascii.yammm")
	// Line 1: "type Foo {\n" (bytes 0-10)
	// Line 2: "}\n" (bytes 11-12)
	content := []byte("type Foo {\n}\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 6, Byte: 5}, // "Foo" starts at byte 5
		End:    location.Position{Line: 1, Column: 9, Byte: 8}, // "Foo" ends at byte 8
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	if !ok {
		t.Fatal("SpanToLSPRange(UTF8) = !ok; want ok")
	}

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// UTF-8 mode: character offset = byte offset from line start
	// Line 1 starts at byte 0, so:
	// Start: byte 5 - byte 0 = char 5
	// End: byte 8 - byte 0 = char 8
	if startLine != 0 || startChar != 5 {
		t.Errorf("start = [%d, %d]; want [0, 5]", startLine, startChar)
	}
	if endLine != 0 || endChar != 8 {
		t.Errorf("end = [%d, %d]; want [0, 8]", endLine, endChar)
	}
}

func TestSpanToLSPRange_UTF8_MultibyteChars(t *testing.T) {
	// Tests SpanToLSPRange with UTF-8 encoding and multi-byte UTF-8 characters.
	// This verifies that UTF-8 mode uses byte offsets, not rune counts.
	// The character "é" is 2 bytes in UTF-8.
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8multi.yammm")
	// "héllo\n" - 'é' is bytes 1-2 (2 bytes), total 7 bytes for line
	// Byte offsets: h=0, é=1-2, l=3, l=4, o=5, \n=6
	content := []byte("héllo\nworld\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Span covering "llo" which starts at byte 3, ends at byte 6
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 4, Byte: 3}, // 'l' at byte 3
		End:    location.Position{Line: 1, Column: 7, Byte: 6}, // after 'o' at byte 6
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	if !ok {
		t.Fatal("SpanToLSPRange(UTF8) = !ok; want ok")
	}

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// UTF-8 mode: character offset = byte offset from line start (byte 0)
	// Start: byte 3 - 0 = char 3
	// End: byte 6 - 0 = char 6
	// Note: In UTF-16 mode this would be different due to 'é' counting as 1 code unit
	if startLine != 0 || startChar != 3 {
		t.Errorf("start = [%d, %d]; want [0, 3]", startLine, startChar)
	}
	if endLine != 0 || endChar != 6 {
		t.Errorf("end = [%d, %d]; want [0, 6]", endLine, endChar)
	}
}

func TestSpanToLSPRange_UTF8_SecondLine(t *testing.T) {
	// Tests SpanToLSPRange with UTF-8 encoding on a non-first line.
	// Verifies byte offset is calculated relative to line start, not file start.
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8line2.yammm")
	// Line 1: "abc\n" (bytes 0-3, newline at 3)
	// Line 2: "defgh\n" (bytes 4-9, newline at 9)
	content := []byte("abc\ndefgh\n")
	if err := sources.Register(sourceID, content); err != nil {
		t.Fatalf("Register() error: %v", err)
	}

	// Span on line 2 covering "ef" (bytes 5-7, relative to file start)
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 2, Column: 2, Byte: 5}, // 'e' at byte 5
		End:    location.Position{Line: 2, Column: 4, Byte: 7}, // after 'f' at byte 7
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	if !ok {
		t.Fatal("SpanToLSPRange(UTF8) = !ok; want ok")
	}

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Line 2 starts at byte 4, so:
	// Start: byte 5 - 4 = char 1
	// End: byte 7 - 4 = char 3
	if startLine != 1 || startChar != 1 {
		t.Errorf("start = [%d, %d]; want [1, 1]", startLine, startChar)
	}
	if endLine != 1 || endChar != 3 {
		t.Errorf("end = [%d, %d]; want [1, 3]", endLine, endChar)
	}
}
