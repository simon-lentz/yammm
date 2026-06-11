package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

// TestConstraintsForSchema_Golden pins the complete default-option statement
// list per schema fixture: statement text, statement count, and emission
// order. Absences are load-bearing — the abstract_types and inheritance
// goldens contain no statements for their abstract types, and the per-fixture
// type mappings (aliases, enum/pattern, lists, UUID→STRING) are pinned in
// full rather than per-substring.
func TestConstraintsForSchema_Golden(t *testing.T) {
	t.Parallel()
	fixtures := []string{
		"abstract_types", "aliases", "basic", "composite_pk", "enum_pattern",
		"inheritance", "list_properties", "multiple_types", "part_types",
	}
	for _, fixture := range fixtures {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			s := loadSchema(t, fixture+".yammm")
			stmts, result := New().ConstraintsForSchema(context.Background(), s)
			if err := result.Err(); err != nil {
				t.Fatalf("ConstraintsForSchema(%s): %v", fixture, err)
			}
			yammmtest.Golden(t, "constraints_"+fixture, []byte(strings.Join(stmts, "\n")+"\n"))
		})
	}
}

func TestConstraints_NamedConstraints(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New() // Default: named=true.

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Every statement should contain a name before IF NOT EXISTS.
	for _, stmt := range stmts {
		prefix := "CREATE CONSTRAINT "
		after := strings.TrimPrefix(stmt, prefix)
		if strings.HasPrefix(after, "IF NOT EXISTS") {
			t.Errorf("named constraint missing name: %s", stmt)
		}
	}

	// Check specific names.
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_unique IF NOT EXISTS")
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_not_null IF NOT EXISTS")
	assertContains(t, stmts, "CREATE CONSTRAINT basic_test__Entity_id_type IF NOT EXISTS")
}

func TestConstraints_UnnamedConstraints(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNamedConstraints(false))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Every statement should have no name — "CREATE CONSTRAINT IF NOT EXISTS".
	for _, stmt := range stmts {
		if !strings.HasPrefix(stmt, "CREATE CONSTRAINT IF NOT EXISTS") {
			t.Errorf("unnamed constraint has unexpected format: %s", stmt)
		}
	}
}

func TestConstraints_NodeKey(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithNodeKeyConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Should use NODE KEY instead of UNIQUE.
	assertContains(t, stmts, "REQUIRE n.id IS NODE KEY")
	assertNotContains(t, stmts, "IS UNIQUE")

	// PK property NOT NULL should be omitted (NODE KEY implies NOT NULL).
	assertNotContains(t, stmts, "REQUIRE n.id IS NOT NULL")

	// Non-PK required properties should still have NOT NULL.
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_NodeKeyComposite(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New(WithNodeKeyConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	assertContains(t, stmts, "REQUIRE (n.schema_id, n.record_id) IS NODE KEY")
	assertNotContains(t, stmts, "IS UNIQUE")

	// PK NOT NULL should be omitted.
	assertNotContains(t, stmts, "REQUIRE n.schema_id IS NOT NULL")
	assertNotContains(t, stmts, "REQUIRE n.record_id IS NOT NULL")

	// Non-PK required should remain.
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_CommunityEdition(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithEdition(Community))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Only UNIQUE constraints should be present.
	for _, stmt := range stmts {
		if !strings.Contains(stmt, "IS UNIQUE") {
			t.Errorf("Community edition should only have UNIQUE, got: %s", stmt)
		}
	}
	if len(stmts) != 1 {
		t.Errorf("expected exactly 1 UNIQUE constraint, got %d", len(stmts))
	}
}

func TestConstraints_RequiredOnlyTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithRequiredOnlyTypeConstraints(true))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Required properties should have TYPE constraints.
	assertContains(t, stmts, "REQUIRE n.id IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.name IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.count IS :: INTEGER")
	assertContains(t, stmts, "REQUIRE n.active IS :: BOOLEAN")
	assertContains(t, stmts, "REQUIRE n.created_at IS :: ZONED DATETIME")

	// Optional properties should NOT have TYPE constraints.
	assertNotContains(t, stmts, "REQUIRE n.description IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.score IS :: FLOAT")
	assertNotContains(t, stmts, "REQUIRE n.birth_date IS :: DATE")
	assertNotContains(t, stmts, "REQUIRE n.ref IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.embedding IS ::")
}

func TestConstraints_ScalarTypesDisabled(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	a := New(WithScalarTypeConstraints(false))

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// LIST TYPE constraints should still be generated.
	assertContains(t, stmts, "REQUIRE n.tags IS :: LIST<STRING NOT NULL>")

	// Scalar TYPE constraints should NOT be generated.
	assertNotContains(t, stmts, "REQUIRE n.id IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.name IS :: STRING")
	assertNotContains(t, stmts, "REQUIRE n.active IS :: BOOLEAN")
}

func TestConstraints_DeterministicOrder(t *testing.T) {
	t.Parallel()
	// Call-to-call determinism; the emission order itself is pinned by the
	// per-fixture goldens in TestConstraintsForSchema_Golden.
	s := loadSchema(t, "multiple_types.yammm")
	a := New()

	stmts1, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	stmts2, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if !slices.Equal(stmts1, stmts2) {
		t.Error("ConstraintsForSchema produced different output on second call")
	}
}

// TestConstraintsStructured_Golden pins the full structured form — Name,
// Kind, Label, Properties, TypeExpr, and the complete Statement — for the
// default options over the basic fixture.
func TestConstraintsStructured_Golden(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")

	constraints, result := New().ConstraintsStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsStructured failed: %v", err)
	}
	yammmtest.GoldenJSON(t, "constraints_structured_basic", constraints)
}

func assertContains(t *testing.T, stmts []string, substring string) {
	t.Helper()
	for _, stmt := range stmts {
		if strings.Contains(stmt, substring) {
			return
		}
	}
	t.Errorf("no statement contains %q\nstatements:\n%s", substring, strings.Join(stmts, "\n"))
}

func assertNotContains(t *testing.T, stmts []string, substring string) {
	t.Helper()
	for _, stmt := range stmts {
		if strings.Contains(stmt, substring) {
			t.Errorf("unexpected statement containing %q: %s", substring, stmt)
			return
		}
	}
}
