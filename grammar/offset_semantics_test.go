// Package grammar contains ANTLR offset semantics verification tests.
//
// These tests verify the critical assumption that ANTLR-Go's Token.GetStart()
// and Token.GetStop() return rune (character) indices, NOT byte indices.
// This assumption underlies all span derivation in schema and instance parsing.
//
// Evidence from ANTLR-Go v4.13.1 source:
//   - input_stream.go:53: NewInputStream stores data as []rune
//   - input_stream.go:96-98: Index() returns position in []rune slice
//   - lexer.go:202: TokenStartCharIndex = input.Index() (rune position)
//   - lexer.go:312: Token created with rune-based start/stop indices
//
// If these tests fail after an ANTLR-Go version upgrade, span derivation
// in schema/internal/parse and adapter/json must be re-verified.
package grammar

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/antlr4-go/antlr/v4"
)

// TestInputStreamRuneBasedIndexing verifies that InputStream.Size() returns
// rune count, not byte count, for multi-byte UTF-8 input.
func TestInputStreamRuneBasedIndexing(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedSize  int  // rune count
		byteLen       int  // byte length (for comparison)
		sameAsByteLen bool // true if ASCII-only
	}{
		{
			name:          "ASCII only",
			input:         "hello",
			expectedSize:  5,
			byteLen:       5,
			sameAsByteLen: true,
		},
		{
			name:          "Japanese kanji",
			input:         "日本語",
			expectedSize:  3, // 3 runes
			byteLen:       9, // 3 bytes per kanji
			sameAsByteLen: false,
		},
		{
			name:          "Mixed ASCII and multi-byte",
			input:         "a日b本c語d",
			expectedSize:  7,  // 7 runes
			byteLen:       13, // 4 ASCII + 9 kanji bytes
			sameAsByteLen: false,
		},
		{
			name:          "Emoji (non-BMP)",
			input:         "🎉",
			expectedSize:  1, // 1 rune (despite being 4 bytes)
			byteLen:       4,
			sameAsByteLen: false,
		},
		{
			name:          "ZWJ emoji sequence",
			input:         "👨‍👩‍👧", // family emoji: man + ZWJ + woman + ZWJ + girl
			expectedSize:  5,       // 5 runes (3 emoji + 2 ZWJ)
			byteLen:       18,      // complex encoding
			sameAsByteLen: false,
		},
		{
			name:          "Combining character",
			input:         "e\u0301", // e + combining acute accent = é
			expectedSize:  2,         // 2 runes (base + combining)
			byteLen:       3,         // 1 + 2 bytes
			sameAsByteLen: false,
		},
		{
			name:          "Greek letters",
			input:         "αβγδ",
			expectedSize:  4,
			byteLen:       8, // 2 bytes per Greek letter
			sameAsByteLen: false,
		},
		{
			name:          "Empty string",
			input:         "",
			expectedSize:  0,
			byteLen:       0,
			sameAsByteLen: true,
		},
		{
			name:          "Single ASCII",
			input:         "x",
			expectedSize:  1,
			byteLen:       1,
			sameAsByteLen: true,
		},
		{
			name:          "Single multi-byte",
			input:         "日",
			expectedSize:  1,
			byteLen:       3,
			sameAsByteLen: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := antlr.NewInputStream(tt.input)

			// Verify Size() returns rune count
			if got := is.Size(); got != tt.expectedSize {
				t.Errorf("Size() = %d, want %d (rune count)", got, tt.expectedSize)
			}

			// Verify our expectations about byte length
			if got := len(tt.input); got != tt.byteLen {
				t.Errorf("byte length = %d, want %d", got, tt.byteLen)
			}

			// Verify rune count matches our expectation
			if got := utf8.RuneCountInString(tt.input); got != tt.expectedSize {
				t.Errorf("utf8.RuneCountInString() = %d, want %d", got, tt.expectedSize)
			}

			// Verify Size() != byte length for multi-byte input
			if tt.sameAsByteLen {
				if is.Size() != tt.byteLen {
					t.Errorf("ASCII-only: Size() = %d should equal byte length %d", is.Size(), tt.byteLen)
				}
			} else {
				if is.Size() == tt.byteLen {
					t.Errorf("Multi-byte: Size() = %d should NOT equal byte length %d", is.Size(), tt.byteLen)
				}
			}
		})
	}
}

