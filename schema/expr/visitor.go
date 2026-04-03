package expr

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/antlr4-go/antlr/v4"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/internal/span"
)

// Visitor compiles ANTLR expression parse trees into Expression ASTs.
//
// Note: Function name validation is deferred to the eval layer (instance/eval).
// The visitor compiles all function calls into the AST regardless of whether
// the function name is a known builtin. This allows schemas to be compiled
// without knowing all builtins, supporting runtime extension.
type Visitor struct {
	grammar.BaseYammmGrammarVisitor
	collector *diag.Collector
	sourceID  location.SourceID
	spans     *span.Builder
	hasErrs   bool
}

// NewVisitor creates a new expression visitor.
func NewVisitor(
	collector *diag.Collector,
	sourceID location.SourceID,
	registry location.PositionRegistry,
	converter location.RuneOffsetConverter,
) *Visitor {
	return &Visitor{
		collector: collector,
		sourceID:  sourceID,
		spans:     span.NewBuilder(sourceID, registry, converter),
	}
}

// HasErrors reports whether any errors occurred during compilation.
func (v *Visitor) HasErrors() bool {
	return v.hasErrs
}

// Visit dispatches to the appropriate visit method based on node type.
func (v *Visitor) Visit(tree antlr.ParseTree) Expression {
	if tree == nil {
		return nil
	}
	switch ctx := tree.(type) {
	case *grammar.LiteralContext:
		return v.VisitLiteral(ctx)
	case *grammar.ValueContext:
		return v.VisitValue(ctx)
	case *grammar.VariableContext:
		return v.VisitVariable(ctx)
	case *grammar.MuldivContext:
		return v.VisitMuldiv(ctx)
	case *grammar.PlusminusContext:
		return v.VisitPlusminus(ctx)
	case *grammar.CompareContext:
		return v.VisitCompare(ctx)
	case *grammar.EqualityContext:
		return v.VisitEquality(ctx)
	case *grammar.MatchContext:
		return v.VisitMatch(ctx)
	case *grammar.InContext:
		return v.VisitIn(ctx)
	case *grammar.IfContext:
		return v.VisitIf(ctx)
	case *grammar.GroupContext:
		return v.VisitGroup(ctx)
	case *grammar.AndContext:
		return v.VisitAnd(ctx)
	case *grammar.OrContext:
		return v.VisitOr(ctx)
	case *grammar.NotContext:
		return v.VisitNot(ctx)
	case *grammar.AtContext:
		return v.VisitAt(ctx)
	case *grammar.ListContext:
		return v.VisitList(ctx)
	case *grammar.DatatypeNameContext:
		return v.VisitDatatypeName(ctx)
	case *grammar.DatatypeKeywordContext:
		v.errorf(ctx, "invalid datatype keyword")
		return NewLiteral(nil)
	case *grammar.ArgumentsContext:
		return v.VisitArguments(ctx)
	case *grammar.ParametersContext:
		return v.VisitParameters(ctx)
	case *grammar.FcallContext:
		return v.VisitFcall(ctx)
	case *grammar.UminusContext:
		return v.VisitUminus(ctx)
	case *grammar.LiteralNilContext:
		return NewLiteral(nil)
	case *grammar.NameContext:
		return v.VisitName(ctx)
	case *grammar.RelationNameContext:
		return v.VisitRelationName(ctx)
	case *grammar.PeriodContext:
		return v.VisitPeriod(ctx)
	case *grammar.ExprContext:
		return v.VisitExpr(ctx)
	default:
		v.errorf(nil, "unknown expression context: %T", tree)
		return NewLiteral(nil)
	}
}

// VisitExpr handles unexpected expr contexts.
func (v *Visitor) VisitExpr(ctx *grammar.ExprContext) Expression {
	v.errorf(ctx, "invalid expression: %q", ctx.GetText())
	return NewLiteral(nil)
}

// VisitValue handles value expressions.
func (v *Visitor) VisitValue(ctx *grammar.ValueContext) Expression {
	return v.Visit(ctx.GetLeft())
}

