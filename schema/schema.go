package schema

import (
	"iter"
	"maps"
	"slices"

	"github.com/simon-lentz/yammm/location"
)

// Schema represents a compiled, immutable schema.
// After loading, schemas are thread-safe for concurrent access.
type Schema struct {
	name          string
	sourceID      location.SourceID
	span          location.Span
	doc           string
	types         []*Type
	dataTypes     []*DataType
	imports       []*Import
	sources       *Sources
	typeByName    map[string]*Type
	dataByName    map[string]*DataType
	importByAlias map[string]*Import
	sealed        bool // true after loading is complete; prevents further mutation
}

// NewSchema creates a new Schema. This is primarily for internal use;
// schemas are typically created via Load, LoadString, or Builder.
func NewSchema(
	name string,
	sourceID location.SourceID,
	span location.Span,
	doc string,
) *Schema {
	return &Schema{
		name:          name,
		sourceID:      sourceID,
		span:          span,
		doc:           doc,
		typeByName:    make(map[string]*Type),
		dataByName:    make(map[string]*DataType),
		importByAlias: make(map[string]*Import),
	}
}

// Name returns the schema name.
func (s *Schema) Name() string {
	return s.name
}

// SourceID returns the canonical source identity of this schema.
func (s *Schema) SourceID() location.SourceID {
	return s.sourceID
}

// Span returns the source location of this schema declaration.
func (s *Schema) Span() location.Span {
	return s.span
}

// Documentation returns the documentation comment, if any.
func (s *Schema) Documentation() string {
	return s.doc
}

// Type returns the type with the given name (local types only).
// For imported types, use ResolveType with a qualified TypeRef.
func (s *Schema) Type(name string) (*Type, bool) {
	t, ok := s.typeByName[name]
	return t, ok
}

// Types returns an iterator over all types in this schema.
// Iteration order is lexicographic by name.
func (s *Schema) Types() iter.Seq2[string, *Type] {
	return func(yield func(string, *Type) bool) {
		for _, name := range slices.Sorted(maps.Keys(s.typeByName)) {
			if !yield(name, s.typeByName[name]) {
				return
			}
		}
	}
}

// TypesSlice returns a defensive copy of all types.
func (s *Schema) TypesSlice() []*Type {
	return slices.Clone(s.types)
}

// TypeNames returns all local type names in lexicographic order.
// This ensures deterministic iteration for CLI output and tests.
func (s *Schema) TypeNames() []string {
	return slices.Sorted(maps.Keys(s.typeByName))
}

// TypeCount returns the number of local types in the schema.
func (s *Schema) TypeCount() int {
	return len(s.typeByName)
}

// ResolveType resolves a TypeRef to a Type, handling qualified references
// to imported types.
func (s *Schema) ResolveType(ref TypeRef) (*Type, bool) {
	if ref.qualifier == "" {
		return s.Type(ref.name)
	}
	// Resolve via import alias
	imp, ok := s.ImportByAlias(ref.qualifier)
	if !ok || imp.Schema() == nil {
		return nil, false
	}
	return imp.Schema().Type(ref.name)
}

// DataType returns the data type with the given name (local only).
func (s *Schema) DataType(name string) (*DataType, bool) {
	d, ok := s.dataByName[name]
	return d, ok
}

// DataTypes returns an iterator over all data types in this schema.
// Iteration order is lexicographic by name.
func (s *Schema) DataTypes() iter.Seq2[string, *DataType] {
	return func(yield func(string, *DataType) bool) {
		for _, name := range slices.Sorted(maps.Keys(s.dataByName)) {
			if !yield(name, s.dataByName[name]) {
				return
			}
		}
	}
}

// DataTypesSlice returns a defensive copy of all data types.
func (s *Schema) DataTypesSlice() []*DataType {
	return slices.Clone(s.dataTypes)
}

// DataTypeNames returns all data type names in lexicographic order.
func (s *Schema) DataTypeNames() []string {
	return slices.Sorted(maps.Keys(s.dataByName))
}

// ResolveDataType resolves a DataTypeRef to a DataType, handling qualified
// references to imported data types.
func (s *Schema) ResolveDataType(ref DataTypeRef) (*DataType, bool) {
	if ref.qualifier == "" {
		return s.DataType(ref.name)
	}
	// Resolve via import alias
	imp, ok := s.ImportByAlias(ref.qualifier)
	if !ok || imp.Schema() == nil {
		return nil, false
	}
	return imp.Schema().DataType(ref.name)
}

// Imports returns an iterator over import declarations.
func (s *Schema) Imports() iter.Seq[*Import] {
	return func(yield func(*Import) bool) {
		for _, i := range s.imports {
			if !yield(i) {
				return
			}
		}
	}
}

// ImportsSlice returns a defensive copy of imports.
func (s *Schema) ImportsSlice() []*Import {
	return slices.Clone(s.imports)
}

// ImportCount returns the number of imports in the schema.
func (s *Schema) ImportCount() int {
	return len(s.imports)
}

// ImportByAlias returns the import with the given alias.
func (s *Schema) ImportByAlias(alias string) (*Import, bool) {
	i, ok := s.importByAlias[alias]
	return i, ok
}

// FindImportAlias returns the alias for an imported schema, if it exists.
// Returns empty string if the path is not imported or if it's this schema's own path.
func (s *Schema) FindImportAlias(path location.SourceID) string {
	if path == s.sourceID {
		return ""
	}
	for _, imp := range s.imports {
		if imp.resolvedSourceID == path {
			return imp.alias
		}
	}
	return ""
}

// Sources returns the source content registry for diagnostic rendering.
// May be nil if source content was not retained.
func (s *Schema) Sources() *Sources {
	return s.sources
}

// HasSourceProvider reports whether source content is available for diagnostic
// rendering. Returns false for schemas built programmatically via Builder without
// source content.
func (s *Schema) HasSourceProvider() bool {
	return s.sources != nil
}

// --- Internal setters used during completion ---
// These setters are not part of the public API and will panic if called
// after the schema is sealed. They may be removed or made unexported in
// future versions.

// Seal marks the schema as immutable.
// Called by the loader after all post-completion wiring is done.
// This is not part of the public API and may be removed in future versions.
func (s *Schema) Seal() {
	s.sealed = true
}

// IsSealed reports whether the schema has been sealed.
func (s *Schema) IsSealed() bool {
	return s.sealed
}

// SetTypes sets the types (called during completion).
func (s *Schema) SetTypes(types []*Type) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.types = types
	clear(s.typeByName)
	for _, t := range types {
		s.typeByName[t.name] = t
	}
}

// SetDataTypes sets the data types (called during completion).
func (s *Schema) SetDataTypes(dataTypes []*DataType) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.dataTypes = dataTypes
	clear(s.dataByName)
	for _, d := range dataTypes {
		s.dataByName[d.name] = d
	}
}

// SetImports sets the imports (called during completion).
func (s *Schema) SetImports(imports []*Import) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.imports = imports
	clear(s.importByAlias)
	for _, i := range imports {
		s.importByAlias[i.alias] = i
	}
}

// SetSources sets the source registry (called during loading).
func (s *Schema) SetSources(sources *Sources) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.sources = sources
}
