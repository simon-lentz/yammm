// Package span provides utilities for building location.Span values from ANTLR tokens.
package span

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/location"
)

// Builder creates location.Span values from ANTLR tokens.
// It handles the conversion from ANTLR's rune-based positions to
// byte-based positions required by the schema layer.
type Builder struct {
	sourceID  location.SourceID
	registry  location.PositionRegistry
	converter location.RuneOffsetConverter
}

// NewBuilder creates a Builder for the given source.
func NewBuilder(
	sourceID location.SourceID,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) *Builder {
	return &Builder{
		sourceID:  sourceID,
		registry:  registry,
		converter: converter,
	}
}

// FromToken creates a Span from a single ANTLR token.
func (b *Builder) FromToken(token antlr.Token) location.Span {
	if token == nil {
		return location.Span{}
	}

	startRune := token.GetStart()
	// End is exclusive; token.GetStop() is the last character index
	endRune := token.GetStop() + 1

	return b.fromRuneOffsets(startRune, endRune)
}

// FromContext creates a Span covering the entire parser rule context.
func (b *Builder) FromContext(ctx antlr.ParserRuleContext) location.Span {
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
func (b *Builder) FromTokens(start, stop antlr.Token) location.Span {
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
func (b *Builder) fromRuneOffsets(startRune, endRune int) location.Span {
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
//
// A zero Position during schema parsing indicates:
//   - A bug in RuneToByteOffset (rune→byte conversion)
//   - A bug in the ANTLR offset derivation pipeline
//   - Source ID mismatch (wrong SourceID passed to registry)
//   - Race condition (source cleared mid-parse)
//
// All of these are programmer errors, not content errors.
func mustPositionAt(reg location.PositionRegistry, src location.SourceID, byteOffset int) location.Position {
	pos := reg.PositionAt(src, byteOffset)
	if pos.IsZero() {
		panic(fmt.Sprintf("schema parsing invariant: PositionAt(%s, %d) returned zero Position", src, byteOffset))
	}
	return pos
}

// MustPositionAt is the exported version of mustPositionAt for use by other
// packages that need the same invariant enforcement.
func MustPositionAt(reg location.PositionRegistry, src location.SourceID, byteOffset int) location.Position {
	return mustPositionAt(reg, src, byteOffset)
}

// Registry returns the underlying PositionRegistry.
func (b *Builder) Registry() location.PositionRegistry {
	return b.registry
}

// Converter returns the underlying RuneOffsetConverter.
func (b *Builder) Converter() location.RuneOffsetConverter {
	return b.converter
}
