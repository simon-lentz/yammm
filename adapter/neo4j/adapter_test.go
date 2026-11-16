package neo4j

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/simon-lentz/yammm/diag"
)

// TestAdapter_ShapesUnaffectedByEdition pins that edition gates constraint
// output only; the graph shape is identical either way. (Community's reduced
// constraint output itself is asserted in TestConstraints_CommunityEdition.)
func TestAdapter_ShapesUnaffectedByEdition(t *testing.T) {
	t.Parallel()
	s := loadSchema(t, "basic.yammm")
	a := New(WithEdition(Community))

	shape, result := a.shapeFor(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatal(err)
	}
	if _, ok := shape.types[typeID(t, s, "Entity")]; !ok {
		t.Error("shapes should be unaffected by edition")
	}
}

// TestAdapter_LabelOptionConsistency walks one labeling option through every
// surface that renders a label — Label(), constraint statements, the graph
// shape, and the MERGE statement of a batch write — and requires they all
// agree. TestAdapter_LabelConsistency covers the default options.
func TestAdapter_LabelOptionConsistency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		opt       Option
		wantLabel string
	}{
		{"custom separator", WithLabelSeparator("_"), "basic_test_Entity"},
		{"custom prefix", WithLabelPrefix("app_"), "app_basic_test__Entity"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, v := loadSchemaAndValidator(t, "basic.yammm")
			a := New(tc.opt)
			ctx := context.Background()

			if got := a.Label(ctx, s.Name(), "Entity"); got != tc.wantLabel {
				t.Errorf("Label() = %q; want %q", got, tc.wantLabel)
			}

			stmts, result := a.ConstraintsForSchema(ctx, s)
			if err := result.Err(); err != nil {
				t.Fatal(err)
			}
			constraintHasLabel := false
			for _, stmt := range stmts {
				if strings.Contains(stmt, tc.wantLabel) {
					constraintHasLabel = true
					break
				}
			}
			if !constraintHasLabel {
				t.Errorf("no constraint contains label %q", tc.wantLabel)
			}

			shape, result := a.shapeFor(ctx, s)
			if err := result.Err(); err != nil {
				t.Fatal(err)
			}
			if ns, ok := shape.types[typeID(t, s, "Entity")]; !ok || ns.label != tc.wantLabel {
				t.Errorf("shape label = %q; want %q", shape.types[typeID(t, s, "Entity")].label, tc.wantLabel)
			}

			graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
				"Entity": {{"id": "e1", "name": "test", "count": int64(1), "active": true, "created_at": "2024-01-01T00:00:00Z"}},
			})
			nodeQueries, err := a.BatchNodeQueries(ctx, graphResult)
			if err != nil {
				t.Fatal(err)
			}
			if len(nodeQueries) != 1 {
				t.Fatalf("expected 1 node query, got %d", len(nodeQueries))
			}
			if !strings.Contains(nodeQueries[0].Statement, tc.wantLabel) {
				t.Errorf("MERGE statement uses wrong label: %s", nodeQueries[0].Statement)
			}
		})
	}
}