// TestInputStreamGetTextRuneBased verifies that GetText uses rune indices.
func TestInputStreamGetTextRuneBased(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		start    int // rune index
		stop     int // rune index (inclusive, as ANTLR uses)
		expected string
	}{
		{
			name:     "ASCII substring",
			input:    "hello",
			start:    1,
			stop:     3,
			expected: "ell",
		},
		{
			name:     "Japanese first char",
			input:    "日本語",
			start:    0,
			stop:     0,
			expected: "日",
		},
		{
			name:     "Japanese middle char",
			input:    "日本語",
			start:    1,
			stop:     1,
			expected: "本",
		},
		{
			name:     "Japanese last char",
			input:    "日本語",
			start:    2,
			stop:     2,
			expected: "語",
		},
		{
			name:     "Mixed: extract kanji from mixed string",
			input:    "a日b",
			start:    1,
			stop:     1,
			expected: "日", // If byte-based, this would fail or return garbage
		},
		{
			name:     "Extract emoji",
			input:    "a🎉b",
			start:    1,
			stop:     1,
			expected: "🎉",
		},
		{
			name:     "Full string",
			input:    "日本語",
			start:    0,
			stop:     2,
			expected: "日本語",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is := antlr.NewInputStream(tt.input)
			got := is.GetText(tt.start, tt.stop)
			if got != tt.expected {
				t.Errorf("GetText(%d, %d) = %q, want %q", tt.start, tt.stop, got, tt.expected)
			}
		})
	}
}

// TestInputStreamIndexIsRuneBased verifies that Index() and Seek() use rune positions.
func TestInputStreamIndexIsRuneBased(t *testing.T) {
	input := "a日b" // 3 runes, 5 bytes

	is := antlr.NewInputStream(input)

	// Initial index should be 0
	if got := is.Index(); got != 0 {
		t.Errorf("Initial Index() = %d, want 0", got)
	}

	// After consuming 'a', index should be 1 (not 1 byte)
	is.Consume()
	if got := is.Index(); got != 1 {
		t.Errorf("After first Consume(), Index() = %d, want 1", got)
	}

	// After consuming '日', index should be 2 (not 4 bytes)
	is.Consume()
	if got := is.Index(); got != 2 {
		t.Errorf("After second Consume(), Index() = %d, want 2 (rune-based)", got)
	}

	// Seek back to position 1 (the '日' character)
	is.Seek(1)
	if got := is.Index(); got != 1 {
		t.Errorf("After Seek(1), Index() = %d, want 1", got)
	}

	// LA(1) at position 1 should return the rune value of '日'
	if got := is.LA(1); got != int('日') {
		t.Errorf("LA(1) at position 1 = %d (%c), want %d (%c)", got, got, int('日'), '日')
	}
}

// TestNewIoStreamConsistency verifies NewIoStream has the same rune-based semantics.
func TestNewIoStreamConsistency(t *testing.T) {
	input := "日本語abc"

	// Via NewInputStream
	is1 := antlr.NewInputStream(input)

	// Via NewIoStream
	is2 := antlr.NewIoStream(strings.NewReader(input))

	// Both should report same Size() (rune count)
	if is1.Size() != is2.Size() {
		t.Errorf("Size mismatch: NewInputStream=%d, NewIoStream=%d", is1.Size(), is2.Size())
	}

	expectedRuneCount := 6 // 3 kanji + 3 ASCII
	if is1.Size() != expectedRuneCount {
		t.Errorf("Size() = %d, want %d (rune count)", is1.Size(), expectedRuneCount)
	}

	// GetText should return same content
	text1 := is1.GetText(0, is1.Size()-1)
	text2 := is2.GetText(0, is2.Size()-1)
	if text1 != text2 {
		t.Errorf("GetText mismatch: NewInputStream=%q, NewIoStream=%q", text1, text2)
	}
	if text1 != input {
		t.Errorf("GetText() = %q, want %q", text1, input)
	}
}

