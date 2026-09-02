package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) *schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, result := schema.Load(context.Background(), filepath.Join("testdata", name))
	if err := result.Err(); err != nil {
		t.Fatalf("schema %s has errors: %v", name, err)
	}
	return s
}

// loadSchemaAndValidator loads a test schema and returns the schema + validator.
func loadSchemaAndValidator(t *testing.T, name string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	s := loadSchema(t, name)
	return s, instance.NewValidator(s)
}

// setupWrite loads a fixture and prepares the full write-path harness:
// a default adapter, the sealed schema, its validator, and the graph shape.
func setupWrite(t *testing.T, fixture string) (*Adapter, *schema.Schema, *instance.Validator, *GraphShape) {
	t.Helper()
	s, v := loadSchemaAndValidator(t, fixture)
	a := New()
	shape, result := a.ShapeForSchema(context.Background(), s)
	if err := result.Err(); err != nil {
		t.Fatalf("ShapeForSchema(%s): %v", fixture, err)
	}
	return a, s, v, shape
}

// buildGraphResult validates instances and builds a graph.Snapshot.
func buildGraphResult(t *testing.T, s *schema.Schema, v *instance.Validator, instances map[string][]map[string]any) *graph.Snapshot {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)
	for typeName, records := range instances {
		for _, props := range records {
			valid, valResult := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
			if !valResult.OK() {
				t.Fatalf("validate %s failed: %v", typeName, valResult)
			}
			addResult := g.Add(ctx, valid)
			if err := addResult.Err(); err != nil {
				t.Fatalf("graph.Add %s issues: %v", typeName, err)
			}
		}
	}
	return g.Snapshot()
}

// typeID resolves a type name against the schema. GraphShape is keyed by
// identity, so a test that names a type in its fixture resolves it here and
// fails loudly when the fixture and the schema disagree.
func typeID(t *testing.T, s *schema.Schema, name string) schema.TypeID {
	t.Helper()
	ty, ok := s.Type(name)
	if !ok {
		t.Fatalf("schema has no type %q", name)
	}
	return ty.ID()
}
