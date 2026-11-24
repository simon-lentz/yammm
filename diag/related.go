package diag

import "github.com/simon-lentz/yammm/location"

// RelatedInfo describes an additional location associated with a diagnostic.
//
// Used for supplementary context like "previous definition here" or
// showing the edges of a composition cycle.
type RelatedInfo struct {
	// Span identifies the related source location.
	Span location.Span

	// Message provides context about why this location is related.
	Message string
}
