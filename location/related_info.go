package location

// Common RelatedInfo message constants for consistent diagnostic output.
// Using these constants ensures uniform casing and punctuation across the codebase.
const (
	MsgPreviousDefinition = "previous definition here"
	MsgImportedFrom       = "imported from here"
	MsgDeclaredHere       = "declared here"
	MsgRequiredBy         = "required by this constraint"
	MsgReferencedFrom     = "referenced from here"
	MsgDefinedHere        = "defined here"
)

// RelatedInfo describes an additional location associated with a diagnostic.
//
// Used for supplementary context like "previous definition here" or
// showing edges of an import cycle.
type RelatedInfo struct {
	// Span identifies the related source location.
	Span Span

	// Message provides context about why this location is related.
	// Prefer using the Msg* constants (e.g., MsgPreviousDefinition) for consistency.
	Message string
}

// IsValid reports whether the related info has meaningful content.
// At minimum, either the Span must be valid or the Message must be non-empty.
//
// Valid combinations and use cases:
//   - Both Span and Message: Most common case, e.g., "previous definition here" at a location
//   - Span only: When the location itself provides context without explanation
//   - Message only: When context is needed but no source location exists (e.g., compiler-generated)
//   - Neither: Invalid - IsValid() returns false
func (r RelatedInfo) IsValid() bool {
	return r.Span.IsValid() || r.Message != ""
}

// String returns a human-readable representation.
func (r RelatedInfo) String() string {
	if r.Span.IsZero() {
		return r.Message
	}
	if r.Message == "" {
		return r.Span.String()
	}
	return r.Span.String() + ": " + r.Message
}
