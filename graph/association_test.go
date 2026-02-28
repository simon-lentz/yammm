package graph

import (
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

// Forward Reference Tests
//
// These tests verify that forward references (edges to not-yet-added targets)
// are correctly tracked and resolved when the target instance is added.

func TestGraph_ForwardReference_Basic(t *testing.T) {
	// Add source before target, verify edge resolves when target added
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person (source) with edge to Company (target not yet added)
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})

	result, err := g.Add(ctx, person)
	if err != nil {
		t.Fatalf("Add person error: %v", err)
	}
	// Should succeed (forward ref is allowed)
	if !result.OK() {
		t.Errorf("Add person should succeed: %s", result.String())
	}

	// Snapshot should show unresolved edge
	snap := g.Snapshot()
	assertUnresolvedCount(t, snap, 1)
	assertEdgeCount(t, snap, 0)

	// Add Company (target)
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	result, err = g.Add(ctx, company)
	if err != nil {
		t.Fatalf("Add company error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add company should succeed: %s", result.String())
	}

	// Snapshot should now show resolved edge
	snap = g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 1)

	// Verify edge details
	edges := snap.Edges()
	if edges[0].Source().TypeName() != "Person" {
		t.Errorf("Edge source should be Person, got %s", edges[0].Source().TypeName())
	}
	if edges[0].Target().TypeName() != "Company" {
		t.Errorf("Edge target should be Company, got %s", edges[0].Target().TypeName())
	}
	if edges[0].Relation() != "employer" {
		t.Errorf("Edge relation should be employer, got %s", edges[0].Relation())
	}
}

func TestGraph_ForwardReference_Multiple(t *testing.T) {
	// Multiple sources reference the same target
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add multiple Persons all referencing same Company
	for _, name := range []string{"alice", "bob", "carol"} {
		person := mustValidInstanceWithEdge(t, s, "Person",
			[]any{name}, map[string]any{"name": name},
			"employer", [][]any{{"acme"}})

		if _, err := g.Add(ctx, person); err != nil {
			t.Fatalf("Add %s error: %v", name, err)
		}
	}

	// All 3 should be unresolved
	snap := g.Snapshot()
	assertInstanceCount(t, snap, "Person", 3)
	// All 3 pending edges should be tracked (one per source)
	if len(snap.Unresolved()) != 3 {
		t.Errorf("Expected 3 unresolved edges (one per source), got %d", len(snap.Unresolved()))
	}

	// Add Company (target)
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// All 3 edges should resolve
	snap = g.Snapshot()
	edges := snap.Edges()
	if len(edges) != 3 {
		t.Errorf("Expected 3 resolved edges (one per source), got %d", len(edges))
	}

	// Verify all 3 sources have edges to the target
	sources := make(map[string]bool)
	for _, edge := range edges {
		sources[edge.Source().PrimaryKey().String()] = true
		if edge.Target().PrimaryKey().String() != `["acme"]` {
			t.Errorf("Edge target should be [\"acme\"], got %s", edge.Target().PrimaryKey().String())
		}
		if edge.Relation() != "employer" {
			t.Errorf("Edge relation should be employer, got %s", edge.Relation())
		}
	}
	for _, name := range []string{`["alice"]`, `["bob"]`, `["carol"]`} {
		if !sources[name] {
			t.Errorf("Missing edge from source %s", name)
		}
	}
}

func TestGraph_ForwardReference_Chain(t *testing.T) {
	// A → B → C chain: add A first, then C, then B
	s := testSchemaWithChainedAssociations(t)
	g := New(s)
	ctx := t.Context()

	// Add A (references B which doesn't exist)
	typeA := mustValidInstanceWithEdge(t, s, "TypeA",
		[]any{"a1"}, map[string]any{"name": "A1"},
		"refB", [][]any{{"b1"}})

	if _, err := g.Add(ctx, typeA); err != nil {
		t.Fatalf("Add TypeA error: %v", err)
	}

	// Add C (no forward references)
	typeC := mustValidInstance(t, s, "TypeC",
		[]any{"c1"}, map[string]any{"name": "C1"})

	if _, err := g.Add(ctx, typeC); err != nil {
		t.Fatalf("Add TypeC error: %v", err)
	}

	// Verify A→B still unresolved
	snap := g.Snapshot()
	assertUnresolvedCount(t, snap, 1)

	// Add B (references C, resolves A→B)
	typeB := mustValidInstanceWithEdge(t, s, "TypeB",
		[]any{"b1"}, map[string]any{"name": "B1"},
		"refC", [][]any{{"c1"}})

	if _, err := g.Add(ctx, typeB); err != nil {
		t.Fatalf("Add TypeB error: %v", err)
	}

	// All edges should now be resolved
	snap = g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 2) // A→B and B→C
}

