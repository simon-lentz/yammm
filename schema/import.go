package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// Import represents an import declaration in a schema.
// Imports allow referencing types from other schema files.
type Import struct {
	path             string            // the import path as written in the schema
	alias            string            // the import alias (explicit or derived)
	resolvedSourceID location.SourceID // the resolved canonical source identity
	schema           *Schema           // the resolved schema (set after loading)
	span             location.Span     // source location
	sealed           bool              // true after loading is complete; prevents further mutation
}

// NewImport creates a new Import. This is primarily for internal use;
// imports are typically created during schema parsing.
func NewImport(path, alias string, resolvedSourceID location.SourceID, span location.Span) *Import {
	return &Import{
		path:             path,
		alias:            alias,
		resolvedSourceID: resolvedSourceID,
		span:             span,
	}
}

// Path returns the import path as written in the schema.
func (i *Import) Path() string {
	return i.path
}

// Alias returns the import alias used for qualification.
func (i *Import) Alias() string {
	return i.alias
}

// ResolvedSourceID returns the resolved canonical source identity.
func (i *Import) ResolvedSourceID() location.SourceID {
	return i.resolvedSourceID
}

// ResolvedPath returns the resolved path as a string.
// Returns empty string if not yet resolved.
func (i *Import) ResolvedPath() string {
	if cp, ok := i.resolvedSourceID.CanonicalPath(); ok {
		return cp.String()
	}
	return i.resolvedSourceID.String()
}

// Schema returns the resolved schema, if available.
// Returns nil if the schema has not been loaded yet.
func (i *Import) Schema() *Schema {
	return i.schema
}

// Span returns the source location of this import declaration.
func (i *Import) Span() location.Span {
	return i.span
}

// --- Internal methods used during loading ---

// Seal prevents further mutation of the import.
// Called during loading after resolution is complete.
func (i *Import) Seal() {
	i.sealed = true
}

// IsSealed reports whether the import has been sealed.
func (i *Import) IsSealed() bool {
	return i.sealed
}

// SetResolvedSourceID sets the resolved source identity (called during loading).
// Panics if called after Seal().
func (i *Import) SetResolvedSourceID(id location.SourceID) {
	if i.sealed {
		panic("import: cannot mutate sealed import")
	}
	i.resolvedSourceID = id
}

// SetSchema sets the resolved schema (called during loading).
// Panics if called after Seal().
func (i *Import) SetSchema(s *Schema) {
	if i.sealed {
		panic("import: cannot mutate sealed import")
	}
	i.schema = s
}