// VisitLiteral handles literal values.
func (v *Visitor) VisitLiteral(ctx *grammar.LiteralContext) Expression {
	token := ctx.GetV()
	if token == nil {
		v.errorf(ctx, "missing literal token")
		return NewLiteral(nil)
	}

	switch token.GetTokenType() {
	case grammar.YammmGrammarLexerSTRING:
		s, err := strconv.Unquote(token.GetText())
		if err != nil {
			v.errorf(ctx, "invalid string literal: %v", err)
			return NewLiteral(nil)
		}
		return NewLiteral(s)

	case grammar.YammmGrammarLexerINTEGER:
		i, err := strconv.ParseInt(token.GetText(), 10, 64)
		if err != nil {
			v.errorf(ctx, "invalid integer literal: %v", err)
			return NewLiteral(nil)
		}
		return NewLiteral(i)

	case grammar.YammmGrammarLexerFLOAT:
		f, err := strconv.ParseFloat(token.GetText(), 64)
		if err != nil {
			v.errorf(ctx, "invalid float literal: %v", err)
			return NewLiteral(nil)
		}
		return NewLiteral(f)

	case grammar.YammmGrammarLexerBOOLEAN:
		b, err := strconv.ParseBool(token.GetText())
		if err != nil {
			v.errorf(ctx, "invalid boolean literal: %v", err)
			return NewLiteral(nil)
		}
		return NewLiteral(b)

	case grammar.YammmGrammarLexerREGEXP:
		s := token.GetText()
		re := s[1 : len(s)-1] // Strip delimiters
		r, err := regexp.Compile(re)
		if err != nil {
			v.errorf(ctx, "invalid regexp literal: %v", err)
			return NewLiteral(nil)
		}
		return NewLiteral(r)

	default:
		v.errorf(ctx, "unexpected literal token type: %d", token.GetTokenType())
		return NewLiteral(nil)
	}
}

// VisitUminus handles unary minus.
func (v *Visitor) VisitUminus(ctx *grammar.UminusContext) Expression {
	return SExpr{Op("-x"), v.Visit(ctx.GetRight())}
}

// VisitVariable handles variable references ($name).
func (v *Visitor) VisitVariable(ctx *grammar.VariableContext) Expression {
	s := ctx.GetLeft().GetText()
	return SExpr{Op("$"), NewLiteral(s[1:])} // Strip the $
}

// VisitPeriod handles member access (lhs.name).
func (v *Visitor) VisitPeriod(ctx *grammar.PeriodContext) Expression {
	nameExpr := v.Visit(ctx.GetName())
	// Convert property access to literal name for member access
	if nameExpr.Op() == "p" {
		children := nameExpr.Children()
		if len(children) > 0 {
			nameExpr = children[0]
		}
	}
	return SExpr{Op("."), v.Visit(ctx.GetLeft()), nameExpr}
}

// VisitName handles property name references.
func (v *Visitor) VisitName(ctx *grammar.NameContext) Expression {
	return SExpr{Op("p"), NewLiteral(ctx.GetLeft().GetText())}
}

// VisitRelationName handles relation name references.
func (v *Visitor) VisitRelationName(ctx *grammar.RelationNameContext) Expression {
	return SExpr{Op("p"), NewLiteral(ctx.GetLeft().GetText())}
}

