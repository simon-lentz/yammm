package parse

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/schema/expr"
)

// Binding powers for the invariant-expression grammar, tightest last. Three
// of them contradict a C-family reading; see the package doc.
const (
	precIf     = 7
	precOr     = 8
	precAnd    = 9
	precEq     = 10
	precMatch  = 11
	precIn     = 12
	precCmp    = 13
	precAdd    = 14
	precMul    = 15
	precNot    = 16
	precPeriod = 17
	precFcall  = 18
	precAt     = 19
	precUminus = 20
)

// binaryPrec maps operator token values to binding power for the plain
// left-associative binary operators. "in" is matched by value because it lexes
// as LC_WORD under the keywordless rule table.
var binaryPrec = map[string]int{
	"*": precMul, "/": precMul, "%": precMul,
	"+": precAdd, "-": precAdd,
	">": precCmp, ">=": precCmp, "<": precCmp, "<=": precCmp,
	"in": precIn,
	"=~": precMatch, "!~": precMatch,
	"==": precEq, "!=": precEq,
	"&&": precAnd,
	"||": precOr, "^": precOr,
}

// exprError satisfies participle's Error interface so an expression failure
// carries a real position out through ParseTypeWith into the member parser's
// error path.
type exprError struct {
	pos lexer.Position
	msg string
}

func (e *exprError) Error() string            { return fmt.Sprintf("%s: %s", e.pos, e.msg) }
func (e *exprError) Message() string          { return e.msg }
func (e *exprError) Position() lexer.Position { return e.pos }

// exprDiag records a literal that parsed as the right shape but could not be
// converted — an unquotable string, an out-of-range integer, a regex that does
// not compile. The expression itself is still well-formed, so these are
// reported without failing the parse.
type exprDiag struct {
	Msg        string
	Start, End int
}

type exprParser struct {
	lx      *lexer.PeekingLexer
	diags   *[]exprDiag
	tok     tokenTypes
	lastEnd int
}

func newExprParser(lx *lexer.PeekingLexer, diags *[]exprDiag, tok tokenTypes) *exprParser {
	return &exprParser{lx: lx, diags: diags, tok: tok}
}

// next consumes one token and records its end offset, so the caller can
// recover the expression's exact byte extent after parsing.
func (p *exprParser) next() {
	t := p.lx.Next()
	p.lastEnd = t.Pos.Offset + len(t.Value)
}

func (p *exprParser) errf(t *lexer.Token, format string, args ...any) error {
	return &exprError{pos: t.Pos, msg: fmt.Sprintf(format, args...)}
}

func (p *exprParser) diagf(start, end int, format string, args ...any) {
	*p.diags = append(*p.diags, exprDiag{Msg: fmt.Sprintf(format, args...), Start: start, End: end})
}

func (p *exprParser) parse(minPrec int) (expr.Expression, error) {
	left, err := p.prefix()
	if err != nil {
		return nil, err
	}
	for {
		t := p.lx.Peek()
		if t.EOF() {
			return left, nil
		}
		switch {
		case t.Type == p.tok.lbrack && precAt >= minPrec:
			p.next()
			args, err := p.exprListUntil(p.tok.rbrack, false)
			if err != nil {
				return nil, err
			}
			left = append(expr.SExpr{expr.Op("@"), left}, args...)

		case t.Type == p.tok.arrow && precFcall >= minPrec:
			p.next()
			name := p.lx.Peek()
			if !p.isPlainWord(name) {
				return nil, p.errf(name, "expected function name after '->'")
			}
			p.next()
			node, err := p.fcall(left, name.Value)
			if err != nil {
				return nil, err
			}
			left = node

		case t.Type == p.tok.period && precPeriod >= minPrec:
			p.next()
			right, err := p.parse(precPeriod + 1)
			if err != nil {
				return nil, err
			}
			// Collapse so "a.b" is a two-element access, not an access whose
			// right operand is itself a lookup.
			if right.Op() == "p" {
				if ch := right.Children(); len(ch) > 0 {
					right = ch[0]
				}
			}
			left = expr.SExpr{expr.Op("."), left, right}

		case t.Type == p.tok.qmark && precIf >= minPrec:
			p.next()
			node, err := p.conditional(left)
			if err != nil {
				return nil, err
			}
			left = node

		default:
			prec, ok := binaryPrec[t.Value]
			if !ok || prec < minPrec {
				return left, nil
			}
			p.next()
			right, err := p.parse(prec + 1)
			if err != nil {
				return nil, err
			}
			left = expr.SExpr{expr.Op(t.Value), left, right}
		}
	}
}

