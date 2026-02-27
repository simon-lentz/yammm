package eval

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/internal/trace"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Evaluator evaluates compiled expressions.
//
// Evaluator is stateless and safe for concurrent use. All evaluation state
// is contained in the Scope passed to each evaluation.
type Evaluator struct {
	cfg *evalConfig
}

// NewEvaluator creates a new expression evaluator.
func NewEvaluator(opts ...EvalOption) *Evaluator {
	return &Evaluator{
		cfg: applyOptions(opts),
	}
}

// Evaluate evaluates an expression with the given scope.
// Returns the result value, or an error if evaluation fails.
func (e *Evaluator) Evaluate(expression expr.Expression, scope Scope) (any, error) {
	if expression == nil {
		return nil, nil //nolint:nilnil // nil expression evaluates to nil
	}

	// Operation boundary logging (evaluator doesn't take context)
	op := trace.Begin(context.Background(), e.cfg.logger, "yammm.eval.expr")
	defer func() { op.End(nil) }()

	return e.evaluate(expression, scope)
}

// EvaluateBool evaluates an expression and returns it as a boolean.
// Returns an error if the result is not a boolean.
func (e *Evaluator) EvaluateBool(expression expr.Expression, scope Scope) (bool, error) {
	result, err := e.Evaluate(expression, scope)
	if err != nil {
		return false, err
	}
	if result == nil {
		return false, nil // nil is "falsey"
	}
	b, ok := result.(bool)
	if !ok {
		return false, fmt.Errorf("expected boolean, got %T", result)
	}
	return b, nil
}

// evaluate is the internal evaluation dispatcher.
func (e *Evaluator) evaluate(expression expr.Expression, scope Scope) (any, error) {
	switch ex := expression.(type) {
	case *expr.Literal:
		return ex.Val, nil
	case expr.Op:
		// Op by itself is just its string value
		return string(ex), nil
	case expr.DatatypeLiteral:
		// Return a type checker for the datatype
		return e.datatypeChecker(string(ex))
	case expr.SExpr:
		return e.evalSExpr(ex, scope)
	default:
		return nil, fmt.Errorf("unknown expression type: %T", expression)
	}
}

// evalSExpr evaluates an S-expression.
func (e *Evaluator) evalSExpr(sexpr expr.SExpr, scope Scope) (any, error) {
	op := sexpr.Op()
	children := sexpr.Children()

	trace.Debug(context.Background(), e.cfg.logger, "evaluating s-expression",
		slog.String("op", op),
	)

	// Special forms that don't evaluate all children upfront
	switch op {
	case "&&":
		return e.evalAnd(children, scope)
	case "||":
		return e.evalOr(children, scope)
	case "?":
		return e.evalTernary(children, scope)
	case "$":
		return e.evalVar(children, scope)
	case "p":
		return e.evalProperty(children, scope)
	case ".":
		return e.evalMember(children, scope)
	case "@":
		return e.evalSlice(children, scope)
	case "[]":
		return e.evalList(children, scope)
	}

	// Check if it's a builtin
	if def, ok := lookupBuiltin(strings.ToLower(op)); ok {
		return e.evalBuiltin(def, children, scope)
	}

	// Evaluate children for operators that need all args evaluated
	args := make([]any, len(children))
	for i, child := range children {
		val, err := e.evaluate(child, scope)
		if err != nil {
			return nil, err
		}
		args[i] = val
	}

	// Dispatch to operator
	switch op {
	// Arithmetic
	case "+":
		return e.add(args)
	case "-":
		return e.sub(args)
	case "*":
		return e.mul(args)
	case "/":
		return e.div(args)
	case "%":
		return e.mod(args)
	case "-x":
		return e.negate(args)

	// Comparison
	case "==":
		return e.equal(args)
	case "!=":
		return e.notEqual(args)
	case "<":
		return e.lessThan(args)
	case "<=":
		return e.lessOrEqual(args)
	case ">":
		return e.greaterThan(args)
	case ">=":
		return e.greaterOrEqual(args)

	// Pattern matching
	case "=~":
		return e.match(args)
	case "!~":
		return e.notMatch(args)
	case "in":
		return e.inOp(args)

	// Logical
	case "!":
		return e.not(args)
	case "^":
		return e.xor(args)

	default:
		return nil, fmt.Errorf("unknown operation: %s", op)
	}
}

