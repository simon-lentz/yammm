package neo4j

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// A schema name and a type name that each hold the separator compose one label
// from two pairs: schema "geo" with Regions__Region and schema "geo__Regions"
// with Region both render geo__Regions__Region.
//
// The refusal is what stops the write path from putting two identities on one
// label, so both the diagnostic and the refusal are pinned here. Without
// shapeFor's collector.Collect(mapping.LabelCollisionIssue(...)) call, shapeFor
// builds a shape with one of the two types silently dropped.
func TestLabelCollision_TwoNamePairsComposeOneLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.Load(ctx, filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}

	a := cypher.Config{}
	detect := a.CheckLabels(s)
	if !detect.HasCode(cypher.E_NEO4J_LABEL_COLLISION) {
		t.Errorf("CheckLabels reported no collision for two name pairs rendering one label")
	}

	shape, shapeRes := shapeFor(a, s)
	if shape != nil {
		t.Errorf("shapeFor built a shape over a colliding closure (%d types); the collision must refuse",
			len(shape.types))
	}
	if !shapeRes.HasCode(cypher.E_NEO4J_LABEL_COLLISION) {
		t.Errorf("shapeFor refused without reporting %s", cypher.E_NEO4J_LABEL_COLLISION)
	}
}

// Both write calls refuse a closure whose two name pairs compose one label, as
// shapeFor does: the diagnostic reaches the caller through the error, and no
// query is built that puts two identities on one label.
func TestBatchQueries_AClosureWhoseLabelsCollideIsRefused(t *testing.T) {
	t.Parallel()
	s, v := loadSchemaAndValidator(t, filepath.Join("labelcollide", "entry.yammm"))
	snap := buildGraphResult(t, s, v, nil)
	a := cypher.Config{}
	for name, call := range map[string]func() (int, error){
		"NodeQueries": func() (int, error) { q, err := NodeQueries(t.Context(), a, snap); return len(q), err },
		"EdgeQueries": func() (int, error) { q, err := EdgeQueries(t.Context(), a, snap); return len(q), err },
	} {
		n, err := call()
		if err == nil || n != 0 {
			t.Fatalf("%s: %d queries, error %v; want none and a refusal", name, n, err)
		}
		re, ok := errors.AsType[*diag.ResultError](err)
		if !ok || !re.Result.HasCode(cypher.E_NEO4J_LABEL_COLLISION) {
			t.Errorf("%s: error %v carries no E_NEO4J_LABEL_COLLISION", name, err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error %v is marked ErrUnrepresentable", name, err)
		}
	}
}

// The constraint and index generators refuse a closure whose two name pairs
// compose one label, as the write path does: each emits nothing and reports
// the collision, rather than DDL that puts two identities on one label.
func TestGenerators_AClosureWhoseLabelsCollideIsRefused(t *testing.T) {
	t.Parallel()

	s, res := schema.Load(t.Context(), filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}
	constraints, constraintResult := cypher.Config{}.Constraints(s)
	if constraints != nil || !constraintResult.HasCode(cypher.E_NEO4J_LABEL_COLLISION) {
		t.Errorf("Constraints returned %d statements and %s; want none and the collision", len(constraints), constraintResult)
	}
	indexes, indexResult := cypher.Config{}.Indexes(s)
	if indexes != nil || !indexResult.HasCode(cypher.E_NEO4J_LABEL_COLLISION) {
		t.Errorf("Indexes returned %d statements and %s; want none and the collision", len(indexes), indexResult)
	}
}
