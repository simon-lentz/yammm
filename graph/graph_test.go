package graph_test

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// testSchema creates a simple schema for testing.
func testSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build test schema: %s", result.String())
	}
	return s
}

// testSchemaWithPKLessType creates a schema with a type that has no PK.
func testSchemaWithPKLessType(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Abstract").
		AsAbstract().
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build test schema: %s", result.String())
	}
	return s
}

func TestNew(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	if g == nil {
		t.Fatal("graph.New() returned nil")
	}
}

func TestNew_PanicOnNilSchema(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("graph.New(nil) should panic")
		}
	}()

	graph.New(nil)
}

func TestGraph_Add_NilReceiver(t *testing.T) {
	var g *graph.Graph
	ctx := t.Context()
	s := testSchema(t)
	personType, _ := s.Type("Person")

	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)

	yammmtest.AssertPanics(t, func() { g.Add(ctx, inst) })
}

func TestGraph_Add_NilInstance(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	defer func() {
		if r := recover(); r == nil {
			t.Error("Add(nil) should panic")
		}
	}()
	g.Add(ctx, nil)
}

func TestGraph_Add_SchemaMismatch(t *testing.T) {
	// graph.Instance validated against a completely different schema should fail
	schemaA, _ := schema.NewBuilder().
		WithName("schema_a").
		WithSourceID(location.MustNewSourceID("test://a.yammm")).
		AddType("TypeA").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()

	schemaB, _ := schema.NewBuilder().
		WithName("schema_b").
		WithSourceID(location.MustNewSourceID("test://b.yammm")).
		AddType("TypeB").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()

	g := graph.New(schemaA)
	ctx := t.Context()

	// Create instance from schemaB
	typeB, _ := schemaB.Type("TypeB")
	inst := instance.NewValidInstance(
		"TypeB",
		typeB.ID(), // TypeID points to schemaB
		immutable.WrapKey([]any{"x1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	defer func() {
		if r := recover(); r == nil {
			t.Error("Add with mismatched schema should panic")
		}
	}()
	g.Add(ctx, inst)
}

func TestGraph_Add_ImportedSchemaAllowed(t *testing.T) {
	// graph.Instance from an imported schema should be allowed
	mainSchema, importedSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Create instance from the imported schema (common)
	entityType, _ := importedSchema.Type("Entity")
	inst := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(), // TypeID points to imported schema
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Test Entity"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if !result.OK() {
		t.Errorf("Add with imported schema instance should succeed, got %s", result.String())
	}
}

func TestGraph_Add_ContextCancellation(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // Cancel immediately

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if result.OK() {
		t.Error("Add with canceled context should not be OK")
	}

	hasFatal := false
	for issue := range result.Issues() {
		if issue.Severity() == diag.Fatal {
			hasFatal = true
			break
		}
	}
	if !hasFatal {
		t.Error("Add with canceled context should produce a Fatal diagnostic")
	}
}

func TestGraph_Add_Success(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if err := result.Err(); err != nil {
		t.Errorf("Add() should succeed: %v", err)
	}

	// Verify instance is in graph
	snap := g.Snapshot()
	types := snap.Types()
	if len(types) != 1 || types[0] != "Person" {
		t.Errorf("Types() = %v, want [\"Person\"]", types)
	}

	instances := snap.InstancesOf("Person")
	if len(instances) != 1 {
		t.Fatalf("InstancesOf(\"Person\") returned %d instances, want 1", len(instances))
	}
	if instances[0].TypeName() != "Person" {
		t.Errorf("graph.Instance.TypeName() = %q, want \"Person\"", instances[0].TypeName())
	}
	if instances[0].PrimaryKey().String() != `["alice"]` {
		t.Errorf("graph.Instance.PrimaryKey() = %q, want [\"alice\"]", instances[0].PrimaryKey().String())
	}
}

func TestGraph_Add_DuplicatePK(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Add first instance
	inst1 := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)
	if r := g.Add(ctx, inst1); !r.OK() {
		t.Fatalf("First Add() failed: %s", r.String())
	}

	// Add duplicate
	inst2 := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice 2"}),
		nil, nil, nil,
	)
	result := g.Add(ctx, inst2)
	if result.OK() {
		t.Error("Add() with duplicate PK should fail")
	}

	// Check for E_DUPLICATE_PK
	assertHasCode(t, result, diag.E_DUPLICATE_PK)

	// Verify snapshot has duplicates
	snap := g.Snapshot()
	dups := snap.Duplicates()
	if len(dups) != 1 {
		t.Errorf("graph.Snapshot.Duplicates() returned %d, want 1", len(dups))
	}
}

