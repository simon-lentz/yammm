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
// [BuiltinSpec] describes each pipeline builtin the language defines: its
// receiver and argument kinds, its argument and parameter arity, whether it takes
// a body, and how it types its result and lambda parameters. [LookupBuiltin]
// exposes the catalogue. The evaluator enforces the arity and body fields from
// this table and panics at start on a builtin the table lacks. The schema layer's
// static checker follows types through pipelines with the other fields. Each
// builtin's implementation checks its own kinds and computes its own result, so
// nothing ties those fields to the evaluator.
//
// # Helper Functions
//
// The package provides helper functions for inspecting expression nodes:
//
//   - [StringLiteral]: extract a string value from a Literal expression
//   - [ArgsLiteral]: extract argument list from a Literal expression
//   - [ParamsLiteral]: extract parameter names from a Literal expression
//
// # Thread Safety
//
// An expression tree is safe for concurrent reads while no caller writes to it.
// A [Literal]'s Val and an [SExpr]'s elements are writable, and [Literal.Literal]
// returns Val uncopied, so a holder must not write through them.
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
