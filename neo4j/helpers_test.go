package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, result := schema.Load(context.Background(), filepath.Join("testdata", name))
	if err := result.Err(); err != nil {
		t.Fatalf("schema %s has errors: %v", name, err)
	}
	return s
}

// loadSchemaAndValidator loads a test schema and returns the schema + validator.
func loadSchemaAndValidator(t *testing.T, name string) (schema.Schema, *instance.Validator) {
	t.Helper()
	s := loadSchema(t, name)
	return s, instance.NewValidator(s)
}

// mustShape returns the shape the write path derives for s under a.
func mustShape(t *testing.T, a cypher.Config, s schema.Schema) *graphShape {
	t.Helper()
	shape, res := shapeFor(a, s)
	if res.HasErrors() {
		t.Fatalf("shapeFor: %s", res)
	}
	return shape
}

// buildGraphResult validates instances and builds a graph.Snapshot.
func buildGraphResult(t *testing.T, s schema.Schema, v *instance.Validator, instances map[string][]map[string]any) graph.Snapshot {
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

// typeID resolves a type name against the schema. A shape is keyed by identity,
// so a test that names a type in its fixture resolves it here and fails loudly
// when the fixture and the schema disagree.
func typeID(t *testing.T, s schema.Schema, name string) schema.TypeID {
	t.Helper()
	ty, ok := s.Type(name)
	if !ok {
		t.Fatalf("schema has no type %q", name)
	}
	return ty.ID()
}

// schemaType resolves a type name against the schema, failing the test when
// the schema has no such type.
func schemaType(t *testing.T, s schema.Schema, name string) schema.Type {
	t.Helper()
	ty, ok := s.Type(name)
	if !ok {
		t.Fatalf("schema has no type %q", name)
	}
	return ty
}

// constraintStatements returns the statement of each constraint c emits for
// s, and nil when the result is not OK or there is none.
func constraintStatements(c cypher.Config, s schema.Schema) ([]string, diag.Result) {
	constraints, result := c.Constraints(s)
	if !result.OK() || len(constraints) == 0 {
		return nil, result
	}
	stmts := make([]string, len(constraints))
	for i, con := range constraints {
		stmts[i] = con.Statement
	}
	return stmts, result
}

// indexStatements returns the statement of each index c emits for s, and nil
// when the result is not OK or there is none.
func indexStatements(c cypher.Config, s schema.Schema) ([]string, diag.Result) {
	indexes, result := c.Indexes(s)
	if !result.OK() || len(indexes) == 0 {
		return nil, result
	}
	stmts := make([]string, len(indexes))
	for i, idx := range indexes {
		stmts[i] = idx.Statement
	}
	return stmts, result
}

// loadInline builds a sealed schema from source, failing the test if it does
// not load.
func loadInline(t *testing.T, src string) schema.Schema {
	t.Helper()
	s, result := schema.LoadString(t.Context(), src, "inline.yammm")
	if err := result.Err(); err != nil {
		t.Fatalf("fixture is not valid yammm: %v", err)
	}
	return s
}
