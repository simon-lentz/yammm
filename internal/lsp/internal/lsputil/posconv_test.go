package lsputil

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"

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

	sources := NewTexts()
	sourceID := sourcetest.ID("test://ascii.yammm")
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
			got, ok := ByteOffsetFromLSP(sources, sourceID, tt.line, tt.char)
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(line=%d, char=%d)", tt.line, tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF16_BMP(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://bmp.yammm")
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
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char)
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF16_Surrogate(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://emoji.yammm")
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
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char)
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
		})
	}
}

func TestByteOffsetFromLSP_UTF16_CJK(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://cjk.yammm")
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
			got, ok := ByteOffsetFromLSP(sources, sourceID, 0, tt.char)
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(char=%d)", tt.char)
		})
	}
}

func TestByteOffsetFromLSP_MultiLine(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://multiline.yammm")
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
			got, ok := ByteOffsetFromLSP(sources, sourceID, tt.line, tt.char)
			require.True(t, ok, "ByteOffsetFromLSP returned ok=false")
			assert.Equal(t, tt.wantByte, got,
				"ByteOffsetFromLSP(line=%d, char=%d)", tt.line, tt.char)
		})
	}
}

func TestByteOffsetFromLSP_InvalidLine(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://invalid.yammm")
	content := []byte("hello\n")
	require.NoError(t, sources.Register(sourceID, content), "Register()")

	// Invalid line should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 10, 0)
	assert.False(t, ok, "ByteOffsetFromLSP(line=10) should return ok=false for invalid line")
}

func TestByteOffsetFromLSP_UnknownSource(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://unknown.yammm")

	// Unknown source should return ok=false
	_, ok := ByteOffsetFromLSP(sources, sourceID, 0, 5)
	assert.False(t, ok, "ByteOffsetFromLSP(unknown source) should return ok=false")
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

	sources := NewTexts()
	var span location.Span // zero span

	_, _, ok := SpanToLSPRange(sources, span)
	assert.False(t, ok, "SpanToLSPRange(zero span) = ok; want !ok")
}

func TestSpanToLSPRange_UnknownStart(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://file.yammm")
	span := location.Span{
		Source: sourceID,
		// Start is unknown (zero Position)
	}

	_, _, ok := SpanToLSPRange(sources, span)
	assert.False(t, ok, "SpanToLSPRange(unknown start) = ok; want !ok")
}

func TestSpanToLSPRange_Valid(t *testing.T) {
	t.Parallel()

	sources := NewTexts()
	sourceID := sourcetest.ID("test://file.yammm")
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 10, Column: 5, Offset: 100},
		End:    location.Position{Line: 10, Column: 15, Offset: 110},
	}

	// When content is not registered, falls back to rune column conversion
	start, end, ok := SpanToLSPRange(sources, span)
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

	sources := NewTexts()
	sourceID := sourcetest.ID("test://file.yammm")
	// Point span: only start is set
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 5, Column: 10, Offset: 50},
		// End is zero
	}

	start, end, ok := SpanToLSPRange(sources, span)
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

	sources := NewTexts()
	sourceID := sourcetest.ID("test://file.yammm")
	// Line 1 should become 0, not negative
	span := location.Span{
		Source: sourceID,
		Start:  location.Position{Line: 1, Column: 1, Offset: 0},
	}

	start, _, ok := SpanToLSPRange(sources, span)
	require.True(t, ok, "SpanToLSPRange() = !ok; want ok")

	// Extract values (fixed-size array, safe to index)
	startLine, startChar := start[0], start[1]

	assert.Equal(t, 0, startLine, "start[0] for line 1")
	assert.Equal(t, 0, startChar, "start[1] for column 1")
}