// --- Special forms ---

func (e *Evaluator) evalAnd(children []expr.Expression, scope Scope) (any, error) {
	for _, child := range children {
		val, err := e.evaluate(child, scope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("expected boolean in &&, got %T", val)
		}
		if !b {
			return false, nil // short-circuit
		}
	}
	return true, nil
}

func (e *Evaluator) evalOr(children []expr.Expression, scope Scope) (any, error) {
	for _, child := range children {
		val, err := e.evaluate(child, scope)
		if err != nil {
			return nil, err
		}
		b, ok := val.(bool)
		if !ok {
			return nil, fmt.Errorf("expected boolean in ||, got %T", val)
		}
		if b {
			return true, nil // short-circuit
		}
	}
	return false, nil
}

func (e *Evaluator) evalTernary(children []expr.Expression, scope Scope) (any, error) {
	if len(children) != 3 {
		return nil, errors.New("ternary operator requires 3 operands")
	}

	cond, err := e.evaluate(children[0], scope)
	if err != nil {
		return nil, err
	}

	b, ok := cond.(bool)
	if !ok {
		return nil, fmt.Errorf("expected boolean condition, got %T", cond)
	}

	if b {
		return e.evaluate(children[1], scope)
	}
	return e.evaluate(children[2], scope)
}

func (e *Evaluator) evalVar(children []expr.Expression, scope Scope) (any, error) {
	if len(children) != 1 {
		return nil, errors.New("variable lookup requires 1 operand")
	}

	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return nil, errors.New("variable name must be a string literal")
	}

	// Numeric variables ($0, $1, ...) return nil if not found
	if isNumericVar(name) {
		val, found := scope.Lookup(name)
		if !found {
			return nil, nil //nolint:nilnil // unset numeric vars are nil
		}
		return val.Unwrap(), nil
	}

	// Named variables must be defined
	val, found := scope.Lookup(name)
	if !found {
		return nil, fmt.Errorf("undefined variable: $%s", name)
	}
	return val.Unwrap(), nil
}

// evalProperty performs case-insensitive property lookup within the current scope.
//
// Safety: Schema validation prevents types from having multiple properties
// that fold to the same lowercase name (e.g., "Name" and "name" cannot coexist),
// so LookupFold always returns a deterministic result.
//
// Missing optional properties evaluate to nil, enabling patterns like:
//
//	age lest 0        (default value)
//	age then age > 18 (conditional validation)
func (e *Evaluator) evalProperty(children []expr.Expression, scope Scope) (any, error) {
	if len(children) != 1 {
		return nil, errors.New("property lookup requires 1 operand")
	}

	name, ok := expr.StringLiteral(children[0])
	if !ok {
		return nil, errors.New("property name must be a string literal")
	}

	val, found := scope.LookupFold(name)
	if !found {
		return nil, nil //nolint:nilnil // missing property → nil enables lest/then patterns
	}
	return val.Unwrap(), nil
}

func (e *Evaluator) evalMember(children []expr.Expression, scope Scope) (any, error) {
	if len(children) < 2 {
		return nil, errors.New("member access requires at least 2 operands")
	}

	// Evaluate the receiver
	obj, err := e.evaluate(children[0], scope)
	if err != nil {
		return nil, err
	}

	// Get the member name
	memberName, ok := expr.StringLiteral(children[1])
	if !ok {
		return nil, errors.New("member name must be a string literal")
	}

	// If there are more children (args, params, body), this is a method call
	if len(children) > 2 {
		return e.evalMethodCall(obj, memberName, children[2:], scope)
	}

	// Check if it's a builtin method call (no extra args/body needed)
	if def, found := lookupBuiltin(strings.ToLower(memberName)); found {
		// Use unified validation - validates that no args/params/body are required
		return e.callBuiltin(def, obj, nil, nil, nil, scope)
	}

	// Simple member access on a map
	return e.accessMember(obj, memberName)
}

