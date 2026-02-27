package claude_plugin_test

import (
	"testing"
)

// TestExpressions_SchemaCompilation verifies all expressions.md schemas load.
func TestExpressions_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []string{
		"testdata/expressions/operators.yammm",
		"testdata/expressions/pipelines.yammm",
		"testdata/expressions/nil_handling.yammm",
		"testdata/expressions/composed.yammm",
	}

	for _, path := range schemas {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			loadSchema(t, path)
		})
	}
}

// TestExpressions_Operators tests arithmetic, comparison, logical, and pattern operators.
func TestExpressions_Operators(t *testing.T) {
	t.Parallel()

	data := "testdata/expressions/data.json"
	v := loadSchema(t, "testdata/expressions/operators.yammm")

	t.Run("valid_ops", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "OpRecord")
		assertValid(t, v, "OpRecord", records[0])
	})

	t.Run("invalid_modulo", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "OpRecord__invalid_modulo")
		// a=3, 3%2=1 != 0 (modulo_check fails)
		// a=3, b=5, a < b holds
		// BUT a=3 is odd so modulo_check and possibly a_less_b too... a=3 < b=5 is fine.
		assertInvariantFails(t, v, "OpRecord", records[0], "modulo_check")
	})
}

// TestExpressions_Pipelines tests pipeline chains, lambdas, and reduce.
func TestExpressions_Pipelines(t *testing.T) {
	t.Parallel()

	data := "testdata/expressions/data.json"
	v := loadSchema(t, "testdata/expressions/pipelines.yammm")

	t.Run("valid_with_csv", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "PipeRecord")
		assertValid(t, v, "PipeRecord", records[0])
	})

	t.Run("valid_without_csv", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "PipeRecord")
		assertValid(t, v, "PipeRecord", records[1])
	})

	t.Run("invalid_csv_empty_item", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "PipeRecord__invalid_csv")
		assertInvariantFails(t, v, "PipeRecord", records[0], "csv_items_check")
	})
}

// TestExpressions_NilHandling tests nil guards, Default, Coalesce, Then, Lest, IsNil.
func TestExpressions_NilHandling(t *testing.T) {
	t.Parallel()

	data := "testdata/expressions/data.json"
	v := loadSchema(t, "testdata/expressions/nil_handling.yammm")

	t.Run("valid_with_nickname", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "NilRecord")
		assertValid(t, v, "NilRecord", records[0])
	})

	t.Run("valid_without_optionals", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "NilRecord")
		assertValid(t, v, "NilRecord", records[1])
	})

	t.Run("invalid_empty_nickname", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "NilRecord__invalid_empty_nickname")
		// Empty string "" is non-nil, so nil guards fail (Len("") = 0),
		// Then body evaluates (Upper("") has Len 0), and Lest passes through ""
		assertInvariantFails(t, v, "NilRecord", records[0],
			"nil_guard_underscore", "nil_guard_keyword",
			"nickname_upper", "nickname_fallback")
	})
}

// TestExpressions_Composed tests compilation of composed expression schemas.
// NOTE: Composed invariants using composition collections require graph-layer testing.
func TestExpressions_Composed(t *testing.T) {
	t.Parallel()
	loadSchema(t, "testdata/expressions/composed.yammm")
}
