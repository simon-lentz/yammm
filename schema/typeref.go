package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// TypeRef is a syntactic reference to a type, preserving what was written in
// the schema source. It captures the optional import qualifier and type name.
//
// TypeRef is used for:
//   - Parsing and error messages (shows user's original syntax)
//   - Preserving import qualification for tooling (go-to-definition)
//   - Displaying type references in diagnostics
//
// For semantic equality (comparing types), use TypeID instead.
type TypeRef struct {
	qualifier string        // import alias, empty for local types
	name      string        // type name
	span      location.Span // source location for diagnostics
}

// newTypeRef creates a TypeRef with the given qualifier, name, and span.
func newTypeRef(qualifier, name string, span location.Span) TypeRef {
	return TypeRef{qualifier: qualifier, name: name, span: span}
}

// localTypeRef creates a TypeRef for a local (unqualified) type.
func localTypeRef(name string, span location.Span) TypeRef {
	return TypeRef{name: name, span: span}
}

// Qualifier returns the import alias, or empty string for local types.
func (r TypeRef) Qualifier() string {
	return r.qualifier
}

// Name returns the type name.
func (r TypeRef) Name() string {
	return r.name
}

// Span returns the source location of this type reference.
func (r TypeRef) Span() location.Span {
	return r.span
}

// IsZero reports whether this is the zero value.
func (r TypeRef) IsZero() bool {
	return r.qualifier == "" && r.name == "" && r.span.IsZero()
}

// String returns the fully qualified name (e.g., "parts.Wheel" or "Wheel").
func (r TypeRef) String() string {
	if r.qualifier != "" {
		return r.qualifier + "." + r.name
	}
	return r.name
}

// DataTypeRef is a syntactic reference to a data type alias, similar to TypeRef.
// It captures the optional import qualifier and data type name.
type DataTypeRef struct {
	qualifier string        // import alias, empty for local types
	name      string        // data type name
	span      location.Span // source location for diagnostics
}

// Qualifier returns the import alias, or empty string for local data types.
func (r DataTypeRef) Qualifier() string {
	return r.qualifier
}

// Name returns the data type name.
func (r DataTypeRef) Name() string {
	return r.name
}

// Span returns the source location of this data type reference.
func (r DataTypeRef) Span() location.Span {
	return r.span
}

// IsZero reports whether this is the zero value.
func (r DataTypeRef) IsZero() bool {
	return r.qualifier == "" && r.name == "" && r.span.IsZero()
}

// String returns the fully qualified name (e.g., "common.Money" or "Money").
func (r DataTypeRef) String() string {
	if r.qualifier != "" {
		return r.qualifier + "." + r.name
	}
	return r.name
}
