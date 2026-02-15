package span_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/internal/span"
)

// --- Mock ANTLR Types ---

// mockToken implements antlr.Token for testing.
type mockToken struct {
	antlr.Token
	start int
	stop  int
}

func (m *mockToken) GetStart() int { return m.start }
func (m *mockToken) GetStop() int  { return m.stop }

// mockContext implements antlr.ParserRuleContext for testing.
type mockContext struct {
	antlr.ParserRuleContext
	startToken antlr.Token
	stopToken  antlr.Token
}

func (m *mockContext) GetStart() antlr.Token { return m.startToken }
func (m *mockContext) GetStop() antlr.Token  { return m.stopToken }

// --- Test Helpers ---

func registerSource(t *testing.T, reg *source.Registry, content, name string) location.SourceID {
	t.Helper()
	sourceID := location.MustNewSourceID("test://" + name)
	err := reg.Register(sourceID, []byte(content))
	require.NoError(t, err)
	return sourceID
}

// --- Tests ---

func TestNewBuilder(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test content", "builder")

	b := span.NewBuilder(sourceID, reg, reg)

	assert.NotNil(t, b)
	assert.Equal(t, reg, b.Registry())
	assert.Equal(t, reg, b.Converter())
}

func TestBuilder_FromToken_Nil(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-token")
	b := span.NewBuilder(sourceID, reg, reg)

	result := b.FromToken(nil)

	assert.True(t, result.IsZero())
}

func TestBuilder_FromToken_ASCII(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "hello world", "ascii")
	b := span.NewBuilder(sourceID, reg, reg)

	// Token for "hello" (runes 0-4, bytes 0-4)
	token := &mockToken{start: 0, stop: 4}
	result := b.FromToken(token)

	assert.False(t, result.IsZero())
	assert.Equal(t, sourceID, result.Source)
	assert.Equal(t, 1, result.Start.Line)
	assert.Equal(t, 1, result.Start.Column)
	assert.Equal(t, 0, result.Start.Byte)
	assert.Equal(t, 1, result.End.Line)
	assert.Equal(t, 6, result.End.Column) // Column 6 (after 'o')
	assert.Equal(t, 5, result.End.Byte)   // Byte 5 (exclusive end)
}

func TestBuilder_FromToken_UTF8_TwoByte(t *testing.T) {
	// "café" = c(1) + a(1) + f(1) + é(2) = 5 bytes, 4 runes
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "café", "utf8-2byte")
	b := span.NewBuilder(sourceID, reg, reg)

	// Token for "é" (rune 3, bytes 3-4)
	token := &mockToken{start: 3, stop: 3}
	result := b.FromToken(token)

	assert.False(t, result.IsZero())
	assert.Equal(t, 3, result.Start.Byte)
	assert.Equal(t, 5, result.End.Byte) // 2-byte character
	assert.Equal(t, 4, result.Start.Column)
	assert.Equal(t, 5, result.End.Column)
}

func TestBuilder_FromToken_UTF8_ThreeByte(t *testing.T) {
	// "a中b" = a(1) + 中(3) + b(1) = 5 bytes, 3 runes
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "a中b", "utf8-3byte")
	b := span.NewBuilder(sourceID, reg, reg)

	// Token for "中" (rune 1, bytes 1-3)
	token := &mockToken{start: 1, stop: 1}
	result := b.FromToken(token)

	assert.False(t, result.IsZero())
	assert.Equal(t, 1, result.Start.Byte)
	assert.Equal(t, 4, result.End.Byte) // 3-byte character
}

func TestBuilder_FromToken_UTF8_FourByte(t *testing.T) {
	// "a🎉b" = a(1) + 🎉(4) + b(1) = 6 bytes, 3 runes
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "a🎉b", "utf8-4byte")
	b := span.NewBuilder(sourceID, reg, reg)

	// Token for "🎉" (rune 1, bytes 1-4)
	token := &mockToken{start: 1, stop: 1}
	result := b.FromToken(token)

	assert.False(t, result.IsZero())
	assert.Equal(t, 1, result.Start.Byte)
	assert.Equal(t, 5, result.End.Byte) // 4-byte character
}

func TestBuilder_FromToken_Multiline(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "line1\nline2", "multiline")
	b := span.NewBuilder(sourceID, reg, reg)

	// Token for "line2" (runes 6-10)
	token := &mockToken{start: 6, stop: 10}
	result := b.FromToken(token)

	assert.Equal(t, 2, result.Start.Line)
	assert.Equal(t, 1, result.Start.Column)
	assert.Equal(t, 2, result.End.Line)
}

