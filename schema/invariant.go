package schema

import (
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Invariant represents a constraint expression attached to a type.
// Invariants are validated at runtime; the expression is compiled at schema
// load time and evaluated at instance validation time.
//
// The name is the failure message and also the invariant's identity for
// inheritance: a subtype's invariant overrides an inherited one of the same
// name, one type may not declare a name twice, and two inherited invariants
// sharing a name must carry the same expression.
type Invariant struct {
	name string          // user-facing message shown when invariant fails
	expr expr.Expression // compiled expression
	span location.Span   // source location
	doc  string          // documentation comment
}

// newInvariant creates a new Invariant.
//
// This is a low-level API primarily for:
//   - Internal use during schema parsing
//   - Advanced use cases like building schemas programmatically via Builder
//
// Most users should load schemas from .yammm files using the load package.
func newInvariant(name string, e expr.Expression, span location.Span, doc string) *Invariant {
	return &Invariant{
		name: name,
		expr: e,
		span: span,
		doc:  doc,
	}
}

// Name returns the user-facing message for this invariant.
// This is displayed when the invariant evaluates to false.
func (i *Invariant) Name() string {
	return i.name
}

// Expression returns the compiled expression for this invariant.
func (i *Invariant) Expression() expr.Expression {
	return i.expr
}

// Span returns the source location of this invariant declaration.
func (i *Invariant) Span() location.Span {
	return i.span
}

// Documentation returns the documentation comment, if any.
func (i *Invariant) Documentation() string {
	return i.doc
}
