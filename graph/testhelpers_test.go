package graph_test

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// Test Schema Builders
//
// These helpers create schemas for specific test scenarios.

// testSchemaWithAssociation creates a schema with Person -> Company association.
func testSchemaWithAssociation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("association").
		WithSourceID(location.MustNewSourceID("test://association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), false, false). // required one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build association schema: %s", result.String())
	}
	return s
}

// testSchemaWithOptionalAssociation creates a schema with Person -> Company optional association.
func testSchemaWithOptionalAssociation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("optional_association").
		WithSourceID(location.MustNewSourceID("test://optional_association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), true, false). // optional one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build optional association schema: %s", result.String())
	}
	return s
}

// testSchemaWithManyAssociation creates a schema with Person -> Company many association.
func testSchemaWithManyAssociation(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("many_association").
		WithSourceID(location.MustNewSourceID("test://many_association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employers", schema.LocalTypeRef("Company", location.Span{}), false, true). // required many
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build many association schema: %s", result.String())
	}
	return s
}

// testSchemaWithChainedAssociations creates A -> B -> C chain.
func testSchemaWithChainedAssociations(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("chained").
		WithSourceID(location.MustNewSourceID("test://chained.yammm")).
		AddType("TypeC").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("TypeB").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refC", schema.LocalTypeRef("TypeC", location.Span{}), false, false).
		Done().
		AddType("TypeA").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refB", schema.LocalTypeRef("TypeB", location.Span{}), false, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build chained schema: %s", result.String())
	}
	return s
}

// testSchemaWithComposition creates a schema with Parent -> Child composition.
func testSchemaWithComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("composition").
		WithSourceID(location.MustNewSourceID("test://composition.yammm")).
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("children", schema.LocalTypeRef("Child", location.Span{}), true, true). // optional many
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithOneComposition creates a schema with Parent -> Child (one) composition.
func testSchemaWithOneComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("one_composition").
		WithSourceID(location.MustNewSourceID("test://one_composition.yammm")).
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("child", schema.LocalTypeRef("Child", location.Span{}), true, false). // optional one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build one composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithPKLessChild creates a schema with PKless child composition.
func testSchemaWithPKLessChild(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("pkless").
		WithSourceID(location.MustNewSourceID("test://pkless.yammm")).
		AddType("Item").
		AsPart().
		WithProperty("value", schema.StringConstraint{}).
		Done().
		AddType("Container").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("items", schema.LocalTypeRef("Item", location.Span{}), true, true).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build pkless schema: %s", result.String())
	}
	return s
}

// testSchemaWithNestedComposition creates a schema with nested compositions.
func testSchemaWithNestedComposition(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("nested").
		WithSourceID(location.MustNewSourceID("test://nested.yammm")).
		AddType("GrandChild").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Child").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("grandChildren", schema.LocalTypeRef("GrandChild", location.Span{}), true, true).
		Done().
		AddType("Parent").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithComposition("children", schema.LocalTypeRef("Child", location.Span{}), true, true).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build nested schema: %s", result.String())
	}
	return s
}

// testSchemaWithMultipleCompositions creates a schema with multiple composition relations.
func testSchemaWithMultipleCompositions(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("multi_comp").
		WithSourceID(location.MustNewSourceID("test://multi_comp.yammm")).
		AddType("Note").
		AsPart().
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("text", schema.StringConstraint{}).
		Done().
		AddType("Tag").
		AsPart().
		WithPrimaryKey("name", schema.StringConstraint{}).
		Done().
		AddType("Document").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("title", schema.StringConstraint{}).
		WithComposition("notes", schema.LocalTypeRef("Note", location.Span{}), true, true).
		WithComposition("tags", schema.LocalTypeRef("Tag", location.Span{}), true, true).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build multi composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithCompositeKey creates a schema with composite primary key.
func testSchemaWithCompositeKey(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("composite").
		WithSourceID(location.MustNewSourceID("test://composite.yammm")).
		AddType("Record").
		WithPrimaryKey("region", schema.StringConstraint{}).
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("value", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build composite key schema: %s", result.String())
	}
	return s
}

// testSchemaWithMutualAssociations creates TypeA <-> TypeB mutual associations (A→B→A cycle).
func testSchemaWithMutualAssociations(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("mutual").
		WithSourceID(location.MustNewSourceID("test://mutual.yammm")).
		AddType("TypeA").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refB", schema.LocalTypeRef("TypeB", location.Span{}), true, false). // optional one
		Done().
		AddType("TypeB").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refA", schema.LocalTypeRef("TypeA", location.Span{}), true, false). // optional one
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build mutual associations schema: %s", result.String())
	}
	return s
}

