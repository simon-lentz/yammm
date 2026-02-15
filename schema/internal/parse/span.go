// Package parse re-exports span building utilities from the internal span package.
package parse

import (
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/internal/span"
)

// SpanBuilder is an alias for span.Builder.
// It creates location.Span values from ANTLR tokens.
type SpanBuilder = span.Builder

// NewSpanBuilder creates a SpanBuilder for the given source.
func NewSpanBuilder(
	sourceID location.SourceID,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) *SpanBuilder {
	return span.NewBuilder(sourceID, registry, converter)
}

// MustPositionAt converts a byte offset to a Position, panicking if the
// registry returns a zero Position.
func MustPositionAt(reg location.PositionRegistry, src location.SourceID, byteOffset int) location.Position {
	return span.MustPositionAt(reg, src, byteOffset)
}
