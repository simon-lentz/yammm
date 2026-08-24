package neo4j_test

import (
	"context"
	"maps"
	"strings"
	"testing"
	"time"

	"github.com/simon-lentz/yammm/adapter/neo4j"
	"github.com/simon-lentz/yammm/schema"
)

func loadEdgeRelation(t *testing.T) *schema.Relation {
	t.Helper()
	s, res := schema.Load(context.Background(), "testdata/edge_typed_props.yammm")
	if res.HasErrors() {
		t.Fatalf("load: %v", res.Err())
	}
	st, ok := s.Type("Source")
	if !ok {
		t.Fatal("Source type not found")
	}
	rels := st.AssociationsSlice()
	if len(rels) != 1 {
		t.Fatalf("want 1 association on Source, got %d", len(rels))
	}
	return rels[0]
}

// TestCoerceRelProps_DoesNotMutateItsInput pins the export's copying contract:
// the caller's map keeps every value it had, and the result is storage the
// caller may edit without reaching the input.
func TestCoerceRelProps_DoesNotMutateItsInput(t *testing.T) {
	t.Parallel()
	rel := loadEdgeRelation(t)
	in := map[string]any{
		"observed_at": "2024-01-01T00:00:00Z",
		"weight":      int64(5),
		"undeclared":  "passthrough",
	}
	snapshot := maps.Clone(in)

	out, err := neo4j.CoerceRelProps(in, rel)
	if err != nil {
		t.Fatalf("CoerceRelProps: %v", err)
	}
	if !maps.Equal(in, snapshot) {
		t.Errorf("input mutated: %v", in)
	}
	if _, isTime := out["observed_at"].(time.Time); !isTime {
		t.Errorf("observed_at should be time.Time, got %T", out["observed_at"])
	}
	if _, isFloat := out["weight"].(float64); !isFloat {
		t.Errorf("weight should be float64, got %T", out["weight"])
	}
	out["extra"] = "x"
	if _, leaked := in["extra"]; leaked {
		t.Error("writing to the result reached the input")
	}
}

// TestCoerceRelProps_NilRelationReturnsAnIndependentCopy pins that the
// pass-through case copies too, so the contract does not depend on whether
// the relation declares properties.
func TestCoerceRelProps_NilRelationReturnsAnIndependentCopy(t *testing.T) {
	t.Parallel()
	in := map[string]any{"x": "y"}
	out, err := neo4j.CoerceRelProps(in, nil)
	if err != nil {
		t.Fatalf("CoerceRelProps: %v", err)
	}
	if !maps.Equal(in, out) {
		t.Errorf("values changed under a nil relation: %v", out)
	}
	out["x"] = "z"
	if in["x"] != "y" {
		t.Error("the result shares storage with the input")
	}
	if got, err := neo4j.CoerceRelProps(nil, nil); err != nil || got != nil {
		t.Errorf("a nil map should return nil, got %v, %v", got, err)
	}
}

// TestCoerceRelProps_ReportsTheSortedFirstFailure pins the deterministic
// error the internal path already guarantees.
func TestCoerceRelProps_ReportsTheSortedFirstFailure(t *testing.T) {
	t.Parallel()
	rel := loadEdgeRelation(t)
	_, err := neo4j.CoerceRelProps(map[string]any{
		"weight":      "heavy",
		"observed_at": "not-a-timestamp",
	}, rel)
	if err == nil {
		t.Fatal("expected a coercion error")
	}
	if want := `property "observed_at"`; !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not name the sorted-first failing property %s", err, want)
	}
}