func TestGraph_ForwardReference_Snapshot(t *testing.T) {
	// Verify pending edges appear in Unresolved() before resolution
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person with forward ref
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 1 {
		t.Fatalf("Expected 1 unresolved edge, got %d", len(unresolved))
	}

	ur := unresolved[0]
	if ur.Source.TypeName() != "Person" {
		t.Errorf("Unresolved source should be Person, got %s", ur.Source.TypeName())
	}
	if ur.Relation != "employer" {
		t.Errorf("Unresolved relation should be employer, got %s", ur.Relation)
	}
	if ur.TargetType != "Company" {
		t.Errorf("Unresolved target type should be Company, got %s", ur.TargetType)
	}
	if ur.TargetKey != `["acme"]` {
		t.Errorf("Unresolved target key should be [\"acme\"], got %s", ur.TargetKey)
	}
}

func TestUnresolvedEdge_RequiredAndReasonFields(t *testing.T) {
	// Verify Required and Reason fields are populated correctly
	s := testSchemaWithAssociation(t) // Person -> Company (required)
	g := New(s)
	ctx := t.Context()

	// Add Person with reference to missing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing-company"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 1 {
		t.Fatalf("Expected 1 unresolved edge, got %d", len(unresolved))
	}

	ur := unresolved[0]
	if !ur.Required {
		t.Error("Expected Required=true for required association")
	}
	if ur.Reason != "target_missing" {
		t.Errorf("Expected Reason='target_missing', got %q", ur.Reason)
	}
}

func TestUnresolvedEdge_OptionalAssociation(t *testing.T) {
	// Optional associations should have Required=false
	s := testSchemaWithOptionalAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person with reference to missing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing-company"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 1 {
		t.Fatalf("Expected 1 unresolved edge, got %d", len(unresolved))
	}

	ur := unresolved[0]
	if ur.Required {
		t.Error("Expected Required=false for optional association")
	}
	if ur.Reason != "target_missing" {
		t.Errorf("Expected Reason='target_missing', got %q", ur.Reason)
	}
}

func TestUnresolvedEdge_AbsentReason(t *testing.T) {
	// Absent required association field should have Reason="absent"
	s := testSchemaWithAssociation(t) // Person -> Company (required)
	g := New(s)
	ctx := t.Context()

	// Add Person WITHOUT employer field
	person := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 1 {
		t.Fatalf("Expected 1 unresolved edge, got %d", len(unresolved))
	}

	ur := unresolved[0]
	if !ur.Required {
		t.Error("Expected Required=true for required association")
	}
	if ur.Reason != "absent" {
		t.Errorf("Expected Reason='absent', got %q", ur.Reason)
	}
	if ur.TargetKey != "" {
		t.Errorf("Expected empty TargetKey for absent field, got %q", ur.TargetKey)
	}
}

func TestUnresolvedEdge_EmptyReason(t *testing.T) {
	// Empty required association array should have Reason="empty"
	s := testSchemaWithManyAssociation(t) // Person -> Company (required many)
	g := New(s)
	ctx := t.Context()

	// Add Person with empty employers array
	person := mustValidInstanceWithEmptyEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employers")

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 1 {
		t.Fatalf("Expected 1 unresolved edge, got %d", len(unresolved))
	}

	ur := unresolved[0]
	if !ur.Required {
		t.Error("Expected Required=true for required association")
	}
	if ur.Reason != "empty" {
		t.Errorf("Expected Reason='empty', got %q", ur.Reason)
	}
	if ur.TargetKey != "" {
		t.Errorf("Expected empty TargetKey for empty array, got %q", ur.TargetKey)
	}
}

