package parse

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/internal/expr"
)

// Binding powers for the invariant-expression grammar, tightest last. Two
// of them contradict a C-family reading; see the package doc.
const (
	precIf    = 7
	precOr    = 8
	precAnd   = 9
	precEq    = 10
	precMatch = 11
	precIn    = 12
	precCmp   = 13
	precAdd   = 14
	precMul   = 15
	precNot   = 16
	// Indexing, pipeline calls and property access are one postfix level and
	// chain left to right: "$self.tags[0]" indexes the property, "$i.name ->
	// Len" pipes it.
	precPostfix = 17
	precUminus  = 18
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
	"||": precOr,
}

// exprError satisfies participle's Error interface so an expression failure
// carries a real position out through ParseTypeWith into the member parser's
// error path.
type exprError struct {
	pos lexer.Position
	msg string
	// reach is the byte recovery resumes at when the error owns more of the
	// source than the token at pos, else zero. ends reports that the expression
	// ends there, so the token at reach belongs to what follows it.
	reach int
	ends  bool
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
	// depth is the number of parse calls in progress. Each one's result sits a
	// level below its caller's, so depth never exceeds the tree's height.
	depth int
	// noValue records each literal that has no value, see [exprResult.NoValue].
	noValue []NoValueLiteral
	// deep is the error that refused the expression for its depth, or nil.
	deep *exprError
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

// level returns the height of a node over operands of the given heights, and
// refuses one past MaxNesting at t, the token that adds the level.
func (p *exprParser) level(t *lexer.Token, operands ...int) (int, error) {
	h := 1
	for _, o := range operands {
		h = max(h, o+1)
	}
	if h > MaxNesting {
		return 0, p.tooDeep(t)
	}
	return h, nil
}

// tooDeep refuses the level t adds. The error owns every token of the
// expression, which [skipExpression] reads once the parse has returned.
func (p *exprParser) tooDeep(t *lexer.Token) error {
	p.deep = &exprError{pos: t.Pos, msg: fmt.Sprintf("expression nests deeper than %d levels", MaxNesting)}
	return p.deep
}

// parse parses one expression whose operators bind at least as tightly as
// minPrec, and returns it with the height of its tree: a name or a literal is
// one level, and an operator, a call, an index, a member read, a list and a
// pair of parentheses each add one to the highest operand.
func (p *exprParser) parse(minPrec int) (expr.Expression, int, error) {
	// A chain of prefixes recurses before any height is known.
	if p.depth == MaxNesting {
		return nil, 0, p.tooDeep(p.lx.Peek())
	}
	p.depth++
	e, h, err := p.operators(minPrec)
	p.depth--
	return e, h, err
}

func (p *exprParser) operators(minPrec int) (expr.Expression, int, error) {
	left, h, err := p.prefix()
	if err != nil {
		return nil, 0, err
	}
	for {
		t := p.lx.Peek()
		if t.EOF() {
			return left, h, nil
		}
		switch {
		case t.Type == p.tok.lbrack && precPostfix >= minPrec:
			p.next()
			if p.lx.Peek().Type == p.tok.rbrack {
				return nil, 0, p.errf(p.lx.Peek(), "expected an index before ']'")
			}
			index, ih, err := p.parse(0)
			if err != nil {
				return nil, 0, err
			}
			if err := p.expect(p.tok.rbrack, "']' after one index"); err != nil {
				return nil, 0, err
			}
			if h, err = p.level(t, h, ih); err != nil {
				return nil, 0, err
			}
			left = expr.SExpr{expr.Op("@"), left, index}

		case t.Type == p.tok.arrow && precPostfix >= minPrec:
			p.next()
			name := p.lx.Peek()
			if !p.isWord(name) {
				return nil, 0, p.errf(name, "expected function name after '->'")
			}
			p.next()
			node, ch, err := p.fcall(left, name.Value)
			if err != nil {
				return nil, 0, err
			}
			if h, err = p.level(t, h, ch); err != nil {
				return nil, 0, err
			}
			left = node

		case t.Type == p.tok.period && precPostfix >= minPrec:
			p.next()
			name := p.lx.Peek()
			if !p.isWord(name) {
				return nil, 0, p.errf(name, "expected a member name after '.'")
			}
			p.next()
			if h, err = p.level(t, h); err != nil {
				return nil, 0, err
			}
			left = expr.SExpr{expr.Op("."), left, expr.NewLiteral(name.Value)}

		case t.Type == p.tok.qmark && precIf >= minPrec:
			p.next()
			node, bh, err := p.conditional(left)
			if err != nil {
				return nil, 0, err
			}
			if h, err = p.level(t, h, bh); err != nil {
				return nil, 0, err
			}
			left = node

		default:
			prec, ok := binaryPrec[t.Value]
			if !ok || prec < minPrec {
				return left, h, nil
			}
			p.next()
			right, rh, err := p.parse(prec + 1)
			if err != nil {
				return nil, 0, err
			}
			if h, err = p.level(t, h, rh); err != nil {
				return nil, 0, err
			}
			left = expr.SExpr{expr.Op(t.Value), left, right}
		}
	}
}

// conditional parses the "{ then : else }" tail of a conditional whose '?' the
// caller has already consumed, and returns the higher branch's height. The else
// branch is required: a two-operand conditional can never evaluate, so the
// missing ':' is a syntax error here rather than a guaranteed evaluation error
// later.
func (p *exprParser) conditional(cond expr.Expression) (expr.Expression, int, error) {
	if err := p.expect(p.tok.lbrace, "'{' after '?'"); err != nil {
		return nil, 0, err
	}
	trueBranch, th, err := p.parse(0)
	if err != nil {
		return nil, 0, err
	}
	if err := p.expect(p.tok.colon, "':' and an else branch in a conditional"); err != nil {
		return nil, 0, err
	}
	falseBranch, fh, err := p.parse(0)
	if err != nil {
		return nil, 0, err
	}
	if err := p.expect(p.tok.rbrace, "'}' closing conditional"); err != nil {
		return nil, 0, err
	}
	return expr.SExpr{expr.Op("?"), cond, trueBranch, falseBranch}, max(th, fh), nil
}

func (p *exprParser) prefix() (expr.Expression, int, error) {
	t := p.lx.Peek()
	if t.EOF() {
		return nil, 0, p.errf(t, "expected expression")
	}
	if t.Type == p.tok.invalidNumber {
		return nil, 0, p.errf(t, "%s", InvalidNumberMessage(t.Value))
	}
	switch t.Type {
	case p.tok.minus:
		p.next()
		// A minus before a number is the literal's sign, as it is a bound's,
		// so the lowest int64 can be written.
		if n := p.lx.Peek(); n.Type == p.tok.integer || n.Type == p.tok.float {
			return p.number(n, t), 1, nil
		}
		right, rh, err := p.parse(precUminus)
		if err != nil {
			return nil, 0, err
		}
		h, err := p.level(t, rh)
		if err != nil {
			return nil, 0, err
		}
		return expr.SExpr{expr.Op("-x"), right}, h, nil
	case p.tok.exclamation:
		p.next()
		right, rh, err := p.parse(precNot)
		if err != nil {
			return nil, 0, err
		}
		h, err := p.level(t, rh)
		if err != nil {
			return nil, 0, err
		}
		return expr.SExpr{expr.Op("!"), right}, h, nil
	case p.tok.lpar:
		p.next()
		inner, ih, err := p.parse(0)
		if err != nil {
			return nil, 0, err
		}
		if err := p.expect(p.tok.rpar, "')'"); err != nil {
			return nil, 0, err
		}
		h, err := p.level(t, ih)
		if err != nil {
			return nil, 0, err
		}
		return inner, h, nil
	case p.tok.lbrack:
		p.next()
		elems, eh, err := p.exprListUntil(p.tok.rbrack, false)
		if err != nil {
			return nil, 0, err
		}
		h, err := p.level(t, eh)
		if err != nil {
			return nil, 0, err
		}
		return append(expr.SExpr{expr.Op("[]")}, elems...), h, nil
	case p.tok.variable:
		p.next()
		return expr.SExpr{expr.Op("$"), expr.NewLiteral(t.Value[1:])}, 1, nil
	case p.tok.uscore:
		p.next()
		return expr.NewLiteral(nil), 1, nil
	case p.tok.integer, p.tok.float:
		return p.number(t, nil), 1, nil
	case p.tok.str, p.tok.regexp:
		return p.literal(t), 1, nil
	case p.tok.ucWord:
		if datatypeKeywords[t.Value] {
			return nil, 0, p.errf(t, "unexpected keyword %q in an expression", t.Value)
		}
		p.next()
		return expr.SExpr{expr.Op("p"), expr.NewLiteral(t.Value)}, 1, nil
	case p.tok.lcWord:
		e, err := p.lcWord(t)
		return e, 1, err
	}
	return nil, 0, p.errf(t, "unexpected token %q in an expression", t.Value)
}

// number converts the number token t, which the caller has peeked, with the
// minus token sign before it when the source wrote one. A text no int64 or
// float64 holds is a literal with no value, reported over the sign and the
// digits.
func (p *exprParser) number(t, sign *lexer.Token) expr.Expression {
	p.next()
	start, end := t.Pos.Offset, t.Pos.Offset+len(t.Value)
	text := t.Value
	if sign != nil {
		start, text = sign.Pos.Offset, "-"+text
	}
	if t.Type == p.tok.integer {
		i, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			p.diagf(start, end, "invalid integer literal: %v", err)
			return p.literalWithNoValue(IntegerLiteral)
		}
		return expr.NewLiteral(i)
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		p.diagf(start, end, "invalid float literal: %v", err)
		return p.literalWithNoValue(FloatLiteral)
	}
	return expr.NewLiteral(f)
}