// testSchemaWithCircularChain creates A → B → C → A circular chain.
func testSchemaWithCircularChain(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := schema.NewBuilder().
		WithName("circular").
		WithSourceID(location.MustNewSourceID("test://circular.yammm")).
		AddType("TypeA").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refB", schema.LocalTypeRef("TypeB", location.Span{}), true, false).
		Done().
		AddType("TypeB").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refC", schema.LocalTypeRef("TypeC", location.Span{}), true, false).
		Done().
		AddType("TypeC").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("refA", schema.LocalTypeRef("TypeA", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build circular chain schema: %s", result.String())
	}
	return s
}

// Instance Creation Helpers
//
// Thin wrappers over [instancetest.VI] adding the schema type lookup and
// the edge-target assembly the graph suites repeat. Edge resolution is
// exact-TypeID, so every instance must carry its real schema TypeID.

// mustTypeID resolves a schema type's ID, failing the test if absent.
func mustTypeID(t *testing.T, s *schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	return typ.ID()
}

// edgeData assembles single-relation edge data: every target key shares
// the same edge properties (pass nil for none).
func edgeData(relationName string, edgeProps map[string]any, targetKeys ...[]any) map[string]*instance.ValidEdgeData {
	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, targetKey := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(
			immutable.WrapKey(targetKey),
			immutable.WrapProperties(edgeProps),
		)
	}
	return map[string]*instance.ValidEdgeData{relationName: instance.NewValidEdgeData(targets)}
}

// mustValidInstance creates a ValidInstance for testing.
func mustValidInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
	)
}

// mustValidInstanceWithEdge creates a ValidInstance with edge data.
func mustValidInstanceWithEdge(
	t *testing.T,
	s *schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
	targetKeys [][]any,
) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(edgeData(relationName, nil, targetKeys...)),
	)
}

// mustValidInstanceWithEdgeProps creates a ValidInstance with edge data including properties.
func mustValidInstanceWithEdgeProps(
	t *testing.T,
	s *schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
	targetKey []any,
	edgeProps map[string]any,
) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(edgeData(relationName, edgeProps, targetKey)),
	)
}

// mustValidInstanceWithEmptyEdge creates a ValidInstance with an empty edge (for required many tests).
func mustValidInstanceWithEmptyEdge(
	t *testing.T,
	s *schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
) *instance.ValidInstance {
	t.Helper()
	return instancetest.VI(
		typeName,
		instancetest.TypeID(mustTypeID(t, s, typeName)),
		instancetest.PK(pk...),
		instancetest.Props(props),
		instancetest.Edges(map[string]*instance.ValidEdgeData{relationName: instance.NewValidEdgeData(nil)}),
	)
}

// mustValidPartInstance creates a ValidInstance for a part type.
func mustValidPartInstance(t *testing.T, s *schema.Schema, typeName string, pk []any, props map[string]any) *instance.ValidInstance {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	if !typ.IsPart() {
		t.Fatalf("Type %q is not a part type", typeName)
	}
	return instancetest.VI(
		typeName,
		instancetest.TypeID(typ.ID()),
		instancetest.PK(pk...),
		instancetest.Props(props),
	)
}

// mustValidPKLessInstance creates a ValidInstance for a PK-less part type.
// The zero Key has no [instancetest.VI] option, so this one builds directly.
func mustValidPKLessInstance(t *testing.T, s *schema.Schema, typeName string, props map[string]any) *instance.ValidInstance {
	t.Helper()
	return instance.NewValidInstance(
		typeName,
		mustTypeID(t, s, typeName),
		immutable.Key{}, // No PK
		immutable.WrapProperties(props),
		nil, nil, nil,
	)
}

// Multi-Schema Helpers

