package eval_test

import (
	"log/slog"
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEvaluator_Literals(t *testing.T) {
	tests := []struct {
		name string
		expr expr.Expression
		want any
	}{
		{"int", lit(int64(42)), int64(42)},
		{"float", lit(3.14), 3.14},
		{"string", lit("hello"), "hello"},
		{"bool_true", lit(true), true},
		{"bool_false", lit(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, tt.expr, tt.want)
		})
	}
}

func TestEvaluator_Arithmetic(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		left  any
		right any
		want  any
	}{
		{"add_int", "+", int64(2), int64(3), int64(5)},
		{"add_float", "+", 2.5, 3.5, 6.0},
		{"add_mixed", "+", int64(2), 3.5, 5.5},
		{"sub_int", "-", int64(10), int64(3), int64(7)},
		{"sub_float", "-", 10.5, 3.5, 7.0},
		{"mul_int", "*", int64(4), int64(5), int64(20)},
		{"mul_float", "*", 2.5, 4.0, 10.0},
		{"div_int", "/", int64(15), int64(3), int64(5)},
		{"div_int_truncates", "/", int64(10), int64(3), int64(3)},
		{"div_float", "/", 15.0, 2.0, 7.5},
		{"mod", "%", int64(17), int64(5), int64(2)},
		{"add_strings", "+", "hello", " world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, sx(tt.op, lit(tt.left), lit(tt.right)), tt.want)
		})
	}
}

func TestEvaluator_ArithmeticErrors(t *testing.T) {
	tests := []struct {
		name    string
		e       expr.Expression
		wantErr string
	}{
		{"divide_by_zero_int", sx("/", lit(int64(10)), lit(int64(0))), "division by zero"},
		{"mod_by_zero", sx("%", lit(int64(10)), lit(int64(0))), "modulo by zero"},
		{"mod_with_float", sx("%", lit(5.5), lit(2.0)), "integer operands"},
		// The unary-minus op token is "-x" (the compiler's spelling); a
		// non-numeric operand must fail in negate itself, not fall through
		// to "unknown function".
		{"negate_non_numeric", sx("-x", lit("not a number")), "-x of non-numeric"},
		{"sub_non_numeric", sx("-", lit("a"), lit("b")), "non-numeric"},
		{"mul_non_numeric", sx("*", lit("a"), lit("b")), "non-numeric"},
		{"add_bools", sx("+", lit(true), lit(false)), "non-numeric"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalErr(t, tt.e, tt.wantErr)
		})
	}
}

// TestEvaluator_Comparison is the single comparison table: int, float,
// string, and nil operands across all six operators.
func TestEvaluator_Comparison(t *testing.T) {
	tests := []struct {
		name  string
		op    string
		left  any
		right any
		want  bool
	}{
		{"eq_int_true", "==", int64(5), int64(5), true},
		{"eq_int_false", "==", int64(5), int64(6), false},
		{"neq_true", "!=", int64(5), int64(6), true},
		{"neq_false", "!=", int64(5), int64(5), false},
		{"lt_true", "<", int64(3), int64(5), true},
		{"lt_false", "<", int64(5), int64(3), false},
		{"lte_true", "<=", int64(3), int64(3), true},
		{"gt_true", ">", int64(5), int64(3), true},
		{"gte_true", ">=", int64(3), int64(3), true},

		{"str_eq_true", "==", "hello", "hello", true},
		{"str_eq_false", "==", "hello", "world", false},
		{"str_neq_true", "!=", "hello", "world", true},
		{"str_neq_false", "!=", "hello", "hello", false},
		{"str_lt_true", "<", "apple", "banana", true},
		{"str_lt_false", "<", "banana", "apple", false},
		{"str_lte_true", "<=", "apple", "apple", true},
		{"str_gt_true", ">", "banana", "apple", true},
		{"str_gte_true", ">=", "banana", "banana", true},

		{"float_eq_true", "==", 3.14, 3.14, true},
		{"float_eq_false", "==", 3.14, 2.71, false},
		{"float_lt_true", "<", 2.71, 3.14, true},
		{"float_lte_true", "<=", 3.14, 3.14, true},
		{"float_gt_true", ">", 3.14, 2.71, true},
		{"float_gte_true", ">=", 3.14, 3.14, true},

		{"nil_eq_nil", "==", nil, nil, true},
		{"value_neq_nil", "!=", int64(5), nil, true},
		// Equality with nil is nil-ness for every value kind: an instance,
		// which the total order cannot rank, still answers the null guard.
		{"map_neq_nil", "!=", map[string]any{"qty": int64(1)}, nil, true},
		{"map_eq_nil", "==", map[string]any{"qty": int64(1)}, nil, false},
		{"nil_neq_map", "!=", nil, map[string]any{"qty": int64(1)}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, sx(tt.op, lit(tt.left), lit(tt.right)), tt.want)
		})
	}
}

