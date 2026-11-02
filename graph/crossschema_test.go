package graph_test

import (
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/internal/instancetest"
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
	user := instancetest.Valid(t, mainSchema, "User", map[string]any{"id": "u1", "username": "alice"})

	result := g.Add(ctx, user)
	if err := result.Err(); err != nil {
		t.Errorf("Add user should succeed: %v", err)
	}

	// The name is unqualified, but Add resolves by TypeID and this instance
	// carries the common schema's identity — so it installs under that
	// identity and not under any main-schema one. The rendering is not the
	// address; that is the whole of the v0.12.0 identity break.
	entityType, _ := commonSchema.Type("Entity")
	entity := instancetest.Valid(t, commonSchema, "Entity", map[string]any{"id": "e1", "name": "Entity 1"})

	if res := g.Add(ctx, entity); !res.OK() {
		t.Fatalf("Add resolves by identity, so the unqualified name is irrelevant: %s", res.String())
	}

	snap := g.Snapshot()
	if len(slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "User")))) != 1 {
		t.Errorf("Expected 1 User instance, got %d", len(slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "User")))))
	}
	if n := len(slices.Collect(snap.InstancesOf(entityType.ID()))); n != 1 {
		t.Errorf("Expected the Entity to install under the common schema's identity, got %d", n)
	}
}

func TestGraph_StrictResolution_QualifiedLookup(t *testing.T) {
	// "c.Entity" matches imported c.Entity
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add Entity using qualified name "c.Entity"
	entity := instancetest.Valid(t, mainSchema, "c.Entity", map[string]any{"id": "e1", "name": "Entity 1"})

	result := g.Add(ctx, entity)
	if err := result.Err(); err != nil {
		t.Errorf("Add entity should succeed: %v", err)
	}

	// Verify entity is in graph with qualified type name
	snap := g.Snapshot()
	instances := slices.Collect(snap.InstancesOf(tagID(t, mainSchema, "c.Entity")))
	if len(instances) != 1 {
		t.Errorf("Expected 1 c.Entity instance, got %d", len(instances))
	}
}

func TestGraph_InstanceByKey_Qualified(t *testing.T) {
	// Lookup by alias-qualified type name
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add Entity
	entity := instancetest.Valid(t, mainSchema, "c.Entity", map[string]any{"id": "e1", "name": "Entity 1"})

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
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// Add local User
	user := instancetest.Valid(t, mainSchema, "User", map[string]any{"id": "u1", "username": "alice"})

	g.Add(ctx, user)

	// Add imported Entity
	entity := instancetest.Valid(t, mainSchema, "c.Entity", map[string]any{"id": "e1", "name": "Entity 1"})

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
	mainSchema, _ := testMultiSchemaSetup(t)
	g := graph.New(mainSchema)
	ctx := t.Context()

	// First add Entity (target)
	entity := instancetest.Valid(t, mainSchema, "c.Entity", map[string]any{"id": "e1", "name": "Entity 1"})

	g.Add(ctx, entity)

	// Add User with edge to Entity
	user := instancetest.Valid(t, mainSchema, "User", map[string]any{
		"id": "u1", "username": "alice", "entity": map[string]any{"_target_id": "e1"},
	})

	result := g.Add(ctx, user)
	if err := result.Err(); err != nil {
		t.Errorf("Add user should succeed: %v", err)
	}

	// Verify edge exists from User to c.Entity
	snap := g.Snapshot()
	edgeList := slices.Collect(snap.Edges())
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
	members := loadClosure(t, "a.yammm", map[string]string{
		"b.yammm": `schema "schema_b"

type Resource {
	id String primary
	nameB String required
}
`,
		"c.yammm": `schema "schema_c"

type Resource {
	id String primary
	nameC String required
}
`,
		"a.yammm": `schema "schema_a"

import "b" as b
import "c" as c

type Container {
	id String primary
}
`,
	})
	schemaA := members["schema_a"]

	g := graph.New(schemaA)
	ctx := t.Context()

	// Add b.Resource
	instB := instancetest.Valid(t, schemaA, "b.Resource", map[string]any{"id": "r1", "nameB": "Resource from B"})

	g.Add(ctx, instB)

	// Add c.Resource
	instC := instancetest.Valid(t, schemaA, "c.Resource", map[string]any{"id": "r1", "nameC": "Resource from C"})

	g.Add(ctx, instC)

	// Verify both are in graph with correct types
	snap := g.Snapshot()

	bInstances := slices.Collect(snap.InstancesOf(tagID(t, schemaA, "b.Resource")))
	if len(bInstances) != 1 {
		t.Errorf("Expected 1 b.Resource, got %d", len(bInstances))
	}

	cInstances := slices.Collect(snap.InstancesOf(tagID(t, schemaA, "c.Resource")))
	if len(cInstances) != 1 {
		t.Errorf("Expected 1 c.Resource, got %d", len(cInstances))
	}

	// Verify they are distinct instances
	types := snap.Types()
	if len(types) != 2 {
		t.Errorf("Expected 2 types, got %d: %v", len(types), types)
	}
}
