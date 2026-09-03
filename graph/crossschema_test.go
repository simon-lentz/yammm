package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Cross-Schema Tests
//
// These tests verify proper handling of imported types, alias-qualified names,
// and TypeID-based indexing for cross-schema scenarios.

func TestGraph_StrictResolution_LocalOnly(t *testing.T) {
	// Unqualified name only matches local types
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add a local User instance (should succeed)
	userType, _ := mainSchema.Type("User")
	user := instance.NewValidInstance(
		"User",
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, user)
	if err := result.Err(); err != nil {
		t.Errorf("Add user should succeed: %v", err)
	}

	// The name is unqualified, but Add resolves by TypeID and this instance
	// carries the common schema's identity — so it installs under that
	// identity and not under any main-schema one. The rendering is not the
	// address; that is the whole of the v0.12.0 identity break.
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	if res := g.Add(ctx, entity); !res.OK() {
		t.Fatalf("Add resolves by identity, so the unqualified name is irrelevant: %s", res.String())
	}

	snap := g.Snapshot()
	if len(snap.InstancesOf(tagID(t, mainSchema, "User"))) != 1 {
		t.Errorf("Expected 1 User instance, got %d", len(snap.InstancesOf(tagID(t, mainSchema, "User"))))
	}
	if n := len(snap.InstancesOf(entityType.ID())); n != 1 {
		t.Errorf("Expected the Entity to install under the common schema's identity, got %d", n)
	}
}

func TestGraph_StrictResolution_QualifiedLookup(t *testing.T) {
	// "c.Entity" matches imported c.Entity
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add Entity using qualified name "c.Entity"
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity", // Qualified name matches the import alias
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	result := g.Add(ctx, entity)
	if err := result.Err(); err != nil {
		t.Errorf("Add entity should succeed: %v", err)
	}

	// Verify entity is in graph with qualified type name
	snap := g.Snapshot()
	instances := snap.InstancesOf(tagID(t, mainSchema, "c.Entity"))
	if len(instances) != 1 {
		t.Errorf("Expected 1 c.Entity instance, got %d", len(instances))
	}
}

func TestGraph_StrictResolution_UnknownAlias(t *testing.T) {
	// Instance from completely unknown schema should panic
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Create an instance with unknown alias prefix - schema not in import chain
	unknownType := schema.NewTypeID(location.MustNewSourceID("test://unknown.yammm"), "SomeType")
	inst := instance.NewValidInstance(
		"unknown.SomeType",
		unknownType,
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

func TestGraph_InstanceByKey_Qualified(t *testing.T) {
	// Lookup by alias-qualified type name
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add Entity
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	g.Add(ctx, entity)

	snap := g.Snapshot()

	// Lookup by qualified name should work
	found, ok := snap.InstanceByKey(tagID(t, mainSchema, "c.Entity"), graph.FormatKey("e1"))
	if !ok {
		t.Error("InstanceByKey should find c.Entity")
	}
	if found.TypeName() != "c.Entity" {
		t.Errorf("Instance type should be c.Entity, got %s", found.TypeName())
	}

	// A different type's identity must not find it. The qualified-versus-bare
	// distinction this once tested is gone: an identity names one type, so
	// there is no unqualified spelling of it to get wrong.
	_, ok = snap.InstanceByKey(mustTypeID(t, mainSchema, "User"), graph.FormatKey("e1"))
	if ok {
		t.Error("InstanceByKey should not find an instance under another type's identity")
	}
}

func TestGraph_Types_InstanceTagForm(t *testing.T) {
	// Types() returns mixed local/qualified names
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add local User
	userType, _ := mainSchema.Type("User")
	user := instance.NewValidInstance(
		"User",
		userType.ID(),
		immutable.WrapKey([]any{"u1"}),
		immutable.WrapProperties(map[string]any{"username": "alice"}),
		nil, nil, nil,
	)

	g.Add(ctx, user)

	// Add imported Entity
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	g.Add(ctx, entity)

	snap := g.Snapshot()
	types := snap.Types()

	// Should have both types in sorted order
	if len(types) != 2 {
		t.Fatalf("Expected 2 types, got %d: %v", len(types), types)
	}

	// Types() carries identities, and schema.TagForm renders each as the name
	// a snapshot writes: bare for a local type, alias-qualified for an
	// imported one. Ordering is by TypeID, which groups by schema path.
	got := make([]string, len(types))
	for i, id := range types {
		got[i] = schema.TagForm(mainSchema, id)
	}
	slices.Sort(got)
	want := []string{"User", "c.Entity"}
	if !slices.Equal(got, want) {
		t.Errorf("rendered types = %v, want %v", got, want)
	}
}

func TestGraph_Edge_CrossSchema(t *testing.T) {
	// Association from local to imported type
	mainSchema, commonSchema := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// First add Entity (target)
	entityType, _ := commonSchema.Type("Entity")
	entity := instance.NewValidInstance(
		"c.Entity",
		entityType.ID(),
		immutable.WrapKey([]any{"e1"}),
		immutable.WrapProperties(map[string]any{"name": "Entity 1"}),
		nil, nil, nil,
	)

	g.Add(ctx, entity)

	// Add User with edge to Entity
	userType, _ := mainSchema.Type("User")
	targets := []instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{"e1"}),
			immutable.Properties{},
		),
	}
	edges := map[string]*instance.ValidEdgeData{
		"ENTITY": instance.NewValidEdgeData(targets),
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

	result := g.Add(ctx, user)
	if err := result.Err(); err != nil {
		t.Errorf("Add user should succeed: %v", err)
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
	if edge.Relation() != "ENTITY" {
		t.Errorf("Edge relation should be entity, got %s", edge.Relation())
	}
}

func TestGraph_MultiImport_Disambiguation(t *testing.T) {
	// Multiple imports can be disambiguated by alias
	// Schema A imports B as "b" and C as "c"
	// Both B and C have a type called "Resource"
	reg := schema.NewRegistry()

	// Schema B with Resource
	schemaB, result := schema.NewBuilder().
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
	schemaC, result := schema.NewBuilder().
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
	schemaA, result := schema.NewBuilder().
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

	g := graph.New(schemaA)
	ctx := t.Context()

	// Add b.Resource
	resourceB, _ := schemaB.Type("Resource")
	instB := instance.NewValidInstance(
		"b.Resource",
		resourceB.ID(),
		immutable.WrapKey([]any{"r1"}),
		immutable.WrapProperties(map[string]any{"nameB": "Resource from B"}),
		nil, nil, nil,
	)

	g.Add(ctx, instB)

	// Add c.Resource
	resourceC, _ := schemaC.Type("Resource")
	instC := instance.NewValidInstance(
		"c.Resource",
		resourceC.ID(),
		immutable.WrapKey([]any{"r1"}), // Same PK is OK - different types
		immutable.WrapProperties(map[string]any{"nameC": "Resource from C"}),
		nil, nil, nil,
	)

	g.Add(ctx, instC)

	// Verify both are in graph with correct types
	snap := g.Snapshot()

	bInstances := snap.InstancesOf(tagID(t, schemaA, "b.Resource"))
	if len(bInstances) != 1 {
		t.Errorf("Expected 1 b.Resource, got %d", len(bInstances))
	}

	cInstances := snap.InstancesOf(tagID(t, schemaA, "c.Resource"))
	if len(cInstances) != 1 {
		t.Errorf("Expected 1 c.Resource, got %d", len(cInstances))
	}

	// Verify they are distinct instances
	types := snap.Types()
	if len(types) != 2 {
		t.Errorf("Expected 2 types, got %d: %v", len(types), types)
	}
}