func TestEvaluator_Logical(t *testing.T) {
	tests := []struct {
		name string
		e    expr.Expression
		want bool
	}{
		{"and_true", sx("&&", lit(true), lit(true)), true},
		{"and_false", sx("&&", lit(true), lit(false)), false},
		{"or_true", sx("||", lit(false), lit(true)), true},
		{"or_false", sx("||", lit(false), lit(false)), false},
		{"not_true", sx("!", lit(false)), true},
		{"not_false", sx("!", lit(true)), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, tt.e, tt.want)
		})
	}
}

func TestEvaluator_LogicalErrors(t *testing.T) {
	tests := []struct {
		name    string
		e       expr.Expression
		wantErr string
	}{
		{"and_first_eval_error", sx("&&", sx("$", lit("undefined")), lit(true)), "undefined variable"},
		{"or_first_eval_error", sx("||", sx("$", lit("undefined")), lit(false)), "undefined variable"},
		{"not_nil", sx("!", lit(nil)), "boolean"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalErr(t, tt.e, tt.wantErr)
		})
	}
}

func TestEvaluator_And_ShortCircuit(t *testing.T) {
	// And short-circuits: if the first arg is false, the second is not
	// evaluated. The second arg would error (undefined variable) if reached.
	evalEq(t, sx("&&", lit(false), sx("$", lit("undefined"))), false)
}

func TestEvaluator_Or_ShortCircuit(t *testing.T) {
	// Or short-circuits: if the first arg is true, the second is not evaluated.
	evalEq(t, sx("||", lit(true), sx("$", lit("undefined"))), true)
}

func TestEvaluator_Ternary(t *testing.T) {
	t.Run("true_branch", func(t *testing.T) {
		evalEq(t, sx("?", lit(true), lit("yes"), lit("no")), "yes")
	})
	t.Run("false_branch", func(t *testing.T) {
		evalEq(t, sx("?", lit(false), lit("yes"), lit("no")), "no")
	})
}

func TestEvaluator_TernaryErrors(t *testing.T) {
	t.Run("too_few_args", func(t *testing.T) {
		evalErr(t, sx("?", lit(true), lit("yes")), "3 operands")
	})

	t.Run("condition_eval_error", func(t *testing.T) {
		evalErr(t, sx("?", sx("$", lit("undefined")), lit("yes"), lit("no")), "undefined variable")
	})

	t.Run("nil_condition", func(t *testing.T) {
		evalErr(t, sx("?", lit(nil), lit("yes"), lit("no")), "boolean")
	})
}

