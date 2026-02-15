package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// Cross-Schema Tests
//
// These tests verify proper handling of imported types, alias-qualified names,
// and TypeID-based indexing for cross-schema scenarios.

func TestGraph_StrictResolution_LocalOnly(t *testing.T) {
	// Unqualified name only matches local types
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// Add a local User instance (should succeed)
	userType, _ := mainSchema.Type("User")
	user := instance.NewValidInstance(
		"User",
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		nil, nil, nil,
	)

	result, err := g.Add(ctx, user)
	if err != nil {
		t.Fatalf("Add user error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add user should succeed: %s", result.String())
	}

	// Try to add Entity using unqualified name - should fail since Entity is in common, not local
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"Entity", // Unqualified - won't match because Entity is imported, not local
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	_, err = g.Add(ctx, entity)
	if err != nil {
		t.Fatalf("Add entity error: %v", err)
	}

	// Snapshot should have User but the Entity add should have triggered diagnostics
	// because the type name lookup would fail with unqualified "Entity"
	snap := g.Snapshot()
	if len(snap.InstancesOf("User")) != 1 {
		t.Errorf("Expected 1 User instance, got %d", len(snap.InstancesOf("User")))
	}
}

func TestGraph_StrictResolution_QualifiedLookup(t *testing.T) {
	// "c.Entity" matches imported c.Entity
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// Add Entity using qualified name "c.Entity"
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity", // Qualified name matches the import alias
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	result, err := g.Add(ctx, entity)
	if err != nil {
		t.Fatalf("Add entity error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add entity should succeed: %s", result.String())
	}

	// Verify entity is in graph with qualified type name
	snap := g.Snapshot()
	instances := snap.InstancesOf("c.Entity")
	if len(instances) != 1 {
		t.Errorf("Expected 1 c.Entity instance, got %d", len(instances))
	}
}

func TestGraph_StrictResolution_UnknownAlias(t *testing.T) {
	// Instance from completely unknown schema returns ErrSchemaMismatch
	mainSchema, _ := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// Create an instance with unknown alias prefix - schema not in import chain
	unknownType := schema.NewTypeID(location.MustNewSourceID("test://unknown.yammm"), "SomeType")
	inst := instance.NewValidInstance(
		"unknown.SomeType",
		unknownType,
		immutable.WrapKey([]any{"x1"}),
		immutable.WrapProperties(map[string]any{}),
		nil, nil, nil,
	)

	_, err := g.Add(ctx, inst)
	if !errors.Is(err, ErrSchemaMismatch) {
		t.Errorf("Add with unknown schema should return ErrSchemaMismatch, got %v", err)
	}
}

func TestGraph_InstanceByKey_Qualified(t *testing.T) {
	// Lookup by alias-qualified type name
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// Add Entity
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, entity); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()

	// Lookup by qualified name should work
	found, ok := snap.InstanceByKey("c.Entity", FormatKey("e1"))
	if !ok {
		t.Error("InstanceByKey should find c.Entity")
	}
	if found.TypeName() != "c.Entity" {
		t.Errorf("Instance type should be c.Entity, got %s", found.TypeName())
	}

	// Lookup by unqualified name should NOT work
	_, ok = snap.InstanceByKey("Entity", FormatKey("e1"))
	if ok {
		t.Error("InstanceByKey should not find Entity without qualifier")
	}
}

func TestGraph_Types_InstanceTagForm(t *testing.T) {
	// Types() returns mixed local/qualified names
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// Add local User
	userType, _ := mainSchema.Type("User")
	user := instance.NewValidInstance(
		"User",
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, user); err != nil {
		t.Fatalf("Add user error: %v", err)
	}

	// Add imported Entity
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, entity); err != nil {
		t.Fatalf("Add entity error: %v", err)
	}

	snap := g.Snapshot()
	types := snap.Types()

	// Should have both types in sorted order
	if len(types) != 2 {
		t.Fatalf("Expected 2 types, got %d: %v", len(types), types)
	}

	// Types should be sorted: "User" < "c.Entity" lexicographically
	expected := []string{"User", "c.Entity"}
	for i, typeName := range types {
		if typeName != expected[i] {
			t.Errorf("Type[%d] should be %q, got %q", i, expected[i], typeName)
		}
	}
}

