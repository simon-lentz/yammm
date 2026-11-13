package schema

import (
	"regexp"

	"github.com/simon-lentz/yammm/internal/expr"
	"github.com/simon-lentz/yammm/location"
)

func init() {
	expr.InvariantExpression = func(inv any) expr.Expression {
		return inv.(Invariant).expr //nolint:errcheck // each caller passes an Invariant
	}
	expr.PatternRegexp = func(constraint any) *regexp.Regexp {
		return constraint.(PatternConstraint).compiled //nolint:errcheck // each caller passes a PatternConstraint
	}
}

// Invariant represents a constraint expression attached to a type.
// Invariants are validated at runtime; the expression is compiled at schema
// load time and evaluated at instance validation time.
//
// The name is the failure message and also the invariant's identity for
// inheritance: a subtype's invariant overrides an inherited one of the same
// name, one type may not declare a name twice, and two inherited invariants
// sharing a name must carry the same expression.
type Invariant struct{ *invariantState }

// IsZero reports whether the handle names no record.
func (i Invariant) IsZero() bool { return i.invariantState == nil }

// invariantState is the record an [Invariant] names.
type invariantState struct {
	name string          // user-facing message shown when invariant fails
	expr expr.Expression // compiled expression
	span location.Span   // source location
	doc  string          // documentation comment
}

// newInvariant creates a new Invariant.
func newInvariant(name string, e expr.Expression, span location.Span, doc string) Invariant {
	return Invariant{&invariantState{
		name: name,
		expr: e,
		span: span,
		doc:  doc,
	}}
}

// Name returns the user-facing message for this invariant.
// This is displayed when the invariant evaluates to false.
func (i Invariant) Name() string {
	return i.name
}

// Span returns the source location of this invariant declaration.
func (i Invariant) Span() location.Span {
	return i.span
}

// Documentation returns the documentation comment, if any.
func (i Invariant) Documentation() string {
	return i.doc
}