func (e *Evaluator) evalMethodCall(receiver any, methodName string, rest []expr.Expression, scope Scope) (any, error) {
	// Look up the method as a builtin
	def, ok := lookupBuiltin(strings.ToLower(methodName))
	if !ok {
		return nil, fmt.Errorf("unknown method: %s", methodName)
	}

	// Parse args, params, and body from rest
	var args []any
	var params []string
	var body expr.Expression

	for _, child := range rest {
		// Check if it's an args literal
		if argList, ok := expr.ArgsLiteral(child); ok {
			for _, arg := range argList {
				val, err := e.evaluate(arg, scope)
				if err != nil {
					return nil, err
				}
				args = append(args, val)
			}
			continue
		}
		// Check if it's a params literal
		if paramList, ok := expr.ParamsLiteral(child); ok {
			params = paramList
			continue
		}
		// Otherwise it's the body expression.
		// VisitFcall normalizes missing bodies to NewLiteral(nil) (a non-nil
		// *Literal wrapping nil). Treat that as "no body" so callBuiltin's
		// acceptBody validation works correctly for source-compiled ASTs.
		if !expr.IsNilLiteral(child) {
			body = child
		}
	}

	// Use unified validation and dispatch
	return e.callBuiltin(def, receiver, args, params, body, scope)
}

func (e *Evaluator) accessMember(obj any, name string) (any, error) {
	if obj == nil {
		return nil, nil //nolint:nilnil // member access on nil returns nil
	}

	// Try as map[string]any
	if m, ok := obj.(map[string]any); ok {
		val, exists := m[name]
		if !exists {
			// Case-insensitive fallback: pick alphabetically first key on collision
			// for deterministic behavior (matches immutable.Properties.GetFold behavior)
			lower := strings.ToLower(name)
			var matchKey string
			var matchVal any
			for k, v := range m {
				if strings.ToLower(k) == lower {
					if matchKey == "" || k < matchKey {
						matchKey = k
						matchVal = v
					}
				}
			}
			if matchKey != "" {
				return matchVal, nil
			}
			return nil, nil //nolint:nilnil // missing key returns nil
		}
		return val, nil
	}

	// Try as immutable.Map[string] (e.g. $self bound via WithSelf)
	if m, ok := obj.(immutable.Map[string]); ok {
		val, exists := m.Get(name)
		if !exists {
			// Case-insensitive fallback: pick alphabetically first key on collision
			// for deterministic behavior (matches immutable.Properties.GetFold behavior)
			lower := strings.ToLower(name)
			var matchKey string
			var matchVal immutable.Value
			for k, v := range m.Range() {
				if strings.ToLower(k) == lower {
					if matchKey == "" || k < matchKey {
						matchKey = k
						matchVal = v
					}
				}
			}
			if matchKey != "" {
				return matchVal.Unwrap(), nil
			}
			return nil, nil //nolint:nilnil // missing key returns nil
		}
		return val.Unwrap(), nil
	}

	return nil, fmt.Errorf("cannot access member on %T", obj)
}

func (e *Evaluator) evalSlice(children []expr.Expression, scope Scope) (any, error) {
	if len(children) < 2 {
		return nil, errors.New("slice access requires at least 2 operands")
	}

	// Evaluate the receiver
	obj, err := e.evaluate(children[0], scope)
	if err != nil {
		return nil, err
	}

	// Evaluate the index
	idx, err := e.evaluate(children[1], scope)
	if err != nil {
		return nil, err
	}

	return e.accessIndex(obj, idx)
}