func TestGraph_Add_MissingPK(t *testing.T) {
	s := testSchemaWithPKLessType(t)
	g := graph.New(s)
	ctx := t.Context()

	abstractType, _ := s.Type("Abstract")
	inst := instance.NewValidInstance(
		"Abstract",
		abstractType.ID(),
		immutable.Key{}, // No PK
		immutable.WrapProperties(map[string]any{"name": "Test"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if result.OK() {
		t.Error("Add() with PK-less type should fail")
	}

	// Check for E_GRAPH_MISSING_PK
	assertHasCode(t, result, diag.E_GRAPH_MISSING_PK)
}

func TestGraph_Snapshot_DeterministicOrder(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Add instances in non-sorted order
	names := []string{"charlie", "alice", "bob"}
	for _, name := range names {
		inst := instance.NewValidInstance(
			"Person",
			personType.ID(),
			immutable.WrapKey([]any{name}),
			immutable.WrapProperties(map[string]any{"name": name}),
			nil, nil, nil,
		)
		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add(%s) failed: %s", name, r.String())
		}
	}

	// Verify sorted order in snapshot
	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	if len(instances) != 3 {
		t.Fatalf("Expected 3 instances, got %d", len(instances))
	}

	expected := []string{`["alice"]`, `["bob"]`, `["charlie"]`}
	for i, inst := range instances {
		if inst.PrimaryKey().String() != expected[i] {
			t.Errorf("graph.Instance[%d].PrimaryKey() = %q, want %q",
				i, inst.PrimaryKey().String(), expected[i])
		}
	}
}

func TestGraph_InstanceByKey(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)

	if r := g.Add(ctx, inst); !r.OK() {
		t.Fatalf("Add() failed: %s", r.String())
	}

	snap := g.Snapshot()

	// Lookup by key
	found, ok := snap.InstanceByKey("Person", graph.FormatKey("alice"))
	if !ok {
		t.Fatal("InstanceByKey() should find the instance")
	}
	if found.TypeName() != "Person" {
		t.Errorf("Found instance TypeName() = %q, want \"Person\"", found.TypeName())
	}

	// Lookup non-existent
	_, ok = snap.InstanceByKey("Person", graph.FormatKey("bob"))
	if ok {
		t.Error("InstanceByKey() should not find non-existent instance")
	}

	// Lookup non-existent type
	_, ok = snap.InstanceByKey("NonExistent", graph.FormatKey("alice"))
	if ok {
		t.Error("InstanceByKey() should not find instance of non-existent type")
	}
}

func TestGraph_Check_NilReceiver(t *testing.T) {
	var g *graph.Graph
	yammmtest.AssertPanics(t, func() { g.Check(t.Context()) })
}

func TestSnapshot_NilReceiver(t *testing.T) {
	var r *graph.Snapshot

	// All methods should handle nil gracefully
	if r.Types() != nil {
		t.Error("nil.Types() should return nil")
	}
	if r.InstancesOf("any") != nil {
		t.Error("nil.InstancesOf() should return nil")
	}
	if r.Instances() != nil {
		t.Error("nil.Instances() should return nil")
	}
	if _, ok := r.InstanceByKey("any", "any"); ok {
		t.Error("nil.InstanceByKey() should return false")
	}
	if r.Edges() != nil {
		t.Error("nil.Edges() should return nil")
	}
	if !r.Diagnostics().OK() {
		t.Error("nil.Diagnostics() should be OK")
	}
	if r.Duplicates() != nil {
		t.Error("nil.Duplicates() should return nil")
	}
	if r.Unresolved() != nil {
		t.Error("nil.Unresolved() should return nil")
	}
	if !r.OK() {
		t.Error("nil.OK() should return true")
	}
	if r.HasErrors() {
		t.Error("nil.HasErrors() should return false")
	}
}

func TestInstance_NilReceiver(t *testing.T) {
	var i *graph.Instance

	// All methods should handle nil gracefully
	if i.TypeName() != "" {
		t.Error("nil.TypeName() should return empty string")
	}
	if !i.TypeID().IsZero() {
		t.Error("nil.TypeID() should be zero")
	}
	if i.PrimaryKey().Len() != 0 {
		t.Error("nil.PrimaryKey() should be empty")
	}
	if _, ok := i.Property("any"); ok {
		t.Error("nil.Property() should return false")
	}
	if i.Properties().Len() != 0 {
		t.Error("nil.Properties() should be empty")
	}
	if i.Composed("any") != nil {
		t.Error("nil.Composed() should return nil")
	}
	if i.ComposedCount("any") != 0 {
		t.Error("nil.ComposedCount() should return 0")
	}
	if i.HasComposed("any") {
		t.Error("nil.HasComposed() should return false")
	}
}

func TestEdge_NilReceiver(t *testing.T) {
	var e *graph.Edge

	// All methods should handle nil gracefully
	if e.Relation() != "" {
		t.Error("nil.Relation() should return empty string")
	}
	if e.Source() != nil {
		t.Error("nil.Source() should return nil")
	}
	if e.Target() != nil {
		t.Error("nil.Target() should return nil")
	}
	if _, ok := e.Property("any"); ok {
		t.Error("nil.Property() should return false")
	}
	if e.Properties().Len() != 0 {
		t.Error("nil.Properties() should be empty")
	}
	if e.HasProperties() {
		t.Error("nil.HasProperties() should return false")
	}
}

// Part Type Tests

func TestGraph_Add_PartType_Rejected(t *testing.T) {
	// Part type via Add() → E_GRAPH_INVALID_COMPOSITION
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Try to add a part type directly (should fail)
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result := g.Add(ctx, child)
	if result.OK() {
		t.Error("Add should fail for part type added directly")
	}

	assertHasCode(t, result, diag.E_GRAPH_INVALID_COMPOSITION)
}

func TestGraph_PartType_NoPK_Positional(t *testing.T) {
	// PK-less parts have positional identity - multiple can coexist
	s := testSchemaWithPKLessChild(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Container
	container := mustValidInstance(t, s, "Container",
		[]any{"box1"}, map[string]any{"name": "Box 1"})

	if r := g.Add(ctx, container); !r.OK() {
		t.Fatalf("Add container failed: %s", r.String())
	}

	// Add multiple PK-less items with same data
	for i := range 3 {
		item := mustValidPKLessInstance(t, s, "Item",
			map[string]any{"value": "same-value"})

		result := g.AddComposed(ctx, "Container", graph.FormatKey("box1"), "items", item)
		if err := result.Err(); err != nil {
			t.Errorf("AddComposed item %d should succeed (positional identity): %v", i, err)
		}
	}

	// Verify all 3 items are present
	snap := g.Snapshot()
	containers := snap.InstancesOf("Container")
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}

	assertComposedCount(t, containers[0], "items", 3)
}

func TestGraph_PartType_WithinParent_Uniqueness(t *testing.T) {
	// Same PK in same parent = duplicate
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add parent failed: %s", r.String())
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"shared-pk"}, map[string]any{"name": "Child 1"})

	result := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child1)
	if err := result.Err(); err != nil {
		t.Errorf("First child should succeed: %v", err)
	}

	// Add second child with same PK - should fail
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"shared-pk"}, map[string]any{"name": "Child 2"})

	result = g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child2)
	if result.OK() {
		t.Error("Second child with same PK should fail")
	}
}

func TestGraph_NestedComposition(t *testing.T) {
	// Part containing part (multi-level nesting)
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add parent failed: %s", r.String())
	}

	// Add Child
	child := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	if r := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); !r.OK() {
		t.Fatalf("AddComposed child failed: %s", r.String())
	}

	// Verify structure
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	children := parents[0].Composed("children")
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	if children[0].TypeName() != "Child" {
		t.Errorf("Child type should be Child, got %s", children[0].TypeName())
	}
}

// Ordering and graph.Edge Case Tests

func TestGraph_Duplicates_Ordering(t *testing.T) {
	// Duplicates should be sorted by (type, pk)
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")
	companyType, _ := s.Type("Company")

	// Add first instances
	alice := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)
	acme := instance.NewValidInstance(
		"Company", companyType.ID(),
		immutable.WrapKey([]any{"acme"}),
		immutable.WrapProperties(map[string]any{"name": "Acme"}),
		nil, nil, nil,
	)

	if r := g.Add(ctx, alice); !r.OK() {
		t.Fatal(r.String())
	}
	if r := g.Add(ctx, acme); !r.OK() {
		t.Fatal(r.String())
	}

	// Add duplicates in reverse order (Company before Person)
	acmeDup := instance.NewValidInstance(
		"Company", companyType.ID(),
		immutable.WrapKey([]any{"acme"}),
		immutable.WrapProperties(map[string]any{"name": "Acme Dup"}),
		nil, nil, nil,
	)
	aliceDup := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice Dup"}),
		nil, nil, nil,
	)

	g.Add(ctx, acmeDup)
	g.Add(ctx, aliceDup)

	snap := g.Snapshot()
	dups := snap.Duplicates()

	if len(dups) != 2 {
		t.Fatalf("Expected 2 duplicates, got %d", len(dups))
	}

	// Should be sorted: Company < Person lexicographically
	if dups[0].Instance.TypeName() != "Company" {
		t.Errorf("First duplicate should be Company, got %s", dups[0].Instance.TypeName())
	}
	if dups[1].Instance.TypeName() != "Person" {
		t.Errorf("Second duplicate should be Person, got %s", dups[1].Instance.TypeName())
	}
}

