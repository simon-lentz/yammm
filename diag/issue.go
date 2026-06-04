package diag

import (
	"log/slog"

	"github.com/simon-lentz/yammm/location"
)

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

// LogValue implements [slog.LogValuer] for structured logging.
//
// Returns a GroupValue with the following attributes:
//
//   - "severity" (string): one of "fatal", "error", "warning", "info",
//     "hint" — matches [Severity.String] and is part of the wire-format
//     stability guarantee. Always emitted.
//   - "message" (string): the human-readable description. Always emitted.
//   - "code" (string): the stable [Code] identifier. Omitted when
//     [Code.IsZero] is true.
//   - "path" (string): the canonical instance path. Omitted when empty.
//   - "location" (group): {source, line, column} derived from the issue's
//     span. Omitted when [HasSpan] is false.
//   - "hint" (string): the optional remediation hint. Omitted when empty.
//   - "details" (group): the issue's [Detail] key-value pairs emitted as
//     sub-attributes. Omitted when no details are present.
//
// Issue is passed through slog infrastructure as-is:
//
//	logger.LogAttrs(ctx, slog.LevelError, "validation failed",
//	    slog.Any("issue", issue))
//
// For the issues-slice shape surfaced by [ContextualError.LogValue],
// each entry is materialized via [issueLogMap] (not [LogValue]) so that
// [slog.JSONHandler] and other encoding/json-based handlers render a
// JSON array of objects; slog does not recurse through [slog.LogValuer]
// within slice elements.
func (i Issue) LogValue() slog.Value {
	attrs := make([]slog.Attr, 0, 7)
	attrs = append(
		attrs,
		slog.String("severity", i.severity.String()),
		slog.String("message", i.message),
	)
	if !i.code.IsZero() {
		attrs = append(attrs, slog.String("code", i.code.String()))
	}
	if i.path != "" {
		attrs = append(attrs, slog.String("path", i.path))
	}
	if i.HasSpan() {
		attrs = append(attrs, slog.Group(
			"location",
			slog.String("source", i.span.Source.String()),
			slog.Int("line", i.span.Start.Line),
			slog.Int("column", i.span.Start.Column),
		))
	}
	if i.hint != "" {
		attrs = append(attrs, slog.String("hint", i.hint))
	}
	if len(i.details) > 0 {
		detailAttrs := make([]any, 0, len(i.details))
		for _, d := range i.details {
			detailAttrs = append(detailAttrs, slog.String(d.Key, d.Value))
		}
		attrs = append(attrs, slog.Group("details", detailAttrs...))
	}
	return slog.GroupValue(attrs...)
}

// issueLogMap is the map-shaped sibling of [Issue.LogValue].
//
// The two produce the same structural shape; the map form is used by
// [ContextualError.LogValue] to populate the "issues" slice because
// slog handlers do not recurse through [slog.LogValuer] within slice
// elements — passing []slog.Value would make handlers that route
// through encoding/json (notably [slog.JSONHandler]) emit empty objects
// since slog.Value has no json.Marshaler. A []map[string]any renders
// correctly under those handlers while preserving the per-issue shape
// the LogValue Godoc documents.
func issueLogMap(i Issue) map[string]any {
	m := map[string]any{
		"severity": i.severity.String(),
		"message":  i.message,
	}
	if !i.code.IsZero() {
		m["code"] = i.code.String()
	}
	if i.path != "" {
		m["path"] = i.path
	}
	if i.HasSpan() {
		m["location"] = map[string]any{
			"source": i.span.Source.String(),
			"line":   i.span.Start.Line,
			"column": i.span.Start.Column,
		}
	}
	if i.hint != "" {
		m["hint"] = i.hint
	}
	if len(i.details) > 0 {
		details := make(map[string]any, len(i.details))
		for _, d := range i.details {
			details[d.Key] = d.Value
		}
		m["details"] = details
	}
	return m
}