func TestEvaluator_Variables(t *testing.T) {
	ev := eval.NewEvaluator()

	t.Run("lookup_existing", func(t *testing.T) {
		scope := eval.EmptyScope().WithVar("x", 42)
		result, err := ev.Evaluate(sx("$", lit("x")), scope)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("lookup_undefined", func(t *testing.T) {
		evalErr(t, sx("$", lit("undefined")), "undefined variable")
	})

	t.Run("numeric_var_unset", func(t *testing.T) {
		// Unset numeric vars ($0, $1, ...) evaluate to nil, not an error.
		evalEq(t, sx("$", lit("0")), nil)
	})
}

func TestEvaluator_NumericVarSet(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope().WithVar("0", "first").WithVar("1", "second")

	t.Run("numeric_var_zero", func(t *testing.T) {
		result, err := ev.Evaluate(sx("$", lit("0")), scope)
		require.NoError(t, err)
		assert.Equal(t, "first", result)
	})

	t.Run("numeric_var_one", func(t *testing.T) {
		result, err := ev.Evaluate(sx("$", lit("1")), scope)
		require.NoError(t, err)
		assert.Equal(t, "second", result)
	})
}

func TestEvaluator_Variable_Self(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope().WithVar("self", map[string]any{"name": "Test", "value": int64(42)})

	result, err := ev.Evaluate(sx("$", lit("self")), scope)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestEvaluator_Properties(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.PropertyScopeFromMap(map[string]any{
		"name": "Alice",
		"age":  30,
	})

	t.Run("lookup_property", func(t *testing.T) {
		result, err := ev.Evaluate(sx("p", lit("name")), scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("case_insensitive", func(t *testing.T) {
		result, err := ev.Evaluate(sx("p", lit("NAME")), scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("undefined_property_returns_nil", func(t *testing.T) {
		// Missing optional properties evaluate to nil, enabling patterns like:
		//   age lest 0        (default value)
		//   age then age > 18 (conditional validation)
		result, err := ev.Evaluate(sx("p", lit("unknown")), scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestEvaluator_List(t *testing.T) {
	evalEq(t, list(int64(1), int64(2), int64(3)), []any{int64(1), int64(2), int64(3)})
}

func TestEvaluator_In(t *testing.T) {
	ints := list(int64(1), int64(2), int64(3))

	t.Run("found", func(t *testing.T) {
		evalEq(t, sx("in", lit(int64(2)), ints), true)
	})

	t.Run("not_found", func(t *testing.T) {
		evalEq(t, sx("in", lit(int64(5)), ints), false)
	})

	t.Run("string_elements", func(t *testing.T) {
		evalEq(t, sx("in", lit("banana"), list("apple", "banana", "cherry")), true)
	})

	t.Run("non_array_errors", func(t *testing.T) {
		evalErr(t, sx("in", lit("world"), lit("hello world")), "slice or array")
	})
}

func TestEvaluator_Match(t *testing.T) {
	pattern := regexp.MustCompile(`^hello`)

	t.Run("match_true", func(t *testing.T) {
		evalEq(t, sx("=~", lit("hello world"), lit(pattern)), true)
	})

	t.Run("match_false", func(t *testing.T) {
		evalEq(t, sx("=~", lit("goodbye world"), lit(pattern)), false)
	})

	t.Run("not_match", func(t *testing.T) {
		evalEq(t, sx("!~", lit("goodbye world"), lit(pattern)), true)
	})
}

func TestEvaluator_MatchWithTypeChecker(t *testing.T) {
	t.Run("match_integer", func(t *testing.T) {
		evalEq(t, sx("=~", lit(int64(42)), expr.DatatypeLiteral("integer")), true)
	})

	t.Run("no_match_integer", func(t *testing.T) {
		evalEq(t, sx("=~", lit("not-an-int"), expr.DatatypeLiteral("integer")), false)
	})

	t.Run("not_match_with_type", func(t *testing.T) {
		evalEq(t, sx("!~", lit("not-a-number"), expr.DatatypeLiteral("float")), true)
	})

	t.Run("match_invalid_right_operand", func(t *testing.T) {
		evalErr(t, sx("=~", lit("test"), lit(int64(42))), "regexp or type checker")
	})
}

func TestEvaluator_Negate(t *testing.T) {
	tests := []struct {
		name string
		val  any
		want any
	}{
		{"int", int64(5), int64(-5)},
		{"float", 3.14, -3.14},
		{"negative_int", int64(-10), int64(10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, sx("-x", lit(tt.val)), tt.want)
		})
	}
}

func TestEvaluator_EvaluateBool(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("true", func(t *testing.T) {
		result, err := ev.EvaluateBool(lit(true), scope)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("false", func(t *testing.T) {
		result, err := ev.EvaluateBool(lit(false), scope)
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("non_bool_error", func(t *testing.T) {
		_, err := ev.EvaluateBool(lit("string"), scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "expected boolean")
	})
}

func TestEvaluator_SliceConcat(t *testing.T) {
	evalEq(t,
		sx("+", list(int64(1), int64(2)), list(int64(3), int64(4))),
		[]any{int64(1), int64(2), int64(3), int64(4)})
}

// TestEvaluator_MemberAccess_EdgeCases is the canonical member-access table:
// nil receiver, exact and case-insensitive map keys, missing keys, and
// non-map receivers.
func TestEvaluator_MemberAccess_EdgeCases(t *testing.T) {
	t.Run("access_nil_returns_nil", func(t *testing.T) {
		evalEq(t, sx(".", lit(nil), lit("field")), nil)
	})

	t.Run("access_map_exact_match", func(t *testing.T) {
		m := immutable.WrapMap(map[string]any{"name": "Alice", "age": int64(30)})
		evalEq(t, sx(".", lit(m), lit("name")), "Alice")
	})

	t.Run("access_map_case_insensitive", func(t *testing.T) {
		m := immutable.WrapMap(map[string]any{"Name": "Alice"})
		evalEq(t, sx(".", lit(m), lit("name")), "Alice")
	})

	t.Run("access_map_missing_key_returns_nil", func(t *testing.T) {
		m := immutable.WrapMap(map[string]any{"name": "Alice"})
		evalEq(t, sx(".", lit(m), lit("nonexistent")), nil)
	})

	t.Run("access_non_map_errors", func(t *testing.T) {
		evalErr(t, sx(".", lit(int64(42)), lit("field")), "cannot access member")
	})
}

// TestEvaluator_SliceAccess_EdgeCases is the canonical @-indexing table:
// slices, strings (rune-based), nil, out-of-bounds, negative, and bad
// index/receiver types.
func TestEvaluator_SliceAccess_EdgeCases(t *testing.T) {
	t.Run("index_slice", func(t *testing.T) {
		evalEq(t, sx("@", lit([]any{"a", "b", "c"}), lit(int64(1))), "b")
	})

	t.Run("index_built_list", func(t *testing.T) {
		evalEq(t, sx("@", list("a", "b", "c"), lit(int64(1))), "b")
	})

	t.Run("index_string", func(t *testing.T) {
		evalEq(t, sx("@", lit("hello"), lit(int64(1))), "e")
	})

	t.Run("index_string_unicode", func(t *testing.T) {
		evalEq(t, sx("@", lit("日本語"), lit(int64(1))), "本")
	})

	t.Run("index_nil_returns_nil", func(t *testing.T) {
		evalEq(t, sx("@", lit(nil), lit(int64(0))), nil)
	})

	t.Run("index_out_of_bounds_returns_nil", func(t *testing.T) {
		evalEq(t, sx("@", lit([]any{"a", "b"}), lit(int64(100))), nil)
	})

	t.Run("index_negative_returns_nil", func(t *testing.T) {
		evalEq(t, sx("@", lit([]any{"a", "b"}), lit(int64(-1))), nil)
	})

	t.Run("index_non_integer_errors", func(t *testing.T) {
		evalErr(t, sx("@", lit([]any{"a", "b"}), lit("not an index")), "slice index must be integer")
	})

	t.Run("index_non_indexable_errors", func(t *testing.T) {
		evalErr(t, sx("@", lit(int64(42)), lit(int64(0))), "cannot index")
	})
}

// TestEvaluator_StringIndexing_UTF8 verifies that string indexing operates on
// runes (Unicode code points), not bytes, per the SPEC rule that string
// positions count runes.
func TestEvaluator_StringIndexing_UTF8(t *testing.T) {
	tests := []struct {
		name  string
		str   string
		index int64
		want  any // string if valid, nil if out of bounds
	}{
		// ASCII strings (sanity check)
		{"ascii_first", "hello", 0, "h"},
		{"ascii_middle", "hello", 2, "l"},
		{"ascii_last", "hello", 4, "o"},

		// Multi-byte UTF-8: café has 4 runes, but 5 bytes (é is 2 bytes)
		{"multibyte_ascii_part", "café", 0, "c"},
		{"multibyte_accent", "café", 3, "é"}, // The accent character, not a truncated byte

		// Emoji (4 bytes each)
		{"emoji_first", "🎉test", 0, "🎉"},
		{"emoji_after", "🎉test", 1, "t"},
		{"emoji_only", "🎉", 0, "🎉"},

		// Kanji (3 bytes each)
		{"kanji_first", "日本語", 0, "日"},
		{"kanji_middle", "日本語", 1, "本"},
		{"kanji_last", "日本語", 2, "語"},

		// Mixed content
		{"mixed_start", "A日B", 0, "A"},
		{"mixed_kanji", "A日B", 1, "日"},
		{"mixed_end", "A日B", 2, "B"},

		// Out of bounds (using rune count, not byte count)
		{"kanji_oob", "日本語", 3, nil},         // 3 runes, index 3 is OOB
		{"emoji_oob", "🎉", 1, nil},           // 1 rune, index 1 is OOB
		{"cafe_oob_rune", "café", 4, nil},    // 4 runes, index 4 is OOB
		{"negative_index", "hello", -1, nil}, // Negative always OOB
		{"empty_string", "", 0, nil},         // Empty string
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evalEq(t, sx("@", lit(tt.str), lit(tt.index)), tt.want)
		})
	}
}

// TestEvaluator_IndexBoundsOverflow verifies that large int64 indices don't
// cause incorrect behavior due to int64→int conversion overflow.
// Regression: bounds checks must use int64 comparison, not int.
func TestEvaluator_IndexBoundsOverflow(t *testing.T) {
	largeIndex := int64(1) << 40

	t.Run("slice_large_index", func(t *testing.T) {
		evalEq(t, sx("@", lit([]any{"a", "b", "c"}), lit(largeIndex)), nil)
	})

	t.Run("string_large_index", func(t *testing.T) {
		evalEq(t, sx("@", lit("hello"), lit(largeIndex)), nil)
	})
}

func TestEvaluator_DatatypeLiteral(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// The seven names expr.IsDatatypeCheck admits, in the spelling the
	// evaluator folds them to; the parser only ever emits the capitalized form.
	datatypes := []string{"string", "integer", "float", "boolean", "uuid", "timestamp", "date"}

	for _, name := range datatypes {
		t.Run(name, func(t *testing.T) {
			result, err := ev.Evaluate(expr.DatatypeLiteral(name), scope)
			require.NoError(t, err)
			assert.NotNil(t, result) // result is a TypeChecker function
		})
	}

	t.Run("unknown_datatype_errors", func(t *testing.T) {
		evalErr(t, expr.DatatypeLiteral("unknown"), "unknown datatype")
	})
}

// TestEvaluator_BuiltinCall covers the five-element call form the parser
// emits for every DSL function call — receiver, args literal, params literal,
// body, with an absent part as an empty literal or nil — through the lower-
// case name the pipeline reaches the registry by.
func TestEvaluator_BuiltinCall(t *testing.T) {
	t.Run("len", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit("hello"), "len", nil, nil, nil), int64(5))
	})

	t.Run("abs", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(int64(-42)), "abs", nil, nil, nil), int64(42))
	})

	t.Run("floor", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(3.7), "floor", nil, nil, nil), 3.0)
	})

	t.Run("ceil", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(3.2), "ceil", nil, nil, nil), 4.0)
	})

	t.Run("round", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(3.5), "round", nil, nil, nil), 4.0)
	})

	t.Run("with_params_and_body", func(t *testing.T) {
		// [1,2,3] -> Map(x => x + 1)
		e := makeBuiltinCall(list(int64(1), int64(2), int64(3)), "map", nil, []string{"x"},
			sx("+", sx("$", lit("x")), lit(int64(1))))
		evalEq(t, e, []any{int64(2), int64(3), int64(4)})
	})

	t.Run("args_eval_error", func(t *testing.T) {
		e := makeBuiltinCall(list(int64(1)), "min", []expr.Expression{sx("$", lit("undefined"))}, nil, nil)
		evalErr(t, e, "undefined")
	})

	t.Run("compare_less", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(int64(5)), "compare", []expr.Expression{lit(int64(10))}, nil, nil), int64(-1))
	})

	t.Run("compare_equal", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(int64(5)), "compare", []expr.Expression{lit(int64(5))}, nil, nil), int64(0))
	})
}