func TestGraph_Unresolved_Ordering(t *testing.T) {
	// Unresolved edges should be sorted by (sourceType, sourceKey, relation, targetType, targetKey)
	s := testSchemaWithChainedAssociations(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add instances with forward refs in non-sorted order
	typeC := mustValidInstance(t, s, "TypeC", []any{"c1"}, map[string]any{"name": "C1"})
	typeA2 := mustValidInstanceWithEdge(t, s, "TypeA", []any{"a2"}, map[string]any{"name": "A2"}, "refB", [][]any{{"b-missing"}})
	typeA1 := mustValidInstanceWithEdge(t, s, "TypeA", []any{"a1"}, map[string]any{"name": "A1"}, "refB", [][]any{{"b-missing"}})
	typeB := mustValidInstanceWithEdge(t, s, "TypeB", []any{"b1"}, map[string]any{"name": "B1"}, "refC", [][]any{{"c-missing"}})

	// Add in scrambled order
	if r := g.Add(ctx, typeC); !r.OK() {
		t.Fatal(r.String())
	}
	if r := g.Add(ctx, typeA2); !r.OK() {
		t.Fatal(r.String())
	}
	if r := g.Add(ctx, typeB); !r.OK() {
		t.Fatal(r.String())
	}
	if r := g.Add(ctx, typeA1); !r.OK() {
		t.Fatal(r.String())
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) < 2 {
		t.Fatalf("Expected at least 2 unresolved, got %d", len(unresolved))
	}

	// Verify order is deterministic: TypeA a1 should come before TypeA a2
	foundA1 := false
	for i, ur := range unresolved {
		if ur.Source.TypeName() == "TypeA" && ur.Source.PrimaryKey().String() == `["a1"]` {
			foundA1 = true
			// Check that if there's another TypeA, it has a higher key
			for j := i + 1; j < len(unresolved); j++ {
				if unresolved[j].Source.TypeName() == "TypeA" {
					if unresolved[j].Source.PrimaryKey().String() < ur.Source.PrimaryKey().String() {
						t.Error("Unresolved edges not properly sorted by source key")
					}
				}
			}
			break
		}
	}
	if !foundA1 {
		t.Error("Expected unresolved edge from TypeA a1")
	}
}

func TestGraph_LargeGraph_Performance(t *testing.T) {
	// 1000+ instances, verify ordering is correct
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Add 1000 instances in random order (using a pattern that's not sorted)
	for i := range 1000 {
		pk := fmt.Sprintf("person-%04d", (i*7+13)%1000) // Scrambled order
		inst := instance.NewValidInstance(
			"Person", personType.ID(),
			immutable.WrapKey([]any{pk}),
			immutable.WrapProperties(map[string]any{"name": pk}),
			nil, nil, nil,
		)

		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add %s failed: %s", pk, r.String())
		}
	}

	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")

	if len(instances) != 1000 {
		t.Fatalf("Expected 1000 instances, got %d", len(instances))
	}

	// Verify sorted order
	for i := 1; i < len(instances); i++ {
		prev := instances[i-1].PrimaryKey().String()
		curr := instances[i].PrimaryKey().String()
		if prev >= curr {
			t.Errorf("Instances not sorted: %s >= %s at index %d", prev, curr, i)
			break
		}
	}
}

func TestGraph_SpecialChars_InKeys(t *testing.T) {
	// Spaces, quotes, brackets in PK values
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	specialKeys := []string{
		"hello world",       // space
		"with\"quote",       // quote
		"with'single",       // single quote
		"with[bracket]",     // brackets
		"path/to/thing",     // slashes
		"email@example.com", // at sign
		"special!@#$%",      // various special chars
		"unicode-κόσμος",    // unicode
		"tab\there",         // tab
		"",                  // empty string
		"  leading-space",   // leading spaces
		"trailing-space  ",  // trailing spaces
	}

	for _, key := range specialKeys {
		inst := instance.NewValidInstance(
			"Person", personType.ID(),
			immutable.WrapKey([]any{key}),
			immutable.WrapProperties(map[string]any{"name": key}),
			nil, nil, nil,
		)

		result := g.Add(ctx, inst)
		if err := result.Err(); err != nil {
			t.Errorf("Add key %q should succeed: %v", key, err)
		}
	}

	snap := g.Snapshot()
	if len(snap.InstancesOf("Person")) != len(specialKeys) {
		t.Errorf("Expected %d instances, got %d", len(specialKeys), len(snap.InstancesOf("Person")))
	}

	// Verify lookup works for each key
	for _, key := range specialKeys {
		formatted := graph.FormatKey(key)
		_, ok := snap.InstanceByKey("Person", formatted)
		if !ok {
			t.Errorf("InstanceByKey failed for key %q (formatted: %s)", key, formatted)
		}
	}
}

func TestGraph_Unicode_InKeys(t *testing.T) {
	// Unicode characters in key values
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	unicodeKeys := []string{
		"日本語",            // Japanese
		"한국어",            // Korean
		"中文",             // Chinese
		"العربية",        // Arabic
		"עברית",          // Hebrew
		"Ελληνικά",       // Greek
		"Кириллица",      // Cyrillic
		"🎉🚀🌍",            // Emoji
		"مرحبا-世界-Hello", // Mixed
	}

	for _, key := range unicodeKeys {
		inst := instance.NewValidInstance(
			"Person", personType.ID(),
			immutable.WrapKey([]any{key}),
			immutable.WrapProperties(map[string]any{"name": key}),
			nil, nil, nil,
		)

		result := g.Add(ctx, inst)
		if err := result.Err(); err != nil {
			t.Errorf("Add unicode key %q should succeed: %v", key, err)
		}
	}

	snap := g.Snapshot()
	if len(snap.InstancesOf("Person")) != len(unicodeKeys) {
		t.Errorf("Expected %d instances, got %d", len(unicodeKeys), len(snap.InstancesOf("Person")))
	}
}

func TestGraph_CompositeKey_Large(t *testing.T) {
	// 5+ component composite keys
	s := testSchemaWithCompositeKey(t)
	g := graph.New(s)
	ctx := t.Context()

	recordType, _ := s.Type("Record")

	// Add records with different composite keys
	regions := []string{"us-east", "us-west", "eu-west"}
	for _, region := range regions {
		for i := range 3 {
			inst := instance.NewValidInstance(
				"Record", recordType.ID(),
				immutable.WrapKey([]any{region, fmt.Sprintf("id-%d", i)}),
				immutable.WrapProperties(map[string]any{"value": fmt.Sprintf("%s-%d", region, i)}),
				nil, nil, nil,
			)

			result := g.Add(ctx, inst)
			if err := result.Err(); err != nil {
				t.Fatalf("Add should succeed: %v", err)
			}
		}
	}

	snap := g.Snapshot()
	instances := snap.InstancesOf("Record")

	if len(instances) != 9 {
		t.Fatalf("Expected 9 records, got %d", len(instances))
	}

	// Verify lookup by composite key works
	key := graph.FormatKey("us-east", "id-0")
	found, ok := snap.InstanceByKey("Record", key)
	if !ok {
		t.Errorf("InstanceByKey failed for composite key %s", key)
	}
	if found != nil {
		val, _ := found.Property("value")
		if str, _ := val.String(); str != "us-east-0" {
			t.Errorf("Wrong value for composite key: %s", str)
		}
	}
}

func TestGraph_EmptyProperties(t *testing.T) {
	// graph.Instance with no properties beyond PK
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Add person with minimal properties
	inst := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"minimal"}),
		immutable.WrapProperties(map[string]any{}), // No additional properties
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if err := result.Err(); err != nil {
		t.Fatalf("Add should succeed: %v", err)
	}

	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	if len(instances) != 1 {
		t.Fatalf("Expected 1 instance, got %d", len(instances))
	}

	// Verify the instance exists with empty properties
	found := instances[0]
	if found.Properties().Len() != 0 {
		t.Errorf("Expected 0 properties, got %d", found.Properties().Len())
	}
}

