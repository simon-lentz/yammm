package parse

import (
	"fmt"
	"strings"
	"sync"

	"github.com/alecthomas/participle/v2/lexer"
)

// rules is the complete YAMMM token vocabulary. The order is load-bearing, not
// stylistic; the package doc's Lexical discipline section states why, rule by
// rule, and no entry may be reordered without reading it.
var rules = lexer.Rules{"Root": {
	{Name: "WS", Pattern: `[ \t\r\n]+`},
	{Name: "DOC_COMMENT", Pattern: `/\*(?s:.)*?\*/`},
	{Name: "SL_COMMENT", Pattern: `//[^\r\n]*`},
	{Name: "REGEXP", Pattern: `/(?:\\(?s:.)|[^\\/\r\n])*/`},
	{Name: "STRING", Pattern: `"(?:\\[btnfrux0"'\\]|[^\\"\r\n])*"|'(?:\\[btnfrux0"'\\]|[^\\'\r\n])*'`},
	{Name: "VARIABLE", Pattern: `\$(?:[0-9]+|[a-z][a-zA-Z0-9_]*)`},
	{Name: "FLOAT", Pattern: `[0-9]+\.[0-9]+(?:[eE][-+]?[0-9]+)?\b`},
	{Name: "INVALID_NUMBER", Pattern: `[0-9]+(?:\.[0-9]+(?:[eE][-+]?[0-9]+)?)?[a-zA-Z_][a-zA-Z0-9_]*`},
	{Name: "INTEGER", Pattern: `[0-9]+`},
	{Name: "ASSOC", Pattern: `-->`},
	{Name: "COMP", Pattern: `\*->`},
	{Name: "ARROW", Pattern: `->`},
	{Name: "GTE", Pattern: `>=`},
	{Name: "LTE", Pattern: `<=`},
	{Name: "EQUAL", Pattern: `==`},
	{Name: "MATCH", Pattern: `=~`},
	{Name: "NOTEQUAL", Pattern: `!=`},
	{Name: "NOTMATCH", Pattern: `!~`},
	{Name: "AND", Pattern: `&&`},
	{Name: "OR", Pattern: `\|\|`},
	{Name: "ATAT", Pattern: `@@`},
	{Name: "UC_WORD", Pattern: `[A-Z][a-zA-Z0-9_]*`},
	{Name: "LC_WORD", Pattern: `[a-z][a-zA-Z0-9_]*`},
	{Name: "LBRACE", Pattern: `\{`},
	{Name: "RBRACE", Pattern: `\}`},
	{Name: "LBRACK", Pattern: `\[`},
	{Name: "RBRACK", Pattern: `\]`},
	{Name: "LPAR", Pattern: `\(`},
	{Name: "RPAR", Pattern: `\)`},
	{Name: "COLON", Pattern: `:`},
	{Name: "COMMA", Pattern: `,`},
	{Name: "EQUALS", Pattern: `=`},
	{Name: "SLASH", Pattern: `/`},
	{Name: "USCORE", Pattern: `_`},
	{Name: "STAR", Pattern: `\*`},
	{Name: "AT", Pattern: `@`},
	{Name: "EXCLAMATION", Pattern: `!`},
	{Name: "PLUS", Pattern: `\+`},
	{Name: "MINUS", Pattern: `-`},
	{Name: "QMARK", Pattern: `\?`},
	{Name: "GT", Pattern: `>`},
	{Name: "LT", Pattern: `<`},
	{Name: "DOLLAR", Pattern: `\$`},
	{Name: "PIPE", Pattern: `\|`},
	{Name: "PERIOD", Pattern: `\.`},
	{Name: "PERCENT", Pattern: `%`},
	{Name: "HAT", Pattern: `\^`},
	{Name: "ANY_OTHER", Pattern: `(?s:.)`},
}}

// Token is one lexed token with its byte extent. Kind names the rule that
// matched it, as spelled in the rule table.
type Token struct {
	Kind  string
	Value string
	Start int // byte offset, inclusive
	End   int // byte offset, exclusive
}

