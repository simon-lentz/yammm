package claude_plugin_test

import (
	"testing"
)

// TestExpressions_SchemaCompilation verifies all expressions.md schemas load.
func TestExpressions_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []string{
		"testdata/expressions/operators.yammm",
		"testdata/expressions/operators_extended.yammm",
		"testdata/expressions/pipelines.yammm",
		"testdata/expressions/nil_handling.yammm",
		"testdata/expressions/composed.yammm",
		"testdata/expressions/builtins_extended.yammm",
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

// TestExpressions_ExtendedOperators tests operators documented in expressions.md
// that were not previously covered: &&, ^, in, !~, ternary ?{}, indexing.
func TestExpressions_ExtendedOperators(t *testing.T) {
	t.Parallel()

	data := "testdata/expressions/data.json"
	v := loadSchema(t, "testdata/expressions/operators_extended.yammm")

	t.Run("valid_all_operators", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord")
		assertValid(t, v, "ExtOpRecord", records[0])
	})

	t.Run("invalid_logical_and", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_and")
		// x=-1, y=15: both_positive fails (x > 0 is false, && short-circuits)
		// product_positive fails: -1 * 15 = -15 (not > 0)
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "both_positive", "product_positive")
	})

	t.Run("invalid_xor", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_xor")
		// x=20, y=20: both > 10, XOR of (true ^ true) = false
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "exactly_one_big")
	})

	t.Run("invalid_membership", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_membership")
		// status="unknown", not in ["active", "pending", "closed"]
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "valid_status")
	})

	t.Run("invalid_negated_match", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_negated_match")
		// name="12345": not_numeric_name fails (matches /^[0-9]+$/, so !~ is false)
		// name_starts_upper also fails ("1" does not match /^[A-Z]$/)
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "not_numeric_name", "name_starts_upper")
	})

	t.Run("invalid_ternary", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_ternary")
		// score=-5, not nil so ternary evaluates else: -5 >= 0 is false
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "score_check")
	})

	t.Run("invalid_indexing", func(t *testing.T) {
		t.Parallel()
		records := loadTestData(t, data, "ExtOpRecord__invalid_indexing")
		// name="alice", name[0,1]="a", /^[A-Z]$/ fails on lowercase
		assertInvariantFails(t, v, "ExtOpRecord", records[0], "name_starts_upper")
	})
}

// TestExpressions_BuiltinsExtended tests built-in functions documented in
// expressions.md that were not previously covered in e2e/claude-plugin:
// Sort, Reverse, Flatten, Compact, Unique, First, Last, Abs, Floor, Ceil,
// Round, Min, Max, Compare, Substring, Match, TrimPrefix, TrimSuffix, Join,
// Replace, TypeOf, With, AllOrNone, Count.
func TestExpressions_BuiltinsExtended(t *testing.T) {
	t.Parallel()

	data := "testdata/expressions/data.json"
	v := loadSchema(t, "testdata/expressions/builtins_extended.yammm")

	t.Run("valid_all_builtins", func(t *testing.T) {
		t.Parallel()
		// name="Alice", value=3.7
		// All literal invariants use hardcoded inputs and pass by construction.
		// Field-based invariants:
		//   with_binds: "Alice" -> With -> Upper -> Len = 5 > 0 ✓
		//   typeof_string: TypeOf("Alice") = "string" == "string" ✓
		//   value_bounded: Floor(3.7)=3.0 <= 3.7 && 3.7 <= Ceil(3.7)=4.0 ✓
		records := loadTestData(t, data, "BuiltinRecord")
		assertValid(t, v, "BuiltinRecord", records[0])
	})
}
