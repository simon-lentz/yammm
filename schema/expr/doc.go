// Package expr provides expression compilation for YAMMM schema invariants.
//
// This package contains the compile-time representation of expressions. It
// transforms ANTLR parse trees into typed Expression trees that can be stored
// in the schema and evaluated at runtime by the instance layer.
//
// # Compilation vs Evaluation
//
// The expr package is responsible for compilation only:
//   - Parsing expression syntax from invariant declarations
//   - Building typed AST nodes (SExpr, Literal, Op, DatatypeLiteral)
//   - Collecting syntax errors during parsing
//
// Note: Function name validation is deferred to the eval layer (instance/eval).
// Unknown functions compile successfully into the AST; validation happens at
// evaluation time. This design allows schemas to be compiled without knowing
// all builtins, supporting runtime extension and custom builtin registration.
//
// Evaluation is handled separately by the instance layer, which provides
// the runtime context (property values, variables, etc.) needed to execute
// expressions.
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
// # Usage
//
// Expressions are compiled from ANTLR parse trees:
//
//	ctx := parser.Expr() // ANTLR ExprContext
//	collector := diag.NewCollector(0)
//	expr := expr.Compile(ctx, collector, sourceID, registry, converter)
//	if collector.HasErrors() {
//	    // Handle compilation errors
//	}
//
// The resulting Expression can be stored in an InvariantDecl and evaluated
// later when validating instances.
//
// # Known Limitations
//
//   - Expression nodes do not carry source location (span) information.
//     Spans are captured in diagnostics during compilation but not attached
//     to the resulting AST nodes. Future IDE features may require extending
//     nodes to optionally carry spans.
//
//   - [CompileString] uses a synthetic schema wrapper internally and creates
//     its own source registry. It is intended for testing, not production use.
package expr
