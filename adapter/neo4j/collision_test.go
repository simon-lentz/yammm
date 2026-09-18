package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/schema"
)

// The label collision two SCHEMA names produce.
//
// A rendered TYPE name can no longer collide: every snapshot root is a type the
// entry schema can name, and two such types never render alike. A schema NAME
// still can, because it is a free-form string checked only for emptiness.

// Two SCHEMA names that sanitize alike render one label for every same-named
// type they declare, which is the collision the type-name argument does not
// cover: a schema name is a free-form string checked only for emptiness, while
// SanitizeIdentifier is the identity on the DSL type-name production.
//
// The refusal is what stops the write path from putting two identities on one
// label, so both the diagnostic and the refusal are pinned here. Mutation:
// no-op either collector.Collect(labelCollisionIssue(...)) call and
// ShapeForSchema builds a shape with one of the two types silently dropped.
func TestLabelCollision_TwoSchemaNamesSanitizeAlike(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	s, res := schema.Load(ctx, filepath.Join("testdata", "labelcollide", "entry.yammm"))
	if res.HasErrors() {
		t.Fatalf("load: %s", res.String())
	}

	a := New()
	detect := a.DetectLabelCollisions(ctx, s)
	if !detect.HasCode(E_NEO4J_LABEL_COLLISION) {
		t.Errorf("DetectLabelCollisions reported no collision for two schema names rendering one label")
	}

	shape, shapeRes := a.ShapeForSchema(ctx, s)
	if shape != nil {
		t.Errorf("ShapeForSchema built a shape over a colliding closure (%d types); the collision must refuse",
			len(shape.Types))
	}
	if !shapeRes.HasCode(E_NEO4J_LABEL_COLLISION) {
		t.Errorf("ShapeForSchema refused without reporting %s", E_NEO4J_LABEL_COLLISION)
	}
}
