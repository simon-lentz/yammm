// Package eval provides expression evaluation for instance validation.
//
// This package evaluates compiled expressions from [github.com/simon-lentz/yammm/schema/expr] against
// instance property values. It is used internally by the instance validation
// layer to evaluate invariants and constraints.
//
// # Scope
//
// The [Scope] interface provides variable bindings for expression evaluation.
// A bare property name reads through the scope's LookupFold method, a $ variable
// reads through its Lookup method, and WithVar binds a variable.
//
//	scope := eval.PropertyScopeOf(immutable.WrapMap(map[string]any{
//	    "name": "Alice",
//	    "age":  30,
//	}))
//	result, err := evaluator.Evaluate(ctx, expr, scope)
//
// # Evaluator
//
// The [Evaluator] type evaluates compiled [expr.Expression] nodes. It is
// stateless and safe for concurrent use.
//
//	evaluator := eval.NewEvaluator()
//	result, err := evaluator.Evaluate(ctx, expr, scope)
//	boolResult, err := evaluator.EvaluateBool(ctx, expr, scope)
//
// # Type Checking
//
// The package provides type checkers for all constraint kinds defined in
// [schema]. These are used internally to validate property values.
//
// # Built-in Functions
//
// The evaluator implements every builtin the catalogue in
// [github.com/simon-lentz/yammm/schema/expr] defines, under the names the
// DSL resolves case-insensitively:
//
//   - Collection: Map, Filter, Count, All, Any, AllOrNone, Reduce, Compact, Unique, Len, Sum, First, Last, Sort, Reverse, Flatten, Contains
//   - Numeric: Abs, Floor, Ceil, Round, Min, Max, Compare
//   - String: Upper, Lower, Trim, TrimPrefix, TrimSuffix, Split, Join, StartsWith, EndsWith, Replace, Substring
//   - Control flow: Then, Lest, With
//   - Pattern matching: Match
//   - Utility: TypeOf, IsNil, Default, Coalesce
//
// # Thread Safety
//
// [Evaluator], [Scope], and all related types are immutable and safe for
// concurrent use. The evaluator does not retain any mutable state between
// evaluations.
package eval
