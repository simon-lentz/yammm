package parse

import (
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Model is the syntax-level representation of a YAMMM schema file. It carries
// only the information produced by parsing; semantic completion happens in
// internal/complete.
type Model struct {
	Name          string
	Imports       []*ImportDecl
	Types         []*TypeDecl
	DataTypes     []*DataTypeDecl
	Documentation string
	Span          location.Span
}

// ImportDecl represents an import statement in a schema file.
type ImportDecl struct {
	Path  string        // Raw path string (unescaped)
	Alias string        // Resolved alias (explicit or derived from path)
	Span  location.Span // Source location for diagnostics
}

// TypeRef represents a reference to a type, possibly qualified by an import alias.
type TypeRef struct {
	Qualifier string        // Empty for local types; import alias otherwise
	Name      string        // Type name (UC_WORD)
	Span      location.Span // Source location for diagnostics
}

// IsQualified returns true if the reference has an import qualifier.
func (r TypeRef) IsQualified() bool {
	return r.Qualifier != ""
}

// String returns the fully qualified name (e.g., "parts.Wheel" or "Wheel").
func (r TypeRef) String() string {
	if r.Qualifier != "" {
		return r.Qualifier + "." + r.Name
	}
	return r.Name
}

// ToSchemaTypeRef converts to the public schema.TypeRef type.
func (r TypeRef) ToSchemaTypeRef() schema.TypeRef {
	return schema.NewTypeRef(r.Qualifier, r.Name, r.Span)
}

// TypeDecl represents the raw declaration of a type in the DSL.
// Note: PluralName is not supported in v2.
type TypeDecl struct {
	Name          string
	NameSpan      location.Span // Precise span of just the type name token
	Inherits      []*TypeRef
	Properties    []*PropertyDecl
	Relations     []*RelationDecl
	Invariants    []*InvariantDecl
	IsPart        bool
	IsAbstract    bool
	Documentation string
	Span          location.Span // Span of the entire type declaration
}

// DataTypeDecl represents a named data type alias declaration in the DSL.
type DataTypeDecl struct {
	Name          string
	Constraint    schema.Constraint // Typed constraint (not []string)
	Documentation string
	Span          location.Span
}

// PropertyDecl mirrors the parsed property syntax before semantic validation.
type PropertyDecl struct {
	Name          string
	Constraint    schema.Constraint  // Typed constraint (not []string)
	DataTypeRef   schema.DataTypeRef // Reference to DataType (for alias constraints)
	Optional      bool
	IsPrimaryKey  bool
	Documentation string
	Span          location.Span
}

// RelationKind enumerates the relation shapes supported by the DSL.
type RelationKind uint8

const (
	RelationAssociation RelationKind = iota
	RelationComposition
)

// String returns the string representation of the relation kind.
func (k RelationKind) String() string {
	switch k {
	case RelationAssociation:
		return "association"
	case RelationComposition:
		return "composition"
	default:
		return "unknown"
	}
}

// RelationDecl captures association and composition declarations. Associations
// may embed additional edge properties; compositions do not.
// Note: Where clause is not supported in v2 (use edge properties instead).
type RelationDecl struct {
	Kind            RelationKind
	Name            string
	Target          *TypeRef
	Optional        bool
	Many            bool
	Backref         string
	ReverseOptional bool
	ReverseMany     bool
	Properties      []*PropertyDecl // Edge properties (associations only)
	Documentation   string
	Span            location.Span
}

// InvariantDecl wraps the parsed expression for an invariant attached to a
// type. The expression is kept in compiled form so the instance layer can
// evaluate it without re-parsing.
type InvariantDecl struct {
	Name          string
	Expr          expr.Expression // Compiled expression (nil indicates parse failure)
	Documentation string
	Span          location.Span
}
