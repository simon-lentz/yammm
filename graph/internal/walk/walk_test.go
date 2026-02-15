package walk

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
)

// testSchema creates a simple schema for testing.
func testSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithProperty("age", schema.IntegerConstraint{}).
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

// countingVisitor counts visitor calls.
type countingVisitor struct {
	BaseVisitor
	enterInstance   int
	exitInstance    int
	visitProperty   int
	visitEdge       int
	enterCompose    int
	exitCompose     int
	instanceNames   []string
	propertyNames   []string
	compositionPath []string
}

func (v *countingVisitor) EnterInstance(inst *graph.Instance) error {
	v.enterInstance++
	v.instanceNames = append(v.instanceNames, inst.TypeName()+":"+inst.PrimaryKey().String())
	return nil
}

func (v *countingVisitor) ExitInstance(inst *graph.Instance) error {
	v.exitInstance++
	return nil
}

func (v *countingVisitor) VisitProperty(inst *graph.Instance, name string, value immutable.Value) error {
	v.visitProperty++
	v.propertyNames = append(v.propertyNames, name)
	return nil
}

func (v *countingVisitor) VisitEdge(edge *graph.Edge) error {
	v.visitEdge++
	return nil
}

func (v *countingVisitor) EnterComposition(inst *graph.Instance, relationName string) error {
	v.enterCompose++
	v.compositionPath = append(v.compositionPath, "enter:"+relationName)
	return nil
}

func (v *countingVisitor) ExitComposition(inst *graph.Instance, relationName string) error {
	v.exitCompose++
	v.compositionPath = append(v.compositionPath, "exit:"+relationName)
	return nil
}

func TestWalk_NilResult(t *testing.T) {
	visitor := &countingVisitor{}
	err := Walk(context.Background(), nil, visitor)
	if err != nil {
		t.Errorf("Walk(nil) error: %v", err)
	}
	if visitor.enterInstance != 0 {
		t.Error("Should not visit any instances for nil result")
	}
}

func TestWalk_NilVisitor_ReturnsError(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	result := g.Snapshot()

	err := Walk(context.Background(), result, nil)
	if !errors.Is(err, ErrNilVisitor) {
		t.Errorf("Walk(nil visitor) error = %v, want ErrNilVisitor", err)
	}
}

func TestWalkInstance_NilVisitor_ReturnsError(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	instances := snap.InstancesOf("Person")
	if len(instances) == 0 {
		t.Fatal("No instances found")
	}

	err := WalkInstance(ctx, instances[0], nil)
	if !errors.Is(err, ErrNilVisitor) {
		t.Errorf("WalkInstance(nil visitor) error = %v, want ErrNilVisitor", err)
	}
}

func TestWalk_EmptyResult(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	result := g.Snapshot()

	visitor := &countingVisitor{}
	err := Walk(context.Background(), result, visitor)
	if err != nil {
		t.Errorf("Walk error: %v", err)
	}
	if visitor.enterInstance != 0 {
		t.Error("Should not visit any instances for empty graph")
	}
}

func TestWalk_SingleInstance(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{
			"name": "Alice",
			"age":  30,
		}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	if visitor.enterInstance != 1 {
		t.Errorf("enterInstance = %d, want 1", visitor.enterInstance)
	}
	if visitor.exitInstance != 1 {
		t.Errorf("exitInstance = %d, want 1", visitor.exitInstance)
	}
	if visitor.visitProperty != 2 {
		t.Errorf("visitProperty = %d, want 2", visitor.visitProperty)
	}
}

