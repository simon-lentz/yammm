package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// TestIndexesForSchema_Emission pins the load-bearing statement shapes: a
// property-level @index range index, a type-level @@index composite (declared
// order preserved), a @vector ANN index with its dimension and similarity, an
// inherited @@index emitting on a concrete subtype's own label, and a part-type
// index. Abstract types emit nothing directly.
func TestIndexesForSchema_Emission(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	stmts, result := New().IndexesForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("IndexesForSchema: %v", err)
	}

	// Range index from a property-level @index.
	assertContains(t, stmts,
		"CREATE INDEX index_test__Document_state_idx IF NOT EXISTS FOR (n:index_test__Document) ON (n.state)")
	// Composite range index from a type-level @@index; declared order preserved.
	assertContains(t, stmts,
		"CREATE INDEX index_test__Document_state_published_on_idx IF NOT EXISTS FOR (n:index_test__Document) ON (n.state, n.published_on)")
	// Vector index from @vector(cosine); dimension from Vector[768].
	assertContains(t, stmts,
		"CREATE VECTOR INDEX index_test__Document_embedding_vector_idx IF NOT EXISTS FOR (n:index_test__Document) ON (n.embedding) OPTIONS {indexConfig: {`vector.dimensions`: 768, `vector.similarity_function`: 'cosine'}}")
	// Inherited @@index emits on each concrete subtype's own label.
	assertContains(t, stmts,
		"CREATE INDEX index_test__Note_first_seen_idx IF NOT EXISTS FOR (n:index_test__Note) ON (n.first_seen)")
	// Part types are not skipped (they get a label and constraints today).
	assertContains(t, stmts,
		"CREATE INDEX index_test__Section_heading_idx IF NOT EXISTS FOR (n:index_test__Section) ON (n.heading)")

	// Abstract types emit nothing directly.
	assertNotContains(t, stmts, "index_test__Tracked")
}

// TestIndexesForSchema_Golden pins the complete emission list and order.
func TestIndexesForSchema_Golden(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	stmts, result := New().IndexesForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("IndexesForSchema: %v", err)
	}
	yammmtest.Golden(t, "indexes_all", []byte(strings.Join(stmts, "\n")+"\n"))
}

// TestIndexesStructured_Golden pins the full structured form of each Index.
func TestIndexesStructured_Golden(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("IndexesStructured: %v", err)
	}
	yammmtest.GoldenJSON(t, "indexes_structured_all", indexes)
}

// TestIndexes_DeterministicOrder confirms call-to-call stability; the emission
// order itself is pinned by TestIndexesForSchema_Golden.
func TestIndexes_DeterministicOrder(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes.yammm")
	a := New()

	stmts1, result := a.IndexesForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("first call: %v", err)
	}
	stmts2, result := a.IndexesForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("second call: %v", err)
	}
	if !slices.Equal(stmts1, stmts2) {
		t.Error("IndexesForSchema produced different output on second call")
	}
}

// TestIndexes_NameCollision pins that two @@index composites whose emitted
// names collide (underscore-join ambiguity) are reported, not silently emitted:
// CREATE ... IF NOT EXISTS would skip the second index.
func TestIndexes_NameCollision(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_collision.yammm")
	indexes, result := New().IndexesStructured(context.Background(), s)
	if !result.HasCode(E_NEO4J_INDEX_NAME_COLLISION) {
		t.Fatalf("expected E_NEO4J_INDEX_NAME_COLLISION, got: %v", result.Err())
	}
	if indexes != nil {
		t.Errorf("expected nil indexes on collision, got %d", len(indexes))
	}
}

// TestIndexes_InvalidPropertyIdentifier pins that a property whose name is a
// Cypher reserved keyword is rejected at emission. Such a name is a valid DSL
// property name but not a valid unquoted Neo4j identifier.
func TestIndexes_InvalidPropertyIdentifier(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "indexes_bad_ident.yammm")
	_, result := New().IndexesStructured(context.Background(), s)
	if !result.HasCode(E_NEO4J_INVALID_IDENTIFIER) {
		t.Fatalf("expected E_NEO4J_INVALID_IDENTIFIER, got: %v", result.Err())
	}
}
