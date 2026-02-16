package eval_test

import (
	"regexp"
	"testing"

	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeBuiltinCall creates an SExpr that cAlls a builtin method on the receiver.
func makeBuiltinCall(receiver expr.Expression, method string, params []string, body expr.Expression) expr.SExpr {
	result := expr.SExpr{expr.Op(".")}
	result = append(result, receiver)
	result = append(result, expr.NewLiteral(method))
	if len(params) > 0 {
		result = append(result, expr.NewLiteral(params))
	}
	if body != nil {
		result = append(result, body)
	}
	return result
}

func TestBuiltin_Len(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected int
	}{
		{"string", "hello", 5},
		{"empty_string", "", 0},
		{"nil", nil, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "Len", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Abs(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected any
	}{
		{"positive_int", int64(5), int64(5)},
		{"negative_int", int64(-5), int64(5)},
		{"positive_float", 3.14, 3.14},
		{"negative_float", -3.14, 3.14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "Abs", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Floor(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected any
	}{
		{"positive", 3.7, 3.0},
		{"negative", -3.2, -4.0},
		{"integer", int64(5), int64(5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "Floor", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Ceil(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected any
	}{
		{"positive", 3.2, 4.0},
		{"negative", -3.7, -3.0},
		{"integer", int64(5), int64(5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "Ceil", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Round(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected any
	}{
		{"down", 3.4, 3.0},
		{"up", 3.6, 4.0},
		{"even_half", 3.5, 4.0}, // RoundToEven
		{"integer", int64(5), int64(5)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "Round", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Compact(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// [1, nil, 2, nil, 3].Compact() => [1, 2, 3]
	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(nil),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(nil),
		expr.NewLiteral(int64(3)),
	}

	e := makeBuiltinCall(list, "Compact", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, result)
}

func TestBuiltin_Unique(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(3)),
		expr.NewLiteral(int64(2)),
	}

	e := makeBuiltinCall(list, "Unique", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, result)
}

func TestBuiltin_Then(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// "hello".Then {|x| x + " world"}
	body := expr.SExpr{
		expr.Op("+"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(" world"),
	}

	t.Run("non_nil", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("hello"), "Then", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello world", result)
	})

	t.Run("nil_short_circuits", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Then", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestBuiltin_Lest(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	defaultVal := expr.NewLiteral("default")

	t.Run("non_nil_returns_value", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("actual"), "Lest", nil, defaultVal)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "actual", result)
	})

	t.Run("nil_evaluates_default", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Lest", nil, defaultVal)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "default", result)
	})
}

func TestBuiltin_With(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// 5.With {|x| x * 2}
	body := expr.SExpr{
		expr.Op("*"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(2)),
	}

	e := makeBuiltinCall(expr.NewLiteral(int64(5)), "With", []string{"x"}, body)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, int64(10), result)
}

func TestBuiltin_match(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	pattern := regexp.MustCompile(`hello (\w+)`)

	// Build: "hello world".Match(pattern)
	// This uses the args literal for the regexp argument
	receiver := expr.NewLiteral("hello world")
	e := expr.SExpr{
		expr.Op("."),
		receiver,
		expr.NewLiteral("Match"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(pattern)}),
	}

	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	Matches := result.([]any)
	assert.Len(t, Matches, 2)
	assert.Equal(t, "hello world", Matches[0])
	assert.Equal(t, "world", Matches[1])
}

func TestBuiltin_Compare(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		left     any
		right    any
		expected int
	}{
		{"equal", int64(5), int64(5), 0},
		{"less", int64(3), int64(5), -1},
		{"greater", int64(7), int64(5), 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			receiver := expr.NewLiteral(tt.left)
			e := expr.SExpr{
				expr.Op("."),
				receiver,
				expr.NewLiteral("Compare"),
				expr.NewLiteral([]expr.Expression{expr.NewLiteral(tt.right)}),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_Min(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("two_args", func(t *testing.T) {
		receiver := expr.NewLiteral(int64(5))
		e := expr.SExpr{
			expr.Op("."),
			receiver,
			expr.NewLiteral("Min"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(3))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result)
	})

	t.Run("slice", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(8)),
			expr.NewLiteral(int64(1)),
		}
		e := makeBuiltinCall(list, "Min", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(1), result)
	})
}

func TestBuiltin_Max(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("two_args", func(t *testing.T) {
		receiver := expr.NewLiteral(int64(5))
		e := expr.SExpr{
			expr.Op("."),
			receiver,
			expr.NewLiteral("Max"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(3))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(5), result)
	})

	t.Run("slice", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(8)),
			expr.NewLiteral(int64(1)),
		}
		e := makeBuiltinCall(list, "Max", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(8), result)
	})
}

// --- Collection builtins tests ---

func TestBuiltin_Reduce(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// [1, 2, 3, 4].Reduce {|acc, x| acc + x}
	// Starting With 0, adds: 0+1=1, 1+2=3, 3+3=6, 6+4=10
	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
		expr.NewLiteral(int64(4)),
	}

	// Body: acc + x
	body := expr.SExpr{
		expr.Op("+"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("acc")},
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
	}

	t.Run("sum", func(t *testing.T) {
		e := makeBuiltinCall(list, "Reduce", []string{"acc", "x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(10), result)
	})

	t.Run("empty_list_returns_error", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "Reduce", []string{"acc", "x"}, body)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "empty sequence")
	})

	t.Run("single_element", func(t *testing.T) {
		singleList := expr.SExpr{expr.Op("[]"), expr.NewLiteral(int64(42))}
		e := makeBuiltinCall(singleList, "Reduce", []string{"acc", "x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(42), result)
	})
}

func TestBuiltin_Map(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// [1, 2, 3].Map {|x| x * 2}
	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}

	// Body: x * 2
	body := expr.SExpr{
		expr.Op("*"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(2)),
	}

	t.Run("double", func(t *testing.T) {
		e := makeBuiltinCall(list, "Map", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(2), int64(4), int64(6)}, result)
	})

	t.Run("empty_list", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "Map", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})

	t.Run("nil_receiver", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Map", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result) // Map on nil returns empty slice
	})
}

