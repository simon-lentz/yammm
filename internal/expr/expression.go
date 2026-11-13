package expr

import (
	"regexp"
	"slices"
)

// InvariantExpression returns a schema.Invariant's tree. The schema package sets it
// once, in its init, and nothing writes it after, so a read takes no lock. It takes
// any because this package sits below schema and cannot name the type, and it
// panics on any other argument.
var InvariantExpression func(inv any) Expression

// PatternRegexps returns a schema.PatternConstraint's compiled patterns, the
// constraint's own slice, which a reader must not write. The schema package sets it
// once, in its init, and nothing writes it after, so a read takes no lock. It takes
// any because this package sits below schema, and it panics on any other argument.
var PatternRegexps func(constraint any) []*regexp.Regexp

// SelfVariable is the name bound to the instance under evaluation. The
// completer refuses a member of this name, the static checker binds it in the
// root scope and the evaluator's property scopes bind it to the instance map,
// so all three layers read one constant rather than agreeing by coincidence.
const SelfVariable = "self"

// Expression represents a node in the expression AST.
//
// Expression nodes are immutable after construction. The AST is built during
// schema parsing and stored for later evaluation by the instance layer.
type Expression interface {
	// Op returns the operation name for this node.
	// For SExpr, this is the first element (e.g., "+", "&&", "Any").
	// For Literal, this is "lit".
	// For Op, this is the operation string itself.
	Op() string

	// Children returns the child expressions.
	// For SExpr, these are the operands (excluding the operation).
	// For Literal and Op, this returns an empty slice.
	Children() []Expression

	// Literal returns the literal value for literal nodes.
	// For SExpr, this returns the Op() value.
	// For Literal, this returns the wrapped value.
	// For Op, this returns the operation string.
	Literal() any

	// unexported marker to prevent external implementations
	expression()
}

// SExpr represents an S-expression: an operation with zero or more children.
//
// The first element (accessed via Op()) is the operation name.
// The remaining elements (accessed via Children()) are the operands.
//
// Example: (+ 1 2) is represented as SExpr{Op("+"), Literal{1}, Literal{2}}
type SExpr []Expression

// Op implements Expression.
func (e SExpr) Op() string {
	if len(e) == 0 {
		return ""
	}
	if op, ok := e[0].(Op); ok {
		return string(op)
	}
	return ""
}

// Children implements Expression.
//
// Returns a defensive copy of the child expressions to preserve immutability.
// Callers may safely modify the returned slice without affecting the original.
func (e SExpr) Children() []Expression {
	if len(e) <= 1 {
		return nil
	}
	return slices.Clone(e[1:])
}

// Literal implements Expression.
func (e SExpr) Literal() any {
	return e.Op()
}

// expression implements Expression.
func (SExpr) expression() {}

// Literal represents a literal value in an expression.
//
// Supported value types:
//   - string
//   - int64
//   - float64
//   - bool
//   - *regexp.Regexp
//   - nil
//   - []Expression (for argument lists)
//   - []string (for parameter lists)
type Literal struct {
	Val any
}

// NewLiteral creates a new literal expression.
func NewLiteral(val any) Expression {
	// Unwrap if already a Literal
	if lit, ok := val.(*Literal); ok {
		return lit
	}
	return &Literal{Val: val}
}

// Op implements Expression.
func (*Literal) Op() string {
	return "lit"
}

// Children implements Expression.
func (*Literal) Children() []Expression {
	return nil
}

// Literal implements Expression.
func (l *Literal) Literal() any {
	return l.Val
}

// expression implements Expression.
func (*Literal) expression() {}

// Op represents an operation name in an S-expression.
//
// Common operations:
//   - Arithmetic: "+", "-", "*", "/", "%", "-x" (unary minus)
//   - Comparison: "==", "!=", "<", "<=", ">", ">="
//   - Logical: "&&", "||", "!"
//   - Matching: "=~", "!~", "in"
//   - Control: "?" (ternary)
//   - Access: "$" (variable), "p" (property), "." (member), "@" (index), "[]" (list)
//   - Builtins: "Any", "All", "AllOrNone", "Filter", "Map", "Reduce", etc.
type Op string

// Op implements Expression.
func (o Op) Op() string {
	return string(o)
}

// Children implements Expression.
func (Op) Children() []Expression {
	return nil
}

// Literal implements Expression.
func (o Op) Literal() any {
	return string(o)
}

// expression implements Expression.
func (Op) expression() {}

// StringLiteral extracts the string a [*Literal] wraps. Any other node returns
// false — including an [SExpr], whose Literal() is its op name and is not a
// string literal.
func StringLiteral(expr Expression) (string, bool) {
	lit, ok := expr.(*Literal)
	if !ok {
		return "", false
	}
	str, ok := lit.Val.(string)
	return str, ok
}

// ArgsLiteral extracts a []Expression from a literal expression.
// This is used to extract function call arguments from the AST.
// Returns false if the expression is nil or not an args literal.
func ArgsLiteral(expr Expression) ([]Expression, bool) {
	if expr == nil {
		return nil, false
	}
	lit, ok := expr.(*Literal)
	if !ok {
		return nil, false
	}
	args, ok := lit.Val.([]Expression)
	return args, ok
}

// ParamsLiteral extracts a []string from a literal expression.
// This is used to extract lambda parameter names from the AST.
// Returns false if the expression is nil or not a params literal.
func ParamsLiteral(expr Expression) ([]string, bool) {
	if expr == nil {
		return nil, false
	}
	lit, ok := expr.(*Literal)
	if !ok {
		return nil, false
	}
	params, ok := lit.Val.([]string)
	return params, ok
}
