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
	Alias  aliasCapture `parser:"@@"`
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
	Doc      *string        `parser:"@DOC_COMMENT?"`
	Abstract bool           `parser:"( @'abstract'"`
	Part     bool           `parser:"| @'part' )?"`
	Name     ucWordNode     `parser:"'type' (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
	Extends  extendsCapture `parser:"@@"`
	Open     bool           `parser:"@'{'"`
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
	Doc     *string        `parser:"@DOC_COMMENT?"`
	Name    anyNameNode    `parser:"'-->' @@"`
	Mult    multCapture    `parser:"@@"`
	Target  typeRefNode    `parser:"@@"`
	Reverse reverseCapture `parser:"@@"`
	Body    relBodyCapture `parser:"@@"`
}

type compNode struct {
	Pos     lexer.Position
	EndPos  lexer.Position
	Doc     *string        `parser:"@DOC_COMMENT?"`
	Name    anyNameNode    `parser:"'*->' @@"`
	Mult    multCapture    `parser:"@@"`
	Target  typeRefNode    `parser:"@@"`
	Reverse reverseCapture `parser:"@@"`
}

type reverseNode struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Name   anyNameNode `parser:"'/' @@"`
	Mult   multCapture `parser:"@@"`
}

// reverseCapture is the reverse clause's outcome.
type reverseCapture interface{ reverseCapture() }

type reverseOutcome struct{ groupOutcome[reverseNode] }

func (reverseOutcome) reverseCapture() {}

// numBoundsCapture is the numeric bound list's outcome.
type numBoundsCapture interface{ numBoundsCapture() }

type numBoundsOutcome struct{ groupOutcome[numBoundsC] }

func (numBoundsOutcome) numBoundsCapture() {}

// timFormatCapture is the timestamp format's outcome.
type timFormatCapture interface{ timFormatCapture() }

type timFormatOutcome struct{ groupOutcome[timFormatNode] }

func (timFormatOutcome) timFormatCapture() {}

// aliasCapture is the import alias's outcome.
type aliasCapture interface{ aliasCapture() }

type aliasOutcome struct{ groupOutcome[aliasGroupNode] }

func (aliasOutcome) aliasCapture() {}

// relBodyCapture is the association body's outcome.
type relBodyCapture interface{ relBodyCapture() }

type relBodyOutcome struct{ groupOutcome[relBodyNode] }

func (relBodyOutcome) relBodyCapture() {}

// argsCapture is the annotation argument list's outcome.
type argsCapture interface{ argsCapture() }

type argsOutcome struct{ groupOutcome[argsNode] }

func (argsOutcome) argsCapture() {}

// extendsCapture is the extends clause's outcome.
type extendsCapture interface{ extendsCapture() }

type extendsOutcome struct{ groupOutcome[extendsNode] }

func (extendsOutcome) extendsCapture() {}

// multCapture is the multiplicity group's outcome.
type multCapture interface{ multCapture() }

type multOutcome struct{ groupOutcome[multNode] }

func (multOutcome) multCapture() {}

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
	Doc    *string     `parser:"@DOC_COMMENT?"`
	Name   lcWordNode  `parser:"'@@' (?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Args   argsCapture `parser:"@@"`
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
	Name   lcWordNode  `parser:"'@' (?! 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @@"`
	Args   argsCapture `parser:"@@"`
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
	Kw     bool             `parser:"@'Integer'"`
	Bounds numBoundsCapture `parser:"@@"`
}

type fltC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Kw     bool             `parser:"@'Float'"`
	Bounds numBoundsCapture `parser:"@@"`
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
	Kw     bool             `parser:"@'String'"`
	Bounds lenBoundsCapture `parser:"@@"`
}

// lenBoundsCapture is the outcome of the optional length-bound group.
type lenBoundsCapture interface{ lenBoundsCapture() }

type lenBoundsOutcome struct{ groupOutcome[lenBoundsC] }

func (lenBoundsOutcome) lenBoundsCapture() {}

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
	Kw     bool             `parser:"@'Timestamp'"`
	Format timFormatCapture `parser:"@@"`
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
	Kw     bool             `parser:"@'List'"`
	Elem   *constraintNode  `parser:"'<' @@ '>'"`
	Bounds lenBoundsCapture `parser:"@@"`
}