func TestBuiltin_Filter(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// [1, 2, 3, 4, 5].Filter {|x| x > 2}
	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
		expr.NewLiteral(int64(4)),
		expr.NewLiteral(int64(5)),
	}

	// Body: x > 2
	body := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(2)),
	}

	t.Run("Filter_greater_than_2", func(t *testing.T) {
		e := makeBuiltinCall(list, "Filter", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(3), int64(4), int64(5)}, result)
	})

	t.Run("empty_list", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "Filter", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})

	t.Run("Filter_none", func(t *testing.T) {
		// All elements <= 2
		smAllList := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
		}
		e := makeBuiltinCall(smAllList, "Filter", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})
}

func TestBuiltin_Count(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// [1, 2, 3, 4, 5].Count {|x| x > 2}
	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
		expr.NewLiteral(int64(4)),
		expr.NewLiteral(int64(5)),
	}

	// Body: x > 2
	body := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(2)),
	}

	t.Run("Count_greater_than_2", func(t *testing.T) {
		e := makeBuiltinCall(list, "Count", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 3, result) // Count returns int, not int64
	})

	t.Run("empty_list", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "Count", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result) // Count returns int, not int64
	})

	t.Run("nil_receiver", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Count", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result) // Returns int(0), not int64(0)
	})
}

