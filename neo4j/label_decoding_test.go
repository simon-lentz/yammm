package neo4j

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// Schema "geo" with Regions__Region and schema "geo__Regions" with Region would
// both write geo__Regions__Region. The label rule refuses the schema name that
// holds the separator, so no label reads two ways, and the write path's shape
// is refused with the rule's issue.
func TestShapeFor_RefusesALabelThatDoesNotDecode(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.Load(ctx, filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}

	a := cypher.Config{}
	detect := a.CheckLabels(s)
	if !detect.HasCode(cypher.E_NEO4J_INVALID_IDENTIFIER) || !strings.Contains(detect.String(), `schema "geo__Regions"`) {
		t.Errorf("CheckLabels = %v, want schema geo__Regions refused", detect)
	}

	shape, shapeRes := shapeFor(a, s)
	if shape != nil {
		t.Errorf("shapeFor built a shape over a refused closure (%d types)", len(shape.types))
	}
	if shapeRes.String() != detect.String() {
		t.Errorf("shapeFor refused with\n%s\nwant CheckLabels' result\n%s", shapeRes, detect)
	}
}

// Both write calls refuse a closure whose labels break the rule, as shapeFor
// does: the diagnostic reaches the caller through the error, and no query is
// built.
func TestBatchQueries_AClosureWhoseLabelsDoNotDecodeIsRefused(t *testing.T) {
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
		if !ok || !re.Result.HasCode(cypher.E_NEO4J_INVALID_IDENTIFIER) {
			t.Errorf("%s: error %v carries no E_NEO4J_INVALID_IDENTIFIER", name, err)
		}
		if errors.Is(err, ErrUnrepresentable) {
			t.Errorf("%s: error %v is marked ErrUnrepresentable", name, err)
		}
	}
}

// The constraint and index generators refuse the closure as the write path
// does: each emits nothing and reports the rule's issue.
func TestGenerators_AClosureWhoseLabelsDoNotDecodeIsRefused(t *testing.T) {
	t.Parallel()

	s, res := schema.Load(t.Context(), filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}
	constraints, constraintResult := cypher.Config{}.Constraints(s)
	if constraints != nil || !constraintResult.HasCode(cypher.E_NEO4J_INVALID_IDENTIFIER) {
		t.Errorf("Constraints returned %d statements and %s; want none and the refusal", len(constraints), constraintResult)
	}
	indexes, indexResult := cypher.Config{}.Indexes(s)
	if indexes != nil || !indexResult.HasCode(cypher.E_NEO4J_INVALID_IDENTIFIER) {
		t.Errorf("Indexes returned %d statements and %s; want none and the refusal", len(indexes), indexResult)
	}
}
