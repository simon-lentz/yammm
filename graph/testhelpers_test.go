package graph_test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/internal/snapparts"
	"github.com/simon-lentz/yammm/schema"
)

// Test Schema Builders
//
// These helpers create schemas for specific test scenarios.

// testSchemaWithAssociation creates a schema with Person -> Company association.
func testSchemaWithAssociation(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "association"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (one) Company
}
`, "association.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build association schema: %s", result.String())
	}
	return s
}

// testSchemaWithEdgeProperties is testSchemaWithAssociation with EMPLOYER
// declaring the edge properties role and since.
func testSchemaWithEdgeProperties(t *testing.T) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), `schema "association"

type Company {
	id String primary
	name String
}

type Person {
	id String primary
	name String
	--> EMPLOYER (one) Company {
		role String
		since Integer
	}
}
`, "association.yammm")
	if result.HasErrors() {
		t.Fatalf("Failed to load edge-property schema: %s", result.String())
	}
	return s
}

// testSchemaWithOptionalAssociation creates a schema with Person -> Company optional association.
func testSchemaWithOptionalAssociation(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "optional_association"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYER (_:one) Company
}
`, "optional_association.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build optional association schema: %s", result.String())
	}
	return s
}

// testSchemaWithManyAssociation creates a schema with Person -> Company many association.
func testSchemaWithManyAssociation(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "many_association"

type Company {
	id String primary
	name String required
}

type Person {
	id String primary
	name String required
	--> EMPLOYERS (one:many) Company
}
`, "many_association.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build many association schema: %s", result.String())
	}
	return s
}

// testSchemaWithChainedAssociations creates A -> B -> C chain.
func testSchemaWithChainedAssociations(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "chained"

type TypeC {
	id String primary
	name String required
}

type TypeB {
	id String primary
	name String required
	--> REF_C (one) TypeC
}

type TypeA {
	id String primary
	name String required
	--> REF_B (one) TypeB
}
`, "chained.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build chained schema: %s", result.String())
	}
	return s
}

// testSchemaWithComposition creates a schema with Parent -> Child composition.
func testSchemaWithComposition(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "composition"

part type Child {
	id String primary
	name String required
}

type Parent {
	id String primary
	name String required
	*-> CHILDREN (many) Child
}
`, "composition.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithOneComposition creates a schema with Parent -> Child (one) composition.
func testSchemaWithOneComposition(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "one_composition"

part type Child {
	id String primary
	name String required
}

type Parent {
	id String primary
	name String required
	*-> CHILD (_:one) Child
}
`, "one_composition.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build one composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithPKLessChild creates a schema with PKless child composition.
func testSchemaWithPKLessChild(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "pkless"

part type Item {
	value String required
}

type Container {
	id String primary
	name String required
	*-> ITEMS (many) Item
}
`, "pkless.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build pkless schema: %s", result.String())
	}
	return s
}

// testSchemaWithNestedComposition creates a schema with nested compositions.
func testSchemaWithNestedComposition(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "nested"

part type GrandChild {
	id String primary
	name String required
}

part type Child {
	id String primary
	name String required
	*-> GRAND_CHILDREN (many) GrandChild
}

type Parent {
	id String primary
	name String required
	*-> CHILDREN (many) Child
}
`, "nested.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build nested schema: %s", result.String())
	}
	return s
}

// testSchemaWithMultipleCompositions creates a schema with multiple composition relations.
func testSchemaWithMultipleCompositions(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "multi_comp"

part type Note {
	id String primary
	text String required
}

part type Tag {
	name String primary
}

type Document {
	id String primary
	title String required
	*-> NOTES (many) Note
	*-> TAGS (many) Tag
}
`, "multi_comp.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build multi composition schema: %s", result.String())
	}
	return s
}

// testSchemaWithCompositeKey creates a schema with composite primary key.
func testSchemaWithCompositeKey(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "composite"

type Record {
	region String primary
	id String primary
	value String required
}
`, "composite.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build composite key schema: %s", result.String())
	}
	return s
}

