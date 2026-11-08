package schema

import (
	"iter"
	"slices"
	"strings"
	"sync"

	"github.com/simon-lentz/yammm/location"
)

// Schema represents a compiled schema. No exported method changes what a caller
// reads, and after loading it is safe for concurrent access.
type Schema struct{ *schemaState }

// IsZero reports whether the handle names no record.
func (s Schema) IsZero() bool { return s.schemaState == nil }

// schemaState is the record a [Schema] names.
type schemaState struct {
	name          string
	sourceID      location.SourceID
	span          location.Span
	doc           string
	types         []Type
	dataTypes     []DataType
	imports       []Import
	sources       Sources
	moduleRoot    string
	typeByName    map[string]Type
	dataByName    map[string]DataType
	importByAlias map[string]Import
	sealed        bool // true after loading is complete; prevents further mutation

	// Lazily computed by ensureClosure on first Closure/TypeByID call:
	// import wiring (Import.setSchema) finishes after per-schema completion,
	// so the closure cannot be built eagerly during completion.
	closureOnce sync.Once
	closure     []Schema
	typeByID    map[TypeID]Type
}

// newSchema creates a new Schema. This is for internal use; a caller's schema
// comes from Load, LoadString or LoadSourcesWithEntry.
func newSchema(
	name string,
	sourceID location.SourceID,
	span location.Span,
	doc string,
) Schema {
	return Schema{&schemaState{
		name:          name,
		sourceID:      sourceID,
		span:          span,
		doc:           doc,
		typeByName:    make(map[string]Type),
		dataByName:    make(map[string]DataType),
		importByAlias: make(map[string]Import),
	}}
}

// Name returns the schema name.
func (s Schema) Name() string {
	return s.name
}

// SourceID returns the canonical source identity of this schema.
func (s Schema) SourceID() location.SourceID {
	return s.sourceID
}

// Span returns the source location of this schema declaration.
func (s Schema) Span() location.Span {
	return s.span
}

// Documentation returns the documentation comment, if any.
func (s Schema) Documentation() string {
	return s.doc
}

// Type returns the type with the given name (local types only).
// For imported types, use ResolveType with a qualified TypeRef.
func (s Schema) Type(name string) (Type, bool) {
	t, ok := s.typeByName[name]
	return t, ok
}

// Types returns an iterator over all types in this schema, in declaration order.
// A reader that needs the names in order sorts them.
func (s Schema) Types() iter.Seq[Type] {
	return slices.Values(s.types)
}

// ResolveTypeName resolves an entry-relative type name: a bare name for a
// type this schema declares, or "alias.Name" for a type of a schema this
// schema imports directly. It is the one by-name resolve every name-taking
// entry point applies — the validator's, the instance builder's, the CLI's —
// so a name means the same thing at each. A transitively imported type has no
// name this resolves, though [TagForm] renders it as "schema#Name";
// [Schema.TypeByID] addresses it.
func (s Schema) ResolveTypeName(name string) (Type, bool) {
	return s.ResolveType(typeRefFromName(name))
}

// typeRefFromName reads "alias.Name" as a qualified reference and any other
// spelling — a bare name, a leading dot, a trailing dot — as a local one.
func typeRefFromName(name string) TypeRef {
	if idx := strings.Index(name, "."); idx > 0 {
		return NewTypeRef(name[:idx], name[idx+1:], location.Span{})
	}
	return LocalTypeRef(name, location.Span{})
}

// ResolveType resolves a TypeRef to a Type, handling qualified references
// to imported types.
func (s Schema) ResolveType(ref TypeRef) (Type, bool) {
	if ref.qualifier == "" {
		return s.Type(ref.name)
	}
	// Resolve via import alias
	imp, ok := s.ImportByAlias(ref.qualifier)
	if !ok {
		return Type{}, false
	}
	return imp.Schema().Type(ref.name)
}

// DataType returns the data type with the given name (local only).
func (s Schema) DataType(name string) (DataType, bool) {
	d, ok := s.dataByName[name]
	return d, ok
}

// DataTypes returns an iterator over all data types in this schema, in declaration
// order. A reader that needs the names in order sorts them.
func (s Schema) DataTypes() iter.Seq[DataType] {
	return slices.Values(s.dataTypes)
}

// ResolveDataType resolves a DataTypeRef to a DataType, handling qualified
// references to imported data types.
func (s Schema) ResolveDataType(ref DataTypeRef) (DataType, bool) {
	if ref.qualifier == "" {
		return s.DataType(ref.name)
	}
	// Resolve via import alias
	imp, ok := s.ImportByAlias(ref.qualifier)
	if !ok {
		return DataType{}, false
	}
	return imp.Schema().DataType(ref.name)
}

// Imports returns an iterator over import declarations.
func (s Schema) Imports() iter.Seq[Import] {
	return slices.Values(s.imports)
}

// ImportByAlias returns the import with the given alias.
func (s Schema) ImportByAlias(alias string) (Import, bool) {
	i, ok := s.importByAlias[alias]
	return i, ok
}

// FindImportAlias returns the alias of the import that resolves to path, and false if
// none does. A schema never imports its own path, because a load refuses the cycle.
func (s Schema) FindImportAlias(path location.SourceID) (string, bool) {
	for _, imp := range s.imports {
		if imp.resolvedSourceID == path {
			return imp.alias, true
		}
	}
	return "", false
}

// Sources returns the source content a load held, for diagnostic rendering.
func (s Schema) Sources() Sources {
	return s.sources
}

// ModuleRoot returns the canonicalized module root the load resolved
// module-style imports against: for a plain Load the WithModuleRoot value when
// given, then the directory of the nearest ancestor holding a
// [ModuleRootMarker] file and failing that the entry file's own directory,
// and the canonicalized root argument for LoadSourcesWithEntry (including the
// "." form).
// A load under [WithSyntheticRoot] reports the synthetic root, which is the
// root that load resolved module-style imports against.
//
// Returns "" when no module root was in play — LoadString sources, or an
// empty root passed to LoadSourcesWithEntry.
//
// The entry schema and every import of one load record that load's root.
func (s Schema) ModuleRoot() string {
	return s.moduleRoot
}

// --- Internal setters used during completion ---

// seal marks the schema as immutable.
// Called by the loader after all post-completion wiring is done.
func (s Schema) seal() {
	if s.sealed {
		panic("schema: sealed twice")
	}
	s.sealed = true
}

// isSealed reports whether the schema has been sealed.
func (s Schema) isSealed() bool {
	return s.sealed
}

// setTypes sets the types (called during completion).
func (s Schema) setTypes(types []Type) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.types = types
	clear(s.typeByName)
	for _, t := range types {
		s.typeByName[t.name] = t
	}
}

// setDataTypes sets the data types (called during completion).
func (s Schema) setDataTypes(dataTypes []DataType) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.dataTypes = dataTypes
	clear(s.dataByName)
	for _, d := range dataTypes {
		s.dataByName[d.name] = d
	}
}

// setImports sets the imports (called during completion).
func (s Schema) setImports(imports []Import) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.imports = imports
	clear(s.importByAlias)
	for _, i := range imports {
		s.importByAlias[i.alias] = i
	}
}

// setSources sets the source registry (called during loading).
func (s Schema) setSources(sources Sources) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.sources = sources
}

// setModuleRoot records the load's effective module root (called during loading).
func (s Schema) setModuleRoot(root string) {
	if s.sealed {
		panic("schema: cannot mutate sealed schema")
	}
	s.moduleRoot = root
}