// Lex returns every token in src, in source order, with nothing elided —
// whitespace and comments included, EOF excluded. A caller needing the stream
// alongside the node tree uses [LexAndParse] instead, which returns both from
// one lex; Lex is the stream on its own.
//
// The lexer is total: no input fails to lex, because the last rule matches any
// single rune, so Lex reports no error and never rejects src.
func Lex(src string) []Token {
	ps := mustParsers()
	toks, err := lexAll(ps, src)
	if err != nil {
		panic("parse: " + err.Error())
	}
	out := make([]Token, 0, len(toks))
	for _, t := range toks {
		if t.EOF() {
			break
		}
		out = append(out, Token{
			Kind:  ps.names[t.Type],
			Value: t.Value,
			Start: t.Pos.Offset,
			End:   t.Pos.Offset + len(t.Value),
		})
	}
	return out
}

// definition compiles the rule table. Compilation happens once; the returned
// definition is immutable afterwards and safe for concurrent lexing.
func definition() (*lexer.StatefulDefinition, error) {
	def, err := lexer.New(rules)
	if err != nil {
		return nil, fmt.Errorf("compile lexer rules: %w", err)
	}
	return def, nil
}

// tokenTypes holds every rule name this package looks up, resolved once. A
// bare map lookup yields 0 for a name that is absent, and participle numbers
// rule types from -2 downward, so 0 matches nothing and a renamed rule would
// degrade silently instead of failing.
type tokenTypes struct {
	ws, slComment                   lexer.TokenType
	lbrace, rbrace, lbrack, rbrack  lexer.TokenType
	lpar, rpar                      lexer.TokenType
	arrow, period, qmark, colon     lexer.TokenType
	comma, pipe, minus, exclamation lexer.TokenType
	uscore, variable                lexer.TokenType
	str, integer, float, regexp     lexer.TokenType
	ucWord, lcWord, invalidNumber   lexer.TokenType
	docComment                      lexer.TokenType
}

// resolveTokens reports every name the rule table does not define, rather than
// the first, so a rename is fixed in one pass.
func resolveTokens(syms map[string]lexer.TokenType) (tokenTypes, error) {
	var t tokenTypes
	var missing []string
	get := func(name string) lexer.TokenType {
		tt, ok := syms[name]
		if !ok {
			missing = append(missing, name)
		}
		return tt
	}
	t.ws, t.slComment = get("WS"), get("SL_COMMENT")
	t.lbrace, t.rbrace = get("LBRACE"), get("RBRACE")
	t.lbrack, t.rbrack = get("LBRACK"), get("RBRACK")
	t.lpar, t.rpar = get("LPAR"), get("RPAR")
	t.arrow, t.period = get("ARROW"), get("PERIOD")
	t.qmark, t.colon = get("QMARK"), get("COLON")
	t.comma, t.pipe = get("COMMA"), get("PIPE")
	t.minus, t.exclamation = get("MINUS"), get("EXCLAMATION")
	t.uscore, t.variable = get("USCORE"), get("VARIABLE")
	t.str, t.integer = get("STRING"), get("INTEGER")
	t.float, t.regexp = get("FLOAT"), get("REGEXP")
	t.ucWord, t.lcWord = get("UC_WORD"), get("LC_WORD")
	t.invalidNumber = get("INVALID_NUMBER")
	t.docComment = get("DOC_COMMENT")
	if len(missing) > 0 {
		return t, fmt.Errorf("lexer rule table is missing %s", strings.Join(missing, ", "))
	}
	return t, nil
}

// tokenNames inverts the symbol table so a token type can be reported by the
// name its rule carries.
func tokenNames(syms map[string]lexer.TokenType) map[lexer.TokenType]string {
	out := make(map[lexer.TokenType]string, len(syms))
	for name, t := range syms {
		out[t] = name
	}
	return out
}

// lexerOnce guards the one-time build of the shared lexer definition and
// parser set. Everything it produces is read-only afterwards, which is what
// makes Parse and Lex safe for concurrent use.
var (
	lexerOnce sync.Once
	shared    *parsers
	sharedErr error
)