type aliasC struct {
	Pos    lexer.Position
	EndPos lexer.Position
	Qual   *string    `parser:"(((?! 'schema' | 'type' | 'datatype' | 'required' | 'primary' | 'extends' | 'includes' | 'abstract' | 'one' | 'many' | 'import' | 'as' | 'part' | 'in' | 'nil' | 'true' | 'false') @LC_WORD | (?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @UC_WORD) '.')?"`
	Name   ucWordNode `parser:"(?! 'Integer' | 'Float' | 'Boolean' | 'String' | 'Enum' | 'Pattern' | 'Timestamp' | 'Date' | 'UUID' | 'Vector' | 'List') @@"`
}

// timFormatNode and aliasGroupNode give a node to the two groups the grammar
// would otherwise write inline, because a capture needs a type to parse into.
type timFormatNode struct {
	F strTok `parser:"'[' @@ ']'"`
}

type aliasGroupNode struct {
	A anyNameNode `parser:"'as' @@"`
}

// An optional group written '@@?' is abandoned whole when its inner parse
// fails, so the enclosing node succeeds as though the author wrote nothing and
// the real error is lost. Every such group is instead a capture that always
// succeeds and records which of three things happened. Absence and failure stop
// being the same value, which is what '?' cannot express.

// outcome unwraps a capture field. A capture is populated by its own parse
// function, so the zero outcome only appears where a sub-production's parser
// carries the field but never reaches it.
func outcome[O any](capture any) O {
	o, ok := capture.(O)
	if !ok {
		var zero O
		return zero
	}
	return o
}

// groupOutcome is one optional group's result: V when it parsed, Err and the
// marker offset when it was entered and failed, both zero when it was absent.
type groupOutcome[T any] struct {
	V   *T
	Err error
	At  int
}

// resyncFunc leaves the lexer where the enclosing construct can keep parsing
// after its group failed. cp is the position the marker was seen at.
type resyncFunc func(lx *lexer.PeekingLexer, cp lexer.Checkpoint)

// enterGroup runs the group's own parser when its marker is present. A group
// that is entered always reports: participle's own error, at the token inside
// the group that raised it.
func enterGroup[T any](lx *lexer.PeekingLexer, marker string, p *participle.Parser[T], resync resyncFunc) groupOutcome[T] {
	t := lx.Peek()
	if t.Value != marker {
		return groupOutcome[T]{}
	}
	at := t.Pos.Offset
	cp := lx.MakeCheckpoint()
	v, err := p.ParseFromLexer(lx, participle.AllowTrailing(true))
	if err != nil {
		resync(lx, cp)
		return groupOutcome[T]{Err: err, At: at}
	}
	return groupOutcome[T]{V: v}
}

// skipTo resyncs a delimited group by restoring its start and consuming through
// its closing token, EOF ending the scan. No group nests inside itself, so the
// first closer is always the group's own. An unterminated group therefore
// consumes the enclosing construct's closer, which is what production does.
func skipTo(closer string) resyncFunc {
	return func(lx *lexer.PeekingLexer, cp lexer.Checkpoint) {
		lx.LoadCheckpoint(cp)
		for !lx.Peek().EOF() {
			if lx.Next().Value == closer {
				return
			}
		}
	}
}

// keepFailure resyncs a group with no closing token: what the inner parse
// consumed stays consumed, and the token it failed on goes with it, so the
// enclosing construct does not re-report the same spot.
func keepFailure(lx *lexer.PeekingLexer, _ lexer.Checkpoint) {
	if t := lx.Peek(); !t.EOF() && t.Value != "}" {
		lx.Next()
	}
}

