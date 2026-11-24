package parse

import (
	"errors"
	"slices"

	"github.com/alecthomas/participle/v2/lexer"
)

// MaxNesting is the most levels an expression's tree and a List's element
// nesting each hold. Every walk of either recurses a bounded number of times
// per level, so the bound keeps a source of any size inside the stack. The schema layer holds a List to
// it through the datatypes the List names, which no one type expression shows.
const MaxNesting = 1000

// listLevel is the interface ParseTypeWith is registered for at a List's
// element. Its parse function reads no token: it fails an element nested past
// MaxNesting before the grammar recurses into it.
type listLevel interface{ listLevel() }

type listLevelOK struct{}

func (listLevelOK) listLevel() {}

// errListTooDeep fails the construct that holds the List. participle keeps
// another branch's error over it, so [builder.syntaxErr] names the defect from
// the tokens, as it names a malformed number.
var errListTooDeep = errors.New("a List nests too deeply")

// checkListLevel counts the `List <` pairs that end at the lexer's position,
// which are the Lists the element there is nested in.
func checkListLevel(lx *lexer.PeekingLexer, tok tokenTypes) (listLevel, error) {
	level, opened := 0, false
	for _, t := range slices.Backward(lx.Range(0, lx.RawCursor())) {
		switch {
		case t.Type == tok.ws || t.Type == tok.slComment:
			continue
		case !opened && t.Value == "<":
			opened = true
			continue
		case opened && tok.isList(&t):
			opened = false
			if level++; level > MaxNesting {
				return nil, errListTooDeep
			}
			continue
		}
		break
	}
	return listLevelOK{}, nil
}

func (tok tokenTypes) isList(t *lexer.Token) bool {
	return t.Type == tok.ucWord && t.Value == "List"
}

// firstDeepList returns the first List keyword starting in [from, to) that is
// nested in MaxNesting others, or nil. [checkListLevel] fails at that token,
// and a construct that failed for another cause can hold one too.
func (b *builder) firstDeepList(from, to int) *lexer.Token {
	tok := b.ps.tok
	i, _ := slices.BinarySearchFunc(b.toks, from, func(t lexer.Token, target int) int {
		return t.Pos.Offset - target
	})
	level, opened := 0, true
	for ; i < len(b.toks); i++ {
		t := &b.toks[i]
		switch {
		case t.EOF() || t.Pos.Offset >= to:
			return nil
		case t.Type == tok.ws || t.Type == tok.slComment:
		case opened && tok.isList(t):
			if level++; level > MaxNesting {
				return t
			}
			opened = false
		case !opened && t.Value == "<":
			opened = true
		case tok.isList(t):
			level, opened = 1, false
		default:
			level, opened = 0, true
		}
	}
	return nil
}

// The states of [skipExpression].
const (
	skipOperand  = iota // an operand or a prefix operator is next
	skipOperator        // an operator is next, or the expression ends
	skipCallName        // as skipOperator, after a call's name
	skipCallArgs        // as skipOperator, after a call's arguments
	skipCallBody        // as skipOperator, after a call's parameters
)

// What a bracket [skipExpression] is inside holds.
const (
	skipOne  = iota // one expression: parentheses, an index, a body, an else branch
	skipList        // a list literal's elements
	skipArgs        // a call's arguments
	skipThen        // a conditional's first branch, which a colon ends
)

// skipBracket is a bracket skipExpression is inside.
type skipBracket struct {
	holds int
	close lexer.TokenType
	after int // the state its close returns to
}

// What the token before the one [skipExpression] reads did to a list or an
// argument list: the two close only where an element can start.
const (
	skipInside = iota // neither
	skipOpened        // opened it
	skipComma         // separated two of its elements
)

