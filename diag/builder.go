package diag

import (
	"fmt"

	"github.com/simon-lentz/yammm/location"
)

// IssueBuilder provides fluent construction of [Issue] values.
//
// IssueBuilder is the only valid construction path for Issue values in
// production code. Direct struct literal construction bypasses validity
// checks and will cause panics when the issue is collected.
//
// Example:
//
//	issue := diag.NewIssue(diag.Error, diag.E_TYPE_COLLISION, `type "Person" already defined`).
//	    WithSpan(span).
//	    WithHint("rename one of the types").
//	    Build()
type IssueBuilder struct {
	issue Issue
}

// NewIssue starts building an issue with required fields.
//
// The severity, code, and message are required for a valid issue. Additional
// fields can be set using the With* methods before calling [IssueBuilder.Build].
//
// NewIssue panics if any required field is invalid:
//   - severity must be a valid Severity value (Fatal through Hint)
//   - code must not be zero (use package-defined codes like E_SYNTAX)
//   - message must not be empty
//
// These panics catch programmer errors at construction time rather than
// deferring failure to [Collector.Collect]. This fulfills the builder's
// guarantee that issues constructed via IssueBuilder are always valid.
func NewIssue(severity Severity, code Code, message string) *IssueBuilder {
	if severity > Hint {
		panic(fmt.Sprintf("diag.NewIssue: invalid severity %d (must be 0-%d)", severity, Hint))
	}
	if code.IsZero() {
		panic("diag.NewIssue: zero code (use package-defined codes like E_SYNTAX)")
	}
	if message == "" {
		panic("diag.NewIssue: empty message")
	}
	return &IssueBuilder{
		issue: Issue{
			severity: severity,
			code:     code,
			message:  message,
		},
	}
}

// FromIssue creates an IssueBuilder initialized from an existing issue.
//
// This enables augmenting issues with additional details while preserving
// all original fields. The returned builder creates a new issue; the
// original is not modified.
//
// FromIssue panics if the input issue is zero or invalid. This maintains
// the builder's "valid input → valid output" contract and catches errors
// at the augmentation site rather than at collection time.
//
// Example use case: when merging child validation issues into a parent
// context, additional relation context can be added:
//
//	augmented := diag.FromIssue(childIssue).
//	    WithDetails(diag.PathRelation(rel.Name(), rel.FieldName())...).
//	    Build()
func FromIssue(issue Issue) *IssueBuilder {
	if issue.IsZero() {
		panic("diag.FromIssue: zero-value Issue")
	}
	if !issue.IsValid() {
		panic(fmt.Sprintf("diag.FromIssue: invalid Issue (code=%s)", issue.Code()))
	}
	b := &IssueBuilder{
		issue: Issue{
			severity:   issue.severity,
			code:       issue.code,
			message:    issue.message,
			hint:       issue.hint,
			sourceName: issue.sourceName,
			path:       issue.path,
			span:       issue.span,
		},
	}
	// Deep copy slices to preserve original issue immutability
	if len(issue.related) > 0 {
		b.issue.related = make([]location.RelatedInfo, len(issue.related))
		copy(b.issue.related, issue.related)
	}
	if len(issue.details) > 0 {
		b.issue.details = make([]Detail, len(issue.details))
		copy(b.issue.details, issue.details)
	}
	return b
}

// WithSpan sets the source span.
//
// Use this for issues with source location information (e.g., schema errors).
// No separate HasSpan flag is needed; presence is determined by span.IsZero().
func (b *IssueBuilder) WithSpan(span location.Span) *IssueBuilder {
	b.issue.span = span
	return b
}

// WithPath sets instance provenance.
//
// sourceName is the label for the instance data source (e.g., "data.json").
// path is the canonical instance path (e.g., "$.Car[0].regNbr").
func (b *IssueBuilder) WithPath(sourceName, path string) *IssueBuilder {
	b.issue.sourceName = sourceName
	b.issue.path = path
	return b
}

// WithHint sets the resolution suggestion.
//
// Hints provide actionable guidance for fixing the issue.
func (b *IssueBuilder) WithHint(hint string) *IssueBuilder {
	b.issue.hint = hint
	return b
}

// WithRelated adds related location information.
//
// Related locations provide context like "previous definition here" for
// duplicate type errors or showing edges of an import cycle.
//
// When adding an ordered sequence (e.g., import cycle), provide entries in
// chain order: the first argument is the first step, the last is the final step.
//
// Multiple calls to WithRelated append to the existing related list.
//
// Ordering note: Related entries are compared lexicographically during issue
// sorting (by span, then message). For deterministic output, add entries in
// a consistent order.
func (b *IssueBuilder) WithRelated(related ...location.RelatedInfo) *IssueBuilder {
	b.issue.related = append(b.issue.related, related...)
	return b
}

// WithDetail adds a single key-value detail.
//
// This is a convenience method equivalent to WithDetails(Detail{Key: key, Value: value}).
// Use the standard DetailKey* constants for consistent key naming.
//
// Multiple calls to WithDetail append to the existing details list.
func (b *IssueBuilder) WithDetail(key, value string) *IssueBuilder {
	b.issue.details = append(b.issue.details, Detail{Key: key, Value: value})
	return b
}

// WithDetails adds key-value context.
//
// Details provide structured information that can be programmatically
// inspected by tools. Use the standard DetailKey* constants for consistent
// key naming.
//
// Multiple calls to WithDetails append to the existing details list.
//
// Ordering note: Details are compared lexicographically during issue sorting
// (by key, then value). For deterministic output, add details in a consistent
// order.
func (b *IssueBuilder) WithDetails(details ...Detail) *IssueBuilder {
	b.issue.details = append(b.issue.details, details...)
	return b
}

// WithExpectedGot is a convenience for type mismatch issues.
//
// This is equivalent to calling WithDetails(ExpectedGot(expected, got)...).
func (b *IssueBuilder) WithExpectedGot(expected, got string) *IssueBuilder {
	return b.WithDetails(ExpectedGot(expected, got)...)
}

// Build returns the constructed issue.
//
// Build deep-copies the related and details slices into fresh, tight-capacity
// slices. This ensures builder reuse cannot mutate previously-built issues
// (immutability guarantee).
//
// The returned issue is guaranteed to be valid (IsValid() returns true)
// because NewIssue requires severity, code, and message.
func (b *IssueBuilder) Build() Issue {
	result := b.issue

	// Deep copy slices to ensure immutability
	if len(b.issue.related) > 0 {
		result.related = make([]location.RelatedInfo, len(b.issue.related))
		copy(result.related, b.issue.related)
	}
	if len(b.issue.details) > 0 {
		result.details = make([]Detail, len(b.issue.details))
		copy(result.details, b.issue.details)
	}

	return result
}