func TestEvaluator_BuiltinErrors(t *testing.T) {
	t.Run("too_many_args", func(t *testing.T) {
		e := makeBuiltinCall(lit(int64(5)), "abs", []expr.Expression{lit(int64(1)), lit(int64(2))}, nil, nil)
		evalErr(t, e, "accepts at most")
	})

	t.Run("too_many_params", func(t *testing.T) {
		// map only accepts 1 param
		e := makeBuiltinCall(list(int64(1)), "map", nil, []string{"x", "y", "z"}, lit(int64(1)))
		evalErr(t, e, "accepts at most")
	})
}

// TestEvaluator_MemberAccess_NameOnly pins that the member position holds a
// name and nothing else. A builtin's name there is an ordinary member, an
// S-expression there is an error, and a third operand is an error — the
// pipeline is the only call form.
func TestEvaluator_MemberAccess_NameOnly(t *testing.T) {
	t.Run("builtin_name_is_a_plain_member", func(t *testing.T) {
		evalEq(t, sx(".", lit(immutable.WrapMap(map[string]any{"len": int64(7)})), lit("len")), int64(7))
	})

	t.Run("builtin_name_on_a_list_is_not_a_call", func(t *testing.T) {
		evalErr(t, sx(".", list(int64(1), int64(2)), lit("len")), "cannot access member")
	})

	t.Run("sexpr_member_errors", func(t *testing.T) {
		evalErr(t, sx(".", lit(immutable.WrapMap(map[string]any{"name": "Alice"})), sx("p", lit("name"))), "must be a string literal")
	})

	t.Run("third_operand_errors", func(t *testing.T) {
		evalErr(t, sx(".", list(int64(1)), lit("len"), lit(true)), "exactly 2 operands")
	})
}

