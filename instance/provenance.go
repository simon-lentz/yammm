package instance

import (
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

// Provenance records where an instance came from, so a diagnostic about it can
// name its place: the source's name, the instance's path within the source, and
// its span in the source text when the reader knows one.
//
// A Provenance is a handle. Every method is safe on the zero Provenance, whose
// accessors return the empty name, [path.Root] and the zero span.
type Provenance struct{ *provenanceState }

// IsZero reports whether the handle names no record.
func (p Provenance) IsZero() bool { return p.provenanceState == nil }

// provenanceState is the record a [Provenance] names.
type provenanceState struct {
	sourceName string
	path       path.Path
	span       location.Span
}

// NewProvenance records an instance's source name, its path within the source,
// kept as given, and its span, which may be zero.
func NewProvenance(sourceName string, p path.Path, span location.Span) Provenance {
	return Provenance{&provenanceState{
		sourceName: sourceName,
		path:       p,
		span:       span,
	}}
}

// SourceName returns the name of the source (e.g., filename).
func (p Provenance) SourceName() string {
	if p.IsZero() {
		return ""
	}
	return p.sourceName
}

// Path returns the instance's path within the source, as it was recorded.
func (p Provenance) Path() path.Path {
	if p.IsZero() {
		return path.Root
	}
	return p.path
}

// Span returns the source location span.
func (p Provenance) Span() location.Span {
	if p.IsZero() {
		return location.Span{}
	}
	return p.span
}
