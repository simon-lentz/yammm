package schema

import (
	"github.com/simon-lentz/yammm/location"
)

// TypeID identifies a type across the schemas of one load, and two loads of one
// source give equal TypeIDs for different [Type] handles. It is the semantic
// identity of a type, used for equality comparisons, inheritance deduplication,
// and cross-schema type resolution.
//
// Within one load, two handles name one type if and only if their TypeIDs are
// equal, which enables:
//   - Proper diamond inheritance handling (same ancestor via different paths)
//   - Cross-schema type comparison (types from different schemas are distinct)
//   - Safe use as map keys
//
// TypeID is a value type with comparable semantics; use == for equality.
type TypeID struct {
	schemaPath location.SourceID
	name       string
}

// SchemaPath returns the canonical source identity of the schema containing
// this type. For file-backed schemas, this is the canonicalized file path. For a
// schema loaded from a string or under a synthetic root, it is a synthetic identifier.
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
// so it is machine-local: two hosts that hold one schema text at different
// paths render different strings. Use it for diagnostics and for a sort order on one host, never as a
// persisted identity or an identity compared across hosts. For that reason
// the .ys wire keys a type, and [StructuralHash] a relation target, on (schema
// name, type name).
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