func TestAdapter_LabelConsistency(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, "multiple_types.yammm")
	a := New()
	ctx := context.Background()
	shape := mustShape(t, a, s)

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

	nodeQueries, err := a.BatchNodeQueries(ctx, graphResult)
	if err != nil {
		t.Fatal(err)
	}

	// For each type: Label() == shape.Label == constraint label == MERGE label.
	for t2 := range s.Types() {
		if t2.IsAbstract() {
			continue
		}
		typeName := t2.Name()

		// 1. Label() output.
		directLabel := a.Label(ctx, s.Name(), typeName)

		// 2. Shape label.
		ns, ok := shape.types[t2.ID()]
		if !ok {
			t.Errorf("type %q missing from shape", typeName)
			continue
		}
		if ns.label != directLabel {
			t.Errorf("type %q: shape.Label=%q != Label()=%q", typeName, ns.label, directLabel)
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
	graphResult := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "iss1", "name": "Test Publisher"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test Book", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
	})

	var wg sync.WaitGroup
	errs := make(chan error, 50)

	// Run ConstraintsForSchema, shapeFor, Label, BatchNodeQueries,
	// and BatchEdgeQueries concurrently.
	for range 10 {
		wg.Go(func() {
			_, result := a.ConstraintsForSchema(ctx, s)
			if err := result.Err(); err != nil {
				errs <- fmt.Errorf("constraints: %w", err)
			}
		})
		wg.Go(func() {
			_, result := a.shapeFor(ctx, s)
			if err := result.Err(); err != nil {
				errs <- fmt.Errorf("shape: %w", err)
			}
		})
		wg.Go(func() {
			_ = a.Label(ctx, "test", "Type")
		})
		wg.Go(func() {
			if _, err := a.BatchNodeQueries(ctx, graphResult); err != nil {
				errs <- fmt.Errorf("batch nodes: %w", err)
			}
		})
		wg.Go(func() {
			if _, err := a.BatchEdgeQueries(ctx, graphResult); err != nil {
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

// One snapshot written by two adapters takes each receiver's labels, on node
// and edge statements alike: the labels come from the adapter that writes.
func TestBatchQueries_TheReceiverDecidesTheLabels(t *testing.T) {
	t.Parallel()
	_, s, v := setupWrite(t, "write_basic.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "iss1", "name": "Test Publisher"}},
		"Book":      {{"publisher_id": "iss1", "book_id": "i1", "title": "Test Book", "by_publisher": map[string]any{"_target_publisher_id": "iss1"}}},
	})
	for _, a := range []*Adapter{New(), New(WithLabelPrefix("app_")), New(WithLabelSeparator("_x_"))} {
		book, publisher := a.Label(t.Context(), s.Name(), "Book"), a.Label(t.Context(), s.Name(), "Publisher")
		nodes, err := a.BatchNodeQueries(t.Context(), snap)
		if err != nil {
			t.Fatal(err)
		}
		var written []string
		for _, q := range nodes {
			written = append(written, q.Statement)
		}
		edges, err := a.BatchEdgeQueries(t.Context(), snap)
		if err != nil {
			t.Fatal(err)
		}
		if len(edges) != 1 {
			t.Fatalf("%s: %d edge queries, want 1", book, len(edges))
		}
		all := strings.Join(written, "\n")
		for _, want := range []string{":" + book + " ", ":" + publisher + " "} {
			if !strings.Contains(all, want) {
				t.Errorf("no node statement writes %q:\n%s", want, all)
			}
		}
		for _, want := range []string{":" + book + " ", ":" + publisher + " "} {
			if !strings.Contains(edges[0].Statement, want) {
				t.Errorf("the edge statement does not match %q:\n%s", want, edges[0].Statement)
			}
		}
	}
}

// A label no identifier holds is the schema's fault, not the data's: both calls
// refuse before they emit, and a caller reads the derivation's issue through the
// error.
func TestBatchQueries_ALabelNoIdentifierHoldsIsRefused(t *testing.T) {
	t.Parallel()
	_, s, v := setupWrite(t, "write_basic.yammm")
	snap := buildGraphResult(t, s, v, map[string][]map[string]any{
		"Publisher": {{"publisher_id": "iss1", "name": "Test Publisher"}},
	})
	a := New(WithLabelSeparator("-"))
	for name, call := range map[string]func() (int, error){
		"BatchNodeQueries": func() (int, error) { q, err := a.BatchNodeQueries(t.Context(), snap); return len(q), err },
		"BatchEdgeQueries": func() (int, error) { q, err := a.BatchEdgeQueries(t.Context(), snap); return len(q), err },
	} {
		n, err := call()
		if err == nil || n != 0 {
			t.Fatalf("%s: %d queries, error %v; want none and a refusal", name, n, err)
		}
		re, ok := errors.AsType[*diag.ResultError](err)
		if !ok || !re.Result.HasCode(E_NEO4J_INVALID_IDENTIFIER) {
			t.Errorf("%s: error %v carries no E_NEO4J_INVALID_IDENTIFIER", name, err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error %v is marked ErrUnrepresentable", name, err)
		}
		if !strings.HasPrefix(err.Error(), "label the schema's types: ") {
			t.Errorf("%s: error %q does not name the derivation", name, err)
		}
	}
}