func TestGraph_ComposedRelations_Sorted(t *testing.T) {
	// ComposedRelations() returns sorted relation names
	s := testSchemaWithMultipleCompositions(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Document
	doc := mustValidInstance(t, s, "Document",
		[]any{"doc1"}, map[string]any{"title": "Doc 1"})

	if r := g.Add(ctx, doc); !r.OK() {
		t.Fatalf("Add document failed: %s", r.String())
	}

	// Add children to both relations
	note := mustValidPartInstance(t, s, "Note",
		[]any{"n1"}, map[string]any{"text": "Note 1"})
	tag := mustValidPartInstance(t, s, "Tag",
		[]any{"t1"}, map[string]any{})

	if r := g.AddComposed(ctx, "Document", graph.FormatKey("doc1"), "notes", note); !r.OK() {
		t.Fatalf("AddComposed note failed: %s", r.String())
	}
	if r := g.AddComposed(ctx, "Document", graph.FormatKey("doc1"), "tags", tag); !r.OK() {
		t.Fatalf("AddComposed tag failed: %s", r.String())
	}

	snap := g.Snapshot()
	docs := snap.InstancesOf("Document")
	if len(docs) != 1 {
		t.Fatalf("Expected 1 document, got %d", len(docs))
	}

	relations := docs[0].ComposedRelations()
	if len(relations) != 2 {
		t.Fatalf("Expected 2 composition relations, got %d", len(relations))
	}

	// Should be sorted: "notes" < "tags"
	if relations[0] != "notes" {
		t.Errorf("First relation should be 'notes', got %s", relations[0])
	}
	if relations[1] != "tags" {
		t.Errorf("Second relation should be 'tags', got %s", relations[1])
	}
}

func TestGraph_MultipleCompositions(t *testing.T) {
	// Parent with multiple composition relations
	s := testSchemaWithMultipleCompositions(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Document
	doc := mustValidInstance(t, s, "Document",
		[]any{"doc1"}, map[string]any{"title": "Doc 1"})

	if r := g.Add(ctx, doc); !r.OK() {
		t.Fatalf("Add document failed: %s", r.String())
	}

	// Add multiple notes
	for i := range 3 {
		note := mustValidPartInstance(t, s, "Note",
			[]any{fmt.Sprintf("n%d", i)}, map[string]any{"text": fmt.Sprintf("Note %d", i)})
		if r := g.AddComposed(ctx, "Document", graph.FormatKey("doc1"), "notes", note); !r.OK() {
			t.Fatalf("AddComposed note %d failed: %s", i, r.String())
		}
	}

	// Add multiple tags
	for i := range 2 {
		tag := mustValidPartInstance(t, s, "Tag",
			[]any{fmt.Sprintf("t%d", i)}, map[string]any{})
		if r := g.AddComposed(ctx, "Document", graph.FormatKey("doc1"), "tags", tag); !r.OK() {
			t.Fatalf("AddComposed tag %d failed: %s", i, r.String())
		}
	}

	snap := g.Snapshot()
	docs := snap.InstancesOf("Document")

	assertComposedCount(t, docs[0], "notes", 3)
	assertComposedCount(t, docs[0], "tags", 2)
}

// Verification Tests
//
// These tests verify compliance with behavioral contracts.

func TestContract2_PKRequiredForTopLevel(t *testing.T) {
	// Primary key is required for top-level instances
	s := testSchemaWithPKLessType(t) // Abstract type has no PK
	g := graph.New(s)
	ctx := t.Context()

	abstractType, _ := s.Type("Abstract")
	inst := instance.NewValidInstance(
		"Abstract",
		abstractType.ID(),
		immutable.Key{}, // No PK
		immutable.WrapProperties(map[string]any{"name": "Test"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)
	if result.OK() {
		t.Error(" violation: PK-less top-level instance should be rejected")
	}

	assertHasCode(t, result, diag.E_GRAPH_MISSING_PK)
}

func TestContract6_InstanceTagForm(t *testing.T) {
	// All public APIs use instance tag form
	// Local types: unqualified (e.g., "Person")
	// Imported types: alias-qualified (e.g., "c.Entity")
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add local User
	userType, _ := mainSchema.Type("User")
	user := instance.NewValidInstance(
		"User", // Unqualified for local type
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		nil, nil, nil,
	)

	if r := g.Add(ctx, user); !r.OK() {
		t.Fatalf("Add user failed: %s", r.String())
	}

	// Add imported Entity
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity", // Alias-qualified for imported type
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	if r := g.Add(ctx, entity); !r.OK() {
		t.Fatalf("Add entity failed: %s", r.String())
	}

	snap := g.Snapshot()

	// Verify Types() returns instance tag form
	types := snap.Types()
	hasUser := false
	hasCEntity := false
	for _, t := range types {
		if t == "User" {
			hasUser = true
		}
		if t == "c.Entity" {
			hasCEntity = true
		}
	}
	if !hasUser || !hasCEntity {
		t.Errorf(" violation: Types() should return instance tag form, got %v", types)
	}

	// Verify InstancesOf() works with instance tag form
	if snap.InstancesOf("User") == nil {
		t.Error(" violation: InstancesOf(\"User\") should find local type")
	}
	if snap.InstancesOf("c.Entity") == nil {
		t.Error(" violation: InstancesOf(\"c.Entity\") should find imported type")
	}

	// Verify graph.Instance.TypeName() returns instance tag form
	users := snap.InstancesOf("User")
	if users[0].TypeName() != "User" {
		t.Errorf(" violation: graph.Instance.TypeName() should be \"User\", got %q", users[0].TypeName())
	}
	entities := snap.InstancesOf("c.Entity")
	if entities[0].TypeName() != "c.Entity" {
		t.Errorf(" violation: graph.Instance.TypeName() should be \"c.Entity\", got %q", entities[0].TypeName())
	}
}

func TestContract7_TypeIDIndexing(t *testing.T) {
	// TypeID-based indexing avoids collisions between same-named types
	// Two imports with same type name should be kept separate
	reg := schema.NewRegistry()

	// Schema B with Product type
	schemaB, _ := schema.NewBuilder().
		WithName("schema_b").
		WithSourceID(location.MustNewSourceID("test://b.yammm")).
		AddType("Product").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("sourceB", schema.StringConstraint{}).
		Done().
		Build()
	_ = reg.Register(schemaB)

	// Schema C with Product type (same name!)
	schemaC, _ := schema.NewBuilder().
		WithName("schema_c").
		WithSourceID(location.MustNewSourceID("test://c.yammm")).
		AddType("Product").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("sourceC", schema.StringConstraint{}).
		Done().
		Build()
	_ = reg.Register(schemaC)

	// Main schema imports both
	mainSchema, _ := schema.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(reg).
		AddImport("schema_b", "b").
		AddImport("schema_c", "c").
		AddType("Container").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()

	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add b.Product with PK "p1"
	productB, _ := schemaB.Type("Product")
	instB := instance.NewValidInstance(
		"b.Product",
		productB.ID(),
		immutable.WrapKey([]any{"p1"}), // Same PK
		immutable.WrapProperties(map[string]any{"sourceB": "from B"}),
		nil, nil, nil,
	)

	if r := g.Add(ctx, instB); !r.OK() {
		t.Fatalf("Add b.Product failed: %s", r.String())
	}

	// Add c.Product with same PK - should NOT be a duplicate
	productC, _ := schemaC.Type("Product")
	instC := instance.NewValidInstance(
		"c.Product",
		productC.ID(),
		immutable.WrapKey([]any{"p1"}), // Same PK but different TypeID
		immutable.WrapProperties(map[string]any{"sourceC": "from C"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, instC)
	if err := result.Err(); err != nil {
		t.Errorf(" violation: Same PK with different TypeID should not be duplicate: %v", err)
	}

	// Verify both exist
	snap := g.Snapshot()
	if len(snap.InstancesOf("b.Product")) != 1 {
		t.Error(" violation: b.Product should exist")
	}
	if len(snap.InstancesOf("c.Product")) != 1 {
		t.Error(" violation: c.Product should exist")
	}
}

func TestContract14_ComposedKeyFormat(t *testing.T) {
	// Composed children are identified within their parent
	// Verify that duplicate PK within same parent is detected
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add parent failed: %s", r.String())
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	if r := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child1); !r.OK() {
		t.Fatalf("AddComposed child1 failed: %s", r.String())
	}

	// Add duplicate - should fail with E_DUPLICATE_COMPOSED_PK
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Dup"})

	result := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child2)
	if result.OK() {
		t.Error(" violation: graph.Duplicate child PK should be rejected")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_DUPLICATE_COMPOSED_PK {
			hasCode = true
			// Verify diagnostic has parent context (type detail per)
			details := issue.Details()
			hasType := false
			for _, d := range details {
				if d.Key == diag.DetailKeyTypeName {
					hasType = true
					break
				}
			}
			if !hasType {
				t.Error("E_DUPLICATE_COMPOSED_PK should include type detail")
			}
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_DUPLICATE_COMPOSED_PK")
	}
}

func TestContract19_FailureSemantics(t *testing.T) {
	// Data issues → result with diagnostics; programmer errors → panic;
	// context cancellation → Fatal diagnostic in result
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Test case 1: Data issue (duplicate PK) → result with diagnostic
	inst1 := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)
	inst2 := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"alice"}), // graph.Duplicate
		immutable.WrapProperties(map[string]any{"name": "Alice 2"}),
		nil, nil, nil,
	)

	result1 := g.Add(ctx, inst1)
	if !result1.OK() {
		t.Errorf("First Add should succeed: %s", result1.String())
	}

	result2 := g.Add(ctx, inst2)
	if result2.OK() {
		t.Error(" violation: graph.Duplicate PK result should not be OK")
	}

	// Test case 2: Nil instance → panic
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		g.Add(ctx, nil)
	}()
	if !panicked {
		t.Error(" violation: Nil instance should panic")
	}

	// Test case 3: Context cancellation → Fatal diagnostic in result
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	result4 := g.Add(cancelCtx, inst1)
	if result4.OK() {
		t.Error(" violation: Cancelled context result should not be OK")
	}

	hasFatal := false
	for issue := range result4.Issues() {
		if issue.Severity() == diag.Fatal {
			hasFatal = true
			break
		}
	}
	if !hasFatal {
		t.Error(" violation: Cancelled context should produce a Fatal diagnostic")
	}
}