// testSchemaWithMutualAssociations creates TypeA <-> TypeB mutual associations (A→B→A cycle).
func testSchemaWithMutualAssociations(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "mutual"

type TypeA {
	id String primary
	name String required
	--> REF_B (_:one) TypeB
}

type TypeB {
	id String primary
	name String required
	--> REF_A (_:one) TypeA
}
`, "mutual.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build mutual associations schema: %s", result.String())
	}
	return s
}

// testSchemaWithCircularChain creates A → B → C → A circular chain.
func testSchemaWithCircularChain(t *testing.T) schema.Schema {
	t.Helper()

	s, result := schema.LoadString(t.Context(), `schema "circular"

type TypeA {
	id String primary
	name String required
	--> REF_B (_:one) TypeB
}

type TypeB {
	id String primary
	name String required
	--> REF_C (_:one) TypeC
}

type TypeC {
	id String primary
	name String required
	--> REF_A (_:one) TypeA
}
`, "circular.yammm")

	if result.HasErrors() {
		t.Fatalf("Failed to build circular chain schema: %s", result.String())
	}
	return s
}

// Instance Creation Helpers
//
// Thin wrappers over [instancetest.Valid] adding the validator's input for a
// key, an edge and a child, which the graph suites repeat.

// tagID resolves a tag form — bare or alias-qualified — to the identity it
// renders, so a test written in names reads the same now that Snapshot's
// accessors take identities.
func tagID(t *testing.T, s schema.Schema, tag string) schema.TypeID {
	t.Helper()
	alias, name, qualified := strings.Cut(tag, ".")
	if !qualified {
		return mustTypeID(t, s, tag)
	}
	imp, ok := s.ImportByAlias(alias)
	if !ok || imp.Schema().IsZero() {
		t.Fatalf("import alias %q not found", alias)
	}
	typ, ok := imp.Schema().Type(name)
	if !ok {
		t.Fatalf("type %q not found in schema imported as %q", name, alias)
	}
	return typ.ID()
}

// mustTypeID resolves a schema type's ID, failing the test if absent.
func mustTypeID(t *testing.T, s schema.Schema, typeName string) schema.TypeID {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	return typ.ID()
}

// mustValidInstance validates props, holding the key pk states, as one typeName
// instance over s.
func mustValidInstance(t *testing.T, s schema.Schema, typeName string, pk []any, props map[string]any) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, typeName, keyedProps(t, s, typeName, pk, props))
}

// keyedProps returns props holding each primary-key property the key states,
// where props does not state it. A key property the fixture states is kept as
// written.
func keyedProps(t *testing.T, s schema.Schema, typeName string, pk []any, props map[string]any) map[string]any {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	out := make(map[string]any, len(props)+len(pk))
	maps.Copy(out, props)
	i := 0
	for p := range typ.PrimaryKeys() {
		if _, stated := out[p.Name()]; !stated && i < len(pk) {
			out[p.Name()] = pk[i]
		}
		i++
	}
	return out
}

// targetObject is one association target as the validator reads it: the
// target's key under its _target_ fields, beside the edge properties.
func targetObject(t *testing.T, s schema.Schema, rel schema.Relation, key []any, edgeProps map[string]any) map[string]any {
	t.Helper()
	target, ok := s.TypeByID(rel.TargetID())
	if !ok {
		t.Fatalf("target %s of %s not in the schema", rel.TargetID(), rel.Name())
	}
	obj := make(map[string]any, len(key)+len(edgeProps))
	maps.Copy(obj, edgeProps)
	i := 0
	for pk := range target.PrimaryKeys() {
		if i >= len(key) {
			t.Fatalf("key %v of %s has too few components", key, rel.Name())
		}
		obj["_target_"+pk.Name()] = key[i]
		i++
	}
	return obj
}

// withTargets returns data holding typeName's association relation with one
// target per key: an object for a (one) association, an array for a many one.
func withTargets(t *testing.T, s schema.Schema, typeName string, data map[string]any, relation string, edgeProps map[string]any, keys ...[]any) map[string]any {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	rel, ok := typ.Relation(relation)
	if !ok {
		t.Fatalf("relation %q not found on %q", relation, typeName)
	}
	out := maps.Clone(data)
	if out == nil {
		out = map[string]any{}
	}
	if !rel.IsMany() {
		if len(keys) != 1 {
			t.Fatalf("(one) association %q given %d targets", relation, len(keys))
		}
		out[rel.FieldName()] = targetObject(t, s, rel, keys[0], edgeProps)
		return out
	}
	targets := make([]any, len(keys))
	for i, key := range keys {
		targets[i] = targetObject(t, s, rel, key, edgeProps)
	}
	out[rel.FieldName()] = targets
	return out
}

// mustValidInstanceWithEdge validates an instance holding one target per key
// under relationName.
func mustValidInstanceWithEdge(
	t *testing.T,
	s schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
	targetKeys [][]any,
) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, typeName,
		withTargets(t, s, typeName, keyedProps(t, s, typeName, pk, props), relationName, nil, targetKeys...))
}

// mustValidInstanceWithEdgeProps validates an instance holding one target with
// edge properties under relationName.
func mustValidInstanceWithEdgeProps(
	t *testing.T,
	s schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
	targetKey []any,
	edgeProps map[string]any,
) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, typeName,
		withTargets(t, s, typeName, keyedProps(t, s, typeName, pk, props), relationName, edgeProps, targetKey))
}

// mustValidInstanceWithEmptyEdge validates an instance holding an empty array
// under a many association.
func mustValidInstanceWithEmptyEdge(
	t *testing.T,
	s schema.Schema,
	typeName string,
	pk []any,
	props map[string]any,
	relationName string,
) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, typeName,
		withTargets(t, s, typeName, keyedProps(t, s, typeName, pk, props), relationName, nil))
}

// partData is one part instance as the validator reads it inside its parent.
func partData(t *testing.T, s schema.Schema, typeName string, pk []any, props map[string]any) map[string]any {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	if !typ.IsPart() {
		t.Fatalf("Type %q is not a part type", typeName)
	}
	return keyedProps(t, s, typeName, pk, props)
}

// parentWith validates an instance holding children, each a [partData] or a raw
// child object, under the composition relation.
func parentWith(t *testing.T, s schema.Schema, typeName string, pk []any, props map[string]any, relation string, children ...map[string]any) instance.ValidInstance {
	t.Helper()
	return instancetest.Valid(t, s, typeName, composedData(t, s, typeName, keyedProps(t, s, typeName, pk, props), relation, children...))
}

// composedData returns data holding children under typeName's composition
// relation, in the array a composition always arrives as.
func composedData(t *testing.T, s schema.Schema, typeName string, data map[string]any, relation string, children ...map[string]any) map[string]any {
	t.Helper()
	typ, ok := s.Type(typeName)
	if !ok {
		t.Fatalf("Type %q not found in schema", typeName)
	}
	rel, ok := typ.Relation(relation)
	if !ok {
		t.Fatalf("relation %q not found on %q", relation, typeName)
	}
	out := maps.Clone(data)
	elems := make([]any, len(children))
	for i, child := range children {
		elems[i] = child
	}
	out[rel.FieldName()] = elems
	return out
}

// loadSchemaAt loads source under sourceName. Each call is its own load, so two
// calls give two schemas a graph tells apart, whatever their text.
func loadSchemaAt(t *testing.T, source, sourceName string) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), source, sourceName)
	if res.HasErrors() {
		t.Fatalf("load %s: %s", sourceName, res)
	}
	return s
}

// Multi-Schema Helpers

// loadClosure loads the entry source under a synthetic root and returns each
// member of its import closure by declared schema name.
func loadClosure(t *testing.T, entry string, sources map[string]string) map[string]schema.Schema {
	t.Helper()
	files := make(map[string][]byte, len(sources))
	for key, text := range sources {
		files[key] = []byte(text)
	}
	s, result := schema.LoadSourcesWithEntry(t.Context(), files, entry, "", schema.WithSyntheticRoot("test://graph"))
	if result.HasErrors() {
		t.Fatalf("load %s: %s", entry, result.String())
	}
	members := make(map[string]schema.Schema)
	for member := range s.Closure() {
		members[member.Name()] = member
	}
	return members
}

// testMultiSchemaSetup creates two schemas with imports for cross-schema testing.
// Returns (main schema, imported schema).
func testMultiSchemaSetup(t *testing.T) (schema.Schema, schema.Schema) {
	t.Helper()
	members := loadClosure(t, "main.yammm", map[string]string{
		"common.yammm": `schema "common"

type Entity {
	id String primary
	name String required
}
`,
		"main.yammm": `schema "main"

import "common" as c

type User {
	id String primary
	username String required
	--> ENTITY (_:one) c.Entity
}
`,
	})
	return members["main"], members["common"]
}

// testTripleSchemaSetup creates three schemas for transitive import testing.
// Schema A imports B, B imports C. A cannot directly access C types.
func testTripleSchemaSetup(t *testing.T) (schemaA, schemaB, schemaC schema.Schema) {
	t.Helper()
	members := loadClosure(t, "a.yammm", map[string]string{
		"c.yammm": `schema "schema_c"

type BaseType {
	id String primary
	value String required
}
`,
		"b.yammm": `schema "schema_b"

import "c" as c

type MiddleType {
	id String primary
	name String required
	--> BASE (_:one) c.BaseType
}
`,
		"a.yammm": `schema "schema_a"

import "b" as b

type TopType {
	id String primary
	label String required
	--> MIDDLE (_:one) b.MiddleType
}
`,
	})
	return members["schema_a"], members["schema_b"], members["schema_c"]
}

// Assertion Helpers

// assertInstanceCount verifies the number of instances of a type in the result.
func assertInstanceCount(t *testing.T, result graph.Snapshot, typeID schema.TypeID, expected int) bool {
	t.Helper()

	instances := slices.Collect(result.InstancesOf(typeID))
	if len(instances) != expected {
		t.Errorf("Expected %d instances of %s, got %d", expected, typeID, len(instances))
		return false
	}
	return true
}

// assertEdgeCount verifies the number of edges in the result.
func assertEdgeCount(t *testing.T, result graph.Snapshot, expected int) {
	t.Helper()

	edges := slices.Collect(result.Edges())
	if len(edges) != expected {
		t.Errorf("Expected %d edges, got %d", expected, len(edges))
	}
}

// assertUnresolvedCount verifies the number of unresolved edges in the result.
func assertUnresolvedCount(t *testing.T, result graph.Snapshot, expected int) {
	t.Helper()

	unresolved := slices.Collect(result.Unresolved())
	if len(unresolved) != expected {
		t.Errorf("Expected %d unresolved edges, got %d", expected, len(unresolved))
	}
}

// assertComposedCount verifies the number of composed children for a relation.
func assertComposedCount(t *testing.T, inst graph.Instance, relationName string, expected int) {
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

// mustImport imports snap into a new graph bound to s and fails the test when
// the import is refused.
func mustImport(tb testing.TB, s schema.Schema, snap graph.Snapshot) *graph.Graph {
	tb.Helper()
	g, res := graph.NewFromSnapshot(s, snap)
	if res.HasErrors() {
		tb.Fatalf("NewFromSnapshot: %s", res)
	}
	return g
}

// mustSeed seeds a new assembler bound to s from snap and fails the test when
// the seed is refused.
func mustSeed(tb testing.TB, ctx context.Context, s schema.Schema, snap graph.Snapshot) *graph.BatchAssembler {
	tb.Helper()
	ba, res := graph.NewBatchAssemblerFromSnapshot(ctx, s, snap)
	if res.HasErrors() {
		tb.Fatalf("NewBatchAssemblerFromSnapshot: %s", res)
	}
	return ba
}

// rebuild constructs a snapshot from parts, as the snapshot package does from a
// document it read.
func rebuild(s schema.Schema, parts snapparts.Parts) graph.Snapshot {
	return snapparts.Rebuild[graph.Snapshot](s, parts)
}