func (e *Evaluator) accessIndex(obj, idx any) (any, error) {
	if obj == nil {
		return nil, nil //nolint:nilnil // indexing nil returns nil
	}

	// Get index as int64
	i, ok := value.GetInt64(idx)
	if !ok {
		return nil, fmt.Errorf("slice index must be integer, got %T", idx)
	}

	// Handle []any
	if slice, ok := obj.([]any); ok {
		if i < 0 || i >= int64(len(slice)) {
			return nil, nil //nolint:nilnil // out of bounds returns nil
		}
		return slice[int(i)], nil
	}

	// Handle string - index by rune, not byte (per SPEC)
	// SPEC line 687 states string length is "counted in runes, not bytes"
	if s, ok := obj.(string); ok {
		runes := []rune(s)
		if i < 0 || i >= int64(len(runes)) {
			return nil, nil //nolint:nilnil // out of bounds returns nil
		}
		return string(runes[int(i)]), nil
	}

	return nil, fmt.Errorf("cannot index %T", obj)
}

func (e *Evaluator) evalList(children []expr.Expression, scope Scope) (any, error) {
	result := make([]any, len(children))
	for i, child := range children {
		val, err := e.evaluate(child, scope)
		if err != nil {
			return nil, err
		}
		result[i] = val
	}
	return result, nil
}

// --- Builtins ---

// callBuiltin validates builtin constraints and invokes the function.
// This is the single internal helper that enforces minArgs, maxArgs, maxParams,
// and acceptBody requirements for ALL builtin call paths (function-style,
// method-style with args/body, and method-style without args).
func (e *Evaluator) callBuiltin(def builtinDef, lhs any, args []any, params []string, body expr.Expression, scope Scope) (any, error) {
	// Validate args count
	if len(args) < def.minArgs {
		return nil, fmt.Errorf("%s requires at least %d arguments", def.name, def.minArgs)
	}
	// maxArgs == -1 means unlimited arguments
	if def.maxArgs >= 0 && len(args) > def.maxArgs {
		return nil, fmt.Errorf("%s accepts at most %d arguments", def.name, def.maxArgs)
	}

	// Validate params count
	if len(params) > def.maxParams {
		return nil, fmt.Errorf("%s accepts at most %d parameters", def.name, def.maxParams)
	}

	// Validate body presence/absence based on acceptBody flag
	if body != nil {
		if !def.acceptBody {
			return nil, fmt.Errorf("%s does not accept a lambda expression", def.name)
		}
	} else if def.acceptBody {
		return nil, fmt.Errorf("%s requires a lambda expression", def.name)
	}

	return def.fn(e, lhs, args, params, body, scope)
}

func (e *Evaluator) evalBuiltin(def builtinDef, children []expr.Expression, scope Scope) (any, error) {
	// First child (if present) is the receiver (lhs)
	var lhs any
	var err error
	childStart := 0

	if len(children) > 0 {
		lhs, err = e.evaluate(children[0], scope)
		if err != nil {
			return nil, err
		}
		childStart = 1
	}

	// Parse remaining children for args, params, body
	var args []any
	var params []string
	var body expr.Expression

	for i := childStart; i < len(children); i++ {
		child := children[i]

		// Check if it's an args literal
		if argList, ok := expr.ArgsLiteral(child); ok {
			for _, arg := range argList {
				val, err := e.evaluate(arg, scope)
				if err != nil {
					return nil, err
				}
				args = append(args, val)
			}
			continue
		}

		// Check if it's a params literal
		if paramList, ok := expr.ParamsLiteral(child); ok {
			params = paramList
			continue
		}

		// Otherwise it's the body expression (don't evaluate yet).
		// VisitFcall normalizes missing bodies to NewLiteral(nil) (a non-nil
		// *Literal wrapping nil). Treat that as "no body" so callBuiltin's
		// acceptBody validation works correctly for source-compiled ASTs.
		if !expr.IsNilLiteral(child) {
			body = child
		}
	}

	// Use unified validation and dispatch
	return e.callBuiltin(def, lhs, args, params, body, scope)
}