func TestResult_Instances(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	// Add some instances
	for i := range 3 {
		inst := instance.NewValidInstance(
			"Person", personType.ID(),
			immutable.WrapKey([]any{fmt.Sprintf("person-%d", i)}),
			immutable.WrapProperties(map[string]any{"name": fmt.Sprintf("Name %d", i)}),
			nil, nil, nil,
		)
		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add failed: %s", r.String())
		}
	}

	snap := g.Snapshot()

	// Test Instances() returns a map
	instances := snap.Instances()
	if instances == nil {
		t.Error("Instances() returned nil")
	}

	// Count all instances in the map
	total := 0
	for _, typeInstances := range instances {
		total += len(typeInstances)
	}
	if total != 3 {
		t.Errorf("Instances() returned %d total items, want 3", total)
	}
}

func TestResult_DiagnosticsAndFlags(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	personType, _ := s.Type("Person")

	inst := instance.NewValidInstance(
		"Person", personType.ID(),
		immutable.WrapKey([]any{"test-person"}),
		immutable.WrapProperties(map[string]any{"name": "Test"}),
		nil, nil, nil,
	)
	result := g.Add(ctx, inst)

	// Test OK and HasErrors on the add result
	ok := result.OK()
	hasErrors := result.HasErrors()
	if !ok {
		t.Error("Expected OK to be true for successful add")
	}
	if hasErrors {
		t.Error("Expected HasErrors to be false for successful add")
	}

	// Test snapshot-level methods
	snap := g.Snapshot()
	snapOk := snap.OK()
	snapHasErrors := snap.HasErrors()
	if !snapOk {
		t.Error("Expected snap OK to be true")
	}
	if snapHasErrors {
		t.Error("Expected snap HasErrors to be false")
	}

	// Test Diagnostics returns a diag.Result
	diags := snap.Diagnostics()
	if !diags.OK() {
		t.Error("Expected diagnostics OK to be true")
	}
}

// Tests for extractCompositions - inline composition handling

func TestGraph_Add_InlineCompositions(t *testing.T) {
	// Tests extractCompositions by adding parent with inline children
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Create child instances
	childType, _ := s.Type("Child")
	child1 := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, nil, nil,
	)
	child2 := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c2"}),
		immutable.WrapProperties(map[string]any{"name": "Child 2"}),
		nil, nil, nil,
	)

	// Create parent with inline children using composed map
	parentType, _ := s.Type("Parent")
	childrenSlice := immutable.WrapSlice([]any{child1, child2})
	composed := map[string]immutable.Value{
		"children": immutable.Wrap(childrenSlice),
	}

	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, composed, nil,
	)

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify inline children were extracted
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "children", 2)
}

func TestGraph_Add_NestedInlineCompositions(t *testing.T) {
	// Tests nested extractCompositions
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Create grandchild
	grandchildType, _ := s.Type("GrandChild")
	grandchild := instance.NewValidInstance(
		"GrandChild",
		grandchildType.ID(),
		immutable.WrapKey([]any{"gc1"}),
		immutable.WrapProperties(map[string]any{"name": "GrandChild 1"}),
		nil, nil, nil,
	)

	// Create child with inline grandchild
	childType, _ := s.Type("Child")
	grandchildrenSlice := immutable.WrapSlice([]any{grandchild})
	childComposed := map[string]immutable.Value{
		"grandChildren": immutable.Wrap(grandchildrenSlice),
	}
	child := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, childComposed, nil,
	)

	// Create parent with inline child (which has inline grandchild)
	parentType, _ := s.Type("Parent")
	childrenSlice := immutable.WrapSlice([]any{child})
	parentComposed := map[string]immutable.Value{
		"children": immutable.Wrap(childrenSlice),
	}
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, parentComposed, nil,
	)

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify nested structure
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "children", 1)

	// Verify grandchildren
	children := parents[0].Composed("children")
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	assertComposedCount(t, children[0], "grandChildren", 1)
}

func TestGraph_Add_InlineComposition_EmptySlice(t *testing.T) {
	// Tests extractCompositions with empty composition slice
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Create parent with empty composed slice
	parentType, _ := s.Type("Parent")
	childrenSlice := immutable.WrapSlice([]any{})
	composed := map[string]immutable.Value{
		"children": immutable.Wrap(childrenSlice),
	}

	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, composed, nil,
	)

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify no children
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	assertComposedCount(t, parents[0], "children", 0)
}

// Tests for resolveTypeName edge cases

