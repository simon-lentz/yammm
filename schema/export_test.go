package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// Test-only exports for schema_test files in this directory.
// These are compiled only during `go test ./schema/...` and do not
// appear in go doc or the production binary.

// Construction and mutation helpers — method expressions exposing
// unexported constructors and mutators for test fixtures.

// Constructors.
var (
	TestNewSchema    = newSchema
	TestNewType      = newType
	TestNewRelation  = newRelation
	TestNewProperty  = newProperty
	TestNewDataType  = newDataType
	TestNewInvariant = newInvariant
	TestNewImport    = newImport
)

// Seal methods.
var (
	TestSealSchema   = Schema.seal
	TestSealType     = Type.seal
	TestSealRelation = Relation.seal
	TestSealDataType = DataType.seal
)

// IsSealed accessors.
var (
	TestIsSealedSchema   = func(s Schema) bool { return s.sealed }
	TestIsSealedType     = func(t Type) bool { return t.sealed }
	TestIsSealedRelation = func(r Relation) bool { return r.sealed }
)

// Schema mutators.
var (
	TestSetSchemaTypes     = Schema.setTypes
	TestSetSchemaDataTypes = Schema.setDataTypes
	TestSetSchemaImports   = Schema.setImports
	TestSetSchemaSources   = Schema.setSources
)

// Type mutators.
var (
	TestSetTypeSchemaName            = Type.setSchemaName
	TestSetTypeProperties            = Type.setProperties
	TestSetTypeAssociations          = Type.setAssociations
	TestSetTypeCompositions          = Type.setCompositions
	TestSetTypeInvariants            = Type.setInvariants
	TestSetTypeAnnotations           = Type.setAnnotations
	TestSetTypeAllAnnotations        = Type.setAllAnnotations
	TestSetTypeInherits              = Type.setInherits
	TestSetTypeAllProperties         = Type.setAllProperties
	TestSetTypePrimaryKeys           = Type.setPrimaryKeys
	TestSetTypeAllAssociations       = Type.setAllAssociations
	TestSetTypeAllCompositions       = Type.setAllCompositions
	TestSetTypeSuppressedAnnotations = Type.setSuppressedAnnotations
	TestSetTypeSuperTypes            = Type.setSuperTypes
	TestSetTypeSubTypes              = Type.setSubTypes
)

// Relation mutators.
var TestSetRelationTargetID = Relation.setTargetID

// Property mutators.
var TestSetPropertyConstraint = Property.setConstraint

// DataType mutators.
var TestSetDataTypeConstraint = DataType.setConstraint

// Import mutators.
var (
	TestSetImportSchema = Import.setSchema
)

// Constraint constructors, which only the parser calls in production.
var (
	TestNewStringConstraint     = newStringConstraint
	TestStringMinLen            = stringMinLen
	TestStringMaxLen            = stringMaxLen
	TestStringLenBetween        = stringLenBetween
	TestNewIntegerConstraint    = newIntegerConstraint
	TestIntegerMin              = integerMin
	TestIntegerMax              = integerMax
	TestIntegerBetween          = integerBetween
	TestNewFloatConstraint      = newFloatConstraint
	TestFloatMin                = floatMin
	TestFloatMax                = floatMax
	TestFloatBetween            = floatBetween
	TestNewBooleanConstraint    = newBooleanConstraint
	TestNewTimestampConstraint  = newTimestampConstraint
	TestNewDateConstraint       = newDateConstraint
	TestNewUUIDConstraint       = newUUIDConstraint
	TestNewVectorConstraint     = newVectorConstraint
	TestNewListConstraint       = newListConstraint
	TestListMinLen              = listMinLen
	TestListMaxLen              = listMaxLen
	TestListLenBetween          = listLenBetween
	TestNewAliasConstraint      = newAliasConstraint
	TestParsedEnumConstraint    = parsedEnumConstraint
	TestParsedPatternConstraint = parsedPatternConstraint
)

// Comparisons the completer runs.
var (
	TestConstraintEqual       = func(a, b Constraint) bool { return a.equal(b) }
	TestConstraintNarrowsTo   = func(parent, child Constraint) bool { return parent.narrowsTo(child) }
	TestConstraintIsResolved  = func(c Constraint) bool { return c.isResolved() }
	TestPropertyEqual         = Property.equal
	TestPropertyCanNarrowFrom = Property.canNarrowFrom
	TestRelationEqual         = Relation.equal
)

// References, scopes and sources the loader builds.
var (
	TestNewTypeRef     = newTypeRef
	TestLocalTypeRef   = localTypeRef
	TestTypeScope      = typeScope
	TestRelationScope  = relationScope
	TestNewSources     = newSources
	TestNewDataTypeRef = func(qualifier, name string, span location.Span) DataTypeRef {
		return DataTypeRef{qualifier: qualifier, name: name, span: span}
	}
)

// Internal functions from merged packages.
var (
	TestCompleteModel       = completeModel
	TestNewParser           = newParser
	TestDeriveAliasFromPath = deriveAliasFromPath
)

// AST types (type aliases for test access).
type (
	TestModel         = model
	TestImportDecl    = importDecl
	TestTypeDecl      = typeDecl
	TestDataTypeDecl  = dataTypeDecl
	TestPropertyDecl  = propertyDecl
	TestRelationDecl  = relationDecl
	TestInvariantDecl = invariantDecl
	TestASTTypeRef    = astTypeRef
)

// TestResolvedImportMap is the completer's resolved-import table.
type TestResolvedImportMap = resolvedImportMap

// TestResolvedImport builds a resolved (non-deferred) entry for a
// [TestResolvedImportMap] passed to the completeModel bridge. The
// importResolution fields are unexported, so tests construct entries through
// this helper rather than a struct literal. It is a var (not a func) so the
// Test-prefixed name is not mistaken for a test function.
var TestResolvedImport = func(sourceID location.SourceID) importResolution {
	return importResolution{sourceID: sourceID}
}

// NewTypeID builds an identity no schema has to declare, for tests of how the
// package treats one.
func NewTypeID(schemaPath location.SourceID, name string) TypeID {
	return TypeID{schemaPath: schemaPath, name: name}
}