// TestLexerTokenOffsetsAreRuneBased uses the actual YAMMM lexer to verify
// that token GetStart()/GetStop() return rune indices.
func TestLexerTokenOffsetsAreRuneBased(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		tokenText     string // expected text of first STRING token
		expectedStart int    // expected rune-based start
		expectedStop  int    // expected rune-based stop (inclusive)
		byteStart     int    // what byte-based start would be (same for ASCII-prefix)
		byteStop      int    // what byte-based stop would be (differs for multi-byte content)
	}{
		{
			name:          "ASCII schema name",
			input:         `schema "Test"`,
			tokenText:     `"Test"`,
			expectedStart: 7,  // rune index: "schema " = 7 chars
			expectedStop:  12, // rune index: 7 + len(`"Test"`) - 1 = 7 + 6 - 1 = 12
			byteStart:     7,
			byteStop:      12, // same for ASCII
		},
		{ //nolint:gosec // G101 false positive: test data for offset arithmetic, not credentials
			name:          "Japanese schema name",
			input:         `schema "日本語"`,
			tokenText:     `"日本語"`,
			expectedStart: 7,  // rune index of opening quote (same as byte for ASCII prefix)
			expectedStop:  11, // rune index: 7 + len(`"日本語"` in runes) - 1 = 7 + 5 - 1 = 11
			byteStart:     7,
			byteStop:      16, // byte index: 7 + 1 + 9 + 1 - 1 = 16 (if it were byte-based)
		},
		{
			name:          "Emoji in string",
			input:         `schema "🎉"`,
			tokenText:     `"🎉"`,
			expectedStart: 7,
			expectedStop:  9, // rune index: 7 + 3 - 1 = 9 (quote + emoji + quote = 3 runes)
			byteStart:     7,
			byteStop:      12, // byte index would be: 7 + 1 + 4 + 1 - 1 = 12
		},
		{
			name:          "Mixed content before string",
			input:         `schema "日X"`,
			tokenText:     `"日X"`,
			expectedStart: 7,
			expectedStop:  10, // rune index: 7 + 4 - 1 = 10 (quote + 日 + X + quote = 4 runes)
			byteStart:     7,
			byteStop:      12, // byte index would be: 7 + 1 + 3 + 1 + 1 - 1 = 12
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := antlr.NewInputStream(tt.input)
			lexer := NewYammmGrammarLexer(input)
			stream := antlr.NewCommonTokenStream(lexer, 0)
			stream.Fill()

			// Find the STRING token
			var stringToken antlr.Token
			for _, tok := range stream.GetAllTokens() {
				if tok.GetText() == tt.tokenText {
					stringToken = tok
					break
				}
			}

			if stringToken == nil {
				t.Fatalf("STRING token %q not found in input %q", tt.tokenText, tt.input)
			}

			// Verify start is rune-based
			if got := stringToken.GetStart(); got != tt.expectedStart {
				t.Errorf("GetStart() = %d, want %d (rune-based)", got, tt.expectedStart)
				if got == tt.byteStart && tt.byteStart != tt.expectedStart {
					t.Errorf("  (got byte-based value %d instead of rune-based %d)", tt.byteStart, tt.expectedStart)
				}
			}

			// Verify stop is rune-based
			if got := stringToken.GetStop(); got != tt.expectedStop {
				t.Errorf("GetStop() = %d, want %d (rune-based)", got, tt.expectedStop)
				if got == tt.byteStop && tt.byteStop != tt.expectedStop {
					t.Errorf("  (got byte-based value %d instead of rune-based %d)", tt.byteStop, tt.expectedStop)
				}
			}

			// Verify GetText returns the expected string content
			if got := stringToken.GetText(); got != tt.tokenText {
				t.Errorf("GetText() = %q, want %q", got, tt.tokenText)
			}
		})
	}
}

