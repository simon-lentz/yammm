package cypher

import (
	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/mapping"
)

// Diagnostic codes for Neo4j DDL generation errors.
var (
	// E_NEO4J_LABEL_COLLISION indicates two or more types produce the same
	// Neo4j label after sanitization.
	E_NEO4J_LABEL_COLLISION = mapping.LabelCollision

	// E_NEO4J_INVALID_IDENTIFIER indicates a property name or a composed label
	// is not a valid Neo4j identifier (fails regex or matches a Cypher reserved
	// keyword). A label setting can make a label invalid whose type name is valid.
	E_NEO4J_INVALID_IDENTIFIER = mapping.InvalidIdentifier
	// E_NEO4J_UNSUPPORTED_TYPE indicates a constraint kind cannot be mapped
	// to a Neo4j property type expression.
	E_NEO4J_UNSUPPORTED_TYPE = diag.NewCode("E_NEO4J_UNSUPPORTED_TYPE", diag.CategoryNeo4j)

	// E_NEO4J_UNKNOWN_PROPERTY indicates an index annotation names a property
	// the type does not have. Load-time validation refuses this on every schema
	// a public entry point returns, so the generator's re-check draws this code
	// on none of them.
	E_NEO4J_UNKNOWN_PROPERTY = diag.NewCode("E_NEO4J_UNKNOWN_PROPERTY", diag.CategoryNeo4j)

	// E_NEO4J_INVALID_INDEX_TARGET indicates an index annotation names a
	// property whose type cannot carry that index: @index or @@index on a
	// non-scalar, @fulltext or @@fulltext on a non-text property, @vector on a
	// property that is not a Vector, or @vector on a Vector whose dimension is
	// not positive. Like [E_NEO4J_UNKNOWN_PROPERTY], this is the generator
	// re-checking what load-time validation refuses, so no schema a public entry
	// point returns draws it.
	E_NEO4J_INVALID_INDEX_TARGET = diag.NewCode("E_NEO4J_INVALID_INDEX_TARGET", diag.CategoryNeo4j)
)

// Warning-severity diagnostic codes for edition degradations.
var (
	// W_NEO4J_NODE_KEY_UNSUPPORTED indicates [Config.NodeKeys] was combined
	// with [Community], which cannot hold a NODE KEY
	// constraint. The emitter falls back to the UNIQUE half — the strongest
	// primary-key enforcement Community affords — and reports this warning so the
	// substitution is visible. A warning, not an error: the fallback is correct
	// output, so failing the call would withhold a usable answer.
	W_NEO4J_NODE_KEY_UNSUPPORTED = diag.NewCode("W_NEO4J_NODE_KEY_UNSUPPORTED", diag.CategoryNeo4j)

	// W_NEO4J_EDITION_CONSTRAINT_OMITTED indicates [Community] dropped
	// constraints the schema declares because the edition cannot hold
	// their kind — NOT NULL and PROPERTY_TYPE have no Community equivalent.
	// Reported once per call with a count per omitted kind, so the script's
	// reader knows which declared guarantees the database will not enforce. A
	// warning, not an error: the UNIQUE constraints that survive are correct
	// output for the edition.
	W_NEO4J_EDITION_CONSTRAINT_OMITTED = diag.NewCode("W_NEO4J_EDITION_CONSTRAINT_OMITTED", diag.CategoryNeo4j)
)

// Sentinel errors for identifier validation failures.
var (
	ErrEmptyIdentifier   = mapping.ErrEmptyIdentifier
	ErrReservedKeyword   = mapping.ErrReservedKeyword
	ErrInvalidIdentifier = mapping.ErrInvalidIdentifier
)