// TestEvaluator_BuiltinLenReflectPaths covers len's reflect fallback for
// receivers that are not []any: typed slices, arrays, maps, and their nil
// forms, plus the unsupported-type error.
func TestEvaluator_BuiltinLenReflectPaths(t *testing.T) {
	t.Run("typed_slice_int", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit([]int{1, 2, 3, 4, 5}), "len", nil, nil, nil), int64(5))
	})

	t.Run("typed_slice_string", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit([]string{"a", "b", "c"}), "len", nil, nil, nil), int64(3))
	})

	t.Run("nil_slice_returns_zero", func(t *testing.T) {
		var nilSlice []int
		evalEq(t, makeBuiltinCall(lit(nilSlice), "len", nil, nil, nil), int64(0))
	})

	t.Run("array_type", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit([4]int{1, 2, 3, 4}), "len", nil, nil, nil), int64(4))
	})

	t.Run("map_type", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit(map[string]int{"a": 1, "b": 2}), "len", nil, nil, nil), int64(2))
	})

	t.Run("nil_map_returns_zero", func(t *testing.T) {
		var nilMap map[string]int
		evalEq(t, makeBuiltinCall(lit(nilMap), "len", nil, nil, nil), int64(0))
	})

	t.Run("unsupported_type_errors", func(t *testing.T) {
		evalErr(t, makeBuiltinCall(lit(struct{ X int }{X: 1}), "len", nil, nil, nil), "unsupported")
	})
}

