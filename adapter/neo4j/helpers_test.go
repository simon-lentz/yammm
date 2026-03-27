package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) *schema.Schema {
	t.Helper()
	s, result, err := load.Load(context.Background(), filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("load.Load(%s) failed: %v", name, err)
	}
	if !result.OK() {
		t.Fatalf("schema %s has errors: %v", name, result)
	}
	return s
}

// loadSchemaAndValidator loads a test schema and returns the schema + validator.
func loadSchemaAndValidator(t *testing.T, name string) (*schema.Schema, *instance.Validator) {
	t.Helper()
	s := loadSchema(t, name)
	return s, instance.NewValidator(s)
}

// buildGraphResult validates instances and builds a graph.Result snapshot.
func buildGraphResult(t *testing.T, s *schema.Schema, v *instance.Validator, instances map[string][]map[string]any) *graph.Result {
	t.Helper()
	ctx := context.Background()
	g := graph.New(s)
	for typeName, records := range instances {
		for _, props := range records {
			valid, failure, err := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
			if err != nil {
				t.Fatalf("validate %s: %v", typeName, err)
			}
			if failure != nil {
				t.Fatalf("validate %s failed: %v", typeName, failure.Result.Messages())
			}
			result, err := g.Add(ctx, valid)
			if err != nil {
				t.Fatalf("graph.Add %s: %v", typeName, err)
			}
			if !result.OK() {
				t.Fatalf("graph.Add %s issues: %v", typeName, result.Messages())
			}
		}
	}
	return g.Snapshot()
}