// TestSLCommentTokensHiddenChannelAndNewlineOwnership verifies that SL_COMMENT
// tokens are preserved on the hidden channel and no longer include trailing
// newlines (newlines belong to adjacent WS tokens).
func TestSLCommentTokensHiddenChannelAndNewlineOwnership(t *testing.T) {
	tests := []struct {
		name                string
		input               string
		commentText         string
		expectedStart       int
		expectedStop        int
		expectedByteStop    int
		expectNextWSNewline bool
	}{
		{
			name:                "Standalone comment line",
			input:               "schema \"x\"\n// c1\ntype T {}\n",
			commentText:         "// c1",
			expectedStart:       11,
			expectedStop:        15,
			expectedByteStop:    15,
			expectNextWSNewline: true,
		},
		{
			name:                "Inline comment",
			input:               "schema \"x\"\ntype T {\n\tname String // c2\n}\n",
			commentText:         "// c2",
			expectedStart:       33,
			expectedStop:        37,
			expectedByteStop:    37,
			expectNextWSNewline: true,
		},
		{
			name:                "Comment at EOF without trailing newline",
			input:               "schema \"x\"\n// eof",
			commentText:         "// eof",
			expectedStart:       11,
			expectedStop:        16,
			expectedByteStop:    16,
			expectNextWSNewline: false,
		},
		{
			name:                "Multibyte comment content",
			input:               "schema \"x\"\n// 日本🎉\n",
			commentText:         "// 日本🎉",
			expectedStart:       11,
			expectedStop:        16,
			expectedByteStop:    23,
			expectNextWSNewline: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := antlr.NewInputStream(tt.input)
			lexer := NewYammmGrammarLexer(input)
			stream := antlr.NewCommonTokenStream(lexer, 0)
			stream.Fill()

			tokens := stream.GetAllTokens()

			var commentToken antlr.Token
			commentTokenIdx := -1
			commentCount := 0
			for i, tok := range tokens {
				if tok.GetTokenType() == YammmGrammarLexerSL_COMMENT {
					commentCount++
					if tok.GetText() == tt.commentText {
						commentToken = tok
						commentTokenIdx = i
					}
				}
			}

			if commentCount != 1 {
				t.Fatalf("expected exactly 1 SL_COMMENT token, got %d", commentCount)
			}
			if commentToken == nil {
				t.Fatalf("SL_COMMENT token %q not found", tt.commentText)
			}
			if commentTokenIdx < 0 {
				t.Fatalf("SL_COMMENT token index not found for %q", tt.commentText)
			}

			if got := commentToken.GetTokenType(); got != YammmGrammarLexerSL_COMMENT {
				t.Errorf("SL_COMMENT token type = %d, want %d", got, YammmGrammarLexerSL_COMMENT)
			}
			if got := commentToken.GetChannel(); got != antlr.TokenHiddenChannel {
				t.Errorf("SL_COMMENT channel = %d, want hidden channel %d", got, antlr.TokenHiddenChannel)
			}
			if strings.Contains(commentToken.GetText(), "\n") || strings.Contains(commentToken.GetText(), "\r") {
				t.Errorf("SL_COMMENT text contains newline: %q", commentToken.GetText())
			}

			if got := commentToken.GetStart(); got != tt.expectedStart {
				t.Errorf("SL_COMMENT GetStart() = %d, want %d (rune-based)", got, tt.expectedStart)
			}
			if got := commentToken.GetStop(); got != tt.expectedStop {
				t.Errorf("SL_COMMENT GetStop() = %d, want %d (rune-based)", got, tt.expectedStop)
				if got == tt.expectedByteStop && tt.expectedByteStop != tt.expectedStop {
					t.Errorf(
						"  (got byte-based value %d instead of rune-based %d)",
						tt.expectedByteStop,
						tt.expectedStop,
					)
				}
			}

			if tt.expectNextWSNewline {
				if commentTokenIdx+1 >= len(tokens) {
					t.Fatalf("expected token after SL_COMMENT, but comment is last token")
				}

				next := tokens[commentTokenIdx+1]
				if next.GetTokenType() != YammmGrammarLexerWS {
					t.Fatalf("token after SL_COMMENT type = %d, want WS (%d)", next.GetTokenType(), YammmGrammarLexerWS)
				}
				if !strings.HasPrefix(next.GetText(), "\n") {
					t.Errorf("WS token after SL_COMMENT should start with newline, got %q", next.GetText())
				}
			} else if commentTokenIdx+1 < len(tokens) {
				next := tokens[commentTokenIdx+1]
				if next.GetTokenType() == YammmGrammarLexerWS && strings.HasPrefix(next.GetText(), "\n") {
					t.Errorf("did not expect newline WS token after EOF SL_COMMENT, got %q", next.GetText())
				}
			}
		})
	}
}

