package parse

import (
	"fmt"

	"github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"

	"github.com/simon-lentz/yammm/schema/expr"
)

// The declaration grammar as participle structs. The loop in parse.go owns
// error recovery, not participle, so each parser below covers exactly one
// declaration-level construct and is invoked one construct at a time.
//
// The quoted literal groups spell out the reserved-word split by hand because
// struct tags are compile-time strings and cannot be composed from reservedLC
// and datatypeKeywords. TestTags_LookaheadGroupsAreAtTheirCanonicalSites is
// what keeps the copies honest.
//
// Every negative lookahead stays outside its capture group. At v2.1.4 that
// placement is not what makes it work: a (?! ...) group is zero-width, because
// lookaheadGroup.Parse parses into a throwaway branch and returns an empty
// match. The consuming form is negation, spelled !<expr>, which this grammar
// never uses. The placement is kept so that a version which stops being
// zero-width cannot start capturing the lookahead unnoticed.

// ucWordNode and lcWordNode capture one word token with its exact extent.
// Capturing straight into a lexer.Token field would take the raw stream's
// token, whitespace elision included, so spans come from these instead.
type ucWordNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Value  string `parser:"@UC_WORD"`
}

type lcWordNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Value  string `parser:"@LC_WORD"`
}

type schemaHeadNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Doc    *string `parser:"@DOC_COMMENT?"`
	Name   strTok  `parser:"'schema' @@"`
}

type importNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Path   strTok       `parser:"'import' @@"`
	Alias  *anyNameNode `parser:"('as' @@)?"`
}

// anyNameNode is a word in a position that admits either case and refuses
// every reserved spelling: an import alias, a relation name, a reverse name.
type anyNameNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	LC     *string `parser:"  (?! 'schema' | 'type' | 'datatype' | 'required' | 'primary' | 'extends' | 'includes' | 'abstract' | 'one' | 'many' | 'import' | 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @LC_WORD"`
	UC     *string `parser:"| (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @UC_WORD"`
}

type dtDeclNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Doc    *string     `parser:"@DOC_COMMENT?"`
	Name   ucWordNode  `parser:"'type' (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
	C      builtinNode `parser:"'=' @@"`
}

type typeHeadNode struct {
	Pos      lexer.Position
	EndPos   lexer.Position
	Doc      *string      `parser:"@DOC_COMMENT?"`
	Abstract bool         `parser:"( @'abstract'"`
	Part     bool         `parser:"| @'part' )?"`
	Name     ucWordNode   `parser:"'type' (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
	Extends  *extendsNode `parser:"@@?"`
	Open     bool         `parser:"@'{'"`
}

type extendsNode struct {
	Refs []typeRefNode `parser:"'extends' @@ (',' @@)* ','?"`
}

type typeRefNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Qual   *string    `parser:"(((?! 'schema' | 'type' | 'datatype' | 'required' | 'primary' | 'extends' | 'includes' | 'abstract' | 'one' | 'many' | 'import' | 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @LC_WORD | (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @UC_WORD) '.')?"`
	Name   ucWordNode `parser:"(?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
}

// memberNode is one type-body member. Alternatives are tried in order with
// backtracking, so the shared optional DOC_COMMENT prefix disambiguates one
// token later.
type memberNode struct {
	Inv   *invNode     `parser:"  @@"`
	TAnn  *typeAnnNode `parser:"| @@"`
	Assoc *assocNode   `parser:"| @@"`
	Comp  *compNode    `parser:"| @@"`
	Prop  *propNode    `parser:"| @@"`
}

// assocNode and compNode stay separate rather than sharing one node with a
// kind flag, because only an association takes a body. Merging them would
// accept a composition with edge properties, which the language does not.
type assocNode struct {
	Pos     lexer.Position
	EndPos  lexer.Position
	Doc     *string      `parser:"@DOC_COMMENT?"`
	Name    anyNameNode  `parser:"'-->' @@"`
	Mult    *multNode    `parser:"@@?"`
	Target  typeRefNode  `parser:"@@"`
	Reverse *reverseNode `parser:"@@?"`
	Body    *relBodyNode `parser:"@@?"`
}

type compNode struct {
	Pos     lexer.Position
	EndPos  lexer.Position
	Doc     *string      `parser:"@DOC_COMMENT?"`
	Name    anyNameNode  `parser:"'*->' @@"`
	Mult    *multNode    `parser:"@@?"`
	Target  typeRefNode  `parser:"@@"`
	Reverse *reverseNode `parser:"@@?"`
}

type reverseNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Name   anyNameNode `parser:"'/' @@"`
	Mult   *multNode   `parser:"@@?"`
}

// multNode admits exactly seven spellings. '_' and 'one' each take an optional
// ':one' or ':many' tail; 'many' takes none, so "(many:one)" is not a
// multiplicity.
type multNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Head   string  `parser:"'(' ( @('_' | 'one')"`
	Tail   *string `parser:"(':' @('one' | 'many'))?"`
	Many   bool    `parser:"| @'many' ) ')'"`
}

type relBodyNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Props  []relPropNode `parser:"'{' @@* '}'"`
}

// relPropNode is one edge property. It admits 'required' and nothing else — no
// primary key, no trailing annotations.
type relPropNode struct {
	Pos      lexer.Position
	EndPos   lexer.Position
	Doc      *string        `parser:"@DOC_COMMENT?"`
	Name     lcWordNode     `parser:"(?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Type     constraintNode `parser:"@@"`
	Required bool           `parser:"( @'required' (?! UC_WORD | LC_WORD '.') )?"`
}

type invNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Doc    *string     `parser:"@DOC_COMMENT?"`
	Msg    strTok      `parser:"'!' @@"`
	Expr   exprCapture `parser:"@@"`
}

type typeAnnNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Doc    *string    `parser:"@DOC_COMMENT?"`
	Name   lcWordNode `parser:"'@@' (?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Args   *argsNode  `parser:"@@?"`
}

// propNode's modifier alternatives carry a two-token lookahead. Without it a
// trailing 'primary' or 'required' is ambiguous with the next property's name,
// and the committed-choice parser takes the wrong branch.
type propNode struct {
	Pos      lexer.Position
	EndPos   lexer.Position
	Doc      *string        `parser:"@DOC_COMMENT?"`
	Name     lcWordNode     `parser:"(?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Type     constraintNode `parser:"@@"`
	Primary  bool           `parser:"( @'primary' (?! UC_WORD | LC_WORD '.')"`
	Required bool           `parser:"| @'required' (?! UC_WORD | LC_WORD '.') )?"`
	Anns     []annNode      `parser:"@@*"`
}

type annNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Name   lcWordNode `parser:"'@' (?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Args   *argsNode  `parser:"@@?"`
}

type argsNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Args   []argNode `parser:"'(' @@ (',' @@)* ','? ')'"`
}

type argNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Ident  *string `parser:"(?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @LC_WORD"`
	UC     *string `parser:"| (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @UC_WORD"`
	Lit    *string `parser:"| @(STRING | FLOAT | INTEGER | REGEXP | 'true' | 'false')"`
}

// constraintNode is the built-in-or-alias split a property's datatype admits.
// builtinNode alone is the datatype-alias target: an alias may not alias
// another alias.
type constraintNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	B      *builtinNode `parser:"  @@"`
	Ali    *aliasC      `parser:"| @@"`
}

type builtinNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Int    *intC  `parser:"  @@"`
	Flt    *fltC  `parser:"| @@"`
	Boo    *booC  `parser:"| @@"`
	Str    *strC  `parser:"| @@"`
	Enu    *enuC  `parser:"| @@"`
	Pat    *patC  `parser:"| @@"`
	Tim    *timC  `parser:"| @@"`
	Dat    *datC  `parser:"| @@"`
	UUID   *uuidC `parser:"| @@"`
	Vec    *vecC  `parser:"| @@"`
	Lst    *lstC  `parser:"| @@"`
}

type intC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool        `parser:"@'Integer'"`
	Bounds *numBoundsC `parser:"@@?"`
}

type fltC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool        `parser:"@'Float'"`
	Bounds *numBoundsC `parser:"@@?"`
}

// numBoundsC is the signed numeric bound pair. Each bound keeps its own extent
// so a diagnostic can anchor on the bound token and its minus sign together.
type numBoundsC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	MinTok boundTok `parser:"'[' @@"`
	MaxTok boundTok `parser:"',' @@ ']'"`
}

type boundTok struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Neg    bool   `parser:"@'-'?"`
	Text   string `parser:"@('_' | INTEGER | FLOAT)"`
}

type booC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool `parser:"@'Boolean'"`
}

type strC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool        `parser:"@'String'"`
	Bounds *lenBoundsC `parser:"@@?"`
}

// lenBoundsC is the unsigned length bound pair. Length bounds admit no leading
// minus, which is why they have no negation group.
type lenBoundsC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	MinTok lenTok `parser:"'[' @@"`
	MaxTok lenTok `parser:"',' @@ ']'"`
}

type lenTok struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Text   string `parser:"@('_' | INTEGER)"`
}

type enuC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool     `parser:"@'Enum'"`
	Values []strTok `parser:"'[' @@ (',' @@)+ ','? ']'"`
}

type patC struct {
	Pos      lexer.Position
	EndPos   lexer.Position
	Kw       bool     `parser:"@'Pattern'"`
	Patterns []strTok `parser:"'[' @@ (',' @@)? ']'"`
}

// strTok is one STRING token with its own extent, so per-value diagnostics
// anchor on the value rather than on the whole constraint.
type strTok struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Raw    string `parser:"@STRING"`
}

type timC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool    `parser:"@'Timestamp'"`
	Format *strTok `parser:"('[' @@ ']')?"`
}

type datC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool `parser:"@'Date'"`
}

type uuidC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool `parser:"@'UUID'"`
}

type vecC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool   `parser:"@'Vector'"`
	Dims   intTok `parser:"'[' @@ ']'"`
}

type intTok struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Text   string `parser:"@INTEGER"`
}

type lstC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool            `parser:"@'List'"`
	Elem   *constraintNode `parser:"'<' @@ '>'"`
	Bounds *lenBoundsC     `parser:"@@?"`
}

type aliasC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Qual   *string    `parser:"(((?! 'schema' | 'type' | 'datatype' | 'required' | 'primary' | 'extends' | 'includes' | 'abstract' | 'one' | 'many' | 'import' | 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @LC_WORD | (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @UC_WORD) '.')?"`
	Name   ucWordNode `parser:"(?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
}

