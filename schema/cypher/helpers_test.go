package cypher

import (
	"path/filepath"
	"testing"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/diagtest"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema"
)

// loadSchema loads a test schema from testdata and returns the sealed schema.
func loadSchema(t *testing.T, name string) schema.Schema {
	t.Helper()
	yammmtest.RequireNoModuleRoot(t, schema.FindModuleRoot)
	s, loadErr := schema.Load(t.Context(), filepath.Join("testdata", name))
	if err := loadErr; err != nil {
		t.Fatalf("schema %s has errors: %v", name, err)
	}
	return s
}

// constraintStatements returns the statement of each constraint c emits for
// s, and nil when the call fails or there is none, beside every issue it drew.
func constraintStatements(tb testing.TB, c Config, s schema.Schema) ([]string, diag.Result) {
	tb.Helper()
	constraints, result := diagtest.Capture(tb, func(warn func(diag.Issue)) ([]Constraint, error) {
		return c.Constraints(s, WithWarnings(warn))
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
func indexStatements(tb testing.TB, c Config, s schema.Schema) ([]string, diag.Result) {
	tb.Helper()
	indexes, result := diagtest.Issues(tb, func() ([]Index, error) {
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
