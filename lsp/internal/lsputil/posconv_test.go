package lsputil

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToUInteger_Positive(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input int
		want  uint32
	}{
		{0, 0},
		{1, 1},
		{100, 100},
		{65535, 65535},
	}

	for _, tt := range tests {
		got := ToUInteger(tt.input)
		assert.Equal(t, tt.want, got, "ToUInteger(%d)", tt.input)
	}
}

func TestToUInteger_Negative(t *testing.T) {
	t.Parallel()

	tests := []int{-1, -10, -1000}

	for _, input := range tests {
		got := ToUInteger(input)
		assert.Equal(t, uint32(0), got, "ToUInteger(%d) want 0 for negative", input)
	}
}

func TestByteOffsetFromLSP_UTF16_ASCII(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://ascii.yammm")
	// Line 1: "hello\n" (bytes 0-5, 6 total including newline)
	// Line 2: "world\n" (bytes 6-11)
	content := []byte("hello\nworld\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(line=%d, char=%d)", tt.line, tt.char)
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
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
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
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF16_CJK(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://cjk.yammm")
	// "日本語" = 9 bytes (3 per char), 3 UTF-16 code units (all BMP)
	content := []byte("日本語\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF8_Encoding(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8.yammm")
	content := []byte("héllo\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d, UTF8)", tt.char)
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
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(line=%d, char=%d)", tt.line, tt.char)
		})
	}
}