func TestBuiltin_All(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Body: x > 0
	body := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(0)),
	}

	t.Run("All_positive", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "All", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("one_negative", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(-2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "All", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})

	t.Run("empty_list_returns_true_vacuous", func(t *testing.T) {
		// Vacuous truth: All zero elements satisfy Any predicate
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "All", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("nil_receiver_returns_true_vacuous", func(t *testing.T) {
		// nil is treated as empty slice; vacuous truth applies
		e := makeBuiltinCall(expr.NewLiteral(nil), "All", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})
}

func TestBuiltin_Any(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Body: x > 5
	body := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(5)),
	}

	t.Run("one_matches", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(10)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "Any", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("none_match", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "Any", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})

	t.Run("empty_list_is_false", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "Any", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})

	t.Run("nil_receiver", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Any", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})
}

func TestBuiltin_AllOrNone(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Body: x > 0
	body := expr.SExpr{
		expr.Op(">"),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
		expr.NewLiteral(int64(0)),
	}

	t.Run("all_match", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "AllOrNone", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("none_match", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(-1)),
			expr.NewLiteral(int64(-2)),
			expr.NewLiteral(int64(-3)),
		}
		e := makeBuiltinCall(list, "AllOrNone", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("some_match_fails", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(-2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "AllOrNone", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})

	t.Run("empty_list_is_true", func(t *testing.T) {
		emptyList := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(emptyList, "AllOrNone", []string{"x"}, body)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})
}

func TestBuiltin_Len_WithSlice(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}

	e := makeBuiltinCall(list, "Len", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, 3, result)
}

func TestBuiltin_Len_Unsupported(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Len on int should error
	e := makeBuiltinCall(expr.NewLiteral(int64(42)), "Len", nil, nil)
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported")
}

func TestBuiltin_Floor_Error(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Floor on string should error
	e := makeBuiltinCall(expr.NewLiteral("not-a-number"), "Floor", nil, nil)
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
}

func TestBuiltin_Ceil_Error(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Ceil on string should error
	e := makeBuiltinCall(expr.NewLiteral("not-a-number"), "Ceil", nil, nil)
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
}

func TestBuiltin_Round_Error(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Round on string should error
	e := makeBuiltinCall(expr.NewLiteral("not-a-number"), "Round", nil, nil)
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
}

func TestBuiltin_Min_Floats(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(3.5),
		expr.NewLiteral(1.2),
		expr.NewLiteral(2.8),
	}

	e := makeBuiltinCall(list, "Min", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, 1.2, result)
}

func TestBuiltin_Max_Floats(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(3.5),
		expr.NewLiteral(1.2),
		expr.NewLiteral(2.8),
	}

	e := makeBuiltinCall(list, "Max", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, 3.5, result)
}

func TestBuiltin_Compare_Strings(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// makeBuiltinCallWithArgs creates a cAll With explicit args
	makeBuiltinCallWithArgs := func(receiver expr.Expression, method string, args []expr.Expression) expr.SExpr {
		result := expr.SExpr{expr.Op(".")}
		result = append(result, receiver)
		result = append(result, expr.NewLiteral(method))
		if len(args) > 0 {
			result = append(result, expr.NewLiteral(args))
		}
		return result
	}

	t.Run("less", func(t *testing.T) {
		e := makeBuiltinCallWithArgs(
			expr.NewLiteral("apple"),
			"Compare",
			[]expr.Expression{expr.NewLiteral("banana")},
		)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, -1, result)
	})

	t.Run("equal", func(t *testing.T) {
		e := makeBuiltinCallWithArgs(
			expr.NewLiteral("test"),
			"Compare",
			[]expr.Expression{expr.NewLiteral("test")},
		)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("greater", func(t *testing.T) {
		e := makeBuiltinCallWithArgs(
			expr.NewLiteral("zebra"),
			"Compare",
			[]expr.Expression{expr.NewLiteral("apple")},
		)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 1, result)
	})
}

func TestBuiltin_match_NoMatch(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	pattern := regexp.MustCompile(`^abc$`)

	receiver := expr.NewLiteral("xyz")
	e := expr.SExpr{
		expr.Op("."),
		receiver,
		expr.NewLiteral("Match"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(pattern)}),
	}

	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	// No Match returns nil
	assert.Nil(t, result)
}

func TestBuiltin_match_NonStringReceiver(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	pattern := regexp.MustCompile(`\d+`)

	receiver := expr.NewLiteral(int64(123))
	e := expr.SExpr{
		expr.Op("."),
		receiver,
		expr.NewLiteral("Match"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(pattern)}),
	}

	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "string")
}

func TestBuiltin_match_InvalidPattern(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	receiver := expr.NewLiteral("test")
	e := expr.SExpr{
		expr.Op("."),
		receiver,
		expr.NewLiteral("Match"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(42))}),
	}

	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
}

func TestBuiltin_Count_WithPredicate(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
		expr.NewLiteral(int64(4)),
		expr.NewLiteral(int64(5)),
	}

	// Count even numbers
	body := expr.SExpr{
		expr.Op("=="),
		expr.SExpr{
			expr.Op("%"),
			expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
			expr.NewLiteral(int64(2)),
		},
		expr.NewLiteral(int64(0)),
	}

	e := makeBuiltinCall(list, "Count", []string{"x"}, body)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, 2, result)
}

// --- New Collection Builtins Tests ---

