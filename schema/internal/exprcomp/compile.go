package exprcomp

import (
	"fmt"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// InvalidNumberMessage is the diagnostic text for the lexer's
// INVALID_NUMBER token — a numeric literal running straight into
// identifier characters ("0x10", "42abc", a broken exponent). Shared by
// every parser error listener so the message is identical wherever the
// malformed literal appears: expressions, constraint bounds, or any other
// numeric position.
func InvalidNumberMessage(text string) string {
	return fmt.Sprintf("malformed numeric literal %q: numeric literals are decimal (hexadecimal and suffixed forms are not supported)", text)
}

// syntaxErrorListener collects ANTLR lexer and parser errors into a diag
// collector, so CompileString fails on input the parser could not consume
// cleanly instead of compiling whatever expression error recovery
// salvaged.
type syntaxErrorListener struct {
	*antlr.DefaultErrorListener
	collector *diag.Collector
	sourceID  location.SourceID
	sawError  bool
}

func (l *syntaxErrorListener) SyntaxError(
	_ antlr.Recognizer,
	offendingSymbol any,
	line, column int,
	msg string,
	_ antlr.RecognitionException,
) {
	l.sawError = true
	if token, ok := offendingSymbol.(antlr.Token); ok && token != nil &&
		token.GetTokenType() == grammar.YammmGrammarLexerINVALID_NUMBER {
		msg = InvalidNumberMessage(token.GetText())
	}
	pos := location.Position{Line: line, Column: column + 1, Byte: -1}
	l.collector.Collect(diag.NewIssue(diag.Error, diag.E_SYNTAX, msg).
		WithSpan(location.Span{Source: l.sourceID, Start: pos, End: pos}).Build())
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
) expr.Expression {
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

	// Surface every lexer and parser error into the collector instead of
	// the default console listeners: source that does not parse cleanly
	// must fail compilation rather than compile whatever expression error
	// recovery salvaged.
	el := &syntaxErrorListener{collector: collector, sourceID: sourceID}
	lexer.RemoveErrorListeners()
	lexer.AddErrorListener(el)
	parser.RemoveErrorListeners()
	parser.AddErrorListener(el)

	// Parse the schema to get to the invariant
	schema := parser.Schema()
	if schema == nil || el.sawError {
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

	spans := &localSpanBuilder{sourceID: sourceID, registry: localReg, converter: localReg}
	return Compile(exprCtx, collector, sourceID, spans)
}

// localSpanBuilder is a minimal SpanFromContext for CompileString.
// It duplicates the rune-to-byte conversion logic from schema.spanBuilder
// to avoid importing the parent schema package.
type localSpanBuilder struct {
	sourceID  location.SourceID
	registry  location.PositionRegistry
	converter location.RuneOffsetConverter
}

func (b *localSpanBuilder) FromContext(ctx antlr.ParserRuleContext) location.Span {
	if ctx == nil {
		return location.Span{}
	}
	start := ctx.GetStart()
	stop := ctx.GetStop()
	if start == nil {
		return location.Span{}
	}
	startRune := start.GetStart()
	var endRune int
	if stop != nil {
		endRune = stop.GetStop() + 1
	} else {
		endRune = start.GetStop() + 1
	}
	startByte, _ := b.converter.RuneToByteOffset(b.sourceID, startRune)
	endByte, _ := b.converter.RuneToByteOffset(b.sourceID, endRune)
	startPos := b.registry.PositionAt(b.sourceID, startByte)
	endPos := b.registry.PositionAt(b.sourceID, endByte)
	return location.Span{Source: b.sourceID, Start: startPos, End: endPos}
}
