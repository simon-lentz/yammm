package schema

import (
	"iter"

	"github.com/simon-lentz/yammm/location"
)

// SourceRegistry provides the full source content and position lookup interface.
// This is typically implemented by internal/source.Registry.
type SourceRegistry interface {
	// ContentBySource returns the source content for the given source ID.
	ContentBySource(id location.SourceID) ([]byte, bool)

	// Content returns the source content for the span's source ID.
	Content(span location.Span) ([]byte, bool)

	// PositionAt converts a byte offset to a Position.
	PositionAt(id location.SourceID, byteOffset int) location.Position

	// LineStartByte returns the byte offset of the start of a line.
	LineStartByte(id location.SourceID, line int) (int, bool)

	// RuneToByteOffset converts a rune index to a byte offset.
	RuneToByteOffset(id location.SourceID, runeIndex int) (int, bool)

	// Keys returns all registered source IDs in sorted order.
	// The returned slice must be sorted by SourceID.String() and must be
	// a defensive copy (callers may modify it freely).
	// Implementations must guarantee deterministic iteration order.
	Keys() []location.SourceID

	// Has reports whether the source ID is registered.
	Has(id location.SourceID) bool

	// Len returns the number of registered sources.
	Len() int
}

// Sources provides read-only access to schema source content.
// It wraps an underlying source registry for diagnostic rendering.
type Sources struct {
	registry SourceRegistry
}

// NewSources creates a Sources wrapper around a source registry.
func NewSources(registry SourceRegistry) *Sources {
	if registry == nil {
		return nil
	}
	return &Sources{registry: registry}
}

// ContentBySource returns the source content for the given source ID.
// Returns nil, false if the source is not found.
func (s *Sources) ContentBySource(id location.SourceID) ([]byte, bool) {
	if s == nil || s.registry == nil {
		return nil, false
	}
	return s.registry.ContentBySource(id)
}

// Content returns the source content for the span's source ID.
// Returns nil, false if the source is not found.
// Implements diag.SourceProvider.
func (s *Sources) Content(span location.Span) ([]byte, bool) {
	if s == nil || s.registry == nil {
		return nil, false
	}
	return s.registry.Content(span)
}

// PositionAt converts a byte offset to a Position.
// Returns zero Position if the source is not found or offset is invalid.
func (s *Sources) PositionAt(id location.SourceID, byteOffset int) location.Position {
	if s == nil || s.registry == nil {
		return location.Position{}
	}
	return s.registry.PositionAt(id, byteOffset)
}

// LineStartByte returns the byte offset of the start of a line.
// Returns 0, false if the source is not found or line is invalid.
// Implements diag.LineIndexProvider.
func (s *Sources) LineStartByte(id location.SourceID, line int) (int, bool) {
	if s == nil || s.registry == nil {
		return 0, false
	}
	return s.registry.LineStartByte(id, line)
}

// RuneToByteOffset converts a rune index to a byte offset.
// Returns 0, false if the source is not found or rune index is invalid.
func (s *Sources) RuneToByteOffset(id location.SourceID, runeIndex int) (int, bool) {
	if s == nil || s.registry == nil {
		return 0, false
	}
	return s.registry.RuneToByteOffset(id, runeIndex)
}

// SourceIDs returns all registered source IDs in sorted order.
// Sources are sorted by SourceID.String(), ensuring deterministic ordering.
func (s *Sources) SourceIDs() []location.SourceID {
	if s == nil || s.registry == nil {
		return nil
	}
	return s.registry.Keys()
}

// SourceIDsIter returns an iterator over all source IDs.
// The iteration order matches SourceIDs() (sorted by SourceID.String()).
// This ordering guarantee ensures deterministic iteration across calls.
func (s *Sources) SourceIDsIter() iter.Seq[location.SourceID] {
	return func(yield func(location.SourceID) bool) {
		if s == nil || s.registry == nil {
			return
		}
		for _, id := range s.registry.Keys() {
			if !yield(id) {
				return
			}
		}
	}
}

// Has reports whether the given source ID is registered.
func (s *Sources) Has(id location.SourceID) bool {
	if s == nil || s.registry == nil {
		return false
	}
	return s.registry.Has(id)
}

// Len returns the number of registered sources.
func (s *Sources) Len() int {
	if s == nil || s.registry == nil {
		return 0
	}
	return s.registry.Len()
}