func TestGraph_ForwardReference_AfterResolution(t *testing.T) {
	// Verify pending is removed after target is added
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person with forward ref
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	// Confirm pending exists
	snap1 := g.Snapshot()
	if len(snap1.Unresolved()) == 0 {
		t.Fatal("Expected unresolved edge before adding target")
	}

	// Add Company
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Confirm pending is gone
	snap2 := g.Snapshot()
	if len(snap2.Unresolved()) != 0 {
		t.Errorf("Expected no unresolved edges after adding target, got %d", len(snap2.Unresolved()))
	}

	// Confirm edge exists
	if len(snap2.Edges()) != 1 {
		t.Errorf("Expected 1 edge after resolution, got %d", len(snap2.Edges()))
	}
}

// Check Tests
//
// These tests verify the Check() method for required association validation.

func TestGraph_Check_RequiredMissing(t *testing.T) {
	// Required association target missing → E_UNRESOLVED_REQUIRED
	s := testSchemaWithAssociation(t) // Person -> Company (required)
	g := New(s)
	ctx := t.Context()

	// Add Person with edge to non-existent Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing-company"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Check should report unresolved required
	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Error("Check should fail with unresolved required association")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			hasCode = true
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_UNRESOLVED_REQUIRED diagnostic")
	}
}

func TestGraph_Check_RequiredEmpty(t *testing.T) {
	// Required array empty → E_UNRESOLVED_REQUIRED with "empty" reason
	s := testSchemaWithManyAssociation(t) // Person -> Company (required many)
	g := New(s)
	ctx := t.Context()

	// Add Person with empty edge array
	person := mustValidInstanceWithEmptyEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employers")

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Check should report unresolved required
	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Error("Check should fail with empty required association")
	}

	hasCode := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			hasCode = true
			// Could verify "empty" in message/details
			break
		}
	}
	if !hasCode {
		t.Error("Expected E_UNRESOLVED_REQUIRED diagnostic")
	}
}

func TestGraph_Check_OptionalMissing(t *testing.T) {
	// Optional association unresolved → no error
	s := testSchemaWithOptionalAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person with edge to non-existent Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing-company"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Check should pass (optional unresolved is OK)
	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if !result.OK() {
		t.Errorf("Check should pass with unresolved optional association: %s", result.String())
	}
}

func TestGraph_Check_MultipleUnresolved(t *testing.T) {
	// Multiple unresolved → multiple issues
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add multiple Persons with missing targets
	for _, name := range []string{"alice", "bob", "carol"} {
		person := mustValidInstanceWithEdge(t, s, "Person",
			[]any{name}, map[string]any{"name": name},
			"employer", [][]any{{name + "-company"}}) // Each has unique missing target

		if _, err := g.Add(ctx, person); err != nil {
			t.Fatalf("Add %s error: %v", name, err)
		}
	}

	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Error("Check should fail with multiple unresolved required associations")
	}

	// Count E_UNRESOLVED_REQUIRED issues
	count := 0
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			count++
		}
	}
	if count < 3 {
		t.Errorf("Expected at least 3 E_UNRESOLVED_REQUIRED issues, got %d", count)
	}
}

func TestGraph_Check_Idempotent(t *testing.T) {
	// Multiple Check() calls produce same result
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Person with missing target
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"missing"}})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	// Call Check multiple times
	result1, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check 1 error: %v", err)
	}

	result2, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check 2 error: %v", err)
	}

	result3, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check 3 error: %v", err)
	}

	// All results should have same OK status
	if result1.OK() != result2.OK() || result2.OK() != result3.OK() {
		t.Error("Check results should be consistent across multiple calls")
	}

	// Note: Issue count may accumulate in collector
	// The key invariant is that Check doesn't change graph state
}

// Edge Properties Tests

func TestGraph_Edge_Properties(t *testing.T) {
	// Edge with properties captured correctly
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// First add the Company (target)
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add Person with edge properties
	person := mustValidInstanceWithEdgeProps(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", []any{"acme"},
		map[string]any{"role": "Engineer", "since": int64(2020)})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add person error: %v", err)
	}

	snap := g.Snapshot()
	edges := snap.Edges()
	if len(edges) != 1 {
		t.Fatalf("Expected 1 edge, got %d", len(edges))
	}

	edge := edges[0]
	if !edge.HasProperties() {
		t.Error("Edge should have properties")
	}

	role, ok := edge.Property("role")
	if !ok {
		t.Error("Edge should have 'role' property")
	} else {
		roleStr, _ := role.String()
		if roleStr != "Engineer" {
			t.Errorf("Edge role should be 'Engineer', got %q", roleStr)
		}
	}

	since, ok := edge.Property("since")
	if !ok {
		t.Error("Edge should have 'since' property")
	} else {
		sinceInt, _ := since.Int()
		if sinceInt != 2020 {
			t.Errorf("Edge since should be 2020, got %d", sinceInt)
		}
	}
}

