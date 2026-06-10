// span.go provides utilities for building location.Span values from ANTLR tokens.
//
// It handles the conversion from ANTLR's rune-based positions to
// byte-based positions required by the schema layer.
package schema

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/location"
)

// spanBuilder creates location.Span values from ANTLR tokens.
// It handles the conversion from ANTLR's rune-based positions to
// byte-based positions required by the schema layer.
type spanBuilder struct {
	sourceID  location.SourceID
	registry  location.PositionRegistry
	converter location.RuneOffsetConverter
}

// newSpanBuilder creates a spanBuilder for the given source.
func newSpanBuilder(
	sourceID location.SourceID,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) *spanBuilder {
	return &spanBuilder{
		sourceID:  sourceID,
		registry:  registry,
		converter: converter,
	}
}

// FromToken creates a Span from a single ANTLR token.
func (b *spanBuilder) FromToken(token antlr.Token) location.Span {
	if token == nil {
		return location.Span{}
	}

	startRune := token.GetStart()
	// End is exclusive; token.GetStop() is the last character index
	endRune := token.GetStop() + 1

	return b.fromRuneOffsets(startRune, endRune)
}

// FromContext creates a Span covering the entire parser rule context.
func (b *spanBuilder) FromContext(ctx antlr.ParserRuleContext) location.Span {
	if ctx == nil {
		return location.Span{}
	}

	start := ctx.GetStart()
	stop := ctx.GetStop()

	if start == nil {
		return location.Span{}
	}

	startRune := start.GetStart()
	var endRune int
	if stop != nil {
		endRune = stop.GetStop() + 1
	} else {
		// If no stop token, use end of start token
		endRune = start.GetStop() + 1
	}

	return b.fromRuneOffsets(startRune, endRune)
}

// FromTokens creates a Span covering a range of tokens.
func (b *spanBuilder) FromTokens(start, stop antlr.Token) location.Span {
	if start == nil {
		return location.Span{}
	}

	startRune := start.GetStart()
	var endRune int
	if stop != nil {
		endRune = stop.GetStop() + 1
	} else {
		endRune = start.GetStop() + 1
	}

	return b.fromRuneOffsets(startRune, endRune)
}

// fromRuneOffsets creates a Span from rune-based start/end offsets.
// ANTLR error-recovery tokens (missing tokens, EOF placeholders) carry
// negative offsets; like nil tokens, they have no real source extent and
// yield the zero Span rather than tripping the offset invariants.
func (b *spanBuilder) fromRuneOffsets(startRune, endRune int) location.Span {
	if startRune < 0 || endRune < 0 {
		return location.Span{}
	}
	startByte := mustRuneToByteOffset(b.converter, b.sourceID, startRune)
	endByte := mustRuneToByteOffset(b.converter, b.sourceID, endRune)

	startPos := mustPositionAt(b.registry, b.sourceID, startByte)
	endPos := mustPositionAt(b.registry, b.sourceID, endByte)

	return location.Span{Source: b.sourceID, Start: startPos, End: endPos}
}

// mustRuneToByteOffset converts a rune offset to a byte offset, panicking if
// the source is unknown. This enforces the schema parsing invariant that all
// rune offsets from ANTLR must be resolvable within the source.
func mustRuneToByteOffset(conv location.RuneOffsetConverter, src location.SourceID, runeOffset int) int {
	byteOffset, ok := conv.RuneToByteOffset(src, runeOffset)
	if !ok {
		panic(fmt.Sprintf("schema parsing invariant: RuneToByteOffset(%s, %d) returned false (unknown source)", src, runeOffset))
	}
	return byteOffset
}

// mustPositionAt converts a byte offset to a Position, panicking if the
// registry returns a zero Position. This enforces the schema parsing invariant
// that all byte offsets derived from ANTLR tokens must be resolvable.
func mustPositionAt(reg location.PositionRegistry, src location.SourceID, byteOffset int) location.Position {
	pos := reg.PositionAt(src, byteOffset)
	if pos.IsZero() {
		panic(fmt.Sprintf("schema parsing invariant: PositionAt(%s, %d) returned zero Position", src, byteOffset))
	}
	return pos
}

// Registry returns the underlying PositionRegistry.
func (b *spanBuilder) Registry() location.PositionRegistry {
	return b.registry
}

// Converter returns the underlying RuneOffsetConverter.
func (b *spanBuilder) Converter() location.RuneOffsetConverter {
	return b.converter
}
