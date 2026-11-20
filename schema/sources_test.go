package schema_test

import (
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest/sourcetest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func registerTestSource(t *testing.T, reg *source.Registry, content, name string) location.SourceID {
	t.Helper()
	sourceID := sourcetest.ID("test://" + name)
	err := reg.Register(sourceID, []byte(content))
	require.NoError(t, err)
	return sourceID
}

func TestNewSources_NilRegistry(t *testing.T) {
	s := schema.TestNewSources(nil)

	assert.True(t, s.IsZero())
}

func TestNewSources_ValidRegistry(t *testing.T) {
	reg := source.NewRegistry()

	s := schema.TestNewSources(reg)

	assert.False(t, s.IsZero())
}

func TestSources_ContentBySource_ZeroHandle(t *testing.T) {
	var s schema.Sources

	content, ok := s.ContentBySource(location.SourceID{})

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_Content_ZeroHandle(t *testing.T) {
	var s schema.Sources

	content, ok := s.Content(location.Span{})

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_SourceIDs_ZeroHandle(t *testing.T) {
	var s schema.Sources

	ids := s.SourceIDs()

	assert.Nil(t, ids)
}

func TestSources_ContentBySource_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "hello world", "content")
	s := schema.TestNewSources(reg)

	content, ok := s.ContentBySource(sourceID)

	assert.True(t, ok)
	assert.Equal(t, []byte("hello world"), content)
}

func TestSources_ContentBySource_NotFound(t *testing.T) {
	reg := source.NewRegistry()
	s := schema.TestNewSources(reg)
	unknownID := sourcetest.ID("test://unknown")

	content, ok := s.ContentBySource(unknownID)

	assert.Nil(t, content)
	assert.False(t, ok)
}

func TestSources_Content_Valid(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := registerTestSource(t, reg, "test content", "span-content")
	s := schema.TestNewSources(reg)
	span := location.Span{Source: sourceID}

	content, ok := s.Content(span)

	assert.True(t, ok)
	assert.Equal(t, []byte("test content"), content)
}

func TestSources_SourceIDs_Valid(t *testing.T) {
	reg := source.NewRegistry()
	id1 := registerTestSource(t, reg, "content1", "source1")
	id2 := registerTestSource(t, reg, "content2", "source2")
	s := schema.TestNewSources(reg)

	ids := s.SourceIDs()

	assert.Len(t, ids, 2)
	assert.Contains(t, ids, id1)
	assert.Contains(t, ids, id2)
}