// Multiple Forward References Tests - verify all edges are tracked and resolved

func TestGraph_Check_MultipleUnresolved_SameTarget(t *testing.T) {
	// Multiple sources reference the SAME missing target
	// Each should emit a separate E_UNRESOLVED_REQUIRED
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add 3 Persons all referencing same non-existent Company
	for _, name := range []string{"alice", "bob", "carol"} {
		person := mustValidInstanceWithEdge(t, s, "Person",
			[]any{name}, map[string]any{"name": name},
			"employer", [][]any{{"missing-acme"}}) // Same target!

		if _, err := g.Add(ctx, person); err != nil {
			t.Fatalf("Add %s error: %v", name, err)
		}
	}

	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Error("Check should fail with multiple unresolved required associations")
	}

	// Count E_UNRESOLVED_REQUIRED issues - should be 3
	count := 0
	sources := make(map[string]bool)
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			count++
			// Extract source from details (using standard pk key per)
			for _, d := range issue.Details() {
				if d.Key == diag.DetailKeyPrimaryKey {
					sources[d.Value] = true
				}
			}
		}
	}
	if count != 3 {
		t.Errorf("Expected exactly 3 E_UNRESOLVED_REQUIRED issues, got %d", count)
	}
	// Verify all 3 sources are reported
	for _, name := range []string{`["alice"]`, `["bob"]`, `["carol"]`} {
		if !sources[name] {
			t.Errorf("Missing diagnostic for source %s", name)
		}
	}
}

func TestGraph_ForwardReference_Multiple_Unresolved_Snapshot(t *testing.T) {
	// Multiple sources reference same target - verify Unresolved() returns all
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add 3 Persons all referencing same non-existent Company
	for _, name := range []string{"alice", "bob", "carol"} {
		person := mustValidInstanceWithEdge(t, s, "Person",
			[]any{name}, map[string]any{"name": name},
			"employer", [][]any{{"acme"}})

		if _, err := g.Add(ctx, person); err != nil {
			t.Fatalf("Add %s error: %v", name, err)
		}
	}

	snap := g.Snapshot()
	unresolved := snap.Unresolved()

	if len(unresolved) != 3 {
		t.Errorf("Expected 3 unresolved edges, got %d", len(unresolved))
	}

	// Verify all 3 sources are represented
	sources := make(map[string]bool)
	for _, ur := range unresolved {
		sources[ur.Source.PrimaryKey().String()] = true
		if ur.TargetKey != `["acme"]` {
			t.Errorf("Unresolved target key should be [\"acme\"], got %s", ur.TargetKey)
		}
	}
	for _, name := range []string{`["alice"]`, `["bob"]`, `["carol"]`} {
		if !sources[name] {
			t.Errorf("Missing unresolved edge from source %s", name)
		}
	}
}

func TestGraph_Check_RequiredAbsent(t *testing.T) {
	// Required association field is completely absent (not provided at all)
	// Should emit E_UNRESOLVED_REQUIRED with reason="absent"
	s := testSchemaWithAssociation(t) // Person -> Company (required)
	g := New(s)
	ctx := t.Context()

	// Add Person WITHOUT employer field at all
	// mustValidInstance creates instance without edges
	person := mustValidInstance(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"})

	if _, err := g.Add(ctx, person); err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Error("Check should fail with absent required association")
	}

	foundAbsent := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			for _, d := range issue.Details() {
				if d.Key == "reason" && d.Value == "absent" {
					foundAbsent = true
					break
				}
			}
		}
	}
	if !foundAbsent {
		t.Error("Expected E_UNRESOLVED_REQUIRED with reason='absent'")
	}
}

