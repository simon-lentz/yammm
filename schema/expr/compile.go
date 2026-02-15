package expr

import (
	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
)

// Compile compiles an ANTLR expression context into an Expression AST.
//
// Compilation errors are collected in the provided collector. The function
// returns nil if the context is nil or if there were fatal compilation errors.
//
// The registry and converter are used for span derivation (converting ANTLR's
// rune-based positions to byte-based positions for diagnostics).
func Compile(
	ctx grammar.IExprContext,
	collector *diag.Collector,
	sourceID location.SourceID,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) Expression {
	if ctx == nil {
		return nil
	}

	visitor := NewVisitor(collector, sourceID, registry, converter)
	return visitor.Visit(ctx)
}

// CompileString compiles an expression from a string.
//
// This is a convenience function for testing and programmatic expression
// creation. It parses the expression string and returns the compiled AST.
//
// Note: This function uses a synthetic schema wrapper internally and creates
// its own source registry. It is intended for testing, not production use.
//
// Errors are collected in the provided collector.
func CompileString(
	exprSource string,
	collector *diag.Collector,
	sourceID location.SourceID,
) Expression {
	// Create a minimal schema wrapper to use the parser
	schemaSource := `schema "expr" type T { ! "test" ` + exprSource + ` }`

	// Create a local registry for this synthetic source so span building works.
	// Register cannot fail for a new registry with valid sourceID.
	localReg := source.NewRegistry()
	if err := localReg.Register(sourceID, []byte(schemaSource)); err != nil {
		return nil
	}

	input := antlr.NewInputStream(schemaSource)
	lexer := grammar.NewYammmGrammarLexer(input)
	stream := antlr.NewCommonTokenStream(lexer, antlr.TokenDefaultChannel)
	parser := grammar.NewYammmGrammarParser(stream)

	// Remove default error listeners to avoid console spam
	parser.RemoveErrorListeners()

	// Parse the schema to get to the invariant
	schema := parser.Schema()
	if schema == nil {
		return nil
	}

	// Navigate to the expression in the invariant
	types := schema.AllType_()
	if len(types) == 0 {
		return nil
	}

	typeCtx, ok := types[0].(*grammar.TypeContext)
	if !ok || typeCtx == nil {
		return nil
	}

	typeBody := typeCtx.Type_body()
	if typeBody == nil {
		return nil
	}

	invariants := typeBody.AllInvariant()
	if len(invariants) == 0 {
		return nil
	}

	exprCtx := invariants[0].Expr()
	if exprCtx == nil {
		return nil
	}

	return Compile(exprCtx, collector, sourceID, localReg, localReg)
}
