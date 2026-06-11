package location

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location/path"
)

func TestProvenance_RawPath_Default(t *testing.T) {
	prov := NewProvenance("test.json", path.Root().Key("Person"), Span{})
	assert.Empty(t, prov.RawPath())
}

func TestProvenance_RawPath_NilReceiver(t *testing.T) {
	var prov *Provenance
	assert.Empty(t, prov.RawPath())
}

func TestProvenance_WithRawPath(t *testing.T) {
	prov := NewProvenance("test.json", path.Root(), Span{})
	withRaw := prov.WithRawPath("$.some.future.path.format")

	assert.Equal(t, "$.some.future.path.format", withRaw.RawPath())
	assert.Equal(t, "test.json", withRaw.SourceName())
	assert.Equal(t, "$", withRaw.Path().String())
}

func TestProvenance_WithRawPath_NilReceiver(t *testing.T) {
	var prov *Provenance
	withRaw := prov.WithRawPath("$.custom")

	assert.Equal(t, "$.custom", withRaw.RawPath())
	assert.Empty(t, withRaw.SourceName())
}

func TestProvenance_WithRawPath_RoundTrip(t *testing.T) {
	prov := NewProvenance("data.json", path.Root().Key("Item"), Span{}).
		WithRawPath("$.Item[special=format]")

	assert.Equal(t, "$.Item[special=format]", prov.RawPath())
	assert.Equal(t, "$.Item", prov.Path().String())
	assert.Equal(t, "data.json", prov.SourceName())
}

func TestProvenance_HasSpan_WithNonZeroSpan(t *testing.T) {
	source := MustNewSourceID("test://file.json")
	span := Point(source, 1, 1)
	prov := NewProvenance("test.json", path.Root(), span)

	assert.True(t, prov.HasSpan())
}

func TestProvenance_HasSpan_WithZeroSpan(t *testing.T) {
	prov := NewProvenance("test.json", path.Root(), Span{})
	assert.False(t, prov.HasSpan())
}

func TestProvenance_HasSpan_NilReceiver(t *testing.T) {
	var prov *Provenance
	assert.False(t, prov.HasSpan())
}
