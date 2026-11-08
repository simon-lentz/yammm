package graph_test

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// testSchema creates a simple schema for testing.
func testSchema(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "test"

type Person {
	id String primary
	name String required
}

type Company {
	id String primary
	name String required
}
`, "test.yammm")

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

func TestNew_PanicOnZeroSchema(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("graph.New(nil) should panic")
		}
	}()

	graph.New(schema.Schema{})
}

func TestGraph_Add_NilReceiver(t *testing.T) {
	var g *graph.Graph
	ctx := t.Context()
	s := testSchema(t)

	inst := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})

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
	g.Add(ctx, instance.ValidInstance{})
}

func TestGraph_Add_SchemaMismatch(t *testing.T) {
	// Instance validated against a completely different schema should fail
	schemaA, _ := schema.LoadString(t.Context(), `schema "schema_a"

type TypeA {
	id String primary
}
`, "a.yammm")

	schemaB, _ := schema.LoadString(t.Context(), `schema "schema_b"

type TypeB {
	id String primary
}
`, "b.yammm")

	g := graph.New(schemaA)
	ctx := t.Context()

	// Create instance from schemaB
	inst := instancetest.Valid(t, schemaB, "TypeB", map[string]any{"id": "x1"})

	defer func() {
		if r := recover(); r == nil {
			t.Error("Add with mismatched schema should panic")
		}
	}()
	g.Add(ctx, inst)
}

func TestGraph_Add_ImportedSchemaAllowed(t *testing.T) {
	// Instance from an imported schema should be allowed
	mainSchema, importedSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Create instance from the imported schema (common)
	inst := instancetest.Valid(t, importedSchema, "Entity", map[string]any{"id": "e1", "name": "Test Entity"})

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

	inst := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})

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

	inst := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})

	result := g.Add(ctx, inst)
	if err := result.Err(); err != nil {
		t.Errorf("Add() should succeed: %v", err)
	}

	// Verify instance is in graph
	snap := g.Snapshot()
	types := snap.Types()
	if len(types) != 1 || types[0] != mustTypeID(t, s, "Person") {
		t.Errorf("Types() = %v, want [Person]", types)
	}

	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	if len(instances) != 1 {
		t.Fatalf("InstancesOf(\"Person\") returned %d instances, want 1", len(instances))
	}
	if instances[0].TypeName() != "Person" {
		t.Errorf("Instance.TypeName() = %q, want \"Person\"", instances[0].TypeName())
	}
	if instances[0].PrimaryKey().String() != `["alice"]` {
		t.Errorf("Instance.PrimaryKey() = %q, want [\"alice\"]", instances[0].PrimaryKey().String())
	}
}

func TestGraph_Add_DuplicatePK(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add first instance
	inst1 := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})
	if r := g.Add(ctx, inst1); !r.OK() {
		t.Fatalf("First Add() failed: %s", r.String())
	}

	// Add duplicate
	inst2 := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice 2"})
	result := g.Add(ctx, inst2)
	if result.OK() {
		t.Error("Add() with duplicate PK should fail")
	}

	// Check for E_DUPLICATE_PK
	assertHasCode(t, result, diag.E_DUPLICATE_PK)

	// Verify snapshot has duplicates
	snap := g.Snapshot()
	dups := slices.Collect(snap.Duplicates())
	if len(dups) != 1 {
		t.Errorf("Snapshot.Duplicates() returned %d, want 1", len(dups))
	}
}

func TestGraph_Snapshot_DeterministicOrder(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add instances in non-sorted order
	names := []string{"charlie", "alice", "bob"}
	for _, name := range names {
		inst := instancetest.Valid(t, s, "Person", map[string]any{"id": name, "name": name})
		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add(%s) failed: %s", name, r.String())
		}
	}

	// Verify sorted order in snapshot
	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	if len(instances) != 3 {
		t.Fatalf("Expected 3 instances, got %d", len(instances))
	}

	expected := []string{`["alice"]`, `["bob"]`, `["charlie"]`}
	for i, inst := range instances {
		if inst.PrimaryKey().String() != expected[i] {
			t.Errorf("Instance[%d].PrimaryKey() = %q, want %q",
				i, inst.PrimaryKey().String(), expected[i])
		}
	}
}

func TestGraph_InstanceByKey(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	inst := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})

	if r := g.Add(ctx, inst); !r.OK() {
		t.Fatalf("Add() failed: %s", r.String())
	}

	snap := g.Snapshot()

	// Lookup by key
	found, ok := snap.InstanceByKey(mustTypeID(t, s, "Person"), graph.FormatKey("alice"))
	if !ok {
		t.Fatal("InstanceByKey() should find the instance")
	}
	if found.TypeName() != "Person" {
		t.Errorf("Found instance TypeName() = %q, want \"Person\"", found.TypeName())
	}

	// Lookup non-existent
	_, ok = snap.InstanceByKey(mustTypeID(t, s, "Person"), graph.FormatKey("bob"))
	if ok {
		t.Error("InstanceByKey() should not find non-existent instance")
	}

	// Lookup non-existent type
	_, ok = snap.InstanceByKey(schema.TypeID{}, graph.FormatKey("alice"))
	if ok {
		t.Error("InstanceByKey() should not find instance of non-existent type")
	}
}

func TestGraph_Check_NilReceiver(t *testing.T) {
	var g *graph.Graph
	yammmtest.AssertPanics(t, func() { g.Check(t.Context()) })
}

func TestSnapshot_NilReceiver(t *testing.T) {
	var r graph.Snapshot

	// All methods should handle nil gracefully
	if r.Types() != nil {
		t.Error("nil.Types() should return nil")
	}
	if slices.Collect(r.InstancesOf(schema.TypeID{})) != nil {
		t.Error("nil.InstancesOf() should return nil")
	}
	if _, ok := r.InstanceByKey(schema.TypeID{}, "any"); ok {
		t.Error("nil.InstanceByKey() should return false")
	}
	if slices.Collect(r.Edges()) != nil {
		t.Error("nil.Edges() should return nil")
	}
	if slices.Collect(r.Duplicates()) != nil {
		t.Error("nil.Duplicates() should return nil")
	}
	if slices.Collect(r.Unresolved()) != nil {
		t.Error("nil.Unresolved() should return nil")
	}
}

func TestInstance_NilReceiver(t *testing.T) {
	var i graph.Instance

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
	if slices.Collect(i.Composed("any")) != nil {
		t.Error("nil.Composed() should return nil")
	}
	if i.ComposedCount("any") != 0 {
		t.Error("nil.ComposedCount() should return 0")
	}
}

func TestEdge_NilReceiver(t *testing.T) {
	var e graph.Edge

	// All methods should handle nil gracefully
	if e.Relation() != "" {
		t.Error("nil.Relation() should return empty string")
	}
	if !e.Source().IsZero() {
		t.Error("nil.Source() should return nil")
	}
	if !e.Target().IsZero() {
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

func TestGraph_PartType_NoPK_Positional(t *testing.T) {
	// PK-less parts have positional identity - multiple can coexist
	s := testSchemaWithPKLessChild(t)
	g := graph.New(s)
	ctx := t.Context()

	// PK-less items with the same data keep their positional identity.
	var items []map[string]any
	for range 3 {
		items = append(items, map[string]any{"value": "same-value"})
	}
	container := parentWith(t, s, "Container", []any{"box1"}, map[string]any{"name": "Box 1"}, "ITEMS", items...)
	if err := g.Add(ctx, container).Err(); err != nil {
		t.Errorf("Add should succeed: %v", err)
	}

	// Verify all 3 items are present
	snap := g.Snapshot()
	containers := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Container")))
	if len(containers) != 1 {
		t.Fatalf("Expected 1 container, got %d", len(containers))
	}

	assertComposedCount(t, containers[0], "ITEMS", 3)
}

func TestGraph_PartType_SameKeyUnderTwoParentsIsNoDuplicate(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	single := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILDREN",
		map[string]any{"id": "c1", "name": "Child 1"},
	)
	if err := g.Add(ctx, single).Err(); err != nil {
		t.Errorf("First child should succeed: %v", err)
	}

	other := parentWith(t, s, "Parent", []any{"p2"}, map[string]any{"name": "Parent 2"}, "CHILDREN",
		map[string]any{"id": "c1", "name": "Child 1"},
	)
	if err := g.Add(ctx, other).Err(); err != nil {
		t.Errorf("A child at the same key under another parent should succeed: %v", err)
	}
}

func TestGraph_NestedComposition(t *testing.T) {
	// Part containing part (multi-level nesting)
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	child := partData(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"})
	parent := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILDREN", child)
	if r := g.Add(ctx, parent); !r.OK() {
		t.Fatalf("Add failed: %s", r.String())
	}

	// Verify structure
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	children := slices.Collect(parents[0].Composed("CHILDREN"))
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	if children[0].TypeName() != "Child" {
		t.Errorf("Child type should be Child, got %s", children[0].TypeName())
	}
}

// Ordering and Edge Case Tests

func TestGraph_Duplicates_Ordering(t *testing.T) {
	// Duplicates should be sorted by (type, pk)
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add first instances
	alice := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})
	acme := instancetest.Valid(t, s, "Company", map[string]any{"id": "acme", "name": "Acme"})

	if r := g.Add(ctx, alice); !r.OK() {
		t.Fatal(r.String())
	}
	if r := g.Add(ctx, acme); !r.OK() {
		t.Fatal(r.String())
	}

	// Add duplicates in reverse order (Company before Person)
	acmeDup := instancetest.Valid(t, s, "Company", map[string]any{"id": "acme", "name": "Acme Dup"})
	aliceDup := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice Dup"})

	g.Add(ctx, acmeDup)
	g.Add(ctx, aliceDup)

	snap := g.Snapshot()
	dups := slices.Collect(snap.Duplicates())

	if len(dups) != 2 {
		t.Fatalf("Expected 2 duplicates, got %d", len(dups))
	}

	// Should be sorted: Company < Person lexicographically
	if dups[0].Instance().TypeName() != "Company" {
		t.Errorf("First duplicate should be Company, got %s", dups[0].Instance().TypeName())
	}
	if dups[1].Instance().TypeName() != "Person" {
		t.Errorf("Second duplicate should be Person, got %s", dups[1].Instance().TypeName())
	}
}

func TestGraph_Unresolved_Ordering(t *testing.T) {
	// Unresolved edges should be sorted by (sourceType, sourceKey, relation, targetType, targetKey)
	s := testSchemaWithChainedAssociations(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add instances with forward refs in non-sorted order
	typeC := mustValidInstance(t, s, "TypeC", []any{"c1"}, map[string]any{"name": "C1"})
	typeA2 := mustValidInstanceWithEdge(t, s, "TypeA", []any{"a2"}, map[string]any{"name": "A2"}, "REF_B", [][]any{{"b-missing"}})
	typeA1 := mustValidInstanceWithEdge(t, s, "TypeA", []any{"a1"}, map[string]any{"name": "A1"}, "REF_B", [][]any{{"b-missing"}})
	typeB := mustValidInstanceWithEdge(t, s, "TypeB", []any{"b1"}, map[string]any{"name": "B1"}, "REF_C", [][]any{{"c-missing"}})

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
	unresolved := slices.Collect(snap.Unresolved())

	if len(unresolved) < 2 {
		t.Fatalf("Expected at least 2 unresolved, got %d", len(unresolved))
	}

	// Every adjacent pair is ordered, so an inverted comparator anywhere in
	// the list fails rather than only one that moves the smallest key.
	if !slices.IsSortedFunc(unresolved, func(a, b graph.UnresolvedEdge) int {
		return cmp.Or(
			cmp.Compare(a.Source().TypeID().String(), b.Source().TypeID().String()),
			cmp.Compare(a.Source().PrimaryKey().String(), b.Source().PrimaryKey().String()),
			cmp.Compare(a.Relation(), b.Relation()),
			cmp.Compare(a.TargetType().String(), b.TargetType().String()),
			cmp.Compare(a.TargetKey(), b.TargetKey()),
		)
	}) {
		t.Errorf("Unresolved is not sorted by the documented tuple: %v", unresolved)
	}

	foundA1 := false
	for _, ur := range unresolved {
		if ur.Source().TypeName() == "TypeA" && ur.Source().PrimaryKey().String() == `["a1"]` {
			foundA1 = true
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

	// Add 1000 instances in random order (using a pattern that's not sorted)
	for i := range 1000 {
		pk := fmt.Sprintf("person-%04d", (i*7+13)%1000) // Scrambled order
		inst := instancetest.Valid(t, s, "Person", map[string]any{"id": pk, "name": pk})

		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add %s failed: %s", pk, r.String())
		}
	}

	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))

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
		inst := instancetest.Valid(t, s, "Person", map[string]any{"id": key, "name": key})

		result := g.Add(ctx, inst)
		if err := result.Err(); err != nil {
			t.Errorf("Add key %q should succeed: %v", key, err)
		}
	}

	snap := g.Snapshot()
	if len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))) != len(specialKeys) {
		t.Errorf("Expected %d instances, got %d", len(specialKeys), len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))))
	}

	// Verify lookup works for each key
	for _, key := range specialKeys {
		formatted := graph.FormatKey(key)
		_, ok := snap.InstanceByKey(mustTypeID(t, s, "Person"), formatted)
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
		inst := instancetest.Valid(t, s, "Person", map[string]any{"id": key, "name": key})

		result := g.Add(ctx, inst)
		if err := result.Err(); err != nil {
			t.Errorf("Add unicode key %q should succeed: %v", key, err)
		}
	}

	snap := g.Snapshot()
	if len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))) != len(unicodeKeys) {
		t.Errorf("Expected %d instances, got %d", len(unicodeKeys), len(slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))))
	}
}

// TestGraph_CompositeKey_TwoComponents pins composite-key indexing and lookup
// over the fixture's own arity, which is two.
func TestGraph_CompositeKey_TwoComponents(t *testing.T) {
	s := testSchemaWithCompositeKey(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add records with different composite keys
	regions := []string{"us-east", "us-west", "eu-west"}
	for _, region := range regions {
		for i := range 3 {
			inst := instancetest.Valid(t, s, "Record", map[string]any{"region": region, "id": fmt.Sprintf("id-%d", i), "value": fmt.Sprintf("%s-%d", region, i)})

			result := g.Add(ctx, inst)
			if err := result.Err(); err != nil {
				t.Fatalf("Add should succeed: %v", err)
			}
		}
	}

	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Record")))

	if len(instances) != 9 {
		t.Fatalf("Expected 9 records, got %d", len(instances))
	}

	// Verify lookup by composite key works
	key := graph.FormatKey("us-east", "id-0")
	found, ok := snap.InstanceByKey(mustTypeID(t, s, "Record"), key)
	if !ok {
		t.Errorf("InstanceByKey failed for composite key %s", key)
	}
	if !found.IsZero() {
		val, _ := found.Property("value")
		if str, _ := val.String(); str != "us-east-0" {
			t.Errorf("Wrong value for composite key: %s", str)
		}
	}
}

func TestGraph_EmptyProperties(t *testing.T) {
	// Instance with no properties beyond PK
	ctx := t.Context()

	s := loadSchemaAt(t, `schema "test"

type Person {
	id String primary
	name String
}
`, "test.yammm")
	g := graph.New(s)
	inst := instancetest.Valid(t, s, "Person", map[string]any{"id": "minimal"})

	result := g.Add(ctx, inst)
	if err := result.Err(); err != nil {
		t.Fatalf("Add should succeed: %v", err)
	}

	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Person")))
	if len(instances) != 1 {
		t.Fatalf("Expected 1 instance, got %d", len(instances))
	}

	// A keyed instance holds its key property, and nothing else here.
	found := instances[0]
	if found.Properties().Len() != 1 {
		t.Errorf("Expected 1 property, got %d", found.Properties().Len())
	}
}

func TestGraph_ComposedRelations_Sorted(t *testing.T) {
	// ComposedRelations() returns sorted relation names
	s := testSchemaWithMultipleCompositions(t)
	g := graph.New(s)
	ctx := t.Context()

	note := partData(t, s, "Note",
		[]any{"n1"}, map[string]any{"text": "Note 1"})
	tag := partData(t, s, "Tag",
		[]any{"t1"}, map[string]any{})
	doc := instancetest.Valid(t, s, "Document", composedData(t, s, "Document",
		composedData(t, s, "Document", keyedProps(t, s, "Document", []any{"doc1"}, map[string]any{"title": "Doc 1"}), "TAGS", tag),
		"NOTES", note))
	if r := g.Add(ctx, doc); !r.OK() {
		t.Fatalf("Add document failed: %s", r.String())
	}

	snap := g.Snapshot()
	docs := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Document")))
	if len(docs) != 1 {
		t.Fatalf("Expected 1 document, got %d", len(docs))
	}

	relations := docs[0].ComposedRelations()
	if len(relations) != 2 {
		t.Fatalf("Expected 2 composition relations, got %d", len(relations))
	}

	// Should be sorted: "notes" < "TAGS"
	if relations[0] != "NOTES" {
		t.Errorf("First relation should be 'notes', got %s", relations[0])
	}
	if relations[1] != "TAGS" {
		t.Errorf("Second relation should be 'tags', got %s", relations[1])
	}
}

func TestGraph_MultipleCompositions(t *testing.T) {
	// Parent with multiple composition relations
	s := testSchemaWithMultipleCompositions(t)
	g := graph.New(s)
	ctx := t.Context()

	var notes, tags []map[string]any
	for i := range 3 {
		notes = append(notes, partData(t, s, "Note",
			[]any{fmt.Sprintf("n%d", i)}, map[string]any{"text": fmt.Sprintf("Note %d", i)}))
	}
	for i := range 2 {
		tags = append(tags, partData(t, s, "Tag",
			[]any{fmt.Sprintf("t%d", i)}, map[string]any{}))
	}
	doc := instancetest.Valid(t, s, "Document", composedData(t, s, "Document",
		composedData(t, s, "Document", keyedProps(t, s, "Document", []any{"doc1"}, map[string]any{"title": "Doc 1"}), "NOTES", notes...),
		"TAGS", tags...))
	if r := g.Add(ctx, doc); !r.OK() {
		t.Fatalf("Add document failed: %s", r.String())
	}

	snap := g.Snapshot()
	docs := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Document")))

	assertComposedCount(t, docs[0], "NOTES", 3)
	assertComposedCount(t, docs[0], "TAGS", 2)
}

// Verification Tests
//
// These tests verify compliance with behavioral contracts.

func TestContract6_InstanceTagForm(t *testing.T) {
	// All public APIs use instance tag form
	// Local types: unqualified (e.g., "Person")
	// Imported types: alias-qualified (e.g., "c.Entity")
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add local User
	user := instancetest.Valid(t, mainSchema, "User", map[string]any{"id": "u1", "username": "alice"})

	if r := g.Add(ctx, user); !r.OK() {
		t.Fatalf("Add user failed: %s", r.String())
	}

	// Add imported Entity
	entity := instancetest.Valid(t, mainSchema, "c.Entity", map[string]any{"id": "e1", "name": "Entity 1"})

	if r := g.Add(ctx, entity); !r.OK() {
		t.Fatalf("Add entity failed: %s", r.String())
	}

	snap := g.Snapshot()

	// Verify Types() returns instance tag form
	types := snap.Types()
	hasUser := false
	hasCEntity := false
	for _, id := range types {
		switch schema.TagForm(mainSchema, id) {
		case "User":
			hasUser = true
		case "c.Entity":
			hasCEntity = true
		}
	}
	if !hasUser || !hasCEntity {
		t.Errorf("Types() should return instance tag form, got %v", types)
	}

	// Verify InstancesOf() works with instance tag form
	if slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "User"))) == nil {
		t.Error("InstancesOf(\"User\") should find local type")
	}
	if slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "c.Entity"))) == nil {
		t.Error("InstancesOf(\"c.Entity\") should find imported type")
	}

	// Verify graph.Instance.TypeName() returns instance tag form
	users := slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "User")))
	if users[0].TypeName() != "User" {
		t.Errorf("Instance.TypeName() should be \"User\", got %q", users[0].TypeName())
	}
	entities := slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "c.Entity")))
	if entities[0].TypeName() != "c.Entity" {
		t.Errorf("Instance.TypeName() should be \"c.Entity\", got %q", entities[0].TypeName())
	}
}

func TestContract7_TypeIDIndexing(t *testing.T) {
	// TypeID-based indexing avoids collisions between same-named types
	// Two imports with same type name should be kept separate
	members := loadClosure(t, "main.yammm", map[string]string{
		"b.yammm": `schema "schema_b"

type Product {
	id String primary
	sourceB String required
}
`,
		"c.yammm": `schema "schema_c"

type Product {
	id String primary
	sourceC String required
}
`,
		"main.yammm": `schema "main"

import "b" as b
import "c" as c

type Container {
	id String primary
}
`,
	})
	mainSchema := members["main"]

	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add b.Product with PK "p1"
	instB := instancetest.Valid(t, mainSchema, "b.Product", map[string]any{"id": "p1", "sourceB": "from B"})

	if r := g.Add(ctx, instB); !r.OK() {
		t.Fatalf("Add b.Product failed: %s", r.String())
	}

	// Add c.Product with same PK - should NOT be a duplicate
	instC := instancetest.Valid(t, mainSchema, "c.Product", map[string]any{"id": "p1", "sourceC": "from C"})

	result := g.Add(ctx, instC)
	if err := result.Err(); err != nil {
		t.Errorf("Same PK with different TypeID should not be duplicate: %v", err)
	}

	// Verify both exist
	snap := g.Snapshot()
	if len(slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "b.Product")))) != 1 {
		t.Error("b.Product should exist")
	}
	if len(slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "c.Product")))) != 1 {
		t.Error("c.Product should exist")
	}
}

func TestContract19_FailureSemantics(t *testing.T) {
	// Data issues → result with diagnostics; programmer errors → panic;
	// context cancellation → Fatal diagnostic in result
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Test case 1: Data issue (duplicate PK) → result with diagnostic
	inst1 := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice"})
	inst2 := instancetest.Valid(t, s, "Person", map[string]any{"id": "alice", "name": "Alice 2"})

	result1 := g.Add(ctx, inst1)
	if !result1.OK() {
		t.Errorf("First Add should succeed: %s", result1.String())
	}

	result2 := g.Add(ctx, inst2)
	if !result2.HasCode(diag.E_DUPLICATE_PK) {
		t.Errorf("Duplicate PK result reported %s, want %s", result2, diag.E_DUPLICATE_PK)
	}

	// Test case 2: Nil instance → panic
	panicked := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		g.Add(ctx, instance.ValidInstance{})
	}()
	if !panicked {
		t.Error("Nil instance should panic")
	}

	// Test case 3: Context cancellation → Fatal diagnostic in result
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	result4 := g.Add(cancelCtx, inst1)
	if result4.OK() {
		t.Error("Cancelled context result should not be OK")
	}

	hasFatal := false
	for issue := range result4.Issues() {
		if issue.Severity() == diag.Fatal {
			hasFatal = true
			break
		}
	}
	if !hasFatal {
		t.Error("Cancelled context should produce a Fatal diagnostic")
	}
}

func TestResult_Instances(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// Add some instances
	for i := range 3 {
		inst := instancetest.Valid(t, s, "Person", map[string]any{"id": fmt.Sprintf("person-%d", i), "name": fmt.Sprintf("Name %d", i)})
		if r := g.Add(ctx, inst); !r.OK() {
			t.Fatalf("Add failed: %s", r.String())
		}
	}

	snap := g.Snapshot()

	// Count all instances via the deterministic iterator.
	total := 0
	for range snap.AllInstances() {
		total++
	}
	if total != 3 {
		t.Errorf("AllInstances() yielded %d items, want 3", total)
	}
}

// Tests for extractCompositions - inline composition handling

func TestGraph_Add_InlineCompositions(t *testing.T) {
	// Tests extractCompositions by adding parent with inline children
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	parent := parentWith(t, s, "Parent", []any{"p1"}, map[string]any{"name": "Parent 1"}, "CHILDREN",
		partData(t, s, "Child", []any{"c1"}, map[string]any{"name": "Child 1"}),
		partData(t, s, "Child", []any{"c2"}, map[string]any{"name": "Child 2"}))

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify inline children were extracted
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "CHILDREN", 2)
}

func TestGraph_Add_NestedInlineCompositions(t *testing.T) {
	// Tests nested extractCompositions
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	parent := instancetest.Valid(t, s, "Parent", map[string]any{
		"id":   "p1",
		"name": "Parent 1",
		"children": []any{map[string]any{
			"id":             "c1",
			"name":           "Child 1",
			"grand_children": []any{map[string]any{"id": "gc1", "name": "GrandChild 1"}},
		}},
	})

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify nested structure
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	assertComposedCount(t, parents[0], "CHILDREN", 1)

	// Verify grandchildren
	children := slices.Collect(parents[0].Composed("CHILDREN"))
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	assertComposedCount(t, children[0], "GRAND_CHILDREN", 1)
}

func TestGraph_Add_InlineComposition_EmptySlice(t *testing.T) {
	// Tests extractCompositions with empty composition slice
	ctx := t.Context()

	// The composition is optional, which the validator takes empty.
	s := loadSchemaAt(t, `schema "composition"

part type Child {
	id String primary
	name String required
}

type Parent {
	id String primary
	name String required
	*-> CHILDREN (_:many) Child
}
`, "composition.yammm")
	g := graph.New(s)
	parent := instancetest.Valid(t, s, "Parent", map[string]any{"id": "p1", "name": "Parent 1", "children": []any{}})

	result := g.Add(ctx, parent)
	if err := result.Err(); err != nil {
		t.Fatalf("Add parent should succeed: %v", err)
	}

	// Verify no children
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	assertComposedCount(t, parents[0], "CHILDREN", 0)
}

func TestAdd_NestedCompositionReachesTheGrandchild(t *testing.T) {
	s := testSchemaWithNestedComposition(t)
	g := graph.New(s)
	ctx := t.Context()

	parent := instancetest.Valid(t, s, "Parent", map[string]any{
		"id":   "p1",
		"name": "Parent 1",
		"children": []any{map[string]any{
			"id":             "c1",
			"name":           "Child 1",
			"grand_children": []any{map[string]any{"id": "gc1", "name": "GrandChild 1"}},
		}},
	})
	if err := g.Add(ctx, parent).Err(); err != nil {
		t.Fatalf("Add should succeed: %v", err)
	}

	// Verify nested structure
	snap := g.Snapshot()
	parents := slices.Collect(snap.InstancesOf(mustTypeID(t, s, "Parent")))
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	// Parent should have child
	children := slices.Collect(parents[0].Composed("CHILDREN"))
	if len(children) != 1 {
		t.Fatalf("Expected 1 child, got %d", len(children))
	}

	grandchildren := slices.Collect(children[0].Composed("GRAND_CHILDREN"))
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

// TestAdd_PerOperationDiagnostics verifies that Add() returns per-operation results,
// not cumulative results. A successful Add after a failed Add should return OK.
func TestAdd_PerOperationDiagnostics(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := t.Context()

	// First add - use an invalid type to trigger an error
	if r := g.Add(ctx, mustValidInstance(t, s, "Person", []any{"dup"}, map[string]any{"name": "Dup"})); !r.OK() {
		t.Fatal(r)
	}
	invalidInst := mustValidInstance(t, s, "Person", []any{"dup"}, map[string]any{"name": "Dup"})

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
		"EMPLOYER", [][]any{{"missing-company"}})

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
		"EMPLOYER", [][]any{{"acme"}})

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
		"EMPLOYER", [][]any{{"missing"}})

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
			name:    "AddOrMerge",
			fn:      func() { g.AddOrMerge(nil, inst) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "graph.AddOrMerge: nil context",
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
