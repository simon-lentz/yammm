package neo4j

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
)

func TestAdapter_DefaultConfig(t *testing.T) {
	t.Parallel()
	a := New()

	ctx := context.Background()

	// Verify defaults by checking observable behavior.
	// separator="__"
	if got := a.Label(ctx, "s", "T"); got != "s__T" {
		t.Errorf("default separator: Label('s','T') = %q; want 's__T'", got)
	}
	// prefix=""
	if got := a.Label(ctx, "s", "T"); !strings.HasPrefix(got, "s") {
		t.Errorf("default prefix: Label should not have prefix, got %q", got)
	}
	// edition=Enterprise: generate NOT NULL constraints
	s := loadSchema(t, "basic.yammm")
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	hasNotNull := false
	hasType := false
	hasNamed := false
	for _, stmt := range stmts {
		if strings.Contains(stmt, "IS NOT NULL") {
			hasNotNull = true
		}
		if strings.Contains(stmt, "IS :: STRING") {
			hasType = true
		}
		// Named: name appears before IF NOT EXISTS.
		after := strings.TrimPrefix(stmt, "CREATE CONSTRAINT ")
		if !strings.HasPrefix(after, "IF NOT EXISTS") {
			hasNamed = true
		}
	}
	if !hasNotNull {
		t.Error("default edition=Enterprise should produce NOT NULL constraints")
	}
	if !hasType {
		t.Error("default scalarTypeConstraints=true should produce TYPE constraints")
	}
	if !hasNamed {
		t.Error("default namedConstraints=true should produce named constraints")
	}
	// nodeKeyConstraints=false: should use UNIQUE, not NODE KEY
	for _, stmt := range stmts {
		if strings.Contains(stmt, "IS NODE KEY") {
			t.Error("default nodeKeyConstraints=false should not produce NODE KEY")
		}
	}
}

func TestAdapter_FullPipeline_BasicSchema(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New()
	ctx := context.Background()

	// Generate constraints.
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if len(stmts) == 0 {
		t.Fatal("expected non-empty constraints")
	}

	// Generate shapes.
	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Verify shapes match constraint labels.
	for _, ns := range shape.Types {
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt, ns.Label) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("shape label %q not found in any constraint statement", ns.Label)
		}
	}
}

func TestAdapter_FullPipeline_WithWrite(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()
	ctx := context.Background()

	// Generate constraints + shapes.
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Build graph with instances.
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	// Generate batch node queries.
	nodeQueries, err := a.BatchNodeQueries(ctx, graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeQueries) == 0 {
		t.Fatal("expected node queries")
	}

	// Verify labels in MERGE statements match constraint labels.
	for _, nq := range nodeQueries {
		// Extract label from MERGE (n:LABEL {
		mergeIdx := strings.Index(nq.Statement, "MERGE (n:")
		if mergeIdx == -1 {
			t.Errorf("no MERGE in node query: %s", nq.Statement)
			continue
		}
		after := nq.Statement[mergeIdx+len("MERGE (n:"):]
		spaceIdx := strings.IndexAny(after, " {")
		if spaceIdx == -1 {
			continue
		}
		mergeLabel := after[:spaceIdx]

		// This label should appear in at least one constraint.
		found := false
		for _, stmt := range stmts {
			if strings.Contains(stmt, mergeLabel) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("MERGE label %q not found in constraints — label consistency violated", mergeLabel)
		}
	}
}

func TestAdapter_CommunityEdition_ReducedOutput(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithEdition(Community))
	ctx := context.Background()

	// Constraints: only UNIQUE.
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range stmts {
		if !strings.Contains(stmt, "IS UNIQUE") {
			t.Errorf("Community should only produce UNIQUE, got: %s", stmt)
		}
	}

	// Shapes: unaffected by edition.
	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := shape.Types["Entity"]; !ok {
		t.Error("shapes should be unaffected by edition")
	}
}

func TestAdapter_CustomSeparator_Consistency(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New(WithLabelSeparator("_"))
	ctx := context.Background()

	expectedLabel := "basic_test_Entity"

	// Label method.
	directLabel := a.Label(ctx, s.Name(), "Entity")
	if directLabel != expectedLabel {
		t.Errorf("Label() = %q; want %q", directLabel, expectedLabel)
	}

	// Constraints use same label.
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range stmts {
		if strings.Contains(stmt, "basic_test__Entity") {
			t.Errorf("constraint uses default separator instead of custom: %s", stmt)
		}
	}
	constraintHasLabel := false
	for _, stmt := range stmts {
		if strings.Contains(stmt, expectedLabel) {
			constraintHasLabel = true
			break
		}
	}
	if !constraintHasLabel {
		t.Errorf("no constraint contains custom-separator label %q", expectedLabel)
	}

	// Shape uses same label.
	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if ns, ok := shape.Types["Entity"]; !ok || ns.Label != expectedLabel {
		t.Errorf("shape label = %q; want %q", shape.Types["Entity"].Label, expectedLabel)
	}

	// Write query uses same label.
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})
	nodeQueries, err := a.BatchNodeQueries(ctx, graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeQueries) == 0 {
		t.Fatal("expected node queries")
	}
	if !strings.Contains(nodeQueries[0].Statement, expectedLabel) {
		t.Errorf("MERGE statement uses wrong label: %s", nodeQueries[0].Statement)
	}
}