func TestGraph_resolveTypeName_QualifiedTypeNotFound(t *testing.T) {
	// Tests the "type not found in imported schema" branch
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Try to add instance with alias prefix pointing to valid import
	// but with a type name that doesn't exist in that schema
	fakeTypeID := schema.NewTypeID(location.MustNewSourceID("test://common.yammm"), "NonExistent")
	inst := instance.NewValidInstance(
		"c.NonExistent", // Valid alias, invalid type
		fakeTypeID,
		immutable.WrapKey([]any{"x1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)

	// Should fail - type not found in imported schema
	if result.OK() {
		t.Error("Add should fail for non-existent type in imported schema")
	}
}

func TestGraph_resolveTypeName_QualifiedImportAliasNotFound(t *testing.T) {
	// graph.Instance from completely unknown schema panics (schema mismatch)
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Try to add instance with unknown schema (not in import chain)
	fakeTypeID := schema.NewTypeID(location.MustNewSourceID("test://unknown.yammm"), "SomeType")
	inst := instance.NewValidInstance(
		"unknown.SomeType", // Unknown alias
		fakeTypeID,
		immutable.WrapKey([]any{"x1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	defer func() {
		if r := recover(); r == nil {
			t.Error("Add with unknown schema should panic")
		}
	}()
	g.Add(ctx, inst)
}

func TestGraph_resolveTypeName_UnqualifiedTypeNotFound(t *testing.T) {
	// Tests the "local type not found" branch
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Try to add instance with non-existent local type
	fakeTypeID := schema.NewTypeID(location.MustNewSourceID("test://test.yammm"), "NonExistent")
	inst := instance.NewValidInstance(
		"NonExistent", // Unqualified, doesn't exist locally
		fakeTypeID,
		immutable.WrapKey([]any{"x1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	result := g.Add(ctx, inst)

	// Should fail - local type not found
	if result.OK() {
		t.Error("Add should fail for non-existent local type")
	}
}

// TestAddComposed_NestedComposition_Extracted tests that AddComposed extracts
// nested compositions from streamed children.
func TestAddComposed_NestedComposition_Extracted(t *testing.T) {
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent first
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add parent failed: %s", r.String())
	}

	// Create grandchild
	grandchildType, _ := s.Type("GrandChild")
	grandchild := instance.NewValidInstance(
		"GrandChild",
		grandchildType.ID(),
		immutable.WrapKey([]any{"gc1"}),
		immutable.WrapProperties(map[string]any{"name": "GrandChild 1"}),
		nil, nil, nil,
	)

	// Create child with inline grandchild - this is the key: child has nested composition
	childType, _ := s.Type("Child")
	grandchildrenSlice := immutable.WrapSlice([]any{grandchild})
	childComposed := map[string]immutable.Value{
		"grandChildren": immutable.Wrap(grandchildrenSlice),
	}
	child := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, childComposed, nil,
	)

	// AddComposed the child (with inline grandchild) to the parent
	result := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child)
	if err := result.Err(); err != nil {
		t.Fatalf("AddComposed should succeed: %v", err)
	}

	// Verify nested structure
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	// Parent should have child
	children := parents[0].Composed("children")
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	// Child should have grandchild (extracted from inline composition)
	grandchildren := children[0].Composed("grandChildren")
	if len(grandchildren) != 1 {
		t.Fatalf("Expected 1 grandchild (nested composition should be extracted), got %d", len(grandchildren))
	}

	// Verify grandchild data
	nameVal, ok := grandchildren[0].Property("name")
	if !ok {
		t.Error("Expected name property on grandchild")
	} else if name, ok := nameVal.String(); !ok || name != "GrandChild 1" {
		t.Errorf("Expected grandchild name 'GrandChild 1', got %q", name)
	}
}

// TestExtractCompositions_OneCardinality_MultipleChildren_Error tests that
// (one) relations with multiple children emit an error.
func TestExtractCompositions_OneCardinality_MultipleChildren_Error(t *testing.T) {
	s := testSchemaWithOneComposition(t) // Parent -> (one) Child
	g := graph.New(s)
	ctx := t.Context()

	// Create two children
	childType, _ := s.Type("Child")
	child1 := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, nil, nil,
	)
	child2 := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c2"}),
		immutable.WrapProperties(map[string]any{"name": "Child 2"}),
		nil, nil, nil,
	)

	// Create parent with TWO children (violates (one) cardinality)
	parentType, _ := s.Type("Parent")
	childrenSlice := immutable.WrapSlice([]any{child1, child2})
	parentComposed := map[string]immutable.Value{
		"child": immutable.Wrap(childrenSlice),
	}
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, parentComposed, nil,
	)

	result := g.Add(ctx, parent)

	// Should have error for (one) cardinality violation
	if result.OK() {
		t.Error("Add should fail for (one) cardinality violation")
	}

	// Verify E_DUPLICATE_COMPOSED_PK was emitted
	assertHasCode(t, result, diag.E_DUPLICATE_COMPOSED_PK)

	// Verify only first child was attached
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	children := parents[0].Composed("child")
	if len(children) != 1 {
		t.Errorf("Expected exactly 1 child (first only), got %d", len(children))
	}
}

// TestExtractCompositions_BareValidInstance tests that extractCompositions
// handles bare *ValidInstance (not wrapped in slice) defensively.
func TestExtractCompositions_BareValidInstance(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Create child
	childType, _ := s.Type("Child")
	child := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, nil, nil,
	)

	// Create parent with bare *ValidInstance (not wrapped in slice)
	// This is defensive handling - the validator normally wraps in slice
	parentType, _ := s.Type("Parent")
	parentComposed := map[string]immutable.Value{
		"children": immutable.Wrap(child), // Bare instance, not slice
	}
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, parentComposed, nil,
	)

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add should succeed: %v", err)
	}

	// Verify child was extracted from bare instance
	snap := g.Snapshot()
	parents := snap.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	children := parents[0].Composed("children")
	if len(children) != 1 {
		t.Fatalf("Expected 1 child (from bare *ValidInstance), got %d", len(children))
	}

	// Verify child data
	nameVal, ok := children[0].Property("name")
	if !ok {
		t.Error("Expected name property on child")
	} else if name, ok := nameVal.String(); !ok || name != "Child 1" {
		t.Errorf("Expected child name 'Child 1', got %q", name)
	}
}