func TestGraph_Check_UnresolvedRequired_HasProvenanceSpan(t *testing.T) {
	// Check diagnostics should include provenance span when source instance has provenance
	s := testSchemaWithAssociation(t) // Person -> Company (required)
	g := New(s)
	ctx := t.Context()

	// Get the Person type for creating the instance
	personType, ok := s.Type("Person")
	if !ok {
		t.Fatal("Person type not found")
	}

	// Create provenance with a specific span
	prov := instance.NewProvenance(
		"test.json",
		path.Root().Key("people").Index(0),
		location.Span{
			Start: location.Position{Line: 5, Column: 3},
			End:   location.Position{Line: 10, Column: 5},
		},
	)

	// Create edge data for missing reference
	targets := []instance.ValidEdgeTarget{
		instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{"missing-company"}),
			immutable.Properties{},
		),
	}
	edges := map[string]*instance.ValidEdgeData{
		"employer": instance.NewValidEdgeData(targets),
	}

	// Create instance with provenance
	inst := instance.NewValidInstance(
		"Person",
		personType.ID(),
		immutable.WrapKey([]any{"alice"}),
		immutable.WrapProperties(map[string]any{"name": "Alice"}),
		edges,
		nil,
		prov,
	)

	_, err := g.Add(ctx, inst)
	if err != nil {
		t.Fatalf("Add error: %v", err)
	}

	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}

	if result.OK() {
		t.Fatal("Check should fail with unresolved required association")
	}

	// Find the E_UNRESOLVED_REQUIRED issue and verify span is attached
	foundWithSpan := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_UNRESOLVED_REQUIRED {
			span := issue.Span()
			if span.Start.Line == 5 && span.Start.Column == 3 {
				foundWithSpan = true
				break
			}
		}
	}

	if !foundWithSpan {
		t.Error("Expected E_UNRESOLVED_REQUIRED diagnostic to have provenance span attached")
	}
}

// Backward Reference Tests
//
// These tests verify that backward references (adding target before source)
// work correctly - edges should be immediately resolved.

func TestGraph_BackwardReference_Basic(t *testing.T) {
	// Add target before source - edge should resolve immediately
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Company (target) FIRST
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	result, err := g.Add(ctx, company)
	if err != nil {
		t.Fatalf("Add company error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add company should succeed: %s", result.String())
	}

	// Snapshot should have no edges and no unresolved
	snap := g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 0)

	// Now add Person (source) with edge to existing Company
	person := mustValidInstanceWithEdge(t, s, "Person",
		[]any{"alice"}, map[string]any{"name": "Alice"},
		"employer", [][]any{{"acme"}})

	result, err = g.Add(ctx, person)
	if err != nil {
		t.Fatalf("Add person error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Add person should succeed: %s", result.String())
	}

	// Edge should be immediately resolved (no pending)
	snap = g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 1)

	// Verify edge details
	edges := snap.Edges()
	if edges[0].Source().TypeName() != "Person" {
		t.Errorf("Edge source should be Person, got %s", edges[0].Source().TypeName())
	}
	if edges[0].Target().TypeName() != "Company" {
		t.Errorf("Edge target should be Company, got %s", edges[0].Target().TypeName())
	}
}

func TestGraph_BackwardReference_Multiple(t *testing.T) {
	// Add target first, then multiple sources
	s := testSchemaWithAssociation(t)
	g := New(s)
	ctx := t.Context()

	// Add Company (target) FIRST
	company := mustValidInstance(t, s, "Company",
		[]any{"acme"}, map[string]any{"name": "Acme Corp"})

	if _, err := g.Add(ctx, company); err != nil {
		t.Fatalf("Add company error: %v", err)
	}

	// Add multiple Persons all referencing existing Company
	for _, name := range []string{"alice", "bob", "carol"} {
		person := mustValidInstanceWithEdge(t, s, "Person",
			[]any{name}, map[string]any{"name": name},
			"employer", [][]any{{"acme"}})

		if _, err := g.Add(ctx, person); err != nil {
			t.Fatalf("Add %s error: %v", name, err)
		}
	}

	// All 3 edges should be immediately resolved
	snap := g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 3)

	// Verify all 3 sources have edges
	sources := make(map[string]bool)
	for _, edge := range snap.Edges() {
		sources[edge.Source().PrimaryKey().String()] = true
		if edge.Target().PrimaryKey().String() != `["acme"]` {
			t.Errorf("Edge target should be [\"acme\"], got %s", edge.Target().PrimaryKey().String())
		}
	}
	for _, name := range []string{`["alice"]`, `["bob"]`, `["carol"]`} {
		if !sources[name] {
			t.Errorf("Missing edge from source %s", name)
		}
	}
}