func TestBuilder_FromContext_Nil(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-ctx")
	b := span.NewBuilder(sourceID, reg, reg)

	result := b.FromContext(nil)

	assert.True(t, result.IsZero())
}

func TestBuilder_FromContext_NilStartToken(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-start")
	b := span.NewBuilder(sourceID, reg, reg)

	ctx := &mockContext{startToken: nil, stopToken: &mockToken{start: 0, stop: 3}}
	result := b.FromContext(ctx)

	assert.True(t, result.IsZero())
}

func TestBuilder_FromContext_NilStopToken(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-stop")
	b := span.NewBuilder(sourceID, reg, reg)

	// When stop is nil, should use end of start token
	ctx := &mockContext{
		startToken: &mockToken{start: 0, stop: 3},
		stopToken:  nil,
	}
	result := b.FromContext(ctx)

	assert.False(t, result.IsZero())
	assert.Equal(t, 0, result.Start.Byte)
	assert.Equal(t, 4, result.End.Byte) // Uses start token's stop + 1
}

func TestBuilder_FromContext_StartAndStop(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "hello world", "ctx-range")
	b := span.NewBuilder(sourceID, reg, reg)

	ctx := &mockContext{
		startToken: &mockToken{start: 0, stop: 4},  // "hello"
		stopToken:  &mockToken{start: 6, stop: 10}, // "world"
	}
	result := b.FromContext(ctx)

	assert.False(t, result.IsZero())
	assert.Equal(t, 0, result.Start.Byte)
	assert.Equal(t, 11, result.End.Byte) // End of "world" + 1
}

func TestBuilder_FromTokens_NilStart(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-start-tok")
	b := span.NewBuilder(sourceID, reg, reg)

	result := b.FromTokens(nil, &mockToken{start: 0, stop: 3})

	assert.True(t, result.IsZero())
}

func TestBuilder_FromTokens_NilStop(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "nil-stop-tok")
	b := span.NewBuilder(sourceID, reg, reg)

	// When stop is nil, should use end of start token
	result := b.FromTokens(&mockToken{start: 0, stop: 3}, nil)

	assert.False(t, result.IsZero())
	assert.Equal(t, 0, result.Start.Byte)
	assert.Equal(t, 4, result.End.Byte)
}

func TestBuilder_FromTokens_Range(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "hello world", "tok-range")
	b := span.NewBuilder(sourceID, reg, reg)

	result := b.FromTokens(
		&mockToken{start: 0, stop: 4},  // "hello"
		&mockToken{start: 6, stop: 10}, // "world"
	)

	assert.False(t, result.IsZero())
	assert.Equal(t, 0, result.Start.Byte)
	assert.Equal(t, 11, result.End.Byte)
}

func TestBuilder_MustPositionAt_ValidSource(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "valid")

	pos := span.MustPositionAt(reg, sourceID, 0)

	assert.Equal(t, 1, pos.Line)
	assert.Equal(t, 1, pos.Column)
	assert.Equal(t, 0, pos.Byte)
}

func TestBuilder_MustPositionAt_UnknownSource_Panics(t *testing.T) {
	reg := source.NewRegistry()
	unknownID := location.MustNewSourceID("test://unknown")
	// Don't register the source

	assert.Panics(t, func() {
		span.MustPositionAt(reg, unknownID, 0)
	})
}

func TestBuilder_Registry_Accessor(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "accessor")
	b := span.NewBuilder(sourceID, reg, reg)

	assert.Same(t, reg, b.Registry())
}

func TestBuilder_Converter_Accessor(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerSource(t, reg, "test", "accessor2")
	b := span.NewBuilder(sourceID, reg, reg)

	assert.Same(t, reg, b.Converter())
}

// F15: Test that mustRuneToByteOffset panics when converter returns false

// mockFailingConverter is a RuneOffsetConverter that always returns false.
type mockFailingConverter struct{}

func (m *mockFailingConverter) RuneToByteOffset(_ location.SourceID, _ int) (int, bool) {
	return 0, false
}

func TestBuilder_FromToken_UnknownSource_Panics(t *testing.T) {
	// Use a mock that always fails rune-to-byte conversion
	reg := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://unknown")
	// Don't register the source in reg
	failingConverter := &mockFailingConverter{}

	b := span.NewBuilder(sourceID, reg, failingConverter)

	assert.Panics(t, func() {
		// This will call mustRuneToByteOffset which will panic
		// because the converter returns false
		b.FromToken(&mockToken{start: 0, stop: 0})
	})
}