func TestBuiltin_Sum(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("integers", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "Sum", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(6), result)
	})

	t.Run("floats", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(1.5),
			expr.NewLiteral(2.5),
			expr.NewLiteral(3.0),
		}
		e := makeBuiltinCall(list, "Sum", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 7.0, result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "Sum", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result) // Returns int(0) for empty
	})

	t.Run("nil", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "Sum", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result) // Returns int(0) for nil
	})
}

func TestBuiltin_First(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("non_empty", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("apple"),
			expr.NewLiteral("banana"),
		}
		e := makeBuiltinCall(list, "First", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "apple", result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "First", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("nil", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "First", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestBuiltin_Last(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("non_empty", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("apple"),
			expr.NewLiteral("banana"),
		}
		e := makeBuiltinCall(list, "Last", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "banana", result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "Last", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestBuiltin_Sort(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("integers", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(3)),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
		}
		e := makeBuiltinCall(list, "Sort", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(1), int64(2), int64(3)}, result)
	})

	t.Run("strings", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("banana"),
			expr.NewLiteral("apple"),
			expr.NewLiteral("cherry"),
		}
		e := makeBuiltinCall(list, "Sort", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{"apple", "banana", "cherry"}, result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "Sort", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})

	t.Run("mixed_types_sorted_by_strata", func(t *testing.T) {
		// Mixed int and string - sorted by type strata (numbers < strings)
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("two"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "Sort", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		// Numbers come before strings in type strata order
		assert.Equal(t, []any{int64(1), int64(3), "two"}, result)
	})

	t.Run("unsupported_map_type_error", func(t *testing.T) {
		// Maps are not comparable - should produce error
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(map[string]any{"a": 1}),
			expr.NewLiteral(map[string]any{"b": 2}),
		}
		e := makeBuiltinCall(list, "Sort", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "sort")
	})
}

func TestBuiltin_Reverse(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("non_empty", func(t *testing.T) {
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := makeBuiltinCall(list, "Reverse", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(3), int64(2), int64(1)}, result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "Reverse", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})
}

func TestBuiltin_Flatten(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Flatten works on []any containing []any
	t.Run("nested", func(t *testing.T) {
		// Construct [[1, 2], [3, 4]]
		inner1 := []any{int64(1), int64(2)}
		inner2 := []any{int64(3), int64(4)}
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(inner1),
			expr.NewLiteral(inner2),
		}
		e := makeBuiltinCall(list, "Flatten", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(1), int64(2), int64(3), int64(4)}, result)
	})

	t.Run("empty", func(t *testing.T) {
		list := expr.SExpr{expr.Op("[]")}
		e := makeBuiltinCall(list, "Flatten", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})
}

func TestBuiltin_Contains(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}

	t.Run("found", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			list,
			expr.NewLiteral("Contains"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(2))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("not_found", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			list,
			expr.NewLiteral("Contains"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(5))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})
}

// --- String Builtins Tests ---

func TestBuiltin_Upper(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := makeBuiltinCall(expr.NewLiteral("hello"), "Upper", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "HELLO", result)
}

func TestBuiltin_Lower(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := makeBuiltinCall(expr.NewLiteral("HELLO"), "Lower", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestBuiltin_Trim(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := makeBuiltinCall(expr.NewLiteral("  hello  "), "Trim", nil, nil)
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestBuiltin_TrimPrefix(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("."),
		expr.NewLiteral("hello world"),
		expr.NewLiteral("TrimPrefix"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello ")}),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "world", result)
}

func TestBuiltin_TrimSuffix(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("."),
		expr.NewLiteral("hello world"),
		expr.NewLiteral("TrimSuffix"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(" world")}),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "hello", result)
}

func TestBuiltin_Split(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("."),
		expr.NewLiteral("a,b,c"),
		expr.NewLiteral("Split"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, []any{"a", "b", "c"}, result)
}

func TestBuiltin_Join(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	list := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral("a"),
		expr.NewLiteral("b"),
		expr.NewLiteral("c"),
	}

	e := expr.SExpr{
		expr.Op("."),
		list,
		expr.NewLiteral("Join"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "a,b,c", result)
}

func TestBuiltin_StartsWith(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("true", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("StartsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("false", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("StartsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("world")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})
}

func TestBuiltin_EndsWith(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("true", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("EndsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("world")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("false", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("EndsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})
}

func TestBuiltin_Replace(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("."),
		expr.NewLiteral("hello world world"),
		expr.NewLiteral("Replace"),
		expr.NewLiteral([]expr.Expression{expr.NewLiteral("world"), expr.NewLiteral("there")}),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, "hello there there", result)
}

func TestBuiltin_Substring(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("with_end", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(0)), expr.NewLiteral(int64(5))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("without_end", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(6))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "world", result)
	})

	t.Run("unicode", func(t *testing.T) {
		// Rune-based substring
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("日本語"),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(0)), expr.NewLiteral(int64(2))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "日本", result)
	})
}