func TestByteOffsetFromLSP_InvalidLine(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://invalid.yammm")
	content := []byte("hello\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Invalid line should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 10, 0, PositionEncodingUTF16)
	assert.False(t, ok, "ByteOffsetFromLSP(line=10) should return ok=false for invalid line")
}

func TestByteOffsetFromLSP_UnknownSource(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown.yammm")

	// Unknown source should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 0, 5, PositionEncodingUTF16)
	assert.False(t, ok, "ByteOffsetFromLSP(unknown source) should return ok=false")
}

func TestByteOffsetFromLSP_DefaultEncoding(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://default.yammm")
	// "a😀" = a(1) + 😀(4) = 5 bytes
	// UTF-16: a(1) + 😀(2) = 3 code units
	content := []byte("a😀\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Unknown encoding should default to UTF-16
	got, ok := ByteOffsetFromLSP(sources, sourceID, 0, 3, PositionEncoding("unknown"))
	require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
	// char 3 in UTF-16 = after emoji = byte 5
	assert.Equal(t, 5, got, "ByteOffsetFromLSP(unknown encoding, char=3)")
}

func TestUtf16CharToByteOffset_Negative(t *testing.T) {
	t.Parallel()

	content := []byte("hello")
	got := UTF16CharToByteOffset(content, 0, -1)
	assert.Equal(t, 0, got, "UTF16CharToByteOffset(charOffset=-1)")

	got = UTF16CharToByteOffset(content, 0, 0)
	assert.Equal(t, 0, got, "UTF16CharToByteOffset(charOffset=0)")
}

func TestUtf16CharToByteOffset_StopsAtNewline(t *testing.T) {
	t.Parallel()

	content := []byte("ab\ncd")
	// Should stop at newline character
	got := UTF16CharToByteOffset(content, 0, 10)
	// Should stop at byte 2 (the newline)
	assert.Equal(t, 2, got, "UTF16CharToByteOffset(past newline)")
}

func TestUtf16CharToByteOffset_InvalidUTF8(t *testing.T) {
	t.Parallel()

	// Invalid UTF-8 sequence: continuation byte without lead byte
	content := []byte{0x80, 0x81, 'a', 'b'}
	got := UTF16CharToByteOffset(content, 0, 2)
	// Each invalid byte should be counted as 1 UTF-16 unit
	// So char 2 should be at byte 2
	assert.Equal(t, 2, got, "UTF16CharToByteOffset(invalid UTF-8)")
}

func TestSpanToLSPRange_ZeroSpan(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	var span location.Span // zero span

	_, _, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	assert.False(t, ok, "SpanToLSPRange(zero span) = ok; want !ok")
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
	assert.False(t, ok, "SpanToLSPRange(unknown start) = ok; want !ok")
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
	require.True(t, ok, "SpanToLSPRange() = !ok; want ok")

	// Extract values (fixed-size arrays, safe to index)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Line: 10 (1-based) -> 9 (0-based)
	assert.Equal(t, 9, startLine, "start[0] (line)")
	// Column: 5 (1-based) -> 4 (0-based) - fallback to rune column
	assert.Equal(t, 4, startChar, "start[1] (char)")

	assert.Equal(t, 9, endLine, "end[0] (line)")
	// Column: 15 (1-based) -> 14 (0-based) - fallback to rune column
	assert.Equal(t, 14, endChar, "end[1] (char)")
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
	require.True(t, ok, "SpanToLSPRange(point span) = !ok; want ok")

	// Extract values (fixed-size arrays, safe to index)
	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Start: line 5 -> 4, column 10 -> 9 (fallback to rune column)
	assert.Equal(t, 4, startLine, "start line")
	assert.Equal(t, 9, startChar, "start char")

	// End should equal start for point span
	assert.Equal(t, startLine, endLine, "end line should equal start line for point span")
	assert.Equal(t, startChar, endChar, "end char should equal start char for point span")
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
	require.True(t, ok, "SpanToLSPRange() = !ok; want ok")

	// Extract values (fixed-size array, safe to index)
	startLine, startChar := start[0], start[1]

	assert.Equal(t, 0, startLine, "start[0] for line 1")
	assert.Equal(t, 0, startChar, "start[1] for column 1")
}

func TestSpanToLSPRange_UnknownByte(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://file.yammm")
	// Register content so hasContent is true
	content := []byte("type MyType {\n  name String\n}\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Create span with Byte: -1 (unknown byte offset, as created by computeTypeNameSpan)
	// Position represents "MyType" starting at column 6 on line 1
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 6, Byte: -1},
		End:    location.Position{Line: 1, Column: 12, Byte: -1},
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	require.True(t, ok, "SpanToLSPRange() = !ok; want ok")

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// When Byte is -1, should fall back to Column-1 (rune column)
	// Line: 1 (1-based) -> 0 (0-based)
	assert.Equal(t, 0, startLine, "start line")
	// Column: 6 (1-based) -> 5 (0-based)
	assert.Equal(t, 5, startChar, "start char (Column-1 fallback)")
	assert.Equal(t, 0, endLine, "end line")
	// Column: 12 (1-based) -> 11 (0-based)
	assert.Equal(t, 11, endChar, "end char (Column-1 fallback)")
}

func TestSpanToLSPRange_MixedByteKnowledge(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://mixed.yammm")
	content := []byte("héllo world\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Start has known byte, End has unknown byte
	// "héllo" = h(1) + é(2) + l(1) + l(1) + o(1) = 6 bytes
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Byte: 0},  // Known byte
		End:    location.Position{Line: 1, Column: 6, Byte: -1}, // Unknown byte
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF16)
	require.True(t, ok, "SpanToLSPRange() = !ok; want ok")

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Start should use UTF-16 conversion (byte 0 -> char 0)
	assert.Equal(t, 0, startLine, "start line")
	assert.Equal(t, 0, startChar, "start char")

	// End should fall back to Column-1 (column 6 -> char 5)
	assert.Equal(t, 0, endLine, "end line")
	assert.Equal(t, 5, endChar, "end char (Column-1 fallback for unknown byte)")
}

func TestByteOffsetFromLSP_UTF8_ClampsToEOL(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8eol.yammm")
	// Line 1: "abc\n" (bytes 0-3, newline at 3)
	// Line 2: "def\n" (bytes 4-7)
	content := []byte("abc\ndef\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

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
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(line=%d, char=%d, UTF8)", tt.line, tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF8_LastLineNoNewline(t *testing.T) {
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://noeol.yammm")
	// Last line has no trailing newline
	content := []byte("abc\ndef")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Line 2 (0-based line 1) has no newline, should clamp to content end
	got, ok := ByteOffsetFromLSP(sources, sourceID, 1, 100, PositionEncodingUTF8)
	require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
	// Content length is 7, so should clamp to 7
	assert.Equal(t, 7, got, "ByteOffsetFromLSP(line=1, char=100, UTF8, no trailing newline)")
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
			got := ClampToLineEnd(content, tt.lineStart, tt.offset)
			assert.Equal(t, tt.want, got,
				"ClampToLineEnd(lineStart=%d, offset=%d)", tt.lineStart, tt.offset)
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
			got := ClampToLineEnd(content, tt.lineStart, tt.offset)
			assert.Equal(t, tt.want, got,
				"ClampToLineEnd(offset=%d)", tt.offset)
		})
	}
}