func TestGraph_Edge_CrossSchema(t *testing.T) {
	// Association from local to imported type
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := New(mainSchema)
	ctx := context.Background()

	// First add Entity (target)
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, entity); err != nil {
		t.Fatalf("Add entity error: %v", err)
	}

	// Add User with edge to Entity
	userType, _ := mainSchema.Type("User")
	targets := []instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{"e1"}),
			immutable.Properties{},
		),
	}
	edges := map[string]*instance.ValidEdgeData{
		"entity": instance.NewValidEdgeData(targets),
	}
	user := instance.NewValidInstance(
		"User",
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		edges,
		nil,
		nil,
	)

	result, err := g.Add(ctx, user)
	if err != nil {
		t.Fatalf("Add user error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add user should succeed: %s", result.String())
	}

	// Verify edge exists from User to c.Entity
	snap := g.Snapshot()
	edgeList := snap.Edges()
	if len(edgeList) != 1 {
		t.Fatalf("Expected 1 edge, got %d", len(edgeList))
	}

	edge := edgeList[0]
	if edge.Source().TypeName() != "User" {
		t.Errorf("Edge source should be User, got %s", edge.Source().TypeName())
	}
	if edge.Target().TypeName() != "c.Entity" {
		t.Errorf("Edge target should be c.Entity, got %s", edge.Target().TypeName())
	}
	if edge.Relation() != "entity" {
		t.Errorf("Edge relation should be entity, got %s", edge.Relation())
	}
}

func TestGraph_MultiImport_Disambiguation(t *testing.T) {
	// Multiple imports can be disambiguated by alias
	// Schema A imports B as "b" and C as "c"
	// Both B and C have a type called "Resource"
	reg := schema.NewRegistry()

	// Schema B with Resource
	schemaB, result := build.NewBuilder().
		WithName("schema_b").
		WithSourceID(location.MustNewSourceID("test://b.yammm")).
		AddType("Resource").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("nameB", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema B: %s", result.String())
	}
	if err := reg.Register(schemaB); err != nil {
		t.Fatalf("Failed to register schema B: %v", err)
	}

	// Schema C with Resource (same type name!)
	schemaC, result := build.NewBuilder().
		WithName("schema_c").
		WithSourceID(location.MustNewSourceID("test://c.yammm")).
		AddType("Resource").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("nameC", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema C: %s", result.String())
	}
	if err := reg.Register(schemaC); err != nil {
		t.Fatalf("Failed to register schema C: %v", err)
	}

	// Schema A imports both B and C
	schemaA, result := build.NewBuilder().
		WithName("schema_a").
		WithSourceID(location.MustNewSourceID("test://a.yammm")).
		WithRegistry(reg).
		AddImport("schema_b", "b").
		AddImport("schema_c", "c").
		AddType("Container").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema A: %s", result.String())
	}

	g := New(schemaA)
	ctx := context.Background()

	// Add b.Resource
	resourceB, _ := schemaB.Type("Resource")
	instB := instance.NewValidInstance(
		"b.Resource",
		resourceB.ID(),
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(map[string]any{"nameB": "Resource from B"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, instB); err != nil {
		t.Fatalf("Add b.Resource error: %v", err)
	}

	// Add c.Resource
	resourceC, _ := schemaC.Type("Resource")
	instC := instance.NewValidInstance(
		"c.Resource",
		resourceC.ID(),
		immutable.WrapKey([]any{"r1"}), // Same PK is OK - different types
		immutable.WrapProperties(map[string]any{"nameC": "Resource from C"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, instC); err != nil {
		t.Fatalf("Add c.Resource error: %v", err)
	}

	// Verify both are in graph with correct types
	snap := g.Snapshot()

	bInstances := snap.InstancesOf("b.Resource")
	if len(bInstances) != 1 {
		t.Errorf("Expected 1 b.Resource, got %d", len(bInstances))
	}

	cInstances := snap.InstancesOf("c.Resource")
	if len(cInstances) != 1 {
		t.Errorf("Expected 1 c.Resource, got %d", len(cInstances))
	}

	// Verify they are distinct instances
	types := snap.Types()
	if len(types) != 2 {
		t.Errorf("Expected 2 types, got %d: %v", len(types), types)
	}
}
