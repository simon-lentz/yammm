package schema

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// model is the syntax-level representation of a YAMMM schema file. It carries
// only the information produced by parsing; semantic completion happens in
// internal/complete.
type model struct {
	Name          string
	Imports       []*importDecl
	Types         []*typeDecl
	DataTypes     []*dataTypeDecl
	Documentation string
	Span          location.Span
}

// importDecl represents an import statement in a schema file.
type importDecl struct {
	Path  string        // Raw path string (unescaped)
	Alias string        // Resolved alias (explicit or derived from path)
	Span  location.Span // Source location for diagnostics

	// PathRefused reports that the source's path literal has no value and is
	// already reported. An alias the source wrote is declared and names no
	// file: the loader looks nothing up for it and binds the alias as failed.
	// With no alias written, Alias is empty; see [importDecl.unnamed].
	PathRefused bool
	// PathRaw is the path literal as the source wrote it, quotes included; a
	// Builder's import, and a path with no value and no alias, have none. It
	// is what a message shows for a path that has no value.
	PathRaw string
}

// shownPath returns the path as a message shows it: the value quoted, or, for
// a path with no value, the literal as the source wrote it. A byte or a
// character of the literal that a message cannot carry, such as the NUL the
// path was refused for, is written as the escape %q gives it.
func (d *importDecl) shownPath() string {
	if !d.PathRefused {
		return strconv.Quote(d.Path)
	}
	var b strings.Builder
	for i := 0; i < len(d.PathRaw); {
		r, size := utf8.DecodeRuneInString(d.PathRaw[i:])
		if r == utf8.RuneError || !strconv.IsPrint(r) {
			quoted := strconv.Quote(d.PathRaw[i : i+size])
			b.WriteString(quoted[1 : len(quoted)-1])
		} else {
			b.WriteString(d.PathRaw[i : i+size])
		}
		i += size
	}
	return b.String()
}

// unnamed reports that the declaration has no alias: its path has no value to
// derive one from and the source wrote none. It is still an import
// declaration, which a context that takes no import refuses and counts, but
// nothing can refer to it, so the completer applies no alias rule to it. A
// Builder's import with an empty alias is not one: its path has a value, and
// the alias rules refuse it.
func (d *importDecl) unnamed() bool {
	return d.PathRefused && d.Alias == ""
}

// astTypeRef represents a reference to a type, possibly qualified by an import alias.
type astTypeRef struct {
	Qualifier string        // Empty for local types; import alias otherwise
	Name      string        // Type name (UC_WORD)
	Span      location.Span // Source location for diagnostics
}

// IsQualified returns true if the reference has an import qualifier.
func (r astTypeRef) IsQualified() bool {
	return r.Qualifier != ""
}

// String returns the fully qualified name (e.g., "parts.Wheel" or "Wheel").
func (r astTypeRef) String() string {
	if r.Qualifier != "" {
		return r.Qualifier + "." + r.Name
	}
	return r.Name
}

// typeDecl represents the raw declaration of a type in the DSL.
// Note: PluralName is not supported in v2.
type typeDecl struct {
	Name          string
	NameSpan      location.Span // Precise span of just the type name token
	Inherits      []*astTypeRef
	Properties    []*propertyDecl
	Relations     []*relationDecl
	Invariants    []*invariantDecl
	Annotations   []*annotationDecl // Type-level @@name(args) members
	IsPart        bool
	IsAbstract    bool
	Documentation string
	Span          location.Span // Span of the entire type declaration
}

// dataTypeDecl represents a named data type alias declaration in the DSL.
type dataTypeDecl struct {
	Name          string
	Constraint    Constraint // Typed constraint (not []string)
	Documentation string
	Span          location.Span
}

// propertyDecl mirrors the parsed property syntax before semantic validation.
type propertyDecl struct {
	Name          string
	Constraint    Constraint  // Typed constraint (not []string)
	DataTypeRef   DataTypeRef // Reference to DataType (for alias constraints)
	Optional      bool
	IsPrimaryKey  bool
	Documentation string
	Annotations   []*annotationDecl // Property-trailing @name(args) decorators
	Span          location.Span
}

// relationDecl captures association and composition declarations. Associations
// may embed additional edge properties; compositions do not.
// Note: Where clause is not supported in v2 (use edge properties instead).
type relationDecl struct {
	Kind          RelationKind
	Name          string
	Target        *astTypeRef
	Optional      bool
	Many          bool
	Properties    []*propertyDecl // Edge properties (associations only)
	Documentation string
	Span          location.Span
}

// invariantDecl wraps the parsed expression for an invariant attached to a
// type. The expression is kept in compiled form so the instance layer can
// evaluate it without re-parsing.
type invariantDecl struct {
	Name string
	Expr expr.Expression // Compiled expression; nil only from a Builder caller, which completion refuses
	// NoValue holds each literal in Expr that stands in for one with no
	// value, with the kind of literal the source wrote.
	NoValue       []parse.NoValueLiteral
	Documentation string
	Span          location.Span
}

// annotationDecl is the parsed form of a @name / @@name annotation, before
// semantic validation. Documentation is only populated for type-level (@@)
// annotations, which may carry a leading doc comment.
type annotationDecl struct {
	Name          string
	Args          []annotationArgDecl
	Documentation string
	Span          location.Span

	// DetachedFromLine is the source line of the property this annotation was
	// parsed as trailing, set only when the annotation begins on a LATER line
	// — the shape where what the author wrote and what the grammar binds can
	// disagree. Zero for a same-line annotation and for every type-level one.
	DetachedFromLine int
}

// annotationArgDecl is one parsed annotation argument. Token records whether
// the argument was an identifier or a literal so completion can distinguish
// "expected a reference, got a literal" from a bad identifier.
type annotationArgDecl struct {
	Text  string
	Token annotationTokenKind
	Span  location.Span

	// Raw is the source spelling of a quoted string, whose Text holds the
	// unquoted value; empty for every other kind.
	Raw string
}
