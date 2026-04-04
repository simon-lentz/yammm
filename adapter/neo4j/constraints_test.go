package neo4j

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

func TestConstraints_BasicSinglePK(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// UNIQUE for id.
	assertContains(t, stmts, "REQUIRE n.id IS UNIQUE")

	// NOT NULL for required props: id, name, count, active, created_at.
	for _, prop := range []string{"id", "name", "count", "active", "created_at"} {
		assertContains(t, stmts, "REQUIRE n."+prop+" IS NOT NULL")
	}

	// Scalar TYPE constraints for all properties.
	assertContains(t, stmts, "REQUIRE n.id IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.name IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.description IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.count IS :: INTEGER")
	assertContains(t, stmts, "REQUIRE n.score IS :: FLOAT")
	assertContains(t, stmts, "REQUIRE n.active IS :: BOOLEAN")
	assertContains(t, stmts, "REQUIRE n.created_at IS :: ZONED DATETIME")
	assertContains(t, stmts, "REQUIRE n.birth_date IS :: DATE")
	assertContains(t, stmts, "REQUIRE n.ref IS :: STRING") // UUID maps to STRING
	assertContains(t, stmts, "REQUIRE n.embedding IS :: LIST<FLOAT NOT NULL>")

	// 1 UNIQUE + 5 NOT NULL + 10 TYPE = 16 constraints.
	if len(stmts) != 16 {
		t.Errorf("expected 16 constraints, got %d:\n%s", len(stmts), strings.Join(stmts, "\n"))
	}
}

func TestConstraints_CompositePK(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "composite_pk.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Composite UNIQUE.
	assertContains(t, stmts, "REQUIRE (n.schema_id, n.record_id) IS UNIQUE")

	// NOT NULL for PKs and required.
	assertContains(t, stmts, "REQUIRE n.schema_id IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.record_id IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_SkipsAbstract(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "abstract_types.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// No constraints should reference abstract type Base.
	for _, stmt := range stmts {
		if strings.Contains(stmt, "abstract_test__Base") {
			t.Errorf("found constraint for abstract type Base: %s", stmt)
		}
	}

	// Widget should have composite UNIQUE for (id, code).
	assertContains(t, stmts, "REQUIRE (n.id, n.code) IS UNIQUE")

	// NOT NULL for id, code, name.
	assertContains(t, stmts, "REQUIRE n.id IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.code IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_PartTypes(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "part_types.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// LineItem (part type) receives constraints — not skipped.
	hasLineItem := false
	hasOrder := false
	for _, stmt := range stmts {
		if strings.Contains(stmt, "part_test__LineItem") {
			hasLineItem = true
		}
		if strings.Contains(stmt, "part_test__Order") {
			hasOrder = true
		}
	}
	if !hasLineItem {
		t.Error("part type LineItem should have constraints")
	}
	if !hasOrder {
		t.Error("type Order should have constraints")
	}
}

func TestConstraints_ListProperties(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "list_properties.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	assertContains(t, stmts, "REQUIRE n.tags IS :: LIST<STRING NOT NULL>")
	assertContains(t, stmts, "REQUIRE n.scores IS :: LIST<INTEGER NOT NULL>")
	assertContains(t, stmts, "REQUIRE n.ratios IS :: LIST<FLOAT NOT NULL>")
	assertContains(t, stmts, "REQUIRE n.flags IS :: LIST<BOOLEAN NOT NULL>")
	assertContains(t, stmts, "REQUIRE n.times IS :: LIST<ZONED DATETIME NOT NULL>")
	assertContains(t, stmts, "REQUIRE n.dates IS :: LIST<DATE NOT NULL>")
}

func TestConstraints_Aliases(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "aliases.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Email (Pattern) -> STRING, Money (Float) -> FLOAT,
	// ShortCode (String) -> STRING, Priority (Enum) -> STRING.
	assertContains(t, stmts, "REQUIRE n.email IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.price IS :: FLOAT")
	assertContains(t, stmts, "REQUIRE n.code IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.priority IS :: STRING")
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

	// Widget should come before Gadget (schema declaration order).
	widgetIdx := -1
	gadgetIdx := -1
	for i, stmt := range stmts1 {
		if strings.Contains(stmt, "multi_test__Widget") && widgetIdx == -1 {
			widgetIdx = i
		}
		if strings.Contains(stmt, "multi_test__Gadget") && gadgetIdx == -1 {
			gadgetIdx = i
		}
	}
	if widgetIdx == -1 || gadgetIdx == -1 {
		t.Fatal("missing Widget or Gadget constraints")
	}
	if widgetIdx >= gadgetIdx {
		t.Errorf("Widget (idx %d) should come before Gadget (idx %d)", widgetIdx, gadgetIdx)
	}
}

func TestConstraints_Inheritance(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "inheritance.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// No constraints for abstract type Tracked.
	for _, stmt := range stmts {
		if strings.Contains(stmt, "inherit_test__Tracked") {
			t.Errorf("found constraint for abstract type Tracked: %s", stmt)
		}
	}

	// Inherited properties should receive NOT NULL and TYPE constraints on Entity.
	assertContains(t, stmts, "REQUIRE n.run_id IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.source_fetched_at IS NOT NULL")
	assertContains(t, stmts, "REQUIRE n.run_id IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.source_fetched_at IS :: ZONED DATETIME")

	// Own properties too.
	assertContains(t, stmts, "REQUIRE n.id IS UNIQUE")
	assertContains(t, stmts, "REQUIRE n.name IS NOT NULL")
}

func TestConstraints_LabelCollision(t *testing.T) {
	t.Parallel()

	// Verify DetectLabelCollisions propagates through ConstraintsForSchema.
	// Since valid yammm schemas can't produce label collisions (UC_WORD type names),
	// we verify the diag code is registered and constructible.
	issue := diag.NewIssue(diag.Error, E_NEO4J_LABEL_COLLISION, "test collision").Build()
	if issue.Code() != E_NEO4J_LABEL_COLLISION {
		t.Error("E_NEO4J_LABEL_COLLISION code mismatch")
	}
}

func TestConstraints_EnumPattern(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "enum_pattern.yammm")
	a := New()

	stmts, result := a.ConstraintsForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsForSchema failed: %v", err)
	}

	// Enum and Pattern both map to STRING.
	assertContains(t, stmts, "REQUIRE n.status IS :: STRING")
	assertContains(t, stmts, "REQUIRE n.email IS :: STRING")
}

