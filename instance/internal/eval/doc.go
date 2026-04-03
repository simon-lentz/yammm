// Package eval provides expression evaluation for instance validation.
//
// This package evaluates compiled expressions from [schema/expr] against
// instance property values. It is used internally by the instance validation
// layer to evaluate invariants and constraints.
//
// # Scope
//
// The [Scope] interface provides variable bindings for expression evaluation.
// Properties are accessed via the scope's Lookup method, and variables can
// be bound using WithVar.
//
//	scope := eval.PropertyScopeFromMap(map[string]any{
//	    "name": "Alice",
//	    "age":  30,
//	})
//	result, err := evaluator.Evaluate(expr, scope)
//
// # Evaluator
//
// The [Evaluator] type evaluates compiled [expr.Expression] nodes. It is
// stateless and safe for concurrent use.
//
//	evaluator := eval.NewEvaluator()
//	result, err := evaluator.Evaluate(expr, scope)
//	boolResult, err := evaluator.EvaluateBool(expr, scope)
//
// # Type Checking
//
// The package provides type checkers for all constraint kinds defined in
// [schema]. These are used internally to validate property values.
//
// # Built-in Functions
//
// The evaluator supports all DSL built-in functions:
//
//   - Collection: map, filter, count, all, any, all_or_none, reduce, compact, unique
//   - Numeric: len, abs, floor, ceil, round, min, max, compare
//   - Control flow: then, lest, with
//   - Pattern matching: match
//
// # Configuration
//
// The evaluator accepts minimal configuration via [NewEvaluator] options.
// Currently only [WithLogger] is available for debug observability. The
// evaluator's behavior is primarily determined by the schema's expression
// definitions rather than runtime configuration. Future options may include
// custom function registration or error recovery strategies.
//
// # Thread Safety
//
// [Evaluator], [Scope], and all related types are immutable and safe for
// concurrent use. The evaluator does not retain any mutable state between
// evaluations.
package eval
