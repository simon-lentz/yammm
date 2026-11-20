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

// PatternRegexp returns a schema.PatternConstraint's compiled pattern, or nil for
// a constraint that holds none. The schema package sets it once, in its init, and
// nothing writes it after, so a read takes no lock. It takes any because this
// package sits below schema, and it panics on any other argument.
var PatternRegexp func(constraint any) *regexp.Regexp

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

// Op returns the operation name, the first element, or "" when the first
// element is no Op or there is none.
func (e SExpr) Op() string {
	if len(e) == 0 {
		return ""
	}
	if op, ok := e[0].(Op); ok {
		return string(op)
	}
	return ""
}

// Children returns the operands, every element after the operation name.
//
// Returns a defensive copy of the child expressions to preserve immutability.
// Callers may safely modify the returned slice without affecting the original.
func (e SExpr) Children() []Expression {
	if len(e) <= 1 {
		return nil
	}
	return slices.Clone(e[1:])
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

// expression implements Expression.
func (Op) expression() {}

// StringLiteral extracts the string a [*Literal] wraps. Any other node, an
// [SExpr] or an [Op] among them, returns false.
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
