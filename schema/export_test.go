package schema

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
	TestSealSchema   = (*Schema).seal
	TestSealType     = (*Type).seal
	TestSealRelation = (*Relation).seal
	TestSealDataType = (*DataType).seal
	TestSealImport   = (*Import).seal
)

// IsSealed accessors.
var (
	TestIsSealedSchema   = (*Schema).isSealed
	TestIsSealedType     = (*Type).isSealed
	TestIsSealedRelation = (*Relation).isSealed
	TestIsSealedDataType = (*DataType).isSealed
	TestIsSealedImport   = (*Import).isSealed
)

// Schema mutators.
var (
	TestSetSchemaTypes     = (*Schema).setTypes
	TestSetSchemaDataTypes = (*Schema).setDataTypes
	TestSetSchemaImports   = (*Schema).setImports
	TestSetSchemaSources   = (*Schema).setSources
)

// Type mutators.
var (
	TestSetTypeSchemaName      = (*Type).setSchemaName
	TestSetTypeNameSpan        = (*Type).setNameSpan
	TestSetTypeProperties      = (*Type).setProperties
	TestSetTypeAssociations    = (*Type).setAssociations
	TestSetTypeCompositions    = (*Type).setCompositions
	TestSetTypeInvariants      = (*Type).setInvariants
	TestSetTypeAllInvariants   = (*Type).setAllInvariants
	TestSetTypeInherits        = (*Type).setInherits
	TestSetTypeAllProperties   = (*Type).setAllProperties
	TestSetTypePrimaryKeys     = (*Type).setPrimaryKeys
	TestSetTypeAllAssociations = (*Type).setAllAssociations
	TestSetTypeAllCompositions = (*Type).setAllCompositions
	TestSetTypeSuperTypes      = (*Type).setSuperTypes
	TestSetTypeSubTypes        = (*Type).setSubTypes
)

// Relation mutators.
var TestSetRelationTargetID = (*Relation).setTargetID

// Property mutators.
var TestSetPropertyConstraint = (*Property).setConstraint

// DataType mutators.
var TestSetDataTypeConstraint = (*DataType).setConstraint

// Import mutators.
var (
	TestSetImportResolvedSourceID = (*Import).setResolvedSourceID
	TestSetImportSchema           = (*Import).setSchema
)

// Internal functions from merged packages.
var (
	TestCompleteModel                      = completeModel
	TestDetectCrossSchemaInheritanceCycles = detectCrossSchemaInheritanceCycles
	TestNewParser                          = newParser
	TestNewSpanBuilder                     = newSpanBuilder
	TestMustPositionAt                     = mustPositionAt
	TestIsValidAlias                       = isValidAlias
	TestIsReservedKeyword                  = isReservedKeyword
	TestReservedKeywords                   = reservedKeywords
	TestDeriveAliasFromPath                = deriveAliasFromPath
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

// Internal interfaces.
type (
	TestCompletionRegistry  = completionRegistry
	TestResolvedImportMap   = resolvedImportMap
	TestCrossSchemaRegistry = crossSchemaRegistry
)

// Load options whose parameter types live in internal packages.
var TestWithSourceRegistry = withSourceRegistry