func TestAdapter_CustomPrefix_Consistency(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "basic.yammm")
	a := New(WithLabelPrefix("app_"))
	ctx := context.Background()

	expectedLabel := "app_basic_test__Entity"

	// Label method.
	directLabel := a.Label(ctx, s.Name(), "Entity")
	if directLabel != expectedLabel {
		t.Errorf("Label() = %q; want %q", directLabel, expectedLabel)
	}

	// Constraints use same label.
	stmts, result := a.ConstraintsForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	constraintHasLabel := false
	for _, stmt := range stmts {
		if strings.Contains(stmt, expectedLabel) {
			constraintHasLabel = true
			break
		}
	}
	if !constraintHasLabel {
		t.Errorf("no constraint contains prefixed label %q", expectedLabel)
	}

	// Shape uses same label.
	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if ns, ok := shape.Types["Entity"]; !ok || ns.Label != expectedLabel {
		t.Errorf("shape label = %q; want %q", shape.Types["Entity"].Label, expectedLabel)
	}

	// Write query uses same label.
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
	})
	nodeQueries, err := a.BatchNodeQueries(ctx, graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodeQueries) == 0 {
		t.Fatal("expected node queries")
	}
	if !strings.Contains(nodeQueries[0].Statement, expectedLabel) {
		t.Errorf("MERGE statement uses wrong label: %s", nodeQueries[0].Statement)
	}
}

func TestAdapter_LabelConsistency(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "multiple_types.yammm")
	a := New()
	ctx := context.Background()

	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	structured, result := a.ConstraintsStructured(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}

	// Build constraint label set.
	constraintLabels := make(map[string]bool)
	for _, c := range structured {
		constraintLabels[c.Label] = true
	}

	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Widget": {{"id": "w1", "code": "C1"}},
		"Gadget": {{"uid": "g1", "sku": "S1"}},
	})

	nodeQueries, err := a.BatchNodeQueries(ctx, graphResult, shape)
	if err != nil {
		t.Fatal(err)
	}

	// For each type: Label() == shape.Label == constraint label == MERGE label.
	for _, t2 := range s.TypesSlice() {
		if t2.IsAbstract() {
			continue
		}
		typeName := t2.Name()

		// 1. Label() output.
		directLabel := a.Label(ctx, s.Name(), typeName)

		// 2. Shape label.
		ns, ok := shape.Types[typeName]
		if !ok {
			t.Errorf("type %q missing from shape", typeName)
			continue
		}
		if ns.Label != directLabel {
			t.Errorf("type %q: shape.Label=%q != Label()=%q", typeName, ns.Label, directLabel)
		}

		// 3. Constraint label.
		if !constraintLabels[directLabel] {
			t.Errorf("type %q: Label()=%q not found in constraint labels", typeName, directLabel)
		}

		// 4. MERGE label.
		mergeFound := false
		for _, nq := range nodeQueries {
			if strings.Contains(nq.Statement, directLabel) {
				mergeFound = true
				break
			}
		}
		if !mergeFound {
			t.Errorf("type %q: Label()=%q not found in any MERGE statement", typeName, directLabel)
		}
	}
}

func TestAdapter_ThreadSafety(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "write_basic.yammm")
	a := New()
	ctx := context.Background()

	// Pre-build immutable inputs for write operations.
	shape, result := a.ShapeForSchema(ctx, s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Issuer": {{"issuer_id": "iss1", "name": "Test Issuer"}},
		"Issue":  {{"issuer_id": "iss1", "issue_id": "i1", "title": "Test Issue", "in_issuer": map[string]any{"_target_issuer_id": "iss1"}}},
	})

	var wg sync.WaitGroup
	errs := make(chan error, 50)

	// Run ConstraintsForSchema, ShapeForSchema, Label, BatchNodeQueries,
	// and BatchEdgeQueries concurrently.
	for range 10 {
		wg.Go(func() {
			_, result := a.ConstraintsForSchema(ctx, s)
			if err := result.Err(); err != nil {
				errs <- fmt.Errorf("constraints: %w", err)
			}
		})
		wg.Go(func() {
			_, result := a.ShapeForSchema(ctx, s)
			if err := result.Err(); err != nil {
				errs <- fmt.Errorf("shape: %w", err)
			}
		})
		wg.Go(func() {
			_ = a.Label(ctx, "test", "Type")
		})
		wg.Go(func() {
			if _, err := a.BatchNodeQueries(ctx, graphResult, shape); err != nil {
				errs <- fmt.Errorf("batch nodes: %w", err)
			}
		})
		wg.Go(func() {
			if _, err := a.BatchEdgeQueries(ctx, graphResult, shape); err != nil {
				errs <- fmt.Errorf("batch edges: %w", err)
			}
		})
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent error: %v", err)
	}
}