func TestWalk_MultipleInstances(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	companyType, _ := s.Type("Company")

	// Add persons
	for _, name := range []string{"charlie", "alice", "bob"} {
		inst := instance.NewValidInstance(
			"Person",
			personType.ID(),
			immutable.WrapKey([]any{name}),
			immutable.WrapProperties(map[string]any{"name": name, "age": 25}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, inst); err != nil {
			t.Fatalf("Add Person error: %v", err)
		}
	}

	// Add companies
	for _, name := range []string{"acme", "zeta"} {
		inst := instance.NewValidInstance(
			"Company",
			companyType.ID(),
			immutable.WrapKey([]any{name}),
			immutable.WrapProperties(map[string]any{"name": name}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, inst); err != nil {
			t.Fatalf("Add Company error: %v", err)
		}
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	if visitor.enterInstance != 5 {
		t.Errorf("enterInstance = %d, want 5", visitor.enterInstance)
	}

	// Verify deterministic order (Company before Person, then by PK)
	expected := []string{
		"Company:[\"acme\"]",
		"Company:[\"zeta\"]",
		"Person:[\"alice\"]",
		"Person:[\"bob\"]",
		"Person:[\"charlie\"]",
	}

	if len(visitor.instanceNames) != len(expected) {
		t.Fatalf("instanceNames length = %d, want %d", len(visitor.instanceNames), len(expected))
	}

	for i, name := range visitor.instanceNames {
		if name != expected[i] {
			t.Errorf("instanceNames[%d] = %q, want %q", i, name, expected[i])
		}
	}
}

func TestWalk_PropertyOrder(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"test"}),
		immutable.WrapProperties(map[string]any{
			"name": "Test",
			"age":  42,
		}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Properties should be in sorted order: "age" before "name"
	if len(visitor.propertyNames) != 2 {
		t.Fatalf("propertyNames length = %d, want 2", len(visitor.propertyNames))
	}
	if visitor.propertyNames[0] != "age" {
		t.Errorf("propertyNames[0] = %q, want \"age\"", visitor.propertyNames[0])
	}
	if visitor.propertyNames[1] != "name" {
		t.Errorf("propertyNames[1] = %q, want \"name\"", visitor.propertyNames[1])
	}
}

func TestWalk_ContextCancellation(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")

	// Add multiple instances
	for i := range 10 {
		name := string(rune('a' + i))
		inst := instance.NewValidInstance(
			"Person",
			personType.ID(),
			immutable.WrapKey([]any{name}),
			immutable.WrapProperties(map[string]any{"name": name, "age": i}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, inst); err != nil {
			t.Fatalf("Add error: %v", err)
		}
	}

	// Cancel context before walk
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result := g.Snapshot()
	visitor := &countingVisitor{}

	err := Walk(cancelCtx, result, visitor)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Walk should return context.Canceled, got %v", err)
	}
}

// errorVisitor returns an error after a certain number of calls.
type errorVisitor struct {
	BaseVisitor
	enterCount int
	errorAfter int
	testErr    error
}

func (v *errorVisitor) EnterInstance(inst *graph.Instance) error {
	v.enterCount++
	if v.enterCount > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_VisitorError(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")

	// Add multiple instances
	for i := range 5 {
		name := string(rune('a' + i))
		inst := instance.NewValidInstance(
			"Person",
			personType.ID(),
			immutable.WrapKey([]any{name}),
			immutable.WrapProperties(map[string]any{"name": name, "age": i}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, inst); err != nil {
			t.Fatalf("Add error: %v", err)
		}
	}

	testErr := errors.New("test error")
	result := g.Snapshot()
	visitor := &errorVisitor{
		errorAfter: 2,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return test error, got %v", err)
	}

	// Should have stopped after 3 calls (2 successful + 1 that returned error)
	if visitor.enterCount != 3 {
		t.Errorf("enterCount = %d, want 3", visitor.enterCount)
	}
}

func TestWalkInstance_NilInstance(t *testing.T) {
	visitor := &countingVisitor{}
	err := WalkInstance(context.Background(), nil, visitor)
	if err != nil {
		t.Errorf("WalkInstance(nil) error: %v", err)
	}
	if visitor.enterInstance != 0 {
		t.Error("Should not visit nil instance")
	}
}

func TestWalkInstance_Single(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice", "age": 30}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	instances := result.InstancesOf("Person")
	if len(instances) != 1 {
		t.Fatalf("Expected 1 instance, got %d", len(instances))
	}

	visitor := &countingVisitor{}
	if err := WalkInstance(ctx, instances[0], visitor); err != nil {
		t.Errorf("WalkInstance error: %v", err)
	}

	if visitor.enterInstance != 1 {
		t.Errorf("enterInstance = %d, want 1", visitor.enterInstance)
	}
	if visitor.exitInstance != 1 {
		t.Errorf("exitInstance = %d, want 1", visitor.exitInstance)
	}
}

func TestBaseVisitor_AllMethodsNoop(t *testing.T) {
	var v BaseVisitor

	// All methods should return nil
	if err := v.EnterInstance(nil); err != nil {
		t.Errorf("EnterInstance returned non-nil: %v", err)
	}
	if err := v.ExitInstance(nil); err != nil {
		t.Errorf("ExitInstance returned non-nil: %v", err)
	}
	if err := v.VisitProperty(nil, "", immutable.Value{}); err != nil {
		t.Errorf("VisitProperty returned non-nil: %v", err)
	}
	if err := v.VisitEdge(nil); err != nil {
		t.Errorf("VisitEdge returned non-nil: %v", err)
	}
	if err := v.EnterComposition(nil, ""); err != nil {
		t.Errorf("EnterComposition returned non-nil: %v", err)
	}
	if err := v.ExitComposition(nil, ""); err != nil {
		t.Errorf("ExitComposition returned non-nil: %v", err)
	}
}

// partialVisitor only implements some methods using embedding.
type partialVisitor struct {
	BaseVisitor
	count int
}

func (v *partialVisitor) EnterInstance(inst *graph.Instance) error {
	v.count++
	return nil
}

func TestBaseVisitor_Embedding(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"test"}),
		immutable.WrapProperties(map[string]any{"name": "Test", "age": 1}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	visitor := &partialVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Only EnterInstance is implemented, but walk should complete without error
	if visitor.count != 1 {
		t.Errorf("count = %d, want 1", visitor.count)
	}
}

// testAssociationSchema creates a schema with Person -> Company association.
func testAssociationSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("association").
		WithSourceID(location.MustNewSourceID("test://association.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), false, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build association schema: %s", result.String())
	}
	return s
}

// testMultiRelationSchema creates a schema with multiple relations for edge sorting tests.
func testMultiRelationSchema(t *testing.T) *schema.Schema {
	t.Helper()

	s, result := build.NewBuilder().
		WithName("multi_relation").
		WithSourceID(location.MustNewSourceID("test://multi_relation.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Department").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employer", schema.LocalTypeRef("Company", location.Span{}), true, false).
		WithRelation("department", schema.LocalTypeRef("Department", location.Span{}), true, false).
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build multi-relation schema: %s", result.String())
	}
	return s
}

// makeEdges creates edge data for an instance.
func makeEdges(relationName string, targetKeys ...[]any) map[string]*instance.ValidEdgeData {
	targets := make([]instance.ValidEdgeTarget, len(targetKeys))
	for i, tk := range targetKeys {
		targets[i] = instance.NewValidEdgeTarget(
			immutable.WrapKey(tk),
			immutable.Properties{},
		)
	}
	return map[string]*instance.ValidEdgeData{
		relationName: instance.NewValidEdgeData(targets),
	}
}

// makeMultiEdges creates edge data for multiple relations.
func makeMultiEdges(relations map[string][][]any) map[string]*instance.ValidEdgeData {
	edges := make(map[string]*instance.ValidEdgeData)
	for relationName, targetKeys := range relations {
		targets := make([]instance.ValidEdgeTarget, len(targetKeys))
		for i, tk := range targetKeys {
			targets[i] = instance.NewValidEdgeTarget(
				immutable.WrapKey(tk),
				immutable.Properties{},
			)
		}
		edges[relationName] = instance.NewValidEdgeData(targets)
	}
	return edges
}

// TestWalk_WithEdges tests walking a graph with edges.
func TestWalk_WithEdges(t *testing.T) {
	s := testAssociationSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	companyType, _ := s.Type("Company")
	personType, _ := s.Type("Person")

	// Add company
	company := instance.NewValidInstance(
		"Company",
		companyType.ID(),
		immutable.WrapKey([]any{"acme"}),
		immutable.WrapProperties(map[string]any{"name": "Acme Corp"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add person with edge to company
	person := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		makeEdges("employer", []any{"acme"}),
		nil, nil,
	)
	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Should visit both instances
	if visitor.enterInstance != 2 {
		t.Errorf("enterInstance = %d, want 2", visitor.enterInstance)
	}

	// Should visit edges
	if visitor.visitEdge != 1 {
		t.Errorf("visitEdge = %d, want 1", visitor.visitEdge)
	}
}

// TestWalk_EdgeSorting tests that edges are visited in deterministic order.
func TestWalk_EdgeSorting(t *testing.T) {
	s := testMultiRelationSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	companyType, _ := s.Type("Company")
	deptType, _ := s.Type("Department")
	personType, _ := s.Type("Person")

	// Add targets (in reverse order to test sorting)
	dept := instance.NewValidInstance(
		"Department",
		deptType.ID(),
		immutable.WrapKey([]any{"eng"}),
		immutable.WrapProperties(map[string]any{"name": "Engineering"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, dept); err != nil {
		t.Fatalf("Add dept error: %v", err)
	}

	company := instance.NewValidInstance(
		"Company",
		companyType.ID(),
		immutable.WrapKey([]any{"acme"}),
		immutable.WrapProperties(map[string]any{"name": "Acme Corp"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add person with edges to both
	person := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		makeMultiEdges(map[string][][]any{
			"employer":   {{"acme"}},
			"department": {{"eng"}},
		}),
		nil, nil,
	)
	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	result := g.Snapshot()

	// Track edge visit order
	var edgeRelations []string
	customVisitor := &edgeOrderVisitor{relations: &edgeRelations}

	if err := Walk(ctx, result, customVisitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Edges should be sorted by relation name (department < employer)
	if len(edgeRelations) != 2 {
		t.Fatalf("Expected 2 edges, got %d", len(edgeRelations))
	}
	if edgeRelations[0] != "department" {
		t.Errorf("First edge should be 'department', got %q", edgeRelations[0])
	}
	if edgeRelations[1] != "employer" {
		t.Errorf("Second edge should be 'employer', got %q", edgeRelations[1])
	}
}

// edgeOrderVisitor tracks the order of edge visits.
type edgeOrderVisitor struct {
	BaseVisitor
	relations *[]string
}

func (v *edgeOrderVisitor) VisitEdge(edge *graph.Edge) error {
	*v.relations = append(*v.relations, edge.Relation())
	return nil
}

// TestWalk_WithCompositions tests walking a graph with compositions.
// Uses testSchemaWithComposition from walk_logging_test.go.
func TestWalk_WithCompositions(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add parent first (without children)
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add children via AddComposed
	for _, cID := range []string{"c1", "c2"} {
		child := instance.NewValidInstance(
			"Child",
			childType.ID(),
			immutable.WrapKey([]any{cID}),
			immutable.WrapProperties(map[string]any{"name": "Child " + cID}),
			nil, nil, nil,
		)
		if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); err != nil {
			t.Fatalf("AddComposed %s error: %v", cID, err)
		}
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Should visit parent + 2 children = 3 instances
	if visitor.enterInstance != 3 {
		t.Errorf("enterInstance = %d, want 3", visitor.enterInstance)
	}
	if visitor.exitInstance != 3 {
		t.Errorf("exitInstance = %d, want 3", visitor.exitInstance)
	}

	// Should enter and exit composition once
	if visitor.enterCompose != 1 {
		t.Errorf("enterCompose = %d, want 1", visitor.enterCompose)
	}
	if visitor.exitCompose != 1 {
		t.Errorf("exitCompose = %d, want 1", visitor.exitCompose)
	}

	// Verify composition path
	expectedPath := []string{"enter:children", "exit:children"}
	if len(visitor.compositionPath) != len(expectedPath) {
		t.Fatalf("compositionPath length = %d, want %d", len(visitor.compositionPath), len(expectedPath))
	}
	for i, p := range visitor.compositionPath {
		if p != expectedPath[i] {
			t.Errorf("compositionPath[%d] = %q, want %q", i, p, expectedPath[i])
		}
	}
}

// Visitor error tests for different methods

// exitErrorVisitor returns an error on ExitInstance.
type exitErrorVisitor struct {
	BaseVisitor
	exitCount  int
	errorAfter int
	testErr    error
}

func (v *exitErrorVisitor) ExitInstance(inst *graph.Instance) error {
	v.exitCount++
	if v.exitCount > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_ExitInstanceError(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice", "age": 30}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	testErr := errors.New("exit error")
	result := g.Snapshot()
	visitor := &exitErrorVisitor{
		errorAfter: 0,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return exit error, got %v", err)
	}
}

// propErrorVisitor returns an error on VisitProperty.
type propErrorVisitor struct {
	BaseVisitor
	propCount  int
	errorAfter int
	testErr    error
}

func (v *propErrorVisitor) VisitProperty(inst *graph.Instance, name string, value immutable.Value) error {
	v.propCount++
	if v.propCount > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_VisitPropertyError(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice", "age": 30}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	testErr := errors.New("property error")
	result := g.Snapshot()
	visitor := &propErrorVisitor{
		errorAfter: 0,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return property error, got %v", err)
	}
}

// edgeErrorVisitor returns an error on VisitEdge.
type edgeErrorVisitor struct {
	BaseVisitor
	edgeCount  int
	errorAfter int
	testErr    error
}

func (v *edgeErrorVisitor) VisitEdge(edge *graph.Edge) error {
	v.edgeCount++
	if v.edgeCount > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_VisitEdgeError(t *testing.T) {
	s := testAssociationSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	companyType, _ := s.Type("Company")
	personType, _ := s.Type("Person")

	// Add company
	company := instance.NewValidInstance(
		"Company",
		companyType.ID(),
		immutable.WrapKey([]any{"acme"}),
		immutable.WrapProperties(map[string]any{"name": "Acme Corp"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add person with edge
	person := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		makeEdges("employer", []any{"acme"}),
		nil, nil,
	)
	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	testErr := errors.New("edge error")
	result := g.Snapshot()
	visitor := &edgeErrorVisitor{
		errorAfter: 0,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return edge error, got %v", err)
	}
}

// enterComposeErrorVisitor returns an error on EnterComposition.
type enterComposeErrorVisitor struct {
	BaseVisitor
	count      int
	errorAfter int
	testErr    error
}

func (v *enterComposeErrorVisitor) EnterComposition(inst *graph.Instance, relationName string) error {
	v.count++
	if v.count > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_EnterCompositionError(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add parent first
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add child via AddComposed
	child := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, nil, nil,
	)
	if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	testErr := errors.New("enter composition error")
	result := g.Snapshot()
	visitor := &enterComposeErrorVisitor{
		errorAfter: 0,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return enter composition error, got %v", err)
	}
}

// exitComposeErrorVisitor returns an error on ExitComposition.
type exitComposeErrorVisitor struct {
	BaseVisitor
	count      int
	errorAfter int
	testErr    error
}

func (v *exitComposeErrorVisitor) ExitComposition(inst *graph.Instance, relationName string) error {
	v.count++
	if v.count > v.errorAfter {
		return v.testErr
	}
	return nil
}

func TestWalk_ExitCompositionError(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add parent first
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add child via AddComposed
	child := instance.NewValidInstance(
		"Child",
		childType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Child 1"}),
		nil, nil, nil,
	)
	if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); err != nil {
		t.Fatalf("AddComposed error: %v", err)
	}

	testErr := errors.New("exit composition error")
	result := g.Snapshot()
	visitor := &exitComposeErrorVisitor{
		errorAfter: 0,
		testErr:    testErr,
	}

	err := Walk(ctx, result, visitor)
	if !errors.Is(err, testErr) {
		t.Errorf("Walk should return exit composition error, got %v", err)
	}
}

// TestWalk_ContextCancellation_DuringCompositions tests context cancellation during composition walk.
func TestWalk_ContextCancellation_DuringCompositions(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add multiple parents with multiple children each
	for i := range 3 {
		pID := string(rune('a' + i))
		// Add parent first
		parent := instance.NewValidInstance(
			"Parent",
			parentType.ID(),
			immutable.WrapKey([]any{pID}),
			immutable.WrapProperties(map[string]any{"name": "Parent " + pID}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, parent); err != nil {
			t.Fatalf("Add parent error: %v", err)
		}

		// Add children via AddComposed
		for j := range 5 {
			cID := pID + string(rune('0'+j))
			child := instance.NewValidInstance(
				"Child",
				childType.ID(),
				immutable.WrapKey([]any{cID}),
				immutable.WrapProperties(map[string]any{"name": "Child " + cID}),
				nil, nil, nil,
			)
			if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey(pID), "children", child); err != nil {
				t.Fatalf("AddComposed %s error: %v", cID, err)
			}
		}
	}

	// Create a context that's already cancelled
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()

	result := g.Snapshot()
	visitor := &countingVisitor{}

	err := Walk(cancelCtx, result, visitor)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Walk should return context.Canceled, got %v", err)
	}
}

// TestWalkInstance_WithComposition tests WalkInstance with compositions.
func TestWalkInstance_WithComposition(t *testing.T) {
	s := testSchemaWithComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add parent first
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add children via AddComposed
	for _, cID := range []string{"c1", "c2"} {
		child := instance.NewValidInstance(
			"Child",
			childType.ID(),
			immutable.WrapKey([]any{cID}),
			immutable.WrapProperties(map[string]any{"name": "Child " + cID}),
			nil, nil, nil,
		)
		if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); err != nil {
			t.Fatalf("AddComposed %s error: %v", cID, err)
		}
	}

	result := g.Snapshot()
	parents := result.InstancesOf("Parent")
	if len(parents) != 1 {
		t.Fatalf("Expected 1 parent, got %d", len(parents))
	}

	visitor := &countingVisitor{}
	if err := WalkInstance(ctx, parents[0], visitor); err != nil {
		t.Errorf("WalkInstance error: %v", err)
	}

	// Should visit parent + 2 children = 3 instances
	if visitor.enterInstance != 3 {
		t.Errorf("enterInstance = %d, want 3", visitor.enterInstance)
	}

	// Should enter/exit composition once
	if visitor.enterCompose != 1 {
		t.Errorf("enterCompose = %d, want 1", visitor.enterCompose)
	}
	if visitor.exitCompose != 1 {
		t.Errorf("exitCompose = %d, want 1", visitor.exitCompose)
	}
}

// TestWalk_EdgeSorting_SameRelationDifferentTargets tests edge sorting with same relation but different targets.
func TestWalk_EdgeSorting_SameRelationDifferentTargets(t *testing.T) {
	// Create a schema where one type can have multiple edges to same relation
	s, result := build.NewBuilder().
		WithName("multi_edge").
		WithSourceID(location.MustNewSourceID("test://multi_edge.yammm")).
		AddType("Company").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done().
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithRelation("employers", schema.LocalTypeRef("Company", location.Span{}), false, true). // many relation
		Done().
		Build()

	if result.HasErrors() {
		t.Fatalf("Failed to build schema: %s", result.String())
	}

	g := graph.New(s)
	ctx := context.Background()

	companyType, _ := s.Type("Company")
	personType, _ := s.Type("Person")

	// Add multiple companies
	for _, id := range []string{"zeta", "acme", "beta"} {
		company := instance.NewValidInstance(
			"Company",
			companyType.ID(),
			immutable.WrapKey([]any{id}),
			immutable.WrapProperties(map[string]any{"name": id}),
			nil, nil, nil,
		)
		if _, err := g.Add(ctx, company); err != nil {
			t.Fatalf("Add company %s error: %v", id, err)
		}
	}

	// Add person with multiple edges to the same relation (employers)
	person := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		makeEdges("employers", []any{"zeta"}, []any{"acme"}, []any{"beta"}),
		nil, nil,
	)
	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	result2 := g.Snapshot()

	// Track edge visit order
	var edgeTargets []string
	customVisitor := &edgeTargetOrderVisitor{targets: &edgeTargets}

	if err := Walk(ctx, result2, customVisitor); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	// Edges should be sorted by target PK within same relation (acme < beta < zeta)
	if len(edgeTargets) != 3 {
		t.Fatalf("Expected 3 edges, got %d", len(edgeTargets))
	}
	expectedOrder := []string{"acme", "beta", "zeta"}
	for i, expected := range expectedOrder {
		if edgeTargets[i] != expected {
			t.Errorf("Edge target[%d] should be %q, got %q", i, expected, edgeTargets[i])
		}
	}
}

// edgeTargetOrderVisitor tracks the order of edge target visits.
type edgeTargetOrderVisitor struct {
	BaseVisitor
	targets *[]string
}

func (v *edgeTargetOrderVisitor) VisitEdge(edge *graph.Edge) error {
	if edge.Target() != nil {
		// Extract the first key component as a string via SingleString helper
		pk := edge.Target().PrimaryKey()
		if s, ok := pk.SingleString(); ok {
			*v.targets = append(*v.targets, s)
		}
	}
	return nil
}

// TestWalk_WithOptions tests walk with WithLogger option.
func TestWalk_WithOptions(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice", "age": 30}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	visitor := &countingVisitor{}

	// Walk with nil logger option (should handle gracefully)
	if err := Walk(ctx, result, visitor, WithLogger(nil)); err != nil {
		t.Errorf("Walk error: %v", err)
	}

	if visitor.enterInstance != 1 {
		t.Errorf("enterInstance = %d, want 1", visitor.enterInstance)
	}
}

// TestWalkInstance_WithOptions tests WalkInstance with options.
func TestWalkInstance_WithOptions(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice", "age": 30}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	instances := result.InstancesOf("Person")

	visitor := &countingVisitor{}
	if err := WalkInstance(ctx, instances[0], visitor, WithLogger(nil)); err != nil {
		t.Errorf("WalkInstance error: %v", err)
	}

	if visitor.enterInstance != 1 {
		t.Errorf("enterInstance = %d, want 1", visitor.enterInstance)
	}
}

// TestNilContext_Panics verifies Walk and WalkInstance panic with helpful
// messages when passed a nil context.
func TestNilContext_Panics(t *testing.T) {
	s := testSchema(t)
	g := graph.New(s)
	ctx := context.Background()

	personType, _ := s.Type("Person")
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{
			"name": "Alice",
			"age":  30,
		}),
		nil, nil, nil,
	)

	if _, err := g.Add(ctx, inst); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result := g.Snapshot()
	instances := result.InstancesOf("Person")

	tests := []struct {
		name    string
		fn      func()
		wantMsg string
	}{
		{
			name:    "Walk",
			fn:      func() { _ = Walk(nil, result, nil) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "walk.Walk: nil context",
		},
		{
			name:    "WalkInstance",
			fn:      func() { _ = WalkInstance(nil, instances[0], nil) }, //nolint:staticcheck // testing nil context panic
			wantMsg: "walk.WalkInstance: nil context",
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

// orderTrackingVisitor tracks the order of Child instance visits.
type orderTrackingVisitor struct {
	BaseVisitor
	visitOrder *[]string
}

func (v *orderTrackingVisitor) EnterInstance(inst *graph.Instance) error {
	if inst.TypeName() == "Child" {
		*v.visitOrder = append(*v.visitOrder, inst.PrimaryKey().String())
	}
	return nil
}

// TestWalk_CompositionChildOrdering verifies that composed children are
// visited in primary key order, not insertion order.
func TestWalk_CompositionChildOrdering(t *testing.T) {
	s := testSchemaWithComposition(t) // Uses existing helper from walk_logging_test.go
	g := graph.New(s)
	ctx := context.Background()

	parentType, _ := s.Type("Parent")
	childType, _ := s.Type("Child")

	// Add parent first
	parent := instance.NewValidInstance(
		"Parent",
		parentType.ID(),
		immutable.WrapKey([]any{"p1"}),
		immutable.WrapProperties(map[string]any{"name": "Parent 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, parent); err != nil {
		t.Fatalf("Add parent error: %v", err)
	}

	// Add children in NON-alphabetical order: charlie, alice, bob
	for _, pk := range []string{"charlie", "alice", "bob"} {
		child := instance.NewValidInstance(
			"Child",
			childType.ID(),
			immutable.WrapKey([]any{pk}),
			immutable.WrapProperties(map[string]any{"name": pk}),
			nil, nil, nil,
		)
		if _, err := g.AddComposed(ctx, "Parent", graph.FormatKey("p1"), "children", child); err != nil {
			t.Fatalf("AddComposed error: %v", err)
		}
	}

	result := g.Snapshot()

	// Track order of child visits
	var visitOrder []string
	visitor := &orderTrackingVisitor{
		BaseVisitor: BaseVisitor{},
		visitOrder:  &visitOrder,
	}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	// Expect alphabetical PK order: alice, bob, charlie
	want := []string{`["alice"]`, `["bob"]`, `["charlie"]`}
	if !slices.Equal(visitOrder, want) {
		t.Errorf("child visit order = %v, want %v", visitOrder, want)
	}
}

// TestWalk_CompositionChildOrdering_PKLess verifies that PK-less composed
// children preserve insertion (index) order.
func TestWalk_CompositionChildOrdering_PKLess(t *testing.T) {
	s := testSchemaWithPKLessComposition(t)
	g := graph.New(s)
	ctx := context.Background()

	containerType, _ := s.Type("Container")
	itemType, _ := s.Type("Item")

	// Add container first
	container := instance.NewValidInstance(
		"Container",
		containerType.ID(),
		immutable.WrapKey([]any{"c1"}),
		immutable.WrapProperties(map[string]any{"name": "Container 1"}),
		nil, nil, nil,
	)
	if _, err := g.Add(ctx, container); err != nil {
		t.Fatalf("Add container error: %v", err)
	}

	// Add PK-less items in specific order: third, first, second
	// These should preserve insertion order (not be sorted alphabetically)
	insertOrder := []string{"third", "first", "second"}
	for _, val := range insertOrder {
		item := instance.NewValidInstance(
			"Item",
			itemType.ID(),
			immutable.WrapKey(nil), // No PK
			immutable.WrapProperties(map[string]any{"value": val}),
			nil, nil, nil,
		)
		if _, err := g.AddComposed(ctx, "Container", graph.FormatKey("c1"), "items", item); err != nil {
			t.Fatalf("AddComposed error: %v", err)
		}
	}

	result := g.Snapshot()

	// Track order of item visits by their "value" property
	var visitOrder []string
	visitor := &pkLessOrderTrackingVisitor{
		BaseVisitor: BaseVisitor{},
		visitOrder:  &visitOrder,
		typeName:    "Item",
		propName:    "value",
	}

	if err := Walk(ctx, result, visitor); err != nil {
		t.Fatalf("Walk error: %v", err)
	}

	// Expect insertion order preserved: third, first, second
	// (NOT alphabetical order: first, second, third)
	if !slices.Equal(visitOrder, insertOrder) {
		t.Errorf("child visit order = %v, want insertion order %v", visitOrder, insertOrder)
	}
}

// pkLessOrderTrackingVisitor tracks visit order by property value for PK-less instances.
type pkLessOrderTrackingVisitor struct {
	BaseVisitor
	visitOrder *[]string
	typeName   string
	propName   string
}

func (v *pkLessOrderTrackingVisitor) EnterInstance(inst *graph.Instance) error {
	if inst.TypeName() == v.typeName {
		if val, ok := inst.Properties().Get(v.propName); ok {
			if s, ok := val.Unwrap().(string); ok {
				*v.visitOrder = append(*v.visitOrder, s)
			}
		}
	}
	return nil
}
