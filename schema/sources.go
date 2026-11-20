package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// sourceRegistry is the source content a [Sources] reads; *source.Registry
// implements it.
type sourceRegistry interface {
	ContentBySource(id location.SourceID) ([]byte, bool)
	Content(span location.Span) ([]byte, bool)
	// Keys returns every registered source ID, sorted by SourceID.String(), in a
	// slice the caller owns.
	Keys() []location.SourceID
}

// Sources provides read-only access to schema source content.
// It wraps an underlying source registry for diagnostic rendering.
type Sources struct{ *sourcesState }

// IsZero reports whether the handle names no record.
func (s Sources) IsZero() bool { return s.sourcesState == nil }

// sourcesState is the record a [Sources] names.
type sourcesState struct {
	registry sourceRegistry
}

// newSources creates a Sources wrapper around a source registry.
func newSources(registry sourceRegistry) Sources {
	if registry == nil {
		return Sources{}
	}
	return Sources{&sourcesState{registry: registry}}
}

// ContentBySource returns the source content for the given source ID.
// Returns nil, false if the source is not found.
func (s Sources) ContentBySource(id location.SourceID) ([]byte, bool) {
	if s.IsZero() {
		return nil, false
	}
	return s.registry.ContentBySource(id)
}

// Content returns the source content for the span's source ID.
// Returns nil, false if the source is not found.
// Implements diag.SourceProvider.
func (s Sources) Content(span location.Span) ([]byte, bool) {
	if s.IsZero() {
		return nil, false
	}
	return s.registry.Content(span)
}

// SourceIDs returns all registered source IDs in sorted order.
// Sources are sorted by SourceID.String(), ensuring deterministic ordering.
func (s Sources) SourceIDs() []location.SourceID {
	if s.IsZero() {
		return nil
	}
	return s.registry.Keys()
}