// testMultiSchemaSetup creates two schemas with imports for cross-schema testing.
// Returns (main schema, imported schema).
func testMultiSchemaSetup(t *testing.T) (*schema.Schema, *schema.Schema) {
	t.Helper()

	reg := schema.NewRegistry()

	// Create the common schema first (to be imported)
	commonSchema, result := schema.NewBuilder().
		WithName("common").
		WithSourceID(location.MustNewSourceID("test://common.yammm")).
		AddType("Entity").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build common schema: %s", result.String())
	}

	// Register common schema
	if err := reg.Register(commonSchema); err != nil {
		t.Fatalf("Failed to register common schema: %v", err)
	}

	// Create main schema that imports common
	mainSchema, result := schema.NewBuilder().
		WithName("main").
		WithSourceID(location.MustNewSourceID("test://main.yammm")).
		WithRegistry(reg).
		AddImport("common", "c").
		AddType("User").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("username", schema.StringConstraint{}).
		WithRelation("entity", schema.NewTypeRef("c", "Entity", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build main schema: %s", result.String())
	}

	return mainSchema, commonSchema
}

// testTripleSchemaSetup creates three schemas for transitive import testing.
// Schema A imports B, B imports C. A cannot directly access C types.
func testTripleSchemaSetup(t *testing.T) (schemaA, schemaB, schemaC *schema.Schema, reg *schema.Registry) {
	t.Helper()

	reg = schema.NewRegistry()

	// Schema C (base)
	schemaC, result := schema.NewBuilder().
		WithName("schema_c").
		WithSourceID(location.MustNewSourceID("test://c.yammm")).
		AddType("BaseType").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("value", schema.StringConstraint{}).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema C: %s", result.String())
	}

	if err := reg.Register(schemaC); err != nil {
		t.Fatalf("Failed to register schema C: %v", err)
	}

	// Schema B imports C
	schemaB, result = schema.NewBuilder().
		WithName("schema_b").
		WithSourceID(location.MustNewSourceID("test://b.yammm")).
		WithRegistry(reg).
		AddImport("schema_c", "c").
		AddType("MiddleType").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("base", schema.NewTypeRef("c", "BaseType", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema B: %s", result.String())
	}

	if err := reg.Register(schemaB); err != nil {
		t.Fatalf("Failed to register schema B: %v", err)
	}

	// Schema A imports B (but not C directly)
	schemaA, result = schema.NewBuilder().
		WithName("schema_a").
		WithSourceID(location.MustNewSourceID("test://a.yammm")).
		WithRegistry(reg).
		AddImport("schema_b", "b").
		AddType("TopType").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("label", schema.StringConstraint{}).
		WithRelation("middle", schema.NewTypeRef("b", "MiddleType", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema A: %s", result.String())
	}

	return schemaA, schemaB, schemaC, reg
}

// Assertion Helpers

// assertInstanceCount verifies the number of instances of a type in the result.
func assertInstanceCount(t *testing.T, result *graph.Snapshot, typeName string, expected int) bool {
	t.Helper()

	instances := result.InstancesOf(typeName)
	if len(instances) != expected {
		t.Errorf("Expected %d instances of %s, got %d", expected, typeName, len(instances))
		return false
	}
	return true
}

// assertEdgeCount verifies the number of edges in the result.
func assertEdgeCount(t *testing.T, result *graph.Snapshot, expected int) {
	t.Helper()

	edges := result.Edges()
	if len(edges) != expected {
		t.Errorf("Expected %d edges, got %d", expected, len(edges))
	}
}

// assertUnresolvedCount verifies the number of unresolved edges in the result.
func assertUnresolvedCount(t *testing.T, result *graph.Snapshot, expected int) {
	t.Helper()

	unresolved := result.Unresolved()
	if len(unresolved) != expected {
		t.Errorf("Expected %d unresolved edges, got %d", expected, len(unresolved))
	}
}

// assertComposedCount verifies the number of composed children for a relation.
func assertComposedCount(t *testing.T, inst *graph.Instance, relationName string, expected int) {
	t.Helper()

	count := inst.ComposedCount(relationName)
	if count != expected {
		t.Errorf("Expected %d composed children for %s, got %d", expected, relationName, count)
	}
}

// assertHasCode fails t unless result carries at least one issue with code.
func assertHasCode(t *testing.T, result diag.Result, code diag.Code) {
	t.Helper()
	if !result.HasCode(code) {
		t.Errorf("expected an issue with code %v in result: %s", code, result.String())
	}
}

// countCode returns the number of issues in result carrying code.
func countCode(result diag.Result, code diag.Code) int {
	count := 0
	for issue := range result.Issues() {
		if issue.Code() == code {
			count++
		}
	}
	return count
}
