package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// A schema name and a type name that each hold the separator compose one label
// from two pairs: schema "geo" with Regions__Region and schema "geo__Regions"
// with Region both render geo__Regions__Region.
//
// The refusal is what stops the write path from putting two identities on one
// label, so both the diagnostic and the refusal are pinned here. Mutation:
// no-op either collector.Collect(labelCollisionIssue(...)) call and
// ShapeForSchema builds a shape with one of the two types silently dropped.
func TestLabelCollision_TwoNamePairsComposeOneLabel(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.Load(ctx, filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}

	a := New()
	detect := a.DetectLabelCollisions(ctx, s)
	if !detect.HasCode(E_NEO4J_LABEL_COLLISION) {
		t.Errorf("DetectLabelCollisions reported no collision for two name pairs rendering one label")
	}

	shape, shapeRes := a.ShapeForSchema(ctx, s)
	if shape != nil {
		t.Errorf("ShapeForSchema built a shape over a colliding closure (%d types); the collision must refuse",
			len(shape.types))
	}
	if !shapeRes.HasCode(E_NEO4J_LABEL_COLLISION) {
		t.Errorf("ShapeForSchema refused without reporting %s", E_NEO4J_LABEL_COLLISION)
	}
}
