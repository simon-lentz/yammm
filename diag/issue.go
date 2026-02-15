package diag

import "github.com/simon-lentz/yammm/location"

// Issue represents a single diagnostic issue.
//
// Issue is immutable after construction. All fields are unexported to preserve
// immutability; use accessor methods to read values. Construct Issues using
// [NewIssue] and [IssueBuilder].
//
// Direct struct literal construction bypasses validity checks and will cause
// panics when the issue is collected via [Collector.Collect].
//
// Zero-value note: The Go zero value for Severity is Fatal (value 0). When
// constructing Issue literals in tests, set severity explicitly to avoid
// unintentionally creating Fatal issues.
type Issue struct {
	span       location.Span          // source location; check HasSpan() or span.IsZero()
	sourceName string                 // instance provenance label (e.g., "data.json")
	path       string                 // canonical instance path (e.g., "$.Car[0].regNbr")
	severity   Severity               // issue severity level
	code       Code                   // stable programmatic identifier
	message    string                 // human-readable description (no embedded locations)
	hint       string                 // optional resolution suggestion
	related    []location.RelatedInfo // additional locations (e.g., "previous definition here")
	details    []Detail               // additional key-value context
}

// Severity returns the issue's severity level.
func (i Issue) Severity() Severity {
	return i.severity
}

// Code returns the issue's stable programmatic identifier.
func (i Issue) Code() Code {
	return i.code
}

// Message returns the human-readable description.
//
// Messages should not contain embedded locations; use [Span], [SourceName],
// and [Path] for location information.
func (i Issue) Message() string {
	return i.message
}

// Span returns the source location span.
//
// Use [HasSpan] to check if the span is present, or check span.IsZero().
func (i Issue) Span() location.Span {
	return i.span
}

// SourceName returns the instance provenance label.
//
// This is typically the filename or identifier of the instance data source
// (e.g., "data.json", "inline:test").
func (i Issue) SourceName() string {
	return i.sourceName
}

// Path returns the canonical instance path.
//
// This is a JSONPath-like path identifying the location within instance data
// (e.g., "$.Car[0].regNbr").
func (i Issue) Path() string {
	return i.path
}

// Hint returns the optional resolution suggestion.
func (i Issue) Hint() string {
	return i.hint
}

// HasSpan reports whether the issue has a non-zero span.
//
// Use this instead of manually checking Span().IsZero() for clarity.
func (i Issue) HasSpan() bool {
	return !i.span.IsZero()
}

// IsZero reports whether the issue is a zero value.
//
// A zero-value issue has no code, no message, and no provenance.
func (i Issue) IsZero() bool {
	return i.code.IsZero() && i.message == "" && i.span.IsZero() &&
		i.sourceName == "" && i.path == ""
}

// IsValid reports whether the issue has the minimum required fields set.
//
// An issue is valid if it has:
//   - A valid code (not zero)
//   - A non-empty message
//   - A valid severity (not an undefined value like Severity(255))
//
// This method exists for documentation and testing; production code using
// [IssueBuilder] never needs to call it because the builder guarantees validity.
// The severity check catches diag-internal mistakes where issues are constructed
// directly rather than via the builder pattern.
func (i Issue) IsValid() bool {
	return !i.code.IsZero() &&
		i.message != "" &&
		i.severity <= Hint // Hint (4) is the highest valid severity value
}

// IsSchemaOnly reports whether the issue has span provenance but no instance path.
//
// Schema-only issues are typically from schema compilation and have source
// location information but no instance path.
//
// Note: SourceName without Path provides file-level instance provenance but is
// not sufficient for instance-specific classification. This method only checks
// Span and Path—issues with only SourceName return false.
func (i Issue) IsSchemaOnly() bool {
	return i.HasSpan() && i.path == ""
}

// IsInstanceOnly reports whether the issue has instance path but no span.
//
// Instance-only issues are typically from instance validation where source
// locations are not available (e.g., database-sourced instances).
//
// Note: SourceName without Path provides file-level provenance but is not
// considered instance-specific. This method requires a non-empty Path for
// instance classification.
func (i Issue) IsInstanceOnly() bool {
	return !i.HasSpan() && i.path != ""
}

// IsHybrid reports whether the issue has both span and path information.
//
// Hybrid issues occur when instance data has source location tracking enabled
// (e.g., JSON adapter with TrackLocations: true).
func (i Issue) IsHybrid() bool {
	return i.HasSpan() && i.path != ""
}

// Related returns a copy of the related location information.
//
// Returns nil if no related info is present. The returned slice is a defensive
// copy; modifications do not affect the original issue.
//
// Ordering contract: When related locations represent an ordered sequence
// (e.g., import cycles, call chains), slice order is significant: index 0
// is the first step, index N-1 is the last. For unordered collections,
// order is arbitrary but stable.
func (i Issue) Related() []location.RelatedInfo {
	if len(i.related) == 0 {
		return nil
	}
	cp := make([]location.RelatedInfo, len(i.related))
	copy(cp, i.related)
	return cp
}

// Details returns a copy of the detail key-value pairs.
//
// Returns nil if no details are present. The returned slice is a defensive
// copy; modifications do not affect the original issue.
func (i Issue) Details() []Detail {
	if len(i.details) == 0 {
		return nil
	}
	cp := make([]Detail, len(i.details))
	copy(cp, i.details)
	return cp
}

// Clone returns a deep copy of the issue.
//
// INVARIANT: All slice element types (RelatedInfo, Detail) must not contain
// mutable reference fields (maps, slices, pointers, funcs, chans). Strings
// are permitted (immutable). If mutable reference fields are ever added to
// these types, this method must be updated to deep-copy their targets to
// preserve immutability guarantees.
func (i Issue) Clone() Issue {
	clone := i
	if len(i.related) > 0 {
		clone.related = make([]location.RelatedInfo, len(i.related))
		copy(clone.related, i.related)
	}
	if len(i.details) > 0 {
		clone.details = make([]Detail, len(i.details))
		copy(clone.details, i.details)
	}
	return clone
}