// dropMarker resyncs a group whose enclosing construct still needs the token
// the inner parse failed on. Only the marker is consumed, so the construct
// carries on and its declaration survives the group's failure.
func dropMarker(lx *lexer.PeekingLexer, cp lexer.Checkpoint) {
	lx.LoadCheckpoint(cp)
	if !lx.Peek().EOF() {
		lx.Next()
	}
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

	// One parser per optional group, so a group can parse itself and report
	// what failed inside it rather than being abandoned whole.
	numBounds *participle.Parser[numBoundsC]
	lenBounds *participle.Parser[lenBoundsC]
	timFormat *participle.Parser[timFormatNode]
	aliasTail *participle.Parser[aliasGroupNode]
	reverse   *participle.Parser[reverseNode]
	relBody   *participle.Parser[relBodyNode]
	args      *participle.Parser[argsNode]
	extends   *participle.Parser[extendsNode]
	mult      *participle.Parser[multNode]
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

// elidedTokens names the kinds the grammar never sees. Both the Elide option
// and the token types Parse hands participle are derived from this one list,
// because participle reads its elide set off the PeekingLexer the caller
// built — so a second hand-kept copy would silently be the live one.
var elidedTokens = []string{"WS", "SL_COMMENT"}

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
	elide := make([]lexer.TokenType, 0, len(elidedTokens))
	for _, name := range elidedTokens {
		tt, ok := syms[name]
		if !ok {
			sharedErr = fmt.Errorf("elided token %q is not in the rule table", name)
			return
		}
		elide = append(elide, tt)
	}
	built := &parsers{
		def:   def,
		tok:   tok,
		names: tokenNames(syms),
		elide: elide,
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
	brackets, parens, braces := skipTo("]"), skipTo(")"), skipTo("}")
	groupOpts := []participle.Option{
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (lenBoundsCapture, error) {
			return lenBoundsOutcome{enterGroup(lx, "[", built.lenBounds, brackets)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (numBoundsCapture, error) {
			return numBoundsOutcome{enterGroup(lx, "[", built.numBounds, brackets)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (timFormatCapture, error) {
			return timFormatOutcome{enterGroup(lx, "[", built.timFormat, brackets)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (argsCapture, error) {
			return argsOutcome{enterGroup(lx, "(", built.args, parens)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (multCapture, error) {
			return multOutcome{enterGroup(lx, "(", built.mult, parens)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (relBodyCapture, error) {
			return relBodyOutcome{enterGroup(lx, "{", built.relBody, braces)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (reverseCapture, error) {
			return reverseOutcome{enterGroup(lx, "/", built.reverse, keepFailure)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (aliasCapture, error) {
			return aliasOutcome{enterGroup(lx, "as", built.aliasTail, keepFailure)}, nil
		}),
		participle.ParseTypeWith(func(lx *lexer.PeekingLexer) (extendsCapture, error) {
			return extendsOutcome{enterGroup(lx, "extends", built.extends, dropMarker)}, nil
		}),
	}
	opts := []participle.Option{
		participle.Lexer(def),
		participle.Elide(elidedTokens...),
		exprOpt,
		participle.UseLookahead(2),
	}
	opts = append(opts, groupOpts...)
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
	for _, b := range []struct {
		name  string
		build func() error
	}{
		{"numeric bounds", func() (e error) { built.numBounds, e = participle.Build[numBoundsC](opts...); return }},
		{"length bounds", func() (e error) { built.lenBounds, e = participle.Build[lenBoundsC](opts...); return }},
		{"timestamp format", func() (e error) { built.timFormat, e = participle.Build[timFormatNode](opts...); return }},
		{"import alias", func() (e error) { built.aliasTail, e = participle.Build[aliasGroupNode](opts...); return }},
		{"reverse clause", func() (e error) { built.reverse, e = participle.Build[reverseNode](opts...); return }},
		{"relation body", func() (e error) { built.relBody, e = participle.Build[relBodyNode](opts...); return }},
		{"annotation arguments", func() (e error) { built.args, e = participle.Build[argsNode](opts...); return }},
		{"extends clause", func() (e error) { built.extends, e = participle.Build[extendsNode](opts...); return }},
		{"multiplicity", func() (e error) { built.mult, e = participle.Build[multNode](opts...); return }},
	} {
		if err := b.build(); err != nil {
			sharedErr = fmt.Errorf("build %s parser: %w", b.name, err)
			return
		}
	}
	if built.member, err = participle.Build[memberNode](opts...); err != nil {
		sharedErr = fmt.Errorf("build member parser: %w", err)
		return
	}
	shared = built
}