// literal converts a string or a pattern token. When the conversion fails it
// substitutes a nil literal and reports a diagnostic, save for text the source
// rules refuse, which they report. The expression stays well-formed either
// way, which is what keeps one bad literal from cascading.
func (p *exprParser) literal(t *lexer.Token) expr.Expression {
	p.next()
	start, end := t.Pos.Offset, t.Pos.Offset+len(t.Value)
	if t.Type == p.tok.str {
		s, err := unquote(t.Value)
		switch {
		case errors.Is(err, errRefusedText):
			// The source rules report the literal's bytes, and one break is
			// one diagnostic.
			return p.literalWithNoValue(StringLiteral)
		case err != nil:
			// This site supplies its own context, so it reports the cause
			// alone rather than double-prefixing a text consumers match.
			p.diagf(start, end, "invalid string literal: %s", strings.TrimPrefix(err.Error(), unquotePrefix))
			return p.literalWithNoValue(StringLiteral)
		}
		return expr.NewLiteral(s)
	}
	// A pattern the source rules refuse has no value, and its bytes are
	// reported where they stand. One holding a byte that is no part of a
	// character has no text to compile. A NUL and a byte order mark are
	// characters regexp reads, so a compile error beside one is the
	// pattern's own and is reported.
	if !utf8.ValidString(t.Value) {
		return p.literalWithNoValue(RegexLiteral)
	}
	re, err := regexp.Compile(t.Value[1 : len(t.Value)-1])
	if err != nil {
		p.diagf(start, end, "invalid regexp literal: %v", err)
		return p.literalWithNoValue(RegexLiteral)
	}
	if _, refused := nextTextFault(t.Value, 0); refused {
		return p.literalWithNoValue(RegexLiteral)
	}
	return expr.NewLiteral(re)
}

