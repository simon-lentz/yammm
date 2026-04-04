package schema

import (
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

// Internal construction and mutation functions.
//
// These functions provide write access to schema types for use by internal
// packages (schema/internal/complete, schema/load, schema/build). They are
// NOT part of the public API. Consumer code should use schema/build.Builder
// for schema construction and schema/load.Load for schema loading.
//
// These functions exist because Go's visibility is package-scoped: the
// underlying methods are unexported (lowercase) on the types, so they
// cannot be called from sub-packages. These exported package-level
// functions bridge that gap within the module.

// --- Constructors ---

// InternalNewSchema creates a new Schema. For internal use only;
// use [load.Load], [load.String], or [build.Builder] instead.
func InternalNewSchema(
	name string,
	sourceID location.SourceID,
	span location.Span,
	doc string,
) *Schema {
	return newSchema(name, sourceID, span, doc)
}

// InternalNewType creates a new Type. For internal use only.
func InternalNewType(
	name string,
	sourceID location.SourceID,
	span location.Span,
	doc string,
	isAbstract, isPart bool,
) *Type {
	return newType(name, sourceID, span, doc, isAbstract, isPart)
}

// InternalNewRelation creates a new Relation. For internal use only.
func InternalNewRelation(
	kind RelationKind,
	name string,
	fieldName string,
	target TypeRef,
	targetID TypeID,
	span location.Span,
	doc string,
	optional, many bool,
	backref string,
	reverseOptional, reverseMany bool,
	owner string,
	properties []*Property,
) *Relation {
	return newRelation(kind, name, fieldName, target, targetID, span, doc,
		optional, many, backref, reverseOptional, reverseMany, owner, properties)
}

// InternalNewProperty creates a new Property. For internal use only.
func InternalNewProperty(
	name string,
	span location.Span,
	doc string,
	constraint Constraint,
	dataTypeRef DataTypeRef,
	optional bool,
	isPrimaryKey bool,
	scope DeclaringScope,
) *Property {
	return newProperty(name, span, doc, constraint, dataTypeRef, optional, isPrimaryKey, scope)
}

// InternalNewDataType creates a new DataType. For internal use only.
func InternalNewDataType(name string, constraint Constraint, span location.Span, doc string) *DataType {
	return newDataType(name, constraint, span, doc)
}

// InternalNewInvariant creates a new Invariant. For internal use only.
func InternalNewInvariant(name string, e expr.Expression, span location.Span, doc string) *Invariant {
	return newInvariant(name, e, span, doc)
}

// InternalNewImport creates a new Import. For internal use only.
func InternalNewImport(path, alias string, resolvedSourceID location.SourceID, span location.Span) *Import {
	return newImport(path, alias, resolvedSourceID, span)
}

// --- Schema mutators ---

// InternalSealSchema marks a Schema as immutable. For internal use only.
func InternalSealSchema(s *Schema) { s.seal() }

// InternalSetSchemaTypes sets the types on a Schema. For internal use only.
func InternalSetSchemaTypes(s *Schema, types []*Type) { s.setTypes(types) }

// InternalSetSchemaDataTypes sets the data types on a Schema. For internal use only.
func InternalSetSchemaDataTypes(s *Schema, dataTypes []*DataType) { s.setDataTypes(dataTypes) }

// InternalSetSchemaImports sets the imports on a Schema. For internal use only.
func InternalSetSchemaImports(s *Schema, imports []*Import) { s.setImports(imports) }

// InternalSetSchemaSources sets the source registry on a Schema. For internal use only.
func InternalSetSchemaSources(s *Schema, sources *Sources) { s.setSources(sources) }

// --- Type mutators ---

// InternalSealType marks a Type as immutable. For internal use only.
func InternalSealType(t *Type) { t.seal() }

// InternalSetTypeSchemaName sets the owning schema name. For internal use only.
func InternalSetTypeSchemaName(t *Type, name string) { t.setSchemaName(name) }

// InternalSetTypeNameSpan sets the precise type name span. For internal use only.
func InternalSetTypeNameSpan(t *Type, span location.Span) { t.setNameSpan(span) }

// InternalSetTypeProperties sets declared properties. For internal use only.
func InternalSetTypeProperties(t *Type, properties []*Property) { t.setProperties(properties) }

// InternalSetTypeAssociations sets declared associations. For internal use only.
func InternalSetTypeAssociations(t *Type, associations []*Relation) { t.setAssociations(associations) }

// InternalSetTypeCompositions sets declared compositions. For internal use only.
func InternalSetTypeCompositions(t *Type, compositions []*Relation) { t.setCompositions(compositions) }

// InternalSetTypeInvariants sets declared invariants. For internal use only.
func InternalSetTypeInvariants(t *Type, invariants []*Invariant) { t.setInvariants(invariants) }

// InternalSetTypeAllInvariants sets all invariants including inherited. For internal use only.
func InternalSetTypeAllInvariants(t *Type, all []*Invariant) { t.setAllInvariants(all) }

// InternalSetTypeInherits sets the extends clause. For internal use only.
func InternalSetTypeInherits(t *Type, inherits []TypeRef) { t.setInherits(inherits) }

// InternalSetTypeAllProperties sets all properties including inherited. For internal use only.
func InternalSetTypeAllProperties(t *Type, all []*Property) { t.setAllProperties(all) }

// InternalSetTypePrimaryKeys sets primary key properties. For internal use only.
func InternalSetTypePrimaryKeys(t *Type, pks []*Property) { t.setPrimaryKeys(pks) }

// InternalSetTypeAllAssociations sets all associations including inherited. For internal use only.
func InternalSetTypeAllAssociations(t *Type, all []*Relation) { t.setAllAssociations(all) }

// InternalSetTypeAllCompositions sets all compositions including inherited. For internal use only.
func InternalSetTypeAllCompositions(t *Type, all []*Relation) { t.setAllCompositions(all) }

// InternalSetTypeSuperTypes sets linearized ancestors. For internal use only.
func InternalSetTypeSuperTypes(t *Type, supers []ResolvedTypeRef) { t.setSuperTypes(supers) }

// InternalSetTypeSubTypes sets known subtypes. For internal use only.
func InternalSetTypeSubTypes(t *Type, subs []ResolvedTypeRef) { t.setSubTypes(subs) }

// --- Relation mutators ---

// InternalSealRelation marks a Relation as immutable. For internal use only.
func InternalSealRelation(r *Relation) { r.seal() }

// InternalSetRelationTargetID sets the resolved target TypeID. For internal use only.
func InternalSetRelationTargetID(r *Relation, id TypeID) { r.setTargetID(id) }

// --- Property mutators ---

// InternalSetPropertyConstraint sets the constraint on a Property. For internal use only.
func InternalSetPropertyConstraint(p *Property, c Constraint) { p.setConstraint(c) }

// --- DataType mutators ---

// InternalSealDataType marks a DataType as immutable. For internal use only.
func InternalSealDataType(d *DataType) { d.seal() }

// InternalSetDataTypeConstraint sets the constraint on a DataType. For internal use only.
func InternalSetDataTypeConstraint(d *DataType, c Constraint) { d.setConstraint(c) }

// --- Import mutators ---

// InternalSealImport marks an Import as immutable. For internal use only.
func InternalSealImport(i *Import) { i.seal() }

// InternalSetImportResolvedSourceID sets the resolved source ID. For internal use only.
func InternalSetImportResolvedSourceID(i *Import, id location.SourceID) { i.setResolvedSourceID(id) }

// InternalSetImportSchema sets the resolved schema pointer. For internal use only.
func InternalSetImportSchema(i *Import, s *Schema) { i.setSchema(s) }

// --- IsSealed accessors (for external test packages) ---

// InternalIsSealedSchema reports whether a Schema is sealed. For internal/test use only.
func InternalIsSealedSchema(s *Schema) bool { return s.isSealed() }

// InternalIsSealedType reports whether a Type is sealed. For internal/test use only.
func InternalIsSealedType(t *Type) bool { return t.isSealed() }

// InternalIsSealedRelation reports whether a Relation is sealed. For internal/test use only.
func InternalIsSealedRelation(r *Relation) bool { return r.isSealed() }

// InternalIsSealedDataType reports whether a DataType is sealed. For internal/test use only.
func InternalIsSealedDataType(d *DataType) bool { return d.isSealed() }

// InternalIsSealedImport reports whether an Import is sealed. For internal/test use only.
func InternalIsSealedImport(i *Import) bool { return i.isSealed() }