// skipExpression moves lx from an expression's first token to the first token
// the expression parser does not read, with no call per level, and returns that
// token's offset. whole reports that a well-formed expression ends there, and
// else the parser fails at that token: the scan reads what [exprParser.parse]
// reads.
func skipExpression(lx *lexer.PeekingLexer, tok tokenTypes) (end int, whole bool) {
	var open []skipBracket
	state, last := skipOperand, skipInside
	push := func(holds int, closer lexer.TokenType, after int) {
		open = append(open, skipBracket{holds: holds, close: closer, after: after})
		state, last = skipOperand, skipInside
		if holds == skipList || holds == skipArgs {
			last = skipOpened
		}
	}
	for {
		t := lx.Peek()
		stop := func() (int, bool) { return t.Pos.Offset, len(open) == 0 && state != skipOperand }
		var in *skipBracket
		if len(open) > 0 {
			in = &open[len(open)-1]
		}
		lists := in != nil && (in.holds == skipList || in.holds == skipArgs)
		before := last
		last = skipInside
		switch {
		case t.EOF():
			return stop()
		case state == skipOperand:
			switch {
			case t.Type == tok.minus, t.Type == tok.exclamation:
			case t.Type == tok.lpar:
				push(skipOne, tok.rpar, skipOperator)
			case t.Type == tok.lbrack:
				push(skipList, tok.rbrack, skipOperator)
			case lists && t.Type == in.close && before != skipInside:
				state, open = in.after, open[:len(open)-1]
			case in != nil && in.holds == skipArgs && t.Type == tok.comma && before == skipOpened:
				// A call takes one comma and no argument.
				lx.Next()
				if closer := lx.Peek(); closer.Type != in.close {
					return closer.Pos.Offset, false
				}
				state, open = in.after, open[:len(open)-1]
			case t.Type == tok.ucWord && datatypeKeywords[t.Value],
				t.Type == tok.lcWord && (t.Value == "as" || t.Value == "part" || t.Value == "in"):
				return stop()
			case t.Type == tok.variable, t.Type == tok.uscore, t.Type == tok.integer, t.Type == tok.float,
				t.Type == tok.str, t.Type == tok.regexp, t.Type == tok.ucWord, t.Type == tok.lcWord:
				state = skipOperator
			default:
				return stop()
			}
		case in != nil && t.Type == in.close:
			if in.holds == skipThen {
				return stop()
			}
			state, open = in.after, open[:len(open)-1]
		case lists && t.Type == tok.comma:
			state, last = skipOperand, skipComma
		case in != nil && in.holds == skipThen && t.Type == tok.colon:
			in.holds, state = skipOne, skipOperand
		case t.Type == tok.lbrack:
			push(skipOne, tok.rbrack, skipOperator)
		case t.Type == tok.arrow, t.Type == tok.period:
			lx.Next()
			if name := lx.Peek(); name.Type != tok.ucWord && name.Type != tok.lcWord {
				return name.Pos.Offset, false
			}
			state = skipOperator
			if t.Type == tok.arrow {
				state = skipCallName
			}
		case t.Type == tok.qmark:
			lx.Next()
			if brace := lx.Peek(); brace.Type != tok.lbrace {
				return brace.Pos.Offset, false
			}
			push(skipThen, tok.rbrace, skipOperator)
		case t.Type == tok.lpar && state == skipCallName:
			push(skipArgs, tok.rpar, skipCallArgs)
		case t.Type == tok.pipe && (state == skipCallName || state == skipCallArgs):
			if bad, ok := skipParameters(lx, tok); !ok {
				return bad, false
			}
			state = skipCallBody
			continue
		case t.Type == tok.lbrace && state != skipOperator:
			push(skipOne, tok.rbrace, skipOperator)
		default:
			if _, binary := binaryPrec[t.Value]; !binary {
				return stop()
			}
			state = skipOperand
		}
		lx.Next()
	}
}

// skipParameters moves lx past a lambda's parameters, whose opening pipe is
// next, as [exprParser.lambdaParams] reads them. It returns false with the
// offset of the token that parser refuses.
func skipParameters(lx *lexer.PeekingLexer, tok tokenTypes) (int, bool) {
	lx.Next()
	for {
		if name := lx.Peek(); name.Type != tok.variable {
			return name.Pos.Offset, false
		}
		lx.Next()
		if lx.Peek().Type != tok.comma {
			break
		}
		lx.Next()
		if lx.Peek().Type == tok.pipe {
			break
		}
	}
	if closer := lx.Peek(); closer.Type != tok.pipe {
		return closer.Pos.Offset, false
	}
	lx.Next()
	return 0, true
}
