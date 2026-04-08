package location

import (
	"github.com/simon-lentz/yammm/location/path"
)

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
	span       Span
	rawPath    string
}

// NewProvenance creates a new Provenance with the given source information.
func NewProvenance(sourceName string, path path.Builder, span Span) *Provenance {
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
func (p *Provenance) Span() Span {
	if p == nil {
		return Span{}
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

// RawPath returns the raw path string preserved from snapshot loading.
//
// Returns an empty string for provenance created through normal construction
// paths (adapter parsing, programmatic NewProvenance). Only set during
// snapshot.Load when path.Parse() fails to parse the persisted path string,
// enabling round-trip preservation of unrecognized path formats.
func (p *Provenance) RawPath() string {
	if p == nil {
		return ""
	}
	return p.rawPath
}

// WithRawPath returns a new Provenance with the raw path string set.
//
// The raw path is used for snapshot round-trip preservation when the
// parsed path.Builder cannot faithfully reproduce the original path string.
func (p *Provenance) WithRawPath(raw string) *Provenance {
	if p == nil {
		return &Provenance{rawPath: raw}
	}
	return &Provenance{
		sourceName: p.sourceName,
		path:       p.path,
		span:       p.span,
		rawPath:    raw,
	}
}

// HasSpan reports whether this provenance has a non-zero source location span.
//
// Returns false for provenance loaded from persisted snapshots (.ys files),
// where source file byte offsets are not persisted. Consumers should use
// HasSpan to guard span access in code that may operate on both constructed
// and loaded provenance.
func (p *Provenance) HasSpan() bool {
	return p != nil && !p.span.IsZero()
}