// TestAdd_PerOperationDiagnostics verifies that Add() returns per-operation results,
// not cumulative results. A successful Add after a failed Add should return OK.
func TestAdd_PerOperationDiagnostics(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// First add - use an invalid type to trigger an error
	invalidInst := mustValidInstanceWithInvalidType(t, s, "NonExistent", []any{"x"}, nil)

	result1 := g.Add(ctx, invalidInst)
	if result1.OK() {
		t.Error("First Add with invalid type should fail")
	}
	issueCount1 := countIssues(result1)
	if issueCount1 != 1 {
		t.Errorf("First Add should return 1 issue, got %d", issueCount1)
	}

	// Second add - valid instance should return OK (per-operation, not cumulative)
	validInst := mustValidInstance(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"})
	result2 := g.Add(ctx, validInst)
	if !result2.OK() {
		t.Errorf("Second Add should return OK (per-operation), but got issues: %s", result2.String())
	}
	issueCount2 := countIssues(result2)
	if issueCount2 != 0 {
		t.Errorf("Second Add should return 0 issues, got %d", issueCount2)
	}

	// graph.Snapshot.Diagnostics() should still contain the first error (cumulative)
	snap := g.Snapshot()
	if snap.Diagnostics().OK() {
		t.Error("graph.Snapshot.Diagnostics() should NOT be OK (cumulative)")
	}
	snapIssueCount := countIssues(snap.Diagnostics())
	if snapIssueCount != 1 {
		t.Errorf("graph.Snapshot.Diagnostics() should have 1 issue, got %d", snapIssueCount)
	}
}

// TestCheck_Idempotent_IssueCount verifies that Check() is idempotent:
// multiple calls return identical results without accumulating issues.
func TestCheck_Idempotent_IssueCount(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Person with reference to missing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing-company"}})

	if r := g.Add(ctx, person); !r.OK() {
		t.Fatalf("Add failed: %s", r.String())
	}

	// Call Check multiple times
	result1 := g.Check(ctx)
	result2 := g.Check(ctx)
	result3 := g.Check(ctx)

	count1 := countIssues(result1)
	count2 := countIssues(result2)
	count3 := countIssues(result3)

	// All results should have identical issue counts (idempotent)
	if count1 != count2 || count2 != count3 {
		t.Errorf("Check should be idempotent: got %d, %d, %d issues",
			count1, count2, count3)
	}

	// Should have exactly 1 unresolved issue
	if count1 != 1 {
		t.Errorf("Expected 1 unresolved issue, got %d", count1)
	}

	// graph.Snapshot.Diagnostics() should NOT include Check() issues
	snap := g.Snapshot()
	if !snap.Diagnostics().OK() {
		t.Error("graph.Snapshot.Diagnostics() should be OK (Check doesn't merge)")
	}
}

// TestCheck_UnresolvedRequired_TargetPK verifies that E_UNRESOLVED_REQUIRED
// includes target_pk detail when the target key is known.
func TestCheck_UnresolvedRequired_TargetPK(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Person with reference to specific missing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})

	if r := g.Add(ctx, person); !r.OK() {
		t.Fatalf("Add failed: %s", r.String())
	}

	result := g.Check(ctx)
	if result.OK() {
		t.Fatal("Check should fail with unresolved required association")
	}

	// Find the unresolved issue and verify target_pk
	var issue diag.Issue
	issueCount := 0
	for i := range result.Issues() {
		issue = i
		issueCount++
	}
	if issueCount != 1 {
		t.Fatalf("Expected 1 issue, got %d", issueCount)
	}

	if issue.Code() != diag.E_UNRESOLVED_REQUIRED {
		t.Fatalf("Expected E_UNRESOLVED_REQUIRED, got %s", issue.Code())
	}

	// Check for target_pk detail
	var foundTargetPK bool
	for _, d := range issue.Details() {
		if d.Key == "target_pk" {
			foundTargetPK = true
			if d.Value != `["acme"]` {
				t.Errorf("Expected target_pk '[\"acme\"]', got %q", d.Value)
			}
			break
		}
	}
	if !foundTargetPK {
		t.Error("Expected target_pk detail in E_UNRESOLVED_REQUIRED issue")
	}
}

// TestCheck_UnresolvedRequired_TargetMissingReason verifies that E_UNRESOLVED_REQUIRED
// uses "target_missing" as the reason token for missing target instances.
func TestCheck_UnresolvedRequired_TargetMissingReason(t *testing.T) {
	s := testSchemaWithAssociation(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add Person with reference to missing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing"}})

	if r := g.Add(ctx, person); !r.OK() {
		t.Fatalf("Add failed: %s", r.String())
	}

	result := g.Check(ctx)

	var issue diag.Issue
	issueCount := 0
	for i := range result.Issues() {
		issue = i
		issueCount++
	}
	if issueCount != 1 {
		t.Fatalf("Expected 1 issue, got %d", issueCount)
	}

	// Verify reason is "target_missing" per
	var foundReason bool
	for _, d := range issue.Details() {
		if d.Key == "reason" {
			foundReason = true
			if d.Value != "target_missing" {
				t.Errorf("Expected reason 'target_missing', got %q", d.Value)
			}
			break
		}
	}
	if !foundReason {
		t.Error("Expected reason detail in E_UNRESOLVED_REQUIRED issue")
	}
}

// TestDuplicateComposedPK_PKDetail verifies that E_DUPLICATE_COMPOSED_PK
// includes "pk" detail with graph.FormatComposedKey format.
func TestDuplicateComposedPK_PKDetail(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add parent
	parent := mustValidInstance(t, s, "Parent",
		[]any{"p1"}, map[string]any{"name": "Parent 1"})

	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add parent failed: %s", r.String())
	}

	// Add first child
	child1 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1"})

	result1 := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child1)
	if !result1.OK() {
		t.Fatalf("First AddComposed should succeed: %s", result1.String())
	}

	// Add duplicate child (same PK)
	child2 := mustValidPartInstance(t, s, "Child",
		[]any{"c1"}, map[string]any{"name": "Child 1 Dup"})

	result2 := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child2)
	if result2.OK() {
		t.Fatal("graph.Duplicate AddComposed should fail")
	}

	var issue diag.Issue
	issueCount := 0
	for i := range result2.Issues() {
		issue = i
		issueCount++
	}
	if issueCount != 1 {
		t.Fatalf("Expected 1 issue, got %d", issueCount)
	}

	if issue.Code() != diag.E_DUPLICATE_COMPOSED_PK {
		t.Fatalf("Expected E_DUPLICATE_COMPOSED_PK, got %s", issue.Code())
	}

	// Verify "pk" detail with composed key format
	var foundPK bool
	for _, d := range issue.Details() {
		if d.Key == "pk" {
			foundPK = true
			// Should be in graph.FormatComposedKey format: [["p1"],"children",["c1"]]
			expected := `[["p1"],"children",["c1"]]`
			if d.Value != expected {
				t.Errorf("Expected pk %q, got %q", expected, d.Value)
			}
			break
		}
	}
	if !foundPK {
		t.Error("Expected pk detail in E_DUPLICATE_COMPOSED_PK issue")
	}
}

// mustValidInstanceWithInvalidType creates a ValidInstance with a type
// that won't match any type in the graph's schema - for testing error handling.
// Uses the same schema but with a non-existent type to avoid a schema mismatch panic.
func mustValidInstanceWithInvalidType(t *testing.T, s *schema.Schema, typeName string, pkValues []any, _ map[string]any) *instance.ValidInstance {
	t.Helper()

	// Create a TypeID that uses the same schema but a non-existent type name
	// This will trigger a "type not found" diagnostic, not a schema mismatch panic
	fakeTypeID := schema.NewTypeID(s.SourceID(), typeName)

	return instance.NewValidInstance(
		typeName,
		fakeTypeID,
		immutable.WrapKey(pkValues),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)
}

// countIssues counts the number of issues in a diag.Result.
func countIssues(result diag.Result) int {
	count := 0
	for range result.Issues() {
		count++
	}
	return count
}

