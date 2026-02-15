package instance

import (
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

// RawInstance represents unvalidated instance data.
//
// RawInstance is the input to the validation pipeline. It contains the raw
// property values from the input source (typically JSON) and optional
// provenance information for error reporting.
type RawInstance struct {
	// Properties contains the raw property values keyed by property name.
	// Property names may use any casing; the validator normalizes them.
	Properties map[string]any

	// Provenance optionally captures source location metadata.
	// If nil, error messages will not include source locations.
	Provenance *Provenance
}

// Provenance captures source location metadata for error reporting.
//
// Provenance links validation errors back to their source location in the
// input document. This enables helpful error messages that point to the
// exact position of invalid data.
//
// # Nil Receiver Behavior
//
// All Provenance methods are safe to call on nil receivers. Navigation methods
// (WithPath, AtKey, AtIndex) convert nil to a new Provenance with the specified
// path but empty source information:
//
//	var prov *Provenance = nil
//	derived := prov.AtKey("foo").AtIndex(0)  // Safe, returns valid Provenance
//
// After any navigation operation on nil, sourceName and span will be zero values.
// This is intentional: it preserves path navigation for diagnostics while indicating
// no source location is available. Accessor methods (SourceName, Path, Span) return
// zero values when called on nil.
type Provenance struct {
	sourceName string
	path       path.Builder
	span       location.Span
}

// NewProvenance creates a new Provenance with the given source information.
func NewProvenance(sourceName string, path path.Builder, span location.Span) *Provenance {
	return &Provenance{
		sourceName: sourceName,
		path:       path,
		span:       span,
	}
}

// SourceName returns the name of the source (e.g., filename).
func (p *Provenance) SourceName() string {
	if p == nil {
		return ""
	}
	return p.sourceName
}

// Path returns the JSONPath-like path to this instance within the source.
func (p *Provenance) Path() path.Builder {
	if p == nil {
		return path.Root()
	}
	return p.path
}

// Span returns the source location span.
func (p *Provenance) Span() location.Span {
	if p == nil {
		return location.Span{}
	}
	return p.span
}

// WithPath returns a new Provenance with a different path.
func (p *Provenance) WithPath(newPath path.Builder) *Provenance {
	if p == nil {
		return &Provenance{path: newPath}
	}
	return &Provenance{
		sourceName: p.sourceName,
		path:       newPath,
		span:       p.span,
	}
}

// AtKey returns a new Provenance with the path extended by a key.
func (p *Provenance) AtKey(key string) *Provenance {
	if p == nil {
		return &Provenance{path: path.Root().Key(key)}
	}
	return &Provenance{
		sourceName: p.sourceName,
		path:       p.path.Key(key),
		span:       p.span,
	}
}

// AtIndex returns a new Provenance with the path extended by an index.
func (p *Provenance) AtIndex(index int) *Provenance {
	if p == nil {
		return &Provenance{path: path.Root().Index(index)}
	}
	return &Provenance{
		sourceName: p.sourceName,
		path:       p.path.Index(index),
		span:       p.span,
	}
}
