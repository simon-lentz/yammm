// Package exprcomp provides expression compilation from ANTLR parse trees.
//
// This package transforms ANTLR expression contexts into typed [expr.Expression]
// trees that can be stored in the schema and evaluated at runtime by the
// instance layer. It also provides the [BuiltinRegistry] for tracking known
// builtin function names.
package exprcomp