// --- Arithmetic operators ---

func (e *Evaluator) add(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("+ requires 2 operands")
	}

	left, right := args[0], args[1]

	// Try numeric addition
	if result, ok := e.numericOp(left, right, func(a, b int64) any { return a + b }, func(a, b float64) any { return a + b }); ok {
		return result, nil
	}

	// Try string concatenation
	if ls, lok := left.(string); lok {
		if rs, rok := right.(string); rok {
			return ls + rs, nil
		}
	}

	// Try slice concatenation
	if lSlice, err := asSlice("+", left); err == nil {
		if rSlice, err := asSlice("+", right); err == nil {
			result := make([]any, 0, len(lSlice)+len(rSlice))
			result = append(result, lSlice...)
			result = append(result, rSlice...)
			return result, nil
		}
	}

	return nil, errors.New("+ of non-numeric values")
}

func (e *Evaluator) sub(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("- requires 2 operands")
	}

	result, ok := e.numericOp(args[0], args[1], func(a, b int64) any { return a - b }, func(a, b float64) any { return a - b })
	if !ok {
		return nil, errors.New("- of non-numeric values")
	}
	return result, nil
}

func (e *Evaluator) mul(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("* requires 2 operands")
	}

	result, ok := e.numericOp(args[0], args[1], func(a, b int64) any { return a * b }, func(a, b float64) any { return a * b })
	if !ok {
		return nil, errors.New("* of non-numeric values")
	}
	return result, nil
}

func (e *Evaluator) div(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("/ requires 2 operands")
	}

	// Check for integer division by zero first (panics without this check)
	li, liok := value.GetInt64(args[0])
	ri, riok := value.GetInt64(args[1])
	if liok && riok {
		if ri == 0 {
			return nil, errors.New("division by zero")
		}
		return li / ri, nil
	}

	// Float division (returns ±Inf for /0, which is valid IEEE 754)
	result, ok := e.numericOp(args[0], args[1],
		func(a, b int64) any { return a / b }, // Won't reach here - integer case handled above
		func(a, b float64) any { return a / b })
	if !ok {
		return nil, errors.New("/ of non-numeric values")
	}
	return result, nil
}

func (e *Evaluator) mod(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("% requires 2 operands")
	}

	// Modulo only works on integers
	left, lok := value.GetInt64(args[0])
	right, rok := value.GetInt64(args[1])
	if !lok || !rok {
		return nil, errors.New("% requires integer operands")
	}

	if right == 0 {
		return nil, errors.New("modulo by zero")
	}

	return left % right, nil
}

func (e *Evaluator) negate(args []any) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("-x requires 1 operand")
	}

	if i, ok := value.GetInt64(args[0]); ok {
		return -i, nil
	}
	if f, ok := value.GetFloat64(args[0]); ok {
		return -f, nil
	}
	return nil, errors.New("-x of non-numeric value")
}

// numericOp applies integer or float operation based on operand types.
func (e *Evaluator) numericOp(left, right any, intOp func(int64, int64) any, floatOp func(float64, float64) any) (any, bool) {
	li, liok := value.GetInt64(left)
	ri, riok := value.GetInt64(right)
	if liok && riok {
		return intOp(li, ri), true
	}

	lf, lfok := value.GetFloat64(left)
	rf, rfok := value.GetFloat64(right)

	// Promote integers to floats if needed
	if liok && rfok {
		return floatOp(float64(li), rf), true
	}
	if lfok && riok {
		return floatOp(lf, float64(ri)), true
	}
	if lfok && rfok {
		return floatOp(lf, rf), true
	}

	return nil, false
}

// --- Comparison operators ---