// VisitMuldiv handles multiplication, division, and modulo.
func (v *Visitor) VisitMuldiv(ctx *grammar.MuldivContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitPlusminus handles addition and subtraction.
func (v *Visitor) VisitPlusminus(ctx *grammar.PlusminusContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitCompare handles comparison operators (<, <=, >, >=).
func (v *Visitor) VisitCompare(ctx *grammar.CompareContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitEquality handles equality operators (==, !=).
func (v *Visitor) VisitEquality(ctx *grammar.EqualityContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitMatch handles pattern matching operators (=~, !~).
func (v *Visitor) VisitMatch(ctx *grammar.MatchContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitIn handles the 'in' operator.
func (v *Visitor) VisitIn(ctx *grammar.InContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitOr handles logical OR (||).
func (v *Visitor) VisitOr(ctx *grammar.OrContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitAnd handles logical AND (&&).
func (v *Visitor) VisitAnd(ctx *grammar.AndContext) Expression {
	return v.binaryExpr(ctx.GetOp(), ctx.GetLeft(), ctx.GetRight())
}

// VisitIf handles ternary conditional (?).
func (v *Visitor) VisitIf(ctx *grammar.IfContext) Expression {
	op := Op(ctx.GetOp().GetText())
	cond := v.Visit(ctx.GetLeft())
	trueBranch := v.Visit(ctx.GetTrueBranch())
	if ctx.GetFalseBranch() != nil {
		return SExpr{op, cond, trueBranch, v.Visit(ctx.GetFalseBranch())}
	}
	return SExpr{op, cond, trueBranch}
}

// VisitNot handles logical NOT (!).
func (v *Visitor) VisitNot(ctx *grammar.NotContext) Expression {
	return SExpr{Op(ctx.GetOp().GetText()), v.Visit(ctx.GetRight())}
}

// VisitGroup handles parenthesized expressions.
func (v *Visitor) VisitGroup(ctx *grammar.GroupContext) Expression {
	return v.Visit(ctx.GetLeft())
}

// VisitAt handles slice/index access (@).
func (v *Visitor) VisitAt(ctx *grammar.AtContext) Expression {
	right := ctx.GetRight()
	args := make([]Expression, len(right))
	for i := range right {
		args[i] = v.Visit(right[i])
	}
	return append(SExpr{Op("@"), v.Visit(ctx.GetLeft())}, args...)
}

// VisitList handles list literals ([]).
func (v *Visitor) VisitList(ctx *grammar.ListContext) Expression {
	values := ctx.GetValues()
	elements := make([]Expression, len(values))
	for i := range values {
		elements[i] = v.Visit(values[i])
	}
	return append(SExpr{Op("[]")}, elements...)
}

// VisitDatatypeName handles data type names.
func (v *Visitor) VisitDatatypeName(ctx *grammar.DatatypeNameContext) Expression {
	return DatatypeLiteral(ctx.GetLeft().GetText())
}

// VisitFcall handles function calls.
func (v *Visitor) VisitFcall(ctx *grammar.FcallContext) Expression {
	lhs := v.Visit(ctx.GetLeft())
	fname := ctx.GetName().GetText()
	args := v.Visit(ctx.GetArgs())
	params := v.Visit(ctx.GetParams())
	body := v.Visit(ctx.GetBody())

	// Note: Function name validation is deferred to the eval layer (instance/eval).
	// This allows schemas to be compiled without knowing all builtins, supporting
	// runtime extension and custom builtin registration. Unknown functions will
	// produce E_EVAL_ERROR at instance validation time.

	// Normalize nil children to empty literals for evaluator consistency.
	// This ensures downstream code doesn't need nil checks for missing args/params/body.
	// Note: After normalization, "body missing" and "body explicitly nil" are
	// indistinguishable (both become NewLiteral(nil)). This is intentional;
	// evaluators should treat them equivalently.
	if args == nil {
		args = NewLiteral([]Expression{})
	}
	if params == nil {
		params = NewLiteral([]string{})
	}
	if body == nil {
		body = NewLiteral(nil)
	}
	return SExpr{Op(fname), lhs, args, params, body}
}

// VisitArguments handles function argument lists.
// Returns a Literal containing []Expression for type-safe AST processing.
func (v *Visitor) VisitArguments(ctx *grammar.ArgumentsContext) Expression {
	args := ctx.GetArgs()
	a := make([]Expression, len(args))
	for i := range args {
		a[i] = v.Visit(args[i])
	}
	return NewLiteral(a)
}

// VisitParameters handles lambda parameter lists.
func (v *Visitor) VisitParameters(ctx *grammar.ParametersContext) Expression {
	params := ctx.GetParams()
	p := make([]string, len(params))
	for i := range params {
		p[i] = params[i].GetText()[1:] // Strip the $
	}
	return NewLiteral(p)
}

// binaryExpr creates a binary expression node.
func (v *Visitor) binaryExpr(op antlr.Token, left, right antlr.ParseTree) Expression {
	return SExpr{Op(op.GetText()), v.Visit(left), v.Visit(right)}
}

// errorf reports an error at the given context.
func (v *Visitor) errorf(ctx antlr.ParserRuleContext, format string, args ...any) {
	v.hasErrs = true
	if v.collector == nil {
		return
	}

	msg := fmt.Sprintf(format, args...)
	var span location.Span
	if ctx != nil {
		span = v.spans.FromContext(ctx)
	}

	issue := diag.NewIssue(diag.Error, diag.E_INVALID_INVARIANT, msg)
	if !span.IsZero() {
		issue = issue.WithSpan(span)
	}
	v.collector.Collect(issue.Build())
}