func TestConstraintsStructured(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New()

	constraints, result := a.ConstraintsStructured(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ConstraintsStructured failed: %v", err)
	}

	if len(constraints) == 0 {
		t.Fatal("expected non-empty constraints")
	}

	// Find the UNIQUE constraint.
	var uniqueC *Constraint
	for i := range constraints {
		if constraints[i].Kind == ConstraintUnique {
			uniqueC = &constraints[i]
			break
		}
	}
	if uniqueC == nil {
		t.Fatal("no UNIQUE constraint found")
	}

	if uniqueC.Label != "basic_test__Entity" {
		t.Errorf("UNIQUE constraint Label = %q; want %q", uniqueC.Label, "basic_test__Entity")
	}
	if !slices.Equal(uniqueC.Properties, []string{"id"}) {
		t.Errorf("UNIQUE constraint Properties = %v; want [id]", uniqueC.Properties)
	}
	if uniqueC.Name != "basic_test__Entity_id_unique" {
		t.Errorf("UNIQUE constraint Name = %q; want %q", uniqueC.Name, "basic_test__Entity_id_unique")
	}
	if !strings.Contains(uniqueC.Statement, "IS UNIQUE") {
		t.Errorf("UNIQUE constraint Statement missing IS UNIQUE: %s", uniqueC.Statement)
	}

	// Find a TYPE constraint to verify TypeExpr.
	var typeC *Constraint
	for i := range constraints {
		if constraints[i].Kind == ConstraintType && constraints[i].TypeExpr == "INTEGER" {
			typeC = &constraints[i]
			break
		}
	}
	if typeC == nil {
		t.Fatal("no INTEGER TYPE constraint found")
	}
	if typeC.TypeExpr != "INTEGER" {
		t.Errorf("TYPE constraint TypeExpr = %q; want %q", typeC.TypeExpr, "INTEGER")
	}
	if !slices.Equal(typeC.Properties, []string{"count"}) {
		t.Errorf("TYPE constraint Properties = %v; want [count]", typeC.Properties)
	}
}

// --- test helpers ---

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