// TestEvaluator_SliceConversion covers asSlice's reflect path: builtins that
// consume sequences must accept typed slices, not just []any.
func TestEvaluator_SliceConversion(t *testing.T) {
	t.Run("sum_typed_slice_int", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit([]int{1, 2, 3, 4, 5}), "sum", nil, nil, nil), int64(15))
	})

	t.Run("sum_typed_slice_float", func(t *testing.T) {
		evalEq(t, makeBuiltinCall(lit([]float64{1.5, 2.5, 3.0}), "sum", nil, nil, nil), 7.0)
	})

	t.Run("non_slice_errors", func(t *testing.T) {
		evalErr(t, makeBuiltinCall(lit(int64(42)), "sum", nil, nil, nil), "slice or array")
	})
}

// --- Logging ---

func TestEvaluator_Logging(t *testing.T) {
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	ev := eval.NewEvaluator(eval.WithLogger(slog.New(h)))

	_, err := ev.Evaluate(sx("+", lit(int64(2)), lit(int64(3))), eval.EmptyScope())
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	records := h.Records()
	if !yammmtest.HasAttr(records, "op", "yammm.eval.expr") {
		t.Error("expected yammm.eval.expr operation to be logged")
	}
	if !yammmtest.HasMessage(records, "evaluating s-expression") {
		t.Error("expected 'evaluating s-expression' message")
	}
}

func TestEvaluator_Logging_SExprOp(t *testing.T) {
	h := yammmtest.NewRecordHandler(slog.LevelDebug)
	ev := eval.NewEvaluator(eval.WithLogger(slog.New(h)))

	_, err := ev.Evaluate(sx("*", lit(int64(4)), lit(int64(5))), eval.EmptyScope())
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if !yammmtest.HasAttr(h.Records(), "op", "*") {
		t.Error("expected op=* attribute in s-expression log")
	}
}

func TestEvaluator_NoLogging_WhenNilLogger(t *testing.T) {
	// No logger — must not panic.
	evalEq(t, sx("+", lit(int64(2)), lit(int64(3))), int64(5))
}
