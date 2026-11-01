package schema

import (
	"iter"
	"slices"
	"strings"
	"sync"

	"github.com/simon-lentz/yammm/location"
)

// Schema represents a compiled schema. No exported method changes what a caller
// reads, and after loading it is safe for concurrent access. [Invariant.Expression] returns the schema's
// own tree, which a caller must not write.
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

// newSchema creates a new Schema. This is primarily for internal use;
// schemas are typically created via Load, LoadString, LoadSourcesWithEntry, or
// the Builder.
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
// name form; [Schema.TypeByID] addresses it.
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
	return func(yield func(Import) bool) {
		for _, i := range s.imports {
			if !yield(i) {
				return
			}
		}
	}
}

// ImportByAlias returns the import with the given alias.
func (s Schema) ImportByAlias(alias string) (Import, bool) {
	i, ok := s.importByAlias[alias]
	return i, ok
}

// FindImportAlias returns the alias for an imported schema, if it exists.
// Returns empty string if the path is not imported. A schema never imports its own
// path, since a load and a Build each refuse the cycle.
func (s Schema) FindImportAlias(path location.SourceID) string {
	for _, imp := range s.imports {
		if imp.resolvedSourceID == path {
			return imp.alias
		}
	}
	return ""
}

// Sources returns the source content registry for diagnostic rendering.
// It is zero if source content was not retained.
func (s Schema) Sources() Sources {
	return s.sources
}

// HasSourceProvider reports whether source content is available for diagnostic
// rendering. Returns false for schemas built programmatically via Builder without
// source content.
func (s Schema) HasSourceProvider() bool {
	return !s.sources.IsZero()
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
// Returns "" when no module root was in play — LoadString sources, an
// empty root passed to LoadSourcesWithEntry, or a Builder-built schema.
//
// The entry schema and every import compiled by the same load record that
// load's root. A schema reused from a shared Registry (see [WithRegistry]) —
// an import, or an entry the registry already held with identical content —
// instead retains the root of the load that first compiled it: sealed
// schemas are immutable, and the registry hands one record to every load.
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
