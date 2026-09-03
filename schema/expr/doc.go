// Package expr provides expression types for YAMMM schema invariants.
//
// This package contains the compile-time representation of expressions. It
// defines typed Expression nodes that can be stored in the schema and evaluated
// at runtime by the instance layer.
//
// # Expression Types
//
// The package provides these expression node types:
//
//   - [SExpr]: S-expression representing an operation with children
//   - [Literal]: Literal value (string, int64, float64, bool, *regexp.Regexp, nil, []Expression, []string)
//   - [Op]: Operation name (e.g., "+", "&&", "Any")
//   - [DatatypeLiteral]: Data type name for type checking expressions
//
// # Compilation vs Evaluation
//
// Compilation (turning parsed expression syntax into Expression trees) is
// handled by the internal parse package. Evaluation is handled separately by
// the instance layer, which provides the runtime context (property values,
// variables, etc.) needed to execute expressions.
//
// # Builtin Catalogue
//
// [BuiltinSpec] describes each pipeline builtin the language defines — its
// argument and parameter arity, whether it takes a body, and how it types its
// result and lambda parameters. [LookupBuiltin] and [Builtins] expose the
// catalogue. The evaluator enforces the arity fields and the schema layer's
// static checker follows types through pipelines with the rest, so the two
// read one table and cannot drift.
//
// # Helper Functions
//
// The package provides helper functions for inspecting expression nodes:
//
//   - [StringLiteral]: extract a string value from a Literal expression
//   - [IsNilLiteral]: check if an expression is a nil literal
//   - [IsRegexpLiteral]: check if an expression is a compiled regexp
//   - [ArgsLiteral]: extract argument list from a Literal expression
//   - [ParamsLiteral]: extract parameter names from a Literal expression
//
// # Thread Safety
//
// All expression types are immutable value types, safe for concurrent access.
//
// # Dependencies
//
//	schema/expr  ──imports──▶  (stdlib only)
//
// # Known Limitations
//
//   - Expression nodes do not carry source location (span) information.
//     Spans are captured in diagnostics during compilation but not attached
//     to the resulting AST nodes. Future IDE features may require extending
//     nodes to optionally carry spans.
package expr
