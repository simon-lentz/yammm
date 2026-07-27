package neo4j

import (
	"errors"

	"github.com/simon-lentz/yammm/diag"
)

// Diagnostic codes for neo4j adapter errors.
var (
	// E_NEO4J_LABEL_COLLISION indicates two or more types produce the same
	// Neo4j label after sanitization.
	E_NEO4J_LABEL_COLLISION = diag.NewCode("E_NEO4J_LABEL_COLLISION", diag.CategoryAdapter)

	// E_NEO4J_INVALID_IDENTIFIER indicates a property or type name is not
	// a valid Neo4j identifier (fails regex or matches a Cypher reserved keyword).
	E_NEO4J_INVALID_IDENTIFIER = diag.NewCode("E_NEO4J_INVALID_IDENTIFIER", diag.CategoryAdapter)

	// E_NEO4J_UNSUPPORTED_TYPE indicates a constraint kind cannot be mapped
	// to a Neo4j property type expression.
	E_NEO4J_UNSUPPORTED_TYPE = diag.NewCode("E_NEO4J_UNSUPPORTED_TYPE", diag.CategoryAdapter)

	// E_NEO4J_UNKNOWN_PROPERTY indicates an index annotation names a property
	// the type does not have. Load-time validation catches this wherever it can
	// resolve the type's full member set; where it must defer — a type whose
	// supertype never resolved — the adapter catches it rather than emitting DDL
	// for a property that will never exist.
	E_NEO4J_UNKNOWN_PROPERTY = diag.NewCode("E_NEO4J_UNKNOWN_PROPERTY", diag.CategoryAdapter)

	// E_NEO4J_INVALID_INDEX_TARGET indicates an index annotation names a
	// property whose type cannot carry that index: @index or @@index on a
	// non-scalar, @vector on a property that is not a Vector, or @vector on a
	// Vector whose dimension is not positive. Like [E_NEO4J_UNKNOWN_PROPERTY],
	// this is the adapter re-checking what load-time validation had to defer
	// because the target's type never resolved.
	E_NEO4J_INVALID_INDEX_TARGET = diag.NewCode("E_NEO4J_INVALID_INDEX_TARGET", diag.CategoryAdapter)
)

// Warning-severity diagnostic codes for neo4j adapter degradations.
var (
	// W_NEO4J_NODE_KEY_UNSUPPORTED indicates [WithNodeKeyConstraints](true) was
	// combined with [WithEdition]([Community]), which cannot hold a NODE KEY
	// constraint. The emitter falls back to the UNIQUE half — the strongest
	// primary-key enforcement Community affords — and reports this warning so the
	// substitution is visible. A warning, not an error: the fallback is correct
	// output, so failing the call would withhold a usable answer.
	W_NEO4J_NODE_KEY_UNSUPPORTED = diag.NewCode("W_NEO4J_NODE_KEY_UNSUPPORTED", diag.CategoryAdapter)
)

// Sentinel errors for validation failures.
var (
	ErrEmptyIdentifier     = errors.New("neo4j adapter: identifier is empty")
	ErrReservedKeyword     = errors.New("neo4j adapter: identifier is a Cypher reserved keyword")
	ErrInvalidIdentifier   = errors.New("neo4j adapter: identifier contains invalid characters")
	ErrUnsupportedListElem = errors.New("neo4j adapter: unsupported list element type")
)

// detailKeyLabel is the neo4j-specific detail key for generated label strings.
const detailKeyLabel = "label"
