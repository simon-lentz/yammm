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

// NewTypeRef creates a TypeRef with the given qualifier, name, and span.
func NewTypeRef(qualifier, name string, span location.Span) TypeRef {
	return TypeRef{qualifier: qualifier, name: name, span: span}
}

// LocalTypeRef creates a TypeRef for a local (unqualified) type.
func LocalTypeRef(name string, span location.Span) TypeRef {
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

// IsQualified reports whether this is a qualified (imported) type reference.
func (r TypeRef) IsQualified() bool {
	return r.qualifier != ""
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

// NewDataTypeRef creates a DataTypeRef with the given qualifier, name, and span.
func NewDataTypeRef(qualifier, name string, span location.Span) DataTypeRef {
	return DataTypeRef{qualifier: qualifier, name: name, span: span}
}

// LocalDataTypeRef creates a DataTypeRef for a local (unqualified) data type.
func LocalDataTypeRef(name string, span location.Span) DataTypeRef {
	return DataTypeRef{name: name, span: span}
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

// IsQualified reports whether this is a qualified (imported) data type reference.
func (r DataTypeRef) IsQualified() bool {
	return r.qualifier != ""
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

// ResolvedTypeRef combines a syntactic TypeRef with its resolved semantic TypeID.
// This is used when both the original source representation and the resolved
// identity are needed.
//
// ResolvedTypeRef is used for:
//   - SuperTypes() and SubTypes() where both display and identity matter
//   - Cross-schema relationships where no import alias exists
//   - Tooling that needs to show qualified names while comparing types
type ResolvedTypeRef struct {
	ref TypeRef // original syntactic reference
	id  TypeID  // resolved canonical identity
}

// NewResolvedTypeRef creates a ResolvedTypeRef from a TypeRef and TypeID.
func NewResolvedTypeRef(ref TypeRef, id TypeID) ResolvedTypeRef {
	return ResolvedTypeRef{ref: ref, id: id}
}

// ResolvedTypeRefFromType creates a ResolvedTypeRef for a *Type, deriving the display
// qualifier from the schema name when viewed from a different schema.
//
// viewingSchemaPath is the schema from whose perspective this reference is being viewed.
// If viewingSchemaPath matches t.SourceID().String(), the type is local and no qualifier is shown.
// Otherwise, a qualifier is derived from the target schema's name.
func ResolvedTypeRefFromType(t *Type, viewingSchemaPath string) ResolvedTypeRef {
	var qualifier string
	if t.SourceID().String() != viewingSchemaPath {
		qualifier = t.SchemaName()
	}
	ref := NewTypeRef(qualifier, t.Name(), t.Span())
	return ResolvedTypeRef{ref: ref, id: t.ID()}
}

// Ref returns the original syntactic TypeRef.
func (r ResolvedTypeRef) Ref() TypeRef {
	return r.ref
}

// ID returns the resolved canonical TypeID.
func (r ResolvedTypeRef) ID() TypeID {
	return r.id
}

// Name returns the type name (from the syntactic reference).
func (r ResolvedTypeRef) Name() string {
	return r.ref.name
}

// Qualifier returns the import qualifier (from the syntactic reference).
func (r ResolvedTypeRef) Qualifier() string {
	return r.ref.qualifier
}

// Span returns the source location (from the syntactic reference).
func (r ResolvedTypeRef) Span() location.Span {
	return r.ref.span
}

// String returns the display string using the syntactic representation.
func (r ResolvedTypeRef) String() string {
	return r.ref.String()
}

// IsZero reports whether this is the zero value.
func (r ResolvedTypeRef) IsZero() bool {
	return r.ref.IsZero() && r.id.IsZero()
}

// IsLocal reports whether this type reference is local (unqualified).
// Returns true if there is no import qualifier.
func (r ResolvedTypeRef) IsLocal() bool {
	return r.ref.qualifier == ""
}

// Equal reports whether two ResolvedTypeRefs refer to the same type.
// Comparison is by TypeID (semantic identity), ignoring syntactic differences
// in the TypeRef (such as different import qualifiers that resolve to the same type).
func (r ResolvedTypeRef) Equal(other ResolvedTypeRef) bool {
	return r.id == other.id
}
