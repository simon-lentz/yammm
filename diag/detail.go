package diag

// Detail provides key-value context for diagnostic issues.
//
// Details are used to add structured information to issues that can be
// programmatically inspected by tools. Use the standard detail key constants
// to ensure consistent key naming across the codebase.
type Detail struct {
	Key   string
	Value string
}

// Standard detail keys for consistent diagnostic metadata.
//
// Use these constants to avoid stringly-typed drift and enable programmatic
// inspection of diagnostic details. Custom detail keys are permitted for
// domain-specific diagnostics; use lower_snake_case for custom keys.
const (
	// DetailKeyExpected is the expected value or type.
	DetailKeyExpected = "expected"

	// DetailKeyGot is the actual value or type received.
	DetailKeyGot = "got"

	// DetailKeyTypeName is the type name involved in the diagnostic.
	DetailKeyTypeName = "type"

	// DetailKeyPropertyName is the property name involved.
	DetailKeyPropertyName = "property"

	// DetailKeyPrefix is the reserved prefix that was violated.
	DetailKeyPrefix = "prefix"

	// DetailKeyRelationName is the relation name involved.
	DetailKeyRelationName = "relation"

	// DetailKeyPrimaryKey is the primary key value.
	DetailKeyPrimaryKey = "pk"

	// DetailKeyReason is the failure reason discriminant.
	// Used with E_UNRESOLVED_REQUIRED ("absent", "empty", "target_missing")
	// and E_UNRESOLVED_REQUIRED_COMPOSITION ("absent", "empty").
	DetailKeyReason = "reason"

	// DetailKeyField is the data-level field name (for unknown/unexpected fields).
	DetailKeyField = "field"

	// DetailKeyJsonField is the normalized JSON field name for relation in path
	// (lower_snake form).
	DetailKeyJsonField = "json_field"

	// DetailKeyDetail is the specific error description (grammar violation,
	// constraint reason, parse error).
	DetailKeyDetail = "detail"

	// DetailKeyFormat is the adapter format identifier (e.g., "json", "csv", "yaml").
	DetailKeyFormat = "format"

	// DetailKeyTargetType is the target type name (for cross-reference errors).
	DetailKeyTargetType = "target_type"

	// DetailKeyTargetPK is the target PK (for cross-reference errors).
	DetailKeyTargetPK = "target_pk"

	// DetailKeyImportPath is the import path (for import resolution errors).
	DetailKeyImportPath = "path"

	// DetailKeyAlias is the import alias (for alias validation errors).
	DetailKeyAlias = "alias"

	// DetailKeyCycle is the cycle participants as JSON array
	// (for cycle detection errors).
	DetailKeyCycle = "cycle"

	// DetailKeyName is the invalid identifier name (for naming errors).
	DetailKeyName = "name"

	// DetailKeyContext is contextual information (e.g., "Builder", "Registry").
	DetailKeyContext = "context"

	// DetailKeyId is the identifier value (e.g., synthetic SourceID).
	DetailKeyId = "id"

	// DetailKeyFunction is the builtin function name
	// (for expression evaluation errors).
	DetailKeyFunction = "function"

	// DetailKeyTypeSchema is the schema path where a type is defined.
	// Used for transitive import diagnostics (E_GRAPH_TYPE_NOT_FOUND).
	DetailKeyTypeSchema = "type_schema"

	// DetailKeyImportedVia is the direct import that provides transitive access.
	// Used for transitive import diagnostics (E_GRAPH_TYPE_NOT_FOUND).
	DetailKeyImportedVia = "imported_via"

	// DetailKeyFirstAlias is the first import alias in duplicate detection.
	DetailKeyFirstAlias = "first_alias"

	// DetailKeyFirstLine is the line number of the first occurrence.
	DetailKeyFirstLine = "first_line"

	// DetailKeyDuplicateAlias is the duplicate import alias.
	DetailKeyDuplicateAlias = "duplicate_alias"

	// DetailKeyDuplicateLine is the line number of the duplicate occurrence.
	DetailKeyDuplicateLine = "duplicate_line"

	// DetailKeyImportCount is the count of imports (for limit diagnostics).
	DetailKeyImportCount = "import_count"
)

// ExpectedGot creates a pair of details for type mismatch diagnostics.
//
// This is the standard pattern for reporting "expected X, got Y" errors.
func ExpectedGot(expected, got string) []Detail {
	return []Detail{
		{Key: DetailKeyExpected, Value: expected},
		{Key: DetailKeyGot, Value: got},
	}
}

// TypeProp creates detail entries for type+property diagnostics.
//
// Use for diagnostics involving a specific property on a type.
func TypeProp(typeName, propName string) []Detail {
	return []Detail{
		{Key: DetailKeyTypeName, Value: typeName},
		{Key: DetailKeyPropertyName, Value: propName},
	}
}

// TypeRelation creates detail entries for type+relation diagnostics.
//
// Use for diagnostics involving a specific relation on a type.
func TypeRelation(typeName, relationName string) []Detail {
	return []Detail{
		{Key: DetailKeyTypeName, Value: typeName},
		{Key: DetailKeyRelationName, Value: relationName},
	}
}

// RelationField creates detail entries for edge field diagnostics.
//
// Use for diagnostics like E_UNKNOWN_EDGE_FIELD.
func RelationField(relationName, fieldName string) []Detail {
	return []Detail{
		{Key: DetailKeyRelationName, Value: relationName},
		{Key: DetailKeyField, Value: fieldName},
	}
}

// TypeField creates detail entries for unknown field diagnostics.
//
// Use for diagnostics like E_UNKNOWN_FIELD.
func TypeField(typeName, fieldName string) []Detail {
	return []Detail{
		{Key: DetailKeyTypeName, Value: typeName},
		{Key: DetailKeyField, Value: fieldName},
	}
}

// PathRelation creates detail entries for diagnostics involving relation path
// segments.
//
// Provides both the schema relation name (for path) and the normalized JSON
// field name (for direct lookup in instance data). The jsonFieldName is
// computed via lower_snake(relationName).
//
// Use with relation-path diagnostics (e.g., E_DUPLICATE_COMPOSED_PK,
// E_UNRESOLVED_REQUIRED_COMPOSITION) to enable users to locate the field
// in their JSON input.
func PathRelation(relationName, jsonFieldName string) []Detail {
	return []Detail{
		{Key: DetailKeyRelationName, Value: relationName},
		{Key: DetailKeyJsonField, Value: jsonFieldName},
	}
}
