package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// --- Test Helpers ---

func registerTestSource(t *testing.T, reg *source.Registry, content, name string) location.SourceID {
	t.Helper()
	sourceID := location.MustNewSourceID("test://" + name)
	err := reg.Register(sourceID, []byte(content))
	require.NoError(t, err)
	return sourceID
}

// --- Constructor Tests ---

func TestNewSources_NilRegistry(t *testing.T) {
	s := schema.NewSources(nil)

	assert.Nil(t, s)
}

func TestNewSources_ValidRegistry(t *testing.T) {
	reg := source.NewRegistry()

	s := schema.NewSources(reg)

	assert.NotNil(t, s)
}

// --- Nil Receiver Tests ---

func TestSources_ContentBySource_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	content, ok := s.ContentBySource(location.SourceID{})

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_Content_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	content, ok := s.Content(location.Span{})

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_PositionAt_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	pos := s.PositionAt(location.SourceID{}, 0)

	assert.Equal(t, location.Position{}, pos)
}

func TestSources_LineStartByte_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	offset, ok := s.LineStartByte(location.SourceID{}, 1)

	assert.Equal(t, 0, offset)
	assert.False(t, ok)
}

func TestSources_RuneToByteOffset_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	offset, ok := s.RuneToByteOffset(location.SourceID{}, 0)

	assert.Equal(t, 0, offset)
	assert.False(t, ok)
}

func TestSources_SourceIDs_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	ids := s.SourceIDs()

	assert.Nil(t, ids)
}

func TestSources_Has_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	result := s.Has(location.SourceID{})

	assert.False(t, result)
}

func TestSources_Len_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	result := s.Len()

	assert.Equal(t, 0, result)
}

// --- Valid Sources Tests ---

func TestSources_ContentBySource_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "hello world", "content")
	s := schema.NewSources(reg)

	content, ok := s.ContentBySource(sourceID)

	assert.True(t, ok)
	assert.Equal(t, []byte("hello world"), content)
}

func TestSources_ContentBySource_NotFound(t *testing.T) {
	reg := source.NewRegistry()
	s := schema.NewSources(reg)
	unknownID := location.MustNewSourceID("test://unknown")

	content, ok := s.ContentBySource(unknownID)

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_Content_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "test content", "span-content")
	s := schema.NewSources(reg)
	span := location.Span{Source: sourceID}

	content, ok := s.Content(span)

	assert.True(t, ok)
	assert.Equal(t, []byte("test content"), content)
}

func TestSources_PositionAt_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "line1\nline2", "position")
	s := schema.NewSources(reg)

	// Position at start of line 2 (byte 6)
	pos := s.PositionAt(sourceID, 6)

	assert.Equal(t, 2, pos.Line)
	assert.Equal(t, 1, pos.Column)
	assert.Equal(t, 6, pos.Byte)
}

func TestSources_LineStartByte_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "line1\nline2\nline3", "linestart")
	s := schema.NewSources(reg)

	// Line 2 starts at byte 6 (after "line1\n")
	offset, ok := s.LineStartByte(sourceID, 2)

	assert.True(t, ok)
	assert.Equal(t, 6, offset)
}

func TestSources_RuneToByteOffset_Valid(t *testing.T) {
	reg := source.NewRegistry()
	// "café" = c(1) + a(1) + f(1) + é(2) = 5 bytes, 4 runes
	sourceID := registerTestSource(t, reg, "café", "rune")
	s := schema.NewSources(reg)

	// Rune 3 (é) starts at byte 3
	offset, ok := s.RuneToByteOffset(sourceID, 3)

	assert.True(t, ok)
	assert.Equal(t, 3, offset)
}

func TestSources_SourceIDs_Valid(t *testing.T) {
	reg := source.NewRegistry()
	id1 := registerTestSource(t, reg, "content1", "source1")
	id2 := registerTestSource(t, reg, "content2", "source2")
	s := schema.NewSources(reg)

	ids := s.SourceIDs()

	assert.Len(t, ids, 2)
	assert.Contains(t, ids, id1)
	assert.Contains(t, ids, id2)
}

func TestSources_Has_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "content", "has")
	s := schema.NewSources(reg)

	assert.True(t, s.Has(sourceID))
	assert.False(t, s.Has(location.MustNewSourceID("test://unknown")))
}

func TestSources_Len_Valid(t *testing.T) {
	reg := source.NewRegistry()
	registerTestSource(t, reg, "content1", "len1")
	registerTestSource(t, reg, "content2", "len2")
	s := schema.NewSources(reg)

	assert.Equal(t, 2, s.Len())
}

// --- SourceIDsIter Tests ---

func TestSources_SourceIDsIter_NilReceiver(t *testing.T) {
	var s *schema.Sources = nil

	var collected []location.SourceID
	for id := range s.SourceIDsIter() {
		collected = append(collected, id)
	}

	assert.Empty(t, collected)
}

func TestSources_SourceIDsIter_Valid(t *testing.T) {
	reg := source.NewRegistry()
	id1 := registerTestSource(t, reg, "content1", "iter1")
	id2 := registerTestSource(t, reg, "content2", "iter2")
	s := schema.NewSources(reg)

	var collected []location.SourceID
	for id := range s.SourceIDsIter() {
		collected = append(collected, id)
	}

	assert.Len(t, collected, 2)
	assert.Contains(t, collected, id1)
	assert.Contains(t, collected, id2)
}

func TestSources_SourceIDsIter_EarlyTermination(t *testing.T) {
	reg := source.NewRegistry()
	registerTestSource(t, reg, "content1", "early1")
	registerTestSource(t, reg, "content2", "early2")
	registerTestSource(t, reg, "content3", "early3")
	s := schema.NewSources(reg)

	// Break after first element
	count := 0
	for range s.SourceIDsIter() {
		count++
		break
	}

	assert.Equal(t, 1, count)
}

func TestSources_SourceIDsIter_MatchesSourceIDs(t *testing.T) {
	reg := source.NewRegistry()
	registerTestSource(t, reg, "content1", "match1")
	registerTestSource(t, reg, "content2", "match2")
	s := schema.NewSources(reg)

	// Collect from iterator
	var fromIter []location.SourceID
	for id := range s.SourceIDsIter() {
		fromIter = append(fromIter, id)
	}

	// Get from slice method
	fromSlice := s.SourceIDs()

	// Should match in content (order is guaranteed by Keys())
	assert.Equal(t, fromSlice, fromIter)
}
