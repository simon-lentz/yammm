package cypher

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestCodes_EveryNeo4jCodeIsInCategoryNeo4j pins each code the package
// declares, the one internal/mapping registers included, to [diag.CategoryNeo4j].
func TestCodes_EveryNeo4jCodeIsInCategoryNeo4j(t *testing.T) {
	t.Parallel()

	for _, code := range []diag.Code{
		E_NEO4J_INVALID_IDENTIFIER,
		E_NEO4J_UNSUPPORTED_TYPE,
		E_NEO4J_UNKNOWN_PROPERTY,
		E_NEO4J_INVALID_INDEX_TARGET,
		W_NEO4J_NODE_KEY_UNSUPPORTED,
		W_NEO4J_EDITION_CONSTRAINT_OMITTED,
	} {
		if got := code.Category(); got != diag.CategoryNeo4j {
			t.Errorf("%s is in category %s, want %s", code, got, diag.CategoryNeo4j)
		}
	}
}