func (e *Evaluator) equal(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("== requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf("== comparison error: %w", err)
	}
	return cmp == 0, nil
}

func (e *Evaluator) notEqual(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("!= requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf("!= comparison error: %w", err)
	}
	return cmp != 0, nil
}

func (e *Evaluator) lessThan(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("< requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf("< comparison error: %w", err)
	}
	return cmp < 0, nil
}

func (e *Evaluator) lessOrEqual(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("<= requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf("<= comparison error: %w", err)
	}
	return cmp <= 0, nil
}

func (e *Evaluator) greaterThan(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("> requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf("> comparison error: %w", err)
	}
	return cmp > 0, nil
}

func (e *Evaluator) greaterOrEqual(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New(">= requires 2 operands")
	}
	cmp, err := value.ValueOrder(args[0], args[1])
	if err != nil {
		return nil, fmt.Errorf(">= comparison error: %w", err)
	}
	return cmp >= 0, nil
}

// --- Pattern matching ---

func (e *Evaluator) match(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("=~ requires 2 operands")
	}

	left, right := args[0], args[1]

	switch matcher := right.(type) {
	case *regexp.Regexp:
		s, ok := left.(string)
		if !ok {
			return nil, fmt.Errorf("=~ left operand must be string, got %T", left)
		}
		return matcher.MatchString(s), nil

	case TypeChecker:
		ok, _ := matcher(left)
		return ok, nil

	default:
		return nil, fmt.Errorf("=~ right operand must be regexp or type checker, got %T", right)
	}
}

func (e *Evaluator) notMatch(args []any) (any, error) {
	result, err := e.match(args)
	if err != nil {
		return nil, err
	}
	b, ok := result.(bool)
	if !ok {
		return nil, errors.New("!~ internal error: match did not return bool")
	}
	return !b, nil
}

func (e *Evaluator) inOp(args []any) (any, error) {
	if len(args) != 2 {
		return nil, errors.New("in requires 2 operands")
	}

	left, right := args[0], args[1]

	slice, err := asSlice("in", right)
	if err != nil {
		return nil, err
	}

	for _, elem := range slice {
		cmp, err := value.ValueOrder(left, elem)
		if err != nil {
			continue // incomparable types are not equal
		}
		if cmp == 0 {
			return true, nil
		}
	}
	return false, nil
}

// --- Logical operators ---

func (e *Evaluator) not(args []any) (any, error) {
	if len(args) != 1 {
		return nil, errors.New("! requires 1 operand")
	}
	b, ok := args[0].(bool)
	if !ok {
		return nil, fmt.Errorf("! expects boolean, got %T", args[0])
	}
	return !b, nil
}

func (e *Evaluator) xor(args []any) (any, error) {
	if len(args) != 2 {
		return nil, fmt.Errorf("^ requires 2 operands, got %d", len(args))
	}
	left, ok := args[0].(bool)
	if !ok {
		return nil, fmt.Errorf("^ expects boolean operands, got %T", args[0])
	}
	right, ok := args[1].(bool)
	if !ok {
		return nil, fmt.Errorf("^ expects boolean operands, got %T", args[1])
	}
	return left != right, nil
}

// --- Helper functions ---

// isNumericVar checks if a variable name is numeric ($0, $1, etc.)
func isNumericVar(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// datatypeChecker returns a TypeChecker for the given datatype name.
func (e *Evaluator) datatypeChecker(name string) (TypeChecker, error) {
	switch strings.ToLower(name) {
	case "string":
		return IsString(), nil
	case "integer", "int":
		return IsInteger(), nil
	case "float", "number":
		return IsFloat(), nil
	case "boolean", "bool":
		return IsBoolean(), nil
	case "uuid":
		return IsUUID(), nil
	case "timestamp":
		return IsTimestamp(), nil
	case "date":
		return IsDate(), nil
	default:
		return nil, fmt.Errorf("unknown datatype: %s", name)
	}
}