// conditional parses the "{ then : else }" tail of a conditional whose '?' the
// caller has already consumed. The else branch is optional.
func (p *exprParser) conditional(cond expr.Expression) (expr.Expression, error) {
	if err := p.expect(p.tok.lbrace, "'{' after '?'"); err != nil {
		return nil, err
	}
	trueBranch, err := p.parse(0)
	if err != nil {
		return nil, err
	}
	var falseBranch expr.Expression
	if p.lx.Peek().Type == p.tok.colon {
		p.next()
		falseBranch, err = p.parse(0)
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect(p.tok.rbrace, "'}' closing conditional"); err != nil {
		return nil, err
	}
	if falseBranch != nil {
		return expr.SExpr{expr.Op("?"), cond, trueBranch, falseBranch}, nil
	}
	return expr.SExpr{expr.Op("?"), cond, trueBranch}, nil
}

func (p *exprParser) prefix() (expr.Expression, error) {
	t := p.lx.Peek()
	if t.EOF() {
		return nil, p.errf(t, "expected expression")
	}
	if t.Type == p.tok.invalidNumber {
		return nil, p.errf(t, "%s", InvalidNumberMessage(t.Value))
	}
	switch t.Type {
	case p.tok.minus:
		p.next()
		right, err := p.parse(precUminus)
		if err != nil {
			return nil, err
		}
		return expr.SExpr{expr.Op("-x"), right}, nil
	case p.tok.exclamation:
		p.next()
		right, err := p.parse(precNot)
		if err != nil {
			return nil, err
		}
		return expr.SExpr{expr.Op("!"), right}, nil
	case p.tok.lpar:
		p.next()
		inner, err := p.parse(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(p.tok.rpar, "')'"); err != nil {
			return nil, err
		}
		return inner, nil
	case p.tok.lbrack:
		p.next()
		elems, err := p.exprListUntil(p.tok.rbrack, false)
		if err != nil {
			return nil, err
		}
		return append(expr.SExpr{expr.Op("[]")}, elems...), nil
	case p.tok.variable:
		p.next()
		return expr.SExpr{expr.Op("$"), expr.NewLiteral(t.Value[1:])}, nil
	case p.tok.uscore:
		p.next()
		return expr.NewLiteral(nil), nil
	case p.tok.str, p.tok.integer, p.tok.float, p.tok.regexp:
		return p.literal(t), nil
	case p.tok.ucWord:
		p.next()
		if datatypeKeywords[t.Value] {
			return expr.DatatypeLiteral(t.Value), nil
		}
		return expr.SExpr{expr.Op("p"), expr.NewLiteral(t.Value)}, nil
	case p.tok.lcWord:
		return p.lcWord(t)
	}
	return nil, p.errf(t, "unexpected token %q in expression", t.Value)
}

// literal converts a literal token, reporting a diagnostic and substituting
// nil when the conversion fails. The expression stays well-formed either way,
// which is what keeps one bad literal from cascading.
func (p *exprParser) literal(t *lexer.Token) expr.Expression {
	p.next()
	start, end := t.Pos.Offset, t.Pos.Offset+len(t.Value)
	switch t.Type {
	case p.tok.str:
		s, err := strconv.Unquote(t.Value)
		if err != nil {
			p.diagf(start, end, "invalid string literal: %v", err)
			return expr.NewLiteral(nil)
		}
		return expr.NewLiteral(s)
	case p.tok.integer:
		i, err := strconv.ParseInt(t.Value, 10, 64)
		if err != nil {
			p.diagf(start, end, "invalid integer literal: %v", err)
			return expr.NewLiteral(nil)
		}
		return expr.NewLiteral(i)
	case p.tok.float:
		f, err := strconv.ParseFloat(t.Value, 64)
		if err != nil {
			p.diagf(start, end, "invalid float literal: %v", err)
			return expr.NewLiteral(nil)
		}
		return expr.NewLiteral(f)
	default: // REGEXP
		re, err := regexp.Compile(t.Value[1 : len(t.Value)-1])
		if err != nil {
			p.diagf(start, end, "invalid regexp literal: %v", err)
			return expr.NewLiteral(nil)
		}
		return expr.NewLiteral(re)
	}
}

// lcWord resolves a lowercase word in operand position: the three literal
// spellings become literals, the spellings that are not names anywhere are a
// syntax error, and everything else is a property reference.
func (p *exprParser) lcWord(t *lexer.Token) (expr.Expression, error) {
	switch t.Value {
	case "nil":
		p.next()
		return expr.NewLiteral(nil), nil
	case "true", "false":
		p.next()
		return expr.NewLiteral(t.Value == "true"), nil
	case "as", "part":
		return nil, p.errf(t, "unexpected keyword %q in expression", t.Value)
	case "in":
		return nil, p.errf(t, "unexpected 'in' in expression")
	}
	p.next()
	return expr.SExpr{expr.Op("p"), expr.NewLiteral(t.Value)}, nil
}

// fcall parses the optional (args), |params| and { body } tail of a pipeline
// call, producing the five-element shape with absent parts normalized to empty
// rather than left nil.
func (p *exprParser) fcall(lhs expr.Expression, name string) (expr.Expression, error) {
	var args, params, body expr.Expression
	if p.lx.Peek().Type == p.tok.lpar {
		p.next()
		list, err := p.exprListUntil(p.tok.rpar, true)
		if err != nil {
			return nil, err
		}
		args = expr.NewLiteral(list)
	}
	if p.lx.Peek().Type == p.tok.pipe {
		names, err := p.lambdaParams()
		if err != nil {
			return nil, err
		}
		params = expr.NewLiteral(names)
	}
	if p.lx.Peek().Type == p.tok.lbrace {
		p.next()
		inner, err := p.parse(0)
		if err != nil {
			return nil, err
		}
		if err := p.expect(p.tok.rbrace, "'}' closing call body"); err != nil {
			return nil, err
		}
		body = inner
	}
	if args == nil {
		args = expr.NewLiteral([]expr.Expression{})
	}
	if params == nil {
		params = expr.NewLiteral([]string{})
	}
	if body == nil {
		body = expr.NewLiteral(nil)
	}
	return expr.SExpr{expr.Op(name), lhs, args, params, body}, nil
}

// lambdaParams parses the |$a, $b| parameter list, whose opening pipe the
// caller has peeked but not consumed.
func (p *exprParser) lambdaParams() ([]string, error) {
	p.next()
	var names []string
	for {
		t := p.lx.Peek()
		if t.Type != p.tok.variable {
			return nil, p.errf(t, "expected lambda parameter")
		}
		p.next()
		names = append(names, t.Value[1:])
		if p.lx.Peek().Type != p.tok.comma {
			break
		}
		p.next()
		if p.lx.Peek().Type == p.tok.pipe { // trailing comma
			break
		}
	}
	if err := p.expect(p.tok.pipe, "closing '|'"); err != nil {
		return nil, err
	}
	return names, nil
}

// exprListUntil parses a possibly-empty, comma-separated, trailing-comma
// tolerant expression list terminated by the named closing token, which it
// consumes. The empty list is non-nil, because exprcomp builds its own through
// make and a nil slice compares unequal to that under cmp and DeepEqual.
func (p *exprParser) exprListUntil(closer lexer.TokenType, bareComma bool) ([]expr.Expression, error) {
	out := []expr.Expression{}
	for {
		t := p.lx.Peek()
		if t.Type == closer {
			p.next()
			return out, nil
		}
		// A call's argument list admits one comma with no arguments at all:
		// the grammar's optional trailing COMMA sits outside its argument
		// group, so "f(,)" is legal. The list and index forms put the same
		// COMMA inside their group, so "[,]" is not, and they pass false.
		if bareComma && len(out) == 0 && t.Type == p.tok.comma {
			p.next()
			if p.lx.Peek().Type != closer {
				return nil, p.errf(p.lx.Peek(), "expected a closing delimiter after ','")
			}
			p.next()
			return out, nil
		}
		e, err := p.parse(0)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
		if p.lx.Peek().Type == p.tok.comma {
			p.next()
			continue
		}
		if p.lx.Peek().Type == closer {
			p.next()
			return out, nil
		}
		return nil, p.errf(p.lx.Peek(), "expected ',' or closing delimiter in expression list")
	}
}

func (p *exprParser) expect(want lexer.TokenType, what string) error {
	t := p.lx.Peek()
	if t.Type != want {
		return p.errf(t, "expected %s", what)
	}
	p.next()
	return nil
}

// isPlainWord reports whether t can stand as an ordinary identifier — the
// function-name position after '->' is the caller.
func (p *exprParser) isPlainWord(t *lexer.Token) bool {
	if t.Type == p.tok.ucWord {
		return !datatypeKeywords[t.Value]
	}
	if t.Type == p.tok.lcWord {
		return !reservedLC[t.Value]
	}
	return false
}

// datatypeKeywords are the eleven built-in type names. They are never ordinary
// identifiers, so UC_WORD positions that admit a name exclude them.
var datatypeKeywords = map[string]bool{
	"Integer": true, "Float": true, "Boolean": true, "String": true,
	"Enum": true, "Pattern": true, "Timestamp": true, "Date": true,
	"UUID": true, "Vector": true, "List": true,
}

// reservedLC are the seventeen lowercase spellings the language treats as
// keywords or literals rather than names. Positions that admit a contextual
// keyword re-admit specific spellings explicitly; property and annotation name
// positions exclude only the six that are not names anywhere.
var reservedLC = map[string]bool{
	"schema": true, "type": true, "datatype": true, "required": true,
	"primary": true, "extends": true, "includes": true, "abstract": true,
	"one": true, "many": true, "import": true, "as": true, "part": true,
	"in": true, "nil": true, "true": true, "false": true,
}

// propertyNameExclusions are the six spellings that cannot be a property or
// annotation name: three are grammar keywords in that position and three are
// literals the lexer's own rules outrank an identifier with.
var propertyNameExclusions = map[string]bool{
	"as": true, "part": true, "in": true,
	"nil": true, "true": true, "false": true,
}
