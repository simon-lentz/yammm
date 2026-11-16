package cypher

import (
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, result := schema.Load(t.Context(), filepath.Join("testdata", name))
	if err := result.Err(); err != nil {
		t.Fatalf("schema %s has errors: %v", name, err)
	}
	return s
}

// constraintStatements returns the statement of each constraint c emits for
// s, and nil when the result is not OK or there is none.
func constraintStatements(c Config, s schema.Schema) ([]string, diag.Result) {
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
func indexStatements(c Config, s schema.Schema) ([]string, diag.Result) {
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
