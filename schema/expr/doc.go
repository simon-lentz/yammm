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
//   - [Literal]: Literal value (string, int64, float64, bool, *regexp.Regexp, nil)
//   - [Op]: Operation name (e.g., "+", "&&", "Any")
//   - [DatatypeLiteral]: Data type name for type checking expressions
//
// # Compilation vs Evaluation
//
// Compilation (transforming ANTLR parse trees into Expression trees) is handled
// by the internal exprcomp package. Evaluation is handled separately by the
// instance layer, which provides the runtime context (property values,
// variables, etc.) needed to execute expressions.
//
// # Known Limitations
//
//   - Expression nodes do not carry source location (span) information.
//     Spans are captured in diagnostics during compilation but not attached
//     to the resulting AST nodes. Future IDE features may require extending
//     nodes to optionally carry spans.
package expr
