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
