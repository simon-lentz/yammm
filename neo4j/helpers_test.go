package neo4j

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/graph"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/cypher"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, loadErr := schema.Load(context.Background(), filepath.Join("testdata", name))
	if err := loadErr; err != nil {
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
	shape, err := shapeFor(a, s)
	if err != nil {
		t.Fatalf("shapeFor: %s", err)
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
			valid, validateErr := v.ValidateOne(ctx, typeName, instance.RawInstance{Properties: props})
			if validateErr != nil {
				t.Fatalf("validate %s failed: %v", typeName, validateErr)
			}
			addErr := g.Add(ctx, valid)
			if err := addErr; err != nil {
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
// s, and nil when the call fails or there is none, beside every issue it drew.
func constraintStatements(tb testing.TB, c cypher.Config, s schema.Schema) ([]string, diag.Result) {
	tb.Helper()
	constraints, result := diagtest.Capture(tb, func(warn func(diag.Issue)) ([]cypher.Constraint, error) {
		return c.Constraints(s, cypher.WithWarnings(warn))
	})
	if (diagtest.Err(result) != nil) || len(constraints) == 0 {
		return nil, result
	}
	stmts := make([]string, len(constraints))
	for i, con := range constraints {
		stmts[i] = con.Statement
	}
	return stmts, result
}

// indexStatements returns the statement of each index c emits for s, and nil
// when the call fails or there is none, beside the failures it drew.
func indexStatements(tb testing.TB, c cypher.Config, s schema.Schema) ([]string, diag.Result) {
	tb.Helper()
	indexes, result := diagtest.Issues(tb, func() ([]cypher.Index, error) {
		return c.Indexes(s)
	})
	if (diagtest.Err(result) != nil) || len(indexes) == 0 {
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
	s, loadErr := schema.LoadString(t.Context(), src, "inline.yammm")
	if err := loadErr; err != nil {
		t.Fatalf("fixture is not valid yammm: %v", err)
	}
	return s
}