// exprCapture is the interface ParseTypeWith is registered for. The concrete
// exprResult carries the compiled expression, its byte extent, and any
// diagnostics raised while compiling literals.
type exprCapture interface{ exprCapture() }

type exprResult struct {
	E          expr.Expression
	Diags      []exprDiag
	Start, End int
}

func (exprResult) exprCapture() {}

// parsers bundles the one-per-construct participle parsers over a single
// shared lexer definition.
type parsers struct {
	def        *lexer.StatefulDefinition
	tok        tokenTypes
	names      map[lexer.TokenType]string
	elide      []lexer.TokenType
	schemaHead *participle.Parser[schemaHeadNode]
	imp        *participle.Parser[importNode]
	dtDecl     *participle.Parser[dtDeclNode]
	typeHead   *participle.Parser[typeHeadNode]
	member     *participle.Parser[memberNode]
}

// mustParsers returns the shared parser set, building it on first use. A
// failure means a defect in this package's own rule table or struct tags, not
// in any input, so it cannot be handled by a caller and is not worth a return
// value on every entry point. TestParsersBuild keeps the path unreachable.
func mustParsers() *parsers {
	lexerOnce.Do(buildParsers)
	if sharedErr != nil {
		panic("parse: " + sharedErr.Error())
	}
	return shared
}

func buildParsers() {
	def, err := definition()
	if err != nil {
		sharedErr = err
		return
	}
	syms := def.Symbols()
	tok, err := resolveTokens(syms)
	if err != nil {
		sharedErr = err
		return
	}
	exprOpt := participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (exprCapture, error) {
		var diags []exprDiag
		p := newExprParser(lx, &diags, tok)
		start := lx.Peek().Pos.Offset
		e, err := p.parse(0)
		if err != nil {
			return nil, err
		}
		return exprResult{E: e, Diags: diags, Start: start, End: p.lastEnd}, nil
	})
	opts := []participle.Option{
		participle.Lexer(def),
		participle.Elide("WS", "SL_COMMENT"),
		exprOpt,
		participle.UseLookahead(2),
	}
	built := &parsers{
		def:   def,
		tok:   tok,
		names: tokenNames(syms),
		elide: []lexer.TokenType{tok.ws, tok.slComment},
	}
	if built.schemaHead, err = participle.Build[schemaHeadNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build schema-header parser: %w", err)
		return
	}
	if built.imp, err = participle.Build[importNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build import parser: %w", err)
		return
	}
	if built.dtDecl, err = participle.Build[dtDeclNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build datatype parser: %w", err)
		return
	}
	if built.typeHead, err = participle.Build[typeHeadNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build type-header parser: %w", err)
		return
	}
	if built.member, err = participle.Build[memberNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build member parser: %w", err)
		return
	}
	shared = built
}