func TestSpanToLSPRange_UTF8_ASCII(t *testing.T) {
	// Tests SpanToLSPRange with UTF-8 encoding and ASCII content.
	// In UTF-8 mode, character offset IS byte offset from line start.
	t.Parallel()

	sources := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://utf8ascii.yammm")
	// Line 1: "type Foo {\n" (bytes 0-10)
	// Line 2: "}\n" (bytes 11-12)
	content := []byte("type Foo {\n}\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 6, Byte: 5}, // "Foo" starts at byte 5
		End:    location.Position{Line: 1, Column: 9, Byte: 8}, // "Foo" ends at byte 8
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	require.True(t, ok, "SpanToLSPRange(UTF8) = !ok; want ok")

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// UTF-8 mode: character offset = byte offset from line start
	// Line 1 starts at byte 0, so:
	// Start: byte 5 - byte 0 = char 5
	// End: byte 8 - byte 0 = char 8
	assert.Equal(t, 0, startLine, "start line")
	assert.Equal(t, 5, startChar, "start char")
	assert.Equal(t, 0, endLine, "end line")
	assert.Equal(t, 8, endChar, "end char")
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
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Span covering "llo" which starts at byte 3, ends at byte 6
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 4, Byte: 3}, // 'l' at byte 3
		End:    location.Position{Line: 1, Column: 7, Byte: 6}, // after 'o' at byte 6
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	require.True(t, ok, "SpanToLSPRange(UTF8) = !ok; want ok")

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// UTF-8 mode: character offset = byte offset from line start (byte 0)
	// Start: byte 3 - 0 = char 3
	// End: byte 6 - 0 = char 6
	// Note: In UTF-16 mode this would be different due to 'é' counting as 1 code unit
	assert.Equal(t, 0, startLine, "start line")
	assert.Equal(t, 3, startChar, "start char")
	assert.Equal(t, 0, endLine, "end line")
	assert.Equal(t, 6, endChar, "end char")
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
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Span on line 2 covering "ef" (bytes 5-7, relative to file start)
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 2, Column: 2, Byte: 5}, // 'e' at byte 5
		End:    location.Position{Line: 2, Column: 4, Byte: 7}, // after 'f' at byte 7
	}

	start, end, ok := SpanToLSPRange(sources, span, PositionEncodingUTF8)
	require.True(t, ok, "SpanToLSPRange(UTF8) = !ok; want ok")

	startLine, startChar := start[0], start[1]
	endLine, endChar := end[0], end[1]

	// Line 2 starts at byte 4, so:
	// Start: byte 5 - 4 = char 1
	// End: byte 7 - 4 = char 3
	assert.Equal(t, 1, startLine, "start line")
	assert.Equal(t, 1, startChar, "start char")
	assert.Equal(t, 1, endLine, "end line")
	assert.Equal(t, 3, endChar, "end char")
}