// Circular Reference Tests
//
// These tests verify that circular references (A → B → A or longer cycles)
// are handled correctly without infinite loops or crashes.

func TestGraph_CircularReference_Basic(t *testing.T) {
	// A → B → A cycle - verify graph handles this correctly
	s := testSchemaWithMutualAssociations(t)
	g := New(s)
	ctx := t.Context()

	// Add TypeA referencing TypeB (forward ref)
	typeA := mustValidInstanceWithEdge(t, s, "TypeA",
		[]any{"a1"}, map[string]any{"name": "A1"},
		"refB", [][]any{{"b1"}})

	if _, err := g.Add(ctx, typeA); err != nil {
		t.Fatalf("Add TypeA error: %v", err)
	}

	// TypeA → TypeB is unresolved
	snap := g.Snapshot()
	assertUnresolvedCount(t, snap, 1)

	// Add TypeB referencing TypeA (creates cycle, resolves A→B)
	typeB := mustValidInstanceWithEdge(t, s, "TypeB",
		[]any{"b1"}, map[string]any{"name": "B1"},
		"refA", [][]any{{"a1"}})

	if _, err := g.Add(ctx, typeB); err != nil {
		t.Fatalf("Add TypeB error: %v", err)
	}

	// Both edges should now be resolved
	snap = g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 2)

	// Check should not infinite loop and should succeed
	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Check should succeed for resolved circular references: %s", result.String())
	}
}

func TestGraph_CircularReference_Chain(t *testing.T) {
	// A → B → C → A cycle - longer cycle
	s := testSchemaWithCircularChain(t)
	g := New(s)
	ctx := t.Context()

	// Add all three in sequence, each creating a forward ref
	typeA := mustValidInstanceWithEdge(t, s, "TypeA",
		[]any{"a1"}, map[string]any{"name": "A1"},
		"refB", [][]any{{"b1"}})

	if _, err := g.Add(ctx, typeA); err != nil {
		t.Fatalf("Add TypeA error: %v", err)
	}

	typeB := mustValidInstanceWithEdge(t, s, "TypeB",
		[]any{"b1"}, map[string]any{"name": "B1"},
		"refC", [][]any{{"c1"}})

	if _, err := g.Add(ctx, typeB); err != nil {
		t.Fatalf("Add TypeB error: %v", err)
	}

	// At this point: A→B resolved, B→C unresolved
	snap := g.Snapshot()
	assertEdgeCount(t, snap, 1)       // A→B
	assertUnresolvedCount(t, snap, 1) // B→C

	// Add TypeC referencing TypeA (completes the cycle)
	typeC := mustValidInstanceWithEdge(t, s, "TypeC",
		[]any{"c1"}, map[string]any{"name": "C1"},
		"refA", [][]any{{"a1"}})

	if _, err := g.Add(ctx, typeC); err != nil {
		t.Fatalf("Add TypeC error: %v", err)
	}

	// All 3 edges should be resolved
	snap = g.Snapshot()
	assertUnresolvedCount(t, snap, 0)
	assertEdgeCount(t, snap, 3) // A→B, B→C, C→A

	// Check should complete without infinite loop
	result, err := g.Check(ctx)
	if err != nil {
		t.Fatalf("Check error: %v", err)
	}
	if !result.OK() {
		t.Errorf("Check should succeed for resolved circular chain: %s", result.String())
	}

	// Verify edges form the expected cycle
	edges := snap.Edges()
	if len(edges) != 3 {
		t.Fatalf("Expected 3 edges in cycle, got %d", len(edges))
	}

	edgeMap := make(map[string]string) // source → target
	for _, edge := range edges {
		src := edge.Source().TypeName()
		tgt := edge.Target().TypeName()
		edgeMap[src] = tgt
	}

	// Verify cycle: A→B, B→C, C→A
	if edgeMap["TypeA"] != "TypeB" {
		t.Errorf("Expected TypeA→TypeB, got TypeA→%s", edgeMap["TypeA"])
	}
	if edgeMap["TypeB"] != "TypeC" {
		t.Errorf("Expected TypeB→TypeC, got TypeB→%s", edgeMap["TypeB"])
	}
	if edgeMap["TypeC"] != "TypeA" {
		t.Errorf("Expected TypeC→TypeA, got TypeC→%s", edgeMap["TypeC"])
	}
}
