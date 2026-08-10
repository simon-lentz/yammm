package exprcomp

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

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