// TestNilContext_Panics verifies that all public APIs panic with helpful
// messages when passed a nil context.
func TestNilContext_Panics(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)

	inst := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	tests := []struct {
		name    string
		fn      func()
		wantMsg string
	}{
		{
			name:    "Add",
			fn:      func() { g.Add(nil, inst) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "graph.Add: nil context",
		},
		{
			name:    "AddComposed",
			fn:      func() { g.AddComposed(nil, "Person", "[\"alice\"]", "rel", inst) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "graph.AddComposed: nil context",
		},
		{
			name:    "Check",
			fn:      func() { g.Check(nil) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "graph.Check: nil context",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				r := recover()
				if r == nil {
					t.Fatal("expected panic, got none")
				}
				msg, ok := r.(string)
				if !ok {
					t.Fatalf("expected string panic, got %T: %v", r, r)
				}
				if msg != tc.wantMsg {
					t.Errorf("panic message = %q, want %q", msg, tc.wantMsg)
				}
			}()
			tc.fn()
		})
	}
}

// hasOpName checks if any record has the given operation name.
func hasOpName(records []slog.Record, opName string) bool {
	for _, r := range records {
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == "op" && a.Value.String() == opName {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// hasMessage checks if any record has the given message.
func hasMessage(records []slog.Record, msg string) bool {
	for _, r := range records {
		if r.Message == msg {
			return true
		}
	}
	return false
}

// hasAttr checks if any record has the given attribute key/value.
func hasAttr(records []slog.Record, key, value string) bool {
	for _, r := range records {
		var found bool
		r.Attrs(func(a slog.Attr) bool {
			if a.Key == key && a.Value.String() == value {
				found = true
				return false
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// countLevel counts records at the given level.
func countLevel(records []slog.Record, level slog.Level) int {
	count := 0
	for _, r := range records {
		if r.Level == level {
			count++
		}
	}
	return count
}

// TestGraph_Logging drives every logged operation through one scenario
// table: each row builds its schema, runs its graph operations against a
// captured logger, then asserts the expected operation names, attributes,
// messages, and warn-level presence in the records.
func TestGraph_Logging(t *testing.T) {
	tests := []struct {
		name      string
		schema    func(*testing.T) *schema.Schema
		run       func(t *testing.T, g *graph.Graph, s *schema.Schema)
		wantOps   []string
		wantAttrs [][2]string
		wantMsgs  []string
		wantWarn  bool
	}{
		{
			name:   "add",
			schema: testSchemaWithAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
				if r := g.Add(t.Context(), company); !r.OK() {
					t.Fatalf("Add failed: %s", r.String())
				}
			},
			wantOps:   []string{"yammm.graph.add"},
			wantAttrs: [][2]string{{"type", "Company"}, {"pk", `["acme"]`}},
		},
		{
			name:   "add duplicate",
			schema: testSchemaWithAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				company1 := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
				if r := g.Add(t.Context(), company1); !r.OK() {
					t.Fatalf("First Add failed: %s", r.String())
				}
				company2 := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Inc"})
				g.Add(t.Context(), company2)
			},
			wantMsgs: []string{"duplicate primary key"},
			wantWarn: true,
		},
		{
			name:   "edge resolution",
			schema: testSchemaWithAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
				if r := g.Add(t.Context(), company); !r.OK() {
					t.Fatalf("Add Company failed: %s", r.String())
				}
				person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
				if r := g.Add(t.Context(), person); !r.OK() {
					t.Fatalf("Add Person failed: %s", r.String())
				}
			},
			wantMsgs: []string{"edge resolved"},
		},
		{
			name:   "forward reference",
			schema: testSchemaWithOptionalAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
				if r := g.Add(t.Context(), person); !r.OK() {
					t.Fatalf("Add Person failed: %s", r.String())
				}
			},
			wantMsgs: []string{"forward reference created"},
		},
		{
			name:   "pending edges resolved",
			schema: testSchemaWithOptionalAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"acme"}})
				if r := g.Add(t.Context(), person); !r.OK() {
					t.Fatalf("Add Person failed: %s", r.String())
				}
				company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
				if r := g.Add(t.Context(), company); !r.OK() {
					t.Fatalf("Add Company failed: %s", r.String())
				}
			},
			wantMsgs: []string{"pending edges resolved"},
		},
		{
			name:   "check",
			schema: testSchemaWithAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				person := mustValidInstance(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"})
				if r := g.Add(t.Context(), person); !r.OK() {
					t.Fatalf("Add failed: %s", r.String())
				}
				g.Check(t.Context())
			},
			wantOps: []string{"yammm.graph.check"},
		},
		{
			name:   "check unresolved",
			schema: testSchemaWithAssociation,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				person := mustValidInstanceWithEdge(t, s, "Person", []any{"alice"}, map[string]any{"name": "Alice"}, "employer", [][]any{{"missing"}})
				if r := g.Add(t.Context(), person); !r.OK() {
					t.Fatalf("Add failed: %s", r.String())
				}
				g.Check(t.Context())
			},
			wantMsgs: []string{"unresolved required association"},
			wantWarn: true,
		},
		{
			name:   "add composed",
			schema: testSchemaWithComposition,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
				if r := g.Add(t.Context(), parent); !r.OK() {
					t.Fatalf("Add parent failed: %s", r.String())
				}
				child := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
				if r := g.AddComposed(t.Context(), "Parent", graph.FormatKey("p1"), "children", child); !r.OK() {
					t.Fatalf("AddComposed failed: %s", r.String())
				}
			},
			wantOps:   []string{"yammm.graph.add_composed"},
			wantAttrs: [][2]string{{"parent_type", "Parent"}, {"relation", "children"}},
		},
		{
			name:   "add composed duplicate",
			schema: testSchemaWithComposition,
			run: func(t *testing.T, g *graph.Graph, s *schema.Schema) {
				t.Helper()
				parent := mustValidInstance(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"})
				if r := g.Add(t.Context(), parent); !r.OK() {
					t.Fatalf("Add parent failed: %s", r.String())
				}
				child1 := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
				if r := g.AddComposed(t.Context(), "Parent", graph.FormatKey("p1"), "children", child1); !r.OK() {
					t.Fatalf("First AddComposed failed: %s", r.String())
				}
				child2 := mustValidPartInstance(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1 Dup"})
				g.AddComposed(t.Context(), "Parent", graph.FormatKey("p1"), "children", child2)
			},
			wantMsgs: []string{"duplicate composed child"},
			wantWarn: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := tt.schema(t)
			h := yammmtest.NewRecordHandler(slog.LevelDebug)
			g := graph.New(s, graph.WithLogger(slog.New(h)))

			tt.run(t, g, s)

			records := h.Records()
			for _, op := range tt.wantOps {
				if !hasOpName(records, op) {
					t.Errorf("expected %s operation to be logged", op)
				}
			}
			for _, attr := range tt.wantAttrs {
				if !hasAttr(records, attr[0], attr[1]) {
					t.Errorf("expected %s=%s attribute", attr[0], attr[1])
				}
			}
			for _, msg := range tt.wantMsgs {
				if !hasMessage(records, msg) {
					t.Errorf("expected %q message", msg)
				}
			}
			if tt.wantWarn && countLevel(records, slog.LevelWarn) == 0 {
				t.Error("expected a Warn-level record")
			}
		})
	}
}

func TestGraph_NoLogging_WhenNilLogger(t *testing.T) {
	s := testSchemaWithAssociation(t)

	// No logger - should not panic
	g := graph.New(s)

	company := mustValidInstance(t, s, "Company", []any{"acme"}, map[string]any{"name": "Acme Corp"})
	if r := g.Add(t.Context(), company); !r.OK() {
		t.Fatalf("Add failed: %s", r.String())
	}

	g.Check(t.Context())
	// Test passes if no panic occurred
}