// literalWithNoValue stands in for a literal of kind that has no value, whose
// own diagnostic is reported where it stands. The nil literal keeps the
// expression well formed, and noValue names it with its kind, so no later
// check judges a nil the source did not write.
func (p *exprParser) literalWithNoValue(kind LiteralKind) expr.Expression {
	lit := &expr.Literal{}
	p.noValue = append(p.noValue, NoValueLiteral{Literal: lit, Kind: kind})
	return lit
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
		return nil, p.errf(t, "unexpected keyword %q in an expression", t.Value)
	case "in":
		return nil, p.errf(t, "unexpected 'in' in an expression")
	}
	p.next()
	return expr.SExpr{expr.Op("p"), expr.NewLiteral(t.Value)}, nil
}

// fcall parses the optional (args), |params| and { body } tail of a pipeline
// call, producing the five-element shape and the height of its highest
// argument or body. Absent args and params are empty lists; an absent body is
// a nil Expression, so a body that is the nil literal — `{ nil }` or `{ _ }` —
// stays a body.
func (p *exprParser) fcall(lhs expr.Expression, name string) (expr.Expression, int, error) {
	var args, params, body expr.Expression
	h := 0
	if p.lx.Peek().Type == p.tok.lpar {
		p.next()
		list, ah, err := p.exprListUntil(p.tok.rpar, true)
		if err != nil {
			return nil, 0, err
		}
		args, h = expr.NewLiteral(list), ah
	}
	if p.lx.Peek().Type == p.tok.pipe {
		names, err := p.lambdaParams()
		if err != nil {
			return nil, 0, err
		}
		params = expr.NewLiteral(names)
	}
	if p.lx.Peek().Type == p.tok.lbrace {
		p.next()
		inner, bh, err := p.parse(0)
		if err != nil {
			return nil, 0, err
		}
		if err := p.expect(p.tok.rbrace, "'}' closing call body"); err != nil {
			return nil, 0, err
		}
		body, h = inner, max(h, bh)
	}
	if args == nil {
		args = expr.NewLiteral([]expr.Expression{})
	}
	if params == nil {
		params = expr.NewLiteral([]string{})
	}
	return expr.SExpr{expr.Op(name), lhs, args, params, body}, h, nil
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
// consumes, and returns the height of its highest element. The empty list is
// non-nil, because a consumer building its own through make and a nil slice
// compares unequal to that under cmp and DeepEqual.
func (p *exprParser) exprListUntil(closer lexer.TokenType, bareComma bool) ([]expr.Expression, int, error) {
	out := []expr.Expression{}
	h := 0
	for {
		t := p.lx.Peek()
		if t.Type == closer {
			p.next()
			return out, h, nil
		}
		// A call's argument list admits one comma with no arguments at all:
		// the grammar's optional trailing COMMA sits outside its argument
		// group, so "f(,)" is legal. The list form puts the same COMMA
		// inside its group, so "[,]" is not, and it passes false.
		if bareComma && len(out) == 0 && t.Type == p.tok.comma {
			p.next()
			if p.lx.Peek().Type != closer {
				return nil, 0, p.errf(p.lx.Peek(), "expected a closing delimiter after ','")
			}
			p.next()
			return out, h, nil
		}
		e, eh, err := p.parse(0)
		if err != nil {
			return nil, 0, err
		}
		out, h = append(out, e), max(h, eh)
		if p.lx.Peek().Type == p.tok.comma {
			p.next()
			continue
		}
		if p.lx.Peek().Type == closer {
			p.next()
			return out, h, nil
		}
		return nil, 0, p.errf(p.lx.Peek(), "expected ',' or closing delimiter in an expression list")
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

// isWord reports whether t is a word of either case. The positions after '.'
// and after '->' take any word: each admits nothing but a name, so a keyword's
// spelling is a name there, and the type's members or the builtin catalogue
// decide whether it resolves.
func (p *exprParser) isWord(t *lexer.Token) bool {
	return t.Type == p.tok.ucWord || t.Type == p.tok.lcWord
}

// datatypeKeywords are the eleven built-in type names. One is no operand and
// no declared name. After '->' and after '.' it is a name, as any word is.
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
// literals. Each lexes as LC_WORD, so the grammar refuses all six by name.
var propertyNameExclusions = map[string]bool{
	"as": true, "part": true, "in": true,
	"nil": true, "true": true, "false": true,
}

// IsReserved reports whether name is a spelling the language refuses in a
// position that admits either case: an import alias, a relation name, a reverse
// name. It is the union of the lowercase keywords and the built-in type names.
func IsReserved(name string) bool {
	return reservedLC[name] || datatypeKeywords[name]
}

// IsDatatypeKeyword reports whether name is one of the eleven built-in type
// names, which no type, datatype or relation name may spell.
func IsDatatypeKeyword(name string) bool {
	return datatypeKeywords[name]
}