// TestCharIndexToByteOffsetConversion verifies the conversion algorithm
// that schema/internal/parse uses to convert ANTLR's rune indices to byte offsets.
func TestCharIndexToByteOffsetConversion(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		charIdx  int // rune index
		expected int // expected byte offset
	}{
		{"ASCII start", "hello", 0, 0},
		{"ASCII middle", "hello", 2, 2},
		{"ASCII end", "hello", 5, 5},
		{"Kanji start", "日本語", 0, 0},
		{"Kanji second", "日本語", 1, 3},
		{"Kanji third", "日本語", 2, 6},
		{"Kanji end", "日本語", 3, 9},
		{"Mixed: before kanji", "a日b", 0, 0},
		{"Mixed: at kanji", "a日b", 1, 1},
		{"Mixed: after kanji", "a日b", 2, 4},
		{"Mixed: at end", "a日b", 3, 5},
		{"Emoji", "a🎉b", 1, 1},
		{"After emoji", "a🎉b", 2, 5},
		{"End after emoji", "a🎉b", 3, 6},
		{"Empty string", "", 0, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := charIndexToByteOffset([]byte(tt.input), tt.charIdx)
			if got != tt.expected {
				t.Errorf("charIndexToByteOffset(%q, %d) = %d, want %d",
					tt.input, tt.charIdx, got, tt.expected)
			}
		})
	}
}

// charIndexToByteOffset converts a character (rune) index to a byte offset.
// This is the canonical conversion algorithm used by v2/schema/internal/parse.
//
// ANTLR's NewInputStream(string) uses character indices (counting runes),
// but source registries use byte offsets. This function performs the conversion.
//
// NOTE: This is an intentional duplicate of the production code in the parse package.
// Having a separate reference implementation in tests ensures the algorithm is
// independently verified and documents the expected behavior for ANTLR integration.
func charIndexToByteOffset(content []byte, charIdx int) int {
	if charIdx <= 0 {
		return 0
	}

	byteOffset := 0
	runeCount := 0
	for byteOffset < len(content) && runeCount < charIdx {
		_, size := utf8.DecodeRune(content[byteOffset:])
		if size == 0 {
			size = 1 // Invalid UTF-8 byte
		}
		byteOffset += size
		runeCount++
	}

	return byteOffset
}

// TestGetStopIsInclusive verifies ANTLR's GetStop() returns an inclusive index.
// This is important because v2/location.Span uses half-open intervals [start, end).
func TestGetStopIsInclusive(t *testing.T) {
	// Use a simple schema with a known token
	input := `schema "X"`
	is := antlr.NewInputStream(input)
	lexer := NewYammmGrammarLexer(is)
	stream := antlr.NewCommonTokenStream(lexer, 0)
	stream.Fill()

	// Find the STRING token "X"
	var stringToken antlr.Token
	for _, tok := range stream.GetAllTokens() {
		if tok.GetText() == `"X"` {
			stringToken = tok
			break
		}
	}

	if stringToken == nil {
		t.Fatal("STRING token not found")
	}

	// For "X" (3 chars: quote, X, quote):
	// Start should be 7 (after "schema ")
	// Stop should be 9 (inclusive, pointing to closing quote)
	// The span should cover indices 7, 8, 9

	start := stringToken.GetStart()
	stop := stringToken.GetStop()

	// Verify the length calculation
	tokenLen := stop - start + 1 // +1 because stop is inclusive
	expectedLen := len(`"X"`)    // 3 characters

	if tokenLen != expectedLen {
		t.Errorf("Token length = %d (stop %d - start %d + 1), want %d",
			tokenLen, stop, start, expectedLen)
	}

	// Verify that GetText from the stream matches
	text := is.GetText(start, stop)
	if text != `"X"` {
		t.Errorf("GetText(%d, %d) = %q, want %q", start, stop, text, `"X"`)
	}

	// Document the half-open conversion needed for v2/location.Span
	// For half-open [start, end), end = stop + 1
	halfOpenEnd := stop + 1
	t.Logf("ANTLR inclusive: [%d, %d], Half-open for Span: [%d, %d)", start, stop, start, halfOpenEnd)
}
