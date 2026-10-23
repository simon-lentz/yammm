package parse

import (
	"regexp"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// File is one parsed schema source. Parse always returns one, so a caller
// reads the diagnostics to learn what failed, never a nil node.
type File struct {
	// Name is the schema name with its quotes removed; NameRaw is the source
	// spelling. Name is empty when the literal could not be unquoted.
	Name     string
	NameRaw  string
	NameSpan location.Span

	// SchemaNameFailed reports that no usable schema name was produced: the
	// header did not parse, or it parsed and its literal did not unquote.
	SchemaNameFailed bool

	Doc  string
	Span location.Span // the schema header's extent, not the whole file

	// Unparsed holds the diagnostics for text the grammar does not match: a
	// construct that did not parse, and an escape the language does not have,
	// save in an invariant expression, where it is E_INVALID_INVARIANT, and in
	// an annotation argument, which keeps its written spelling.
	// They are in position order. Text the source rules refuse inside a
	// comment or a literal, and an escape whose value is not UTF-8, are not
	// among them: the token stream is whole beside either, and the tree keeps
	// its shape, save an invariant whose message has no value, which is left
	// out, and a literal with no value in an invariant expression, which a nil
	// literal stands in for. A reader of those two alone, as the formatter is,
	// refuses a source on Unparsed and on no other diagnostic.
	Unparsed []diag.Issue

	// Expressions holds the extent of every invariant expression the grammar
	// matched, in source order, which a formatter needs to leave an
	// expression's own spacing untouched. It holds the expression of an
	// invariant that is not in the tree because its message has no value.
	Expressions []location.Span

	Imports   []*Import
	DataTypes []*DataTypeDecl
	Types     []*TypeDecl
}

// Import is one import declaration. Alias holds only an alias the source wrote
// out; deriving one from the path is a loading decision and does not happen
// here. PathRefused reports that the path literal has no value, so Path is
// empty and names no file; the declaration is kept because its alias is still
// declared.
type Import struct {
	Path        string
	PathRefused bool
	PathRaw     string
	PathSpan    location.Span

	Alias     string
	HasAlias  bool
	AliasSpan location.Span

	Span location.Span
}

// DataTypeDecl is a named alias for a built-in constraint.
type DataTypeDecl struct {
	Name       string
	NameSpan   location.Span
	Constraint *Constraint
	Doc        string
	Span       location.Span
}

// TypeDecl is one type declaration and everything its body held.
type TypeDecl struct {
	Name       string
	NameSpan   location.Span
	IsAbstract bool
	IsPart     bool

	Extends     []*TypeRef
	Properties  []*Property
	Relations   []*Relation
	Invariants  []*Invariant
	Annotations []*Annotation // the type-level '@@' members

	Doc  string
	Span location.Span
}

// RelationKind distinguishes the two edge forms: an association points at an
// independent type, a composition owns its target.
type RelationKind uint8

const (
	RelationAssociation RelationKind = iota
	RelationComposition
)

// Relation is one association or composition. Optional and Many describe
// the forward direction. Properties holds edge properties, which only an
// association can carry.
type Relation struct {
	Kind     RelationKind
	Name     string
	NameSpan location.Span
	Target   *TypeRef

	Optional bool
	Many     bool

	Properties []*Property
	Doc        string
	Span       location.Span
}

// TypeRef names a type, optionally through an import alias.
type TypeRef struct {
	Qualifier string
	Name      string
	NameSpan  location.Span
	Span      location.Span
}

// Property is one type-body property. IsPrimaryKey and IsRequired are recorded
// as written; whether a primary key implies required is a decision for the
// layer above.
type Property struct {
	Name         string
	NameSpan     location.Span
	Constraint   *Constraint
	IsPrimaryKey bool
	IsRequired   bool
	Annotations  []*Annotation
	Doc          string
	Span         location.Span
}

// Invariant is one '!' member: its message and its expression. The byte
// extent of the expression alone is in [File.Expressions].
type Invariant struct {
	Message     string
	MessageRaw  string
	MessageSpan location.Span

	Expr expr.Expression // never nil: no Invariant is built without one
	// NoValue holds each literal in Expr that has no value. Each is a nil
	// literal the source did not write, and its own diagnostic is reported
	// where it stands.
	NoValue []NoValueLiteral

	Doc  string
	Span location.Span
}

// NoValueLiteral is a nil literal in an invariant expression that stands in
// for a literal with no value, and the kind of literal the source wrote there.
type NoValueLiteral struct {
	Literal *expr.Literal
	Kind    LiteralKind
}

// LiteralKind is the kind of literal token the source wrote.
type LiteralKind uint8

const (
	StringLiteral LiteralKind = iota + 1
	IntegerLiteral
	FloatLiteral
	RegexLiteral
)

// Annotation is one '@name(args)' or '@@name(args)' member. HasParens reports
// whether an argument list was parsed, so it is true exactly when Args is
// non-empty: the grammar requires at least one argument inside parentheses, so
// "@name()" does not parse and records the same shape as a bare "@name" plus
// its own E_SYNTAX. DetachedFromLine records the shape
// where what the author wrote and what the grammar binds can disagree: a
// trailing annotation binds to the property above it even across a line break,
// so an annotation starting on a later line carries that property's line
// number and every other annotation carries zero.
type Annotation struct {
	Name             string
	NameSpan         location.Span
	Args             []Arg
	HasParens        bool
	DetachedFromLine int

	Doc  string
	Span location.Span
}

// ArgKind distinguishes the three shapes an annotation argument can take, so a
// later phase can tell "expected a reference, got a literal" from a bad
// reference.
type ArgKind uint8

const (
	// ArgIdentifier is a bare word: a reference to something named.
	ArgIdentifier ArgKind = iota

	// ArgLiteral is a bare number, boolean or regex, and also a quoted string
	// that failed to unquote, so a broken literal never silently changes value.
	ArgLiteral

	// ArgString is a quoted string that unquoted cleanly; Text holds the value
	// and Raw the source spelling.
	ArgString
)

// Arg is one annotation argument.
type Arg struct {
	Kind ArgKind
	Text string
	Raw  string // source spelling of a quoted string; empty for every other kind
	Span location.Span
}

// ConstraintKind names which of the twelve datatype forms a constraint is.
type ConstraintKind uint8

// The datatype forms, in the order the grammar lists them.
const (
	ConstraintInteger ConstraintKind = iota
	ConstraintFloat
	ConstraintBoolean
	ConstraintString
	ConstraintEnum
	ConstraintPattern
	ConstraintTimestamp
	ConstraintDate
	ConstraintUUID
	ConstraintVector
	ConstraintList
	ConstraintAlias
)

// Bound is one written bound. Text is "_" when that side is unbounded, and Neg
// records a leading minus sign, which Span covers so a diagnostic about the
// bound anchors on the sign as well as the digits.
type Bound struct {
	Text string
	Neg  bool
	Span location.Span
}

// Bounds is a written bound pair with the extent of the pair as a whole.
type Bounds struct {
	Min  Bound
	Max  Bound
	Span location.Span
}

// Literal is one written literal argument of a constraint. Kept reports
// whether the value survived into the constraint: a literal that failed to
// unquote, holds text the source rules refuse, was empty, duplicated an
// earlier value, failed to compile, names a surrogate, or is a pattern after
// MaxPatterns others were kept is recorded here with its extent but does not
// reach the constraint itself. Text is empty for a literal with no
// value.
type Literal struct {
	Raw   string
	Text  string
	Regex *regexp.Regexp // compiled form; Pattern constraints only
	Span  location.Span
	Kept  bool
}

// IntLit is a written integer argument with its own extent.
type IntLit struct {
	Text string
	Span location.Span
}

// Constraint is a parsed datatype reference. It carries three things at once,
// and each has a distinct consumer: the values a constraint is built from, the
// written arguments with the extent every diagnostic anchors on, and the
// element or alias a compound form points at. Values are computed once, here,
// so nothing downstream re-parses a spelling.
type Constraint struct {
	Kind ConstraintKind
	Span location.Span

	// Written arguments.
	Bounds      *Bounds
	EnumLits    []Literal
	PatternLits []Literal
	FormatLit   *Literal
	DimsLit     *IntLit

	// Values, absent when the source omitted the bound or it did not parse.
	IntMin, IntMax     *int64
	FloatMin, FloatMax *float64
	LenMin, LenMax     *int64
	VectorDims         *int

	Elem  *Constraint // List
	Alias *TypeRef    // Alias
}

// EnumValues returns the enum values that survived into the constraint, in
// source order.
func (c *Constraint) EnumValues() []string {
	var out []string
	for _, lit := range c.EnumLits {
		if lit.Kept {
			out = append(out, lit.Text)
		}
	}
	return out
}

// PatternRegexps returns the compiled patterns that survived into the
// constraint, in source order.
func (c *Constraint) PatternRegexps() []*regexp.Regexp {
	var out []*regexp.Regexp
	for _, lit := range c.PatternLits {
		if lit.Kept {
			out = append(out, lit.Regex)
		}
	}
	return out
}

// Format returns the timestamp format and whether the source wrote one that
// unquoted cleanly.
func (c *Constraint) Format() (string, bool) {
	if c.FormatLit == nil || !c.FormatLit.Kept {
		return "", false
	}
	return c.FormatLit.Text, true
}
