package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// TypeID uniquely identifies a type across all schemas. It is the semantic
// identity of a type, used for equality comparisons, inheritance deduplication,
// and cross-schema type resolution.
//
// Two types are equal if and only if they have the same TypeID. This enables:
//   - Proper diamond inheritance handling (same ancestor via different paths)
//   - Cross-schema type comparison (types from different schemas are distinct)
//   - Safe use as map keys
//
// TypeID is a value type with comparable semantics; use == for equality.
type TypeID struct {
	schemaPath location.SourceID
	name       string
}

// NewTypeID creates a TypeID from a schema source ID and type name.
func NewTypeID(schemaPath location.SourceID, name string) TypeID {
	return TypeID{schemaPath: schemaPath, name: name}
}

// SchemaPath returns the canonical source identity of the schema containing
// this type. For file-backed schemas, this is the canonicalized file path.
// For programmatically built schemas, this is a synthetic identifier.
func (id TypeID) SchemaPath() location.SourceID {
	return id.schemaPath
}

// Name returns the type name within the schema.
func (id TypeID) Name() string {
	return id.name
}

// String returns a human-readable representation of the TypeID.
// Format: "schemaPath:typeName" or just "typeName" for empty schema path.
//
// For a disk-loaded schema the rendering embeds an absolute filesystem path,
// so it is machine-local: two hosts loading one schema text render different
// strings. Use it for diagnostics, never as a persisted or compared identity —
// [StructuralHash] and the .ys wire key on (schema name, type name) for that
// reason.
func (id TypeID) String() string {
	if id.schemaPath.IsZero() {
		return id.name
	}
	return id.schemaPath.String() + ":" + id.name
}

// IsZero reports whether the TypeID is the zero value.
func (id TypeID) IsZero() bool {
	return id.schemaPath.IsZero() && id.name == ""
}
