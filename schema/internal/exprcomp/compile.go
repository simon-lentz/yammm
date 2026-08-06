package exprcomp

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// InvalidNumberMessage is the diagnostic text for a numeric literal running
// straight into identifier characters ("0x10", "42abc", a broken exponent).
// The wording lives in the parser, which owns the token; this delegates so the
// message is identical wherever the malformed literal appears.
func InvalidNumberMessage(text string) string {
	return parse.InvalidNumberMessage(text)
}

// Compile compiles an ANTLR expression context into an Expression AST.
//
// Compilation errors are collected in the provided collector. The function
// returns nil if the context is nil or if there were fatal compilation errors.
//
// The spans parameter provides ANTLR-to-Span conversion for diagnostics.
func Compile(
	ctx grammar.IExprContext,
	collector *diag.Collector,
	sourceID location.SourceID,
	spans SpanFromContext,
) expr.Expression {
	if ctx == nil {
		return nil
	}

	visitor := NewVisitor(collector, sourceID, spans)
	return visitor.Visit(ctx)
}

// CompileString compiles an expression from a string, collecting errors in the
// provided collector. It wraps the expression in a synthetic schema, so its
// spans are offsets into that wrapper; it is intended for testing, not
// production use.
//
// Source that does not parse cleanly compiles to nil rather than to whatever
// prefix recovery salvaged. A literal that parsed as the right shape but would
// not convert is reported without nilling the expression.
func CompileString(
	exprSource string,
	collector *diag.Collector,
	sourceID location.SourceID,
) expr.Expression {
	schemaSource := `schema "expr" type T { ! "test" ` + exprSource + ` }`

	file, issues := parse.Parse([]byte(schemaSource), sourceID)
	collector.CollectAll(issues)
	for _, iss := range issues {
		if iss.Code().Category() == diag.CategorySyntax {
			return nil
		}
	}

	if len(file.Types) == 0 || len(file.Types[0].Invariants) == 0 {
		return nil
	}
	return file.Types[0].Invariants[0].Expr
}