// --- Utility Builtins Tests ---

func TestBuiltin_TypeOf(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// TypeOf returns reflect.TypeOf().String() for type names
	tests := []struct {
		name     string
		val      any
		expected string
	}{
		{"string", "hello", "string"},
		{"int64", int64(42), "int64"},
		{"float64", 3.14, "float64"},
		{"bool", true, "bool"},
		{"nil", nil, "nil"},
		{"slice", []any{1, 2, 3}, "[]interface {}"},
		{"map", map[string]any{"a": 1}, "map[string]interface {}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := makeBuiltinCall(expr.NewLiteral(tt.val), "TypeOf", nil, nil)
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuiltin_IsNil(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("nil", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(nil), "IsNil", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("non_nil", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("hello"), "IsNil", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, false, result)
	})
}

func TestBuiltin_Default(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("nil_uses_default", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nil),
			expr.NewLiteral("Default"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("fallback")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "fallback", result)
	})

	t.Run("non_nil_uses_value", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("actual"),
			expr.NewLiteral("Default"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("fallback")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "actual", result)
	})
}

func TestBuiltin_Coalesce(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("first_non_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nil),
			expr.NewLiteral("Coalesce"),
			expr.NewLiteral([]expr.Expression{
				expr.NewLiteral(nil),
				expr.NewLiteral("found"),
				expr.NewLiteral("ignored"),
			}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "found", result)
	})

	t.Run("lhs_non_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("first"),
			expr.NewLiteral("Coalesce"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("second")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "first", result)
	})

	t.Run("all_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nil),
			expr.NewLiteral("Coalesce"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(nil)}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestBuiltin_StringErrorPaths(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("upper_non_string", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(int64(123)), "Upper", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("lower_non_string", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(int64(123)), "Lower", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("trim_non_string", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral(int64(123)), "Trim", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("trim_prefix_non_string_receiver", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("TrimPrefix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("prefix")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("trim_prefix_non_string_arg", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("TrimPrefix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(123))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("trim_suffix_non_string_receiver", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("TrimSuffix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("suffix")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("trim_suffix_non_string_arg", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("TrimSuffix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(123))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("starts_with_non_string", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("StartsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("prefix")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("starts_with_non_string_arg", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("StartsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(123))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("ends_with_non_string", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("EndsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("suffix")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("ends_with_non_string_arg", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("EndsWith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(123))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("replace_non_string", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("Replace"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("old"), expr.NewLiteral("new")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("replace_non_string_old", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("Replace"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(1)), expr.NewLiteral("new")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("replace_non_string_new", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("Replace"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("old"), expr.NewLiteral(int64(1))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("substring_non_string", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(0)), expr.NewLiteral(int64(5))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("substring_non_int_start", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("not int"), expr.NewLiteral(int64(5))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("substring_non_int_length", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("Substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(0)), expr.NewLiteral("not int")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("split_non_string", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(123)),
			expr.NewLiteral("Split"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("split_non_string_separator", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("a,b,c"),
			expr.NewLiteral("Split"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(1))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("join_non_slice", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("not a slice"),
			expr.NewLiteral("Join"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("join_non_string_separator", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral([]any{"a", "b"}),
			expr.NewLiteral("Join"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(1))}),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})
}

func TestBuiltin_Min_Max_Errors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("min_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "Min", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("max_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "Max", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("abs_non_numeric", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not numeric"), "Abs", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("first_empty_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral([]any{}), "First", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("last_empty_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral([]any{}), "Last", nil, nil)
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("first_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "First", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("last_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "Last", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("sort_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "Sort", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("reverse_non_slice", func(t *testing.T) {
		e := makeBuiltinCall(expr.NewLiteral("not a slice"), "Reverse", nil, nil)
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})
}
