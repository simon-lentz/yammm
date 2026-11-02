package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// Import represents an import declaration in a schema.
// Imports allow referencing types from other schema files.
type Import struct{ *importState }

// IsZero reports whether the handle names no record.
func (i Import) IsZero() bool { return i.importState == nil }

// importState is the record an [Import] names.
type importState struct {
	path             string            // the import path as written in the schema
	alias            string            // the import alias (explicit or derived)
	resolvedSourceID location.SourceID // the resolved canonical source identity
	schema           Schema            // the resolved schema (set after loading)
	span             location.Span     // source location
}

// newImport creates a new Import. This is primarily for internal use;
// imports are typically created during schema parsing.
func newImport(path, alias string, resolvedSourceID location.SourceID, span location.Span) Import {
	return Import{&importState{
		path:             path,
		alias:            alias,
		resolvedSourceID: resolvedSourceID,
		span:             span,
	}}
}

// Path returns the import path as written in the schema.
func (i Import) Path() string {
	return i.path
}

// Alias returns the import alias used for qualification.
func (i Import) Alias() string {
	return i.alias
}

// ResolvedSourceID returns the resolved canonical source identity.
func (i Import) ResolvedSourceID() location.SourceID {
	return i.resolvedSourceID
}

// ResolvedPath returns the resolved path as a string.
func (i Import) ResolvedPath() string {
	if cp, ok := i.resolvedSourceID.CanonicalPath(); ok {
		return cp.String()
	}
	return i.resolvedSourceID.String()
}

// Schema returns the resolved schema. Every Schema a load returns has each
// import resolved, so an Import read from one never answers zero here.
func (i Import) Schema() Schema {
	return i.schema
}

// Span returns the source location of this import declaration.
func (i Import) Span() location.Span {
	return i.span
}

// setSchema sets the resolved schema. The load calls it once, before the Import
// is observable.
func (i Import) setSchema(s Schema) {
	i.schema = s
}
