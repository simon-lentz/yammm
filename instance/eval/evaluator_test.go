package eval_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/simon-lentz/yammm/schema/expr"
)

func TestEvaluator_Literals(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		expr     expr.Expression
		expected any
	}{
		{"nil", nil, nil},
		{"int", expr.NewLiteral(int64(42)), int64(42)},
		{"float", expr.NewLiteral(3.14), 3.14},
		{"string", expr.NewLiteral("hello"), "hello"},
		{"bool_true", expr.NewLiteral(true), true},
		{"bool_false", expr.NewLiteral(false), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ev.Evaluate(tt.expr, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Arithmetic(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		op       string
		left     any
		right    any
		expected any
	}{
		{"add_int", "+", int64(2), int64(3), int64(5)},
		{"add_float", "+", 2.5, 3.5, 6.0},
		{"add_mixed", "+", int64(2), 3.5, 5.5},
		{"sub_int", "-", int64(10), int64(3), int64(7)},
		{"sub_float", "-", 10.5, 3.5, 7.0},
		{"mul_int", "*", int64(4), int64(5), int64(20)},
		{"mul_float", "*", 2.5, 4.0, 10.0},
		{"div_int", "/", int64(15), int64(3), int64(5)},
		{"div_float", "/", 15.0, 2.0, 7.5},
		{"mod", "%", int64(17), int64(5), int64(2)},
		{"add_strings", "+", "hello", " world", "hello world"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op(tt.op),
				expr.NewLiteral(tt.left),
				expr.NewLiteral(tt.right),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Comparison(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		op       string
		left     any
		right    any
		expected bool
	}{
		{"eq_int_true", "==", int64(5), int64(5), true},
		{"eq_int_false", "==", int64(5), int64(6), false},
		{"eq_string", "==", "hello", "hello", true},
		{"neq_true", "!=", int64(5), int64(6), true},
		{"neq_false", "!=", int64(5), int64(5), false},
		{"lt_true", "<", int64(3), int64(5), true},
		{"lt_false", "<", int64(5), int64(3), false},
		{"lte_true", "<=", int64(3), int64(3), true},
		{"gt_true", ">", int64(5), int64(3), true},
		{"gte_true", ">=", int64(3), int64(3), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op(tt.op),
				expr.NewLiteral(tt.left),
				expr.NewLiteral(tt.right),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Logical(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		expr     expr.Expression
		expected bool
	}{
		{
			"and_true",
			expr.SExpr{expr.Op("&&"), expr.NewLiteral(true), expr.NewLiteral(true)},
			true,
		},
		{
			"and_false",
			expr.SExpr{expr.Op("&&"), expr.NewLiteral(true), expr.NewLiteral(false)},
			false,
		},
		{
			"or_true",
			expr.SExpr{expr.Op("||"), expr.NewLiteral(false), expr.NewLiteral(true)},
			true,
		},
		{
			"or_false",
			expr.SExpr{expr.Op("||"), expr.NewLiteral(false), expr.NewLiteral(false)},
			false,
		},
		{
			"not_true",
			expr.SExpr{expr.Op("!"), expr.NewLiteral(false)},
			true,
		},
		{
			"not_false",
			expr.SExpr{expr.Op("!"), expr.NewLiteral(true)},
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ev.Evaluate(tt.expr, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Ternary(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name      string
		condition bool
		expected  any
	}{
		{"true_branch", true, "yes"},
		{"false_branch", false, "no"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op("?"),
				expr.NewLiteral(tt.condition),
				expr.NewLiteral("yes"),
				expr.NewLiteral("no"),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Variables(t *testing.T) {
	ev := eval.NewEvaluator()

	t.Run("lookup_existing", func(t *testing.T) {
		scope := eval.EmptyScope().WithVar("x", 42)
		e := expr.SExpr{expr.Op("$"), expr.NewLiteral("x")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 42, result)
	})

	t.Run("lookup_undefined", func(t *testing.T) {
		scope := eval.EmptyScope()
		e := expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined variable")
	})

	t.Run("numeric_var_unset", func(t *testing.T) {
		scope := eval.EmptyScope()
		e := expr.SExpr{expr.Op("$"), expr.NewLiteral("0")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result) // numeric vars return nil when unset
	})
}

func TestEvaluator_Properties(t *testing.T) {
	ev := eval.NewEvaluator()

	props := map[string]any{
		"name": "Alice",
		"age":  30,
	}
	scope := eval.PropertyScopeFromMap(props)

	t.Run("lookup_property", func(t *testing.T) {
		e := expr.SExpr{expr.Op("p"), expr.NewLiteral("name")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("case_insensitive", func(t *testing.T) {
		e := expr.SExpr{expr.Op("p"), expr.NewLiteral("NAME")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("undefined_property_returns_nil", func(t *testing.T) {
		// Missing optional properties evaluate to nil, enabling patterns like:
		//   age lest 0        (default value)
		//   age then age > 18 (conditional validation)
		e := expr.SExpr{expr.Op("p"), expr.NewLiteral("unknown")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestEvaluator_List(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	e := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral(int64(1)),
		expr.NewLiteral(int64(2)),
		expr.NewLiteral(int64(3)),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, []any{int64(1), int64(2), int64(3)}, result)
}

func TestEvaluator_In(t *testing.T) {
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
			expr.Op("in"),
			expr.NewLiteral(int64(2)),
			list,
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("not_found", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("in"),
			expr.NewLiteral(int64(5)),
			list,
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.False(t, result.(bool))
	})
}

func TestEvaluator_Match(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	pattern := regexp.MustCompile(`^hello`)

	t.Run("match_true", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=~"),
			expr.NewLiteral("hello world"),
			expr.NewLiteral(pattern),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("match_false", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=~"),
			expr.NewLiteral("goodbye world"),
			expr.NewLiteral(pattern),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.False(t, result.(bool))
	})

	t.Run("not_match", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("!~"),
			expr.NewLiteral("goodbye world"),
			expr.NewLiteral(pattern),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})
}

func TestEvaluator_Negate(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		val      any
		expected any
	}{
		{"int", int64(5), int64(-5)},
		{"float", 3.14, -3.14},
		{"negative_int", int64(-10), int64(10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op("-x"),
				expr.NewLiteral(tt.val),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_EvaluateBool(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("true", func(t *testing.T) {
		result, err := ev.EvaluateBool(expr.NewLiteral(true), scope)
		require.NoError(t, err)
		assert.True(t, result)
	})

	t.Run("false", func(t *testing.T) {
		result, err := ev.EvaluateBool(expr.NewLiteral(false), scope)
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("nil_is_false", func(t *testing.T) {
		result, err := ev.EvaluateBool(nil, scope)
		require.NoError(t, err)
		assert.False(t, result)
	})

	t.Run("non_bool_error", func(t *testing.T) {
		_, err := ev.EvaluateBool(expr.NewLiteral("string"), scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "expected boolean")
	})
}

func TestEvaluator_SliceConcat(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	left := expr.SExpr{expr.Op("[]"), expr.NewLiteral(int64(1)), expr.NewLiteral(int64(2))}
	right := expr.SExpr{expr.Op("[]"), expr.NewLiteral(int64(3)), expr.NewLiteral(int64(4))}

	e := expr.SExpr{expr.Op("+"), left, right}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Equal(t, []any{int64(1), int64(2), int64(3), int64(4)}, result)
}

func TestEvaluator_Op(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// When Op is evaluated alone, it returns its string value
	result, err := ev.Evaluate(expr.Op("test"), scope)
	require.NoError(t, err)
	assert.Equal(t, "test", result)
}

func TestEvaluator_Comparison_Strings(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		op       string
		left     any
		right    any
		expected bool
	}{
		{"str_eq_true", "==", "hello", "hello", true},
		{"str_eq_false", "==", "hello", "world", false},
		{"str_neq_true", "!=", "hello", "world", true},
		{"str_neq_false", "!=", "hello", "hello", false},
		{"str_lt_true", "<", "apple", "banana", true},
		{"str_lt_false", "<", "banana", "apple", false},
		{"str_lte_true", "<=", "apple", "apple", true},
		{"str_gt_true", ">", "banana", "apple", true},
		{"str_gte_true", ">=", "banana", "banana", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op(tt.op),
				expr.NewLiteral(tt.left),
				expr.NewLiteral(tt.right),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_Comparison_Floats(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		op       string
		left     any
		right    any
		expected bool
	}{
		{"float_eq_true", "==", 3.14, 3.14, true},
		{"float_eq_false", "==", 3.14, 2.71, false},
		{"float_lt_true", "<", 2.71, 3.14, true},
		{"float_lte_true", "<=", 3.14, 3.14, true},
		{"float_gt_true", ">", 3.14, 2.71, true},
		{"float_gte_true", ">=", 3.14, 3.14, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := expr.SExpr{
				expr.Op(tt.op),
				expr.NewLiteral(tt.left),
				expr.NewLiteral(tt.right),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEvaluator_And_ShortCircuit(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// And short-circuits: if first arg is false, second is not evaluated
	// We use a variable that doesn't exist - if evaluated, it would error
	e := expr.SExpr{
		expr.Op("&&"),
		expr.NewLiteral(false),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.False(t, result.(bool))
}

func TestEvaluator_Or_ShortCircuit(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Or short-circuits: if first arg is true, second is not evaluated
	e := expr.SExpr{
		expr.Op("||"),
		expr.NewLiteral(true),
		expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.True(t, result.(bool))
}

func TestEvaluator_Ternary_NilCondition(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Nil condition causes an error (requires boolean)
	e := expr.SExpr{
		expr.Op("?"),
		expr.NewLiteral(nil),
		expr.NewLiteral("yes"),
		expr.NewLiteral("no"),
	}
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "boolean")
}

func TestEvaluator_In_Array(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// "in" checks if element is in array/slice
	arr := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral("apple"),
		expr.NewLiteral("banana"),
		expr.NewLiteral("cherry"),
	}
	e := expr.SExpr{
		expr.Op("in"),
		expr.NewLiteral("banana"),
		arr,
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.True(t, result.(bool))
}

func TestEvaluator_In_NotInArray(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	arr := expr.SExpr{
		expr.Op("[]"),
		expr.NewLiteral("apple"),
		expr.NewLiteral("banana"),
	}
	e := expr.SExpr{
		expr.Op("in"),
		expr.NewLiteral("xyz"),
		arr,
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.False(t, result.(bool))
}

func TestEvaluator_In_InvalidType(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// "in" with string (not array) should error
	e := expr.SExpr{
		expr.Op("in"),
		expr.NewLiteral("world"),
		expr.NewLiteral("hello world"),
	}
	_, err := ev.Evaluate(e, scope)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "slice or array")
}

func TestEvaluator_Arithmetic_Errors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("mod_with_float", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("%"),
			expr.NewLiteral(5.5),
			expr.NewLiteral(2.0),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})

	t.Run("add_bool_error", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("+"),
			expr.NewLiteral(true),
			expr.NewLiteral(false),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})
}

func TestEvaluator_MemberAccess(t *testing.T) {
	ev := eval.NewEvaluator()

	// Test method calls (builtins)
	t.Run("string_len", func(t *testing.T) {
		scope := eval.EmptyScope()
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("slice_len", func(t *testing.T) {
		scope := eval.EmptyScope()
		list := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := expr.SExpr{
			expr.Op("."),
			list,
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 3, result)
	})
}

func TestEvaluator_NotOperator(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("not_true", func(t *testing.T) {
		e := expr.SExpr{expr.Op("!"), expr.NewLiteral(true)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.False(t, result.(bool))
	})

	t.Run("not_false", func(t *testing.T) {
		e := expr.SExpr{expr.Op("!"), expr.NewLiteral(false)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("not_nil_errors", func(t *testing.T) {
		e := expr.SExpr{expr.Op("!"), expr.NewLiteral(nil)}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "boolean")
	})
}

func TestEvaluator_Variable_Self(t *testing.T) {
	ev := eval.NewEvaluator()

	selfData := map[string]any{"name": "Test", "value": int64(42)}
	scope := eval.EmptyScope().WithSelf(selfData)

	e := expr.SExpr{expr.Op("$"), expr.NewLiteral("self")}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.NotNil(t, result)
}

func TestEvaluator_MemberAccess_Nil(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Member access on nil literal returns nil
	e := expr.SExpr{
		expr.Op("."),
		expr.NewLiteral(nil),
		expr.NewLiteral("key"),
	}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Nil(t, result)
}

func TestEvaluator_SliceAccess(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("access_array_element", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
			expr.NewLiteral("b"),
			expr.NewLiteral("c"),
		}
		e := expr.SExpr{
			expr.Op("@"),
			arr,
			expr.NewLiteral(int64(1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "b", result)
	})

	t.Run("access_string_char", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral("hello"),
			expr.NewLiteral(int64(0)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "h", result)
	})

	t.Run("nil_returns_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(nil),
			expr.NewLiteral(int64(0)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("out_of_bounds_returns_nil", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
		}
		e := expr.SExpr{
			expr.Op("@"),
			arr,
			expr.NewLiteral(int64(10)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("negative_index_returns_nil", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
		}
		e := expr.SExpr{
			expr.Op("@"),
			arr,
			expr.NewLiteral(int64(-1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("non_int_index_errors", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
		}
		e := expr.SExpr{
			expr.Op("@"),
			arr,
			expr.NewLiteral("not-int"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "integer")
	})

	t.Run("unsupported_type_errors", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(int64(42)), // int can't be indexed
			expr.NewLiteral(int64(0)),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot index")
	})
}

// TestEvaluator_StringIndexing_UTF8 verifies that string indexing operates on
// runes (Unicode code points), not bytes. This is required by SPEC line 687:
// string length is "counted in runes, not bytes".
func TestEvaluator_StringIndexing_UTF8(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	tests := []struct {
		name     string
		str      string
		index    int64
		expected any // string if valid, nil if out of bounds
	}{
		// ASCII strings (sanity check)
		{"ascii_first", "hello", 0, "h"},
		{"ascii_middle", "hello", 2, "l"},
		{"ascii_last", "hello", 4, "o"},

		// Multi-byte UTF-8: café has 4 runes, but 5 bytes (é is 2 bytes)
		{"multibyte_ascii_part", "café", 0, "c"},
		{"multibyte_accent", "café", 3, "é"}, // The accent character, not truncated byte

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
			e := expr.SExpr{
				expr.Op("@"),
				expr.NewLiteral(tt.str),
				expr.NewLiteral(tt.index),
			}
			result, err := ev.Evaluate(e, scope)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestEvaluator_IndexBoundsOverflow verifies that large int64 indices don't
// cause incorrect behavior due to int64→int conversion overflow.
// This is P2.1 fix: bounds checks must use int64 comparison, not int.
func TestEvaluator_IndexBoundsOverflow(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Index larger than any realistic slice - would overflow to small/negative
	// number if incorrectly converted to int32 on 32-bit systems
	largeIndex := int64(1) << 40 // ~1 trillion

	t.Run("slice_large_index", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral([]any{"a", "b", "c"}),
			expr.NewLiteral(largeIndex),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result, "large index should return nil (out of bounds)")
	})

	t.Run("string_large_index", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral("hello"),
			expr.NewLiteral(largeIndex),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result, "large index should return nil (out of bounds)")
	})
}

func TestEvaluator_DatatypeLiteral(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	datatypes := []struct {
		name    string
		aliases []string
	}{
		{"string", nil},
		{"integer", []string{"int"}},
		{"float", []string{"number"}},
		{"boolean", []string{"bool"}},
		{"uuid", nil},
		{"timestamp", nil},
		{"date", nil},
	}

	for _, dt := range datatypes {
		t.Run(dt.name, func(t *testing.T) {
			result, err := ev.Evaluate(expr.DatatypeLiteral(dt.name), scope)
			require.NoError(t, err)
			assert.NotNil(t, result)
			// Result should be a TypeChecker function
		})

		for _, alias := range dt.aliases {
			t.Run(alias, func(t *testing.T) {
				result, err := ev.Evaluate(expr.DatatypeLiteral(alias), scope)
				require.NoError(t, err)
				assert.NotNil(t, result)
			})
		}
	}

	t.Run("unknown_datatype_errors", func(t *testing.T) {
		_, err := ev.Evaluate(expr.DatatypeLiteral("unknown"), scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unknown datatype")
	})
}

func TestEvaluator_MatchWithTypeChecker(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("match_integer", func(t *testing.T) {
		// value =~ integer
		e := expr.SExpr{
			expr.Op("=~"),
			expr.NewLiteral(int64(42)),
			expr.DatatypeLiteral("integer"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("no_match_integer", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=~"),
			expr.NewLiteral("not-an-int"),
			expr.DatatypeLiteral("integer"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.False(t, result.(bool))
	})

	t.Run("not_match_with_type", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("!~"),
			expr.NewLiteral("not-a-number"),
			expr.DatatypeLiteral("float"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("match_invalid_right_operand", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=~"),
			expr.NewLiteral("test"),
			expr.NewLiteral(int64(42)), // int is not valid matcher
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "regexp or type checker")
	})
}

func TestEvaluator_BuiltinLen(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("nil_returns_zero", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nil),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("string_len", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("array_len", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
			expr.NewLiteral("b"),
			expr.NewLiteral("c"),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 3, result)
	})
}

func TestEvaluator_WithLogger(t *testing.T) {
	// Test that WithLogger option works
	ev := eval.NewEvaluator(eval.WithLogger(nil))
	scope := eval.EmptyScope()

	result, err := ev.Evaluate(expr.NewLiteral(42), scope)
	require.NoError(t, err)
	assert.Equal(t, 42, result)
}

func TestEvaluator_DirectBuiltinCall(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Test direct function call syntax: ["len", receiver]
	t.Run("len_direct", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("len"),
			expr.NewLiteral("hello"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("abs_direct", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("abs"),
			expr.NewLiteral(int64(-42)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(42), result)
	})

	t.Run("floor_direct", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("floor"),
			expr.NewLiteral(3.7),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 3.0, result)
	})

	t.Run("ceil_direct", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("ceil"),
			expr.NewLiteral(3.2),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 4.0, result)
	})

	t.Run("round_direct", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("round"),
			expr.NewLiteral(3.5),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 4.0, result)
	})

	t.Run("with_params_and_body", func(t *testing.T) {
		// map([1,2,3], x => x + 1)
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := expr.SExpr{
			expr.Op("map"),
			arr,
			expr.NewLiteral([]string{"x"}),
			expr.SExpr{
				expr.Op("+"),
				expr.SExpr{expr.Op("$"), expr.NewLiteral("x")},
				expr.NewLiteral(int64(1)),
			},
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		expected := []any{int64(2), int64(3), int64(4)}
		assert.Equal(t, expected, result)
	})

	t.Run("args_eval_error", func(t *testing.T) {
		// Args evaluation that causes an error
		e := expr.SExpr{
			expr.Op("min"),
			expr.SExpr{
				expr.Op("[]"),
				expr.NewLiteral(int64(1)),
			},
			expr.NewLiteral([]expr.Expression{
				expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
			}),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "undefined")
	})
}

func TestEvaluator_MoreComparisons(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Test int/int comparisons
	t.Run("int_int_lt", func(t *testing.T) {
		e := expr.SExpr{expr.Op("<"), expr.NewLiteral(int64(5)), expr.NewLiteral(int64(10))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("int_int_lte", func(t *testing.T) {
		e := expr.SExpr{expr.Op("<="), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(10))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("int_int_gt", func(t *testing.T) {
		e := expr.SExpr{expr.Op(">"), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(5))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("int_int_gte", func(t *testing.T) {
		e := expr.SExpr{expr.Op(">="), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(10))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	// Test float comparisons with tolerance
	t.Run("float_lt", func(t *testing.T) {
		e := expr.SExpr{expr.Op("<"), expr.NewLiteral(3.14), expr.NewLiteral(3.15)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("float_lte", func(t *testing.T) {
		e := expr.SExpr{expr.Op("<="), expr.NewLiteral(3.14), expr.NewLiteral(3.14)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})
}

func TestEvaluator_MoreArithmetic(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("sub_int", func(t *testing.T) {
		e := expr.SExpr{expr.Op("-"), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(3))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(7), result)
	})

	t.Run("sub_float", func(t *testing.T) {
		e := expr.SExpr{expr.Op("-"), expr.NewLiteral(10.5), expr.NewLiteral(3.5)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 7.0, result)
	})

	t.Run("mul_int", func(t *testing.T) {
		e := expr.SExpr{expr.Op("*"), expr.NewLiteral(int64(5)), expr.NewLiteral(int64(3))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(15), result)
	})

	t.Run("mul_float", func(t *testing.T) {
		e := expr.SExpr{expr.Op("*"), expr.NewLiteral(2.5), expr.NewLiteral(4.0)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 10.0, result)
	})

	t.Run("div_int", func(t *testing.T) {
		e := expr.SExpr{expr.Op("/"), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(3))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(3), result) // integer division
	})

	t.Run("div_float", func(t *testing.T) {
		e := expr.SExpr{expr.Op("/"), expr.NewLiteral(10.0), expr.NewLiteral(4.0)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 2.5, result)
	})

	t.Run("negate_int", func(t *testing.T) {
		e := expr.SExpr{expr.Op("-x"), expr.NewLiteral(int64(42))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(-42), result)
	})

	t.Run("negate_float", func(t *testing.T) {
		e := expr.SExpr{expr.Op("-x"), expr.NewLiteral(3.14)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, -3.14, result)
	})
}

func TestEvaluator_BuiltinErrors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("too_many_args", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("abs"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral([]expr.Expression{
				expr.NewLiteral(int64(1)),
				expr.NewLiteral(int64(2)),
			}),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts at most")
	})

	t.Run("too_many_params", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
		}
		// map only accepts 1 param
		e := expr.SExpr{
			expr.Op("map"),
			arr,
			expr.NewLiteral([]string{"x", "y", "z"}),
			expr.NewLiteral(int64(1)),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts at most")
	})
}

// TestMethodStyleCallValidation verifies that method-style calls enforce the
// same validation constraints as function-style calls. This tests the fix for
// P0.1 where method-style calls were bypassing validation.
func TestMethodStyleCallValidation(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("method_style_no_args_requires_body", func(t *testing.T) {
		// xs.map (no body) should error because map requires a body
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("map"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "requires a lambda expression")
	})

	t.Run("method_style_with_body_requires_body", func(t *testing.T) {
		// xs.filter { true } should work - filter requires a body
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("filter"),
			expr.NewLiteral(true), // body expression
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		// All elements pass filter (true)
		assert.Equal(t, []any{int64(1), int64(2)}, result)
	})

	t.Run("method_style_unexpected_body", func(t *testing.T) {
		// xs.len { true } should error because len does not accept a body
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("len"),
			expr.NewLiteral(true), // body expression - not allowed
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not accept a lambda expression")
	})

	t.Run("method_style_compact_unexpected_body", func(t *testing.T) {
		// xs.compact { true } should error because compact does not accept a body
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(nil),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("compact"),
			expr.NewLiteral(true), // body expression - not allowed
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "does not accept a lambda expression")
	})

	t.Run("method_style_too_many_params", func(t *testing.T) {
		// xs.map(|a, b, c| ...) should error because map only accepts 1 param
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("map"),
			expr.NewLiteral([]string{"a", "b", "c"}), // 3 params - too many
			expr.NewLiteral(int64(1)),                // body
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts at most")
	})

	t.Run("method_style_reduce_too_many_params", func(t *testing.T) {
		// xs.reduce(|a, b, c| ...) should error because reduce only accepts 2 params
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("reduce"),
			expr.NewLiteral([]string{"acc", "val", "extra"}), // 3 params - too many
			expr.SExpr{
				expr.Op("+"),
				expr.SExpr{expr.Op("$"), expr.NewLiteral("acc")},
				expr.SExpr{expr.Op("$"), expr.NewLiteral("val")},
			},
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "accepts at most")
	})
}

// =============================================================================
// Additional Coverage Tests for Uncovered Paths
// =============================================================================

func TestEvaluator_MemberAccessOnNonMap(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// accessMember on non-map type should return error
	t.Run("member_access_on_int", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(42)),
			expr.NewLiteral("property"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot access member")
	})

	t.Run("member_access_on_bool", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(true),
			expr.NewLiteral("field"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "cannot access member")
	})
}

func TestEvaluator_MapMemberAccess(t *testing.T) {
	ev := eval.NewEvaluator()

	// Test member access on map - case insensitive lookup
	t.Run("case_insensitive_match", func(t *testing.T) {
		props := map[string]any{"Name": "Alice", "AGE": int64(30)}
		scope := eval.PropertyScopeFromMap(props)

		// Access with different case should work
		e := expr.SExpr{expr.Op("p"), expr.NewLiteral("name")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("missing_key_returns_nil", func(t *testing.T) {
		props := map[string]any{"name": "Alice"}
		scope := eval.PropertyScopeFromMap(props)

		e := expr.SExpr{expr.Op("p"), expr.NewLiteral("missing")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})
}

func TestEvaluator_BuiltinLenReflectPaths(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Test Len with typed slices (not []any) to hit reflect path
	t.Run("typed_slice_int", func(t *testing.T) {
		typedSlice := []int{1, 2, 3, 4, 5}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(typedSlice),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 5, result)
	})

	t.Run("typed_slice_string", func(t *testing.T) {
		typedSlice := []string{"a", "b", "c"}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(typedSlice),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 3, result)
	})

	t.Run("nil_slice_returns_zero", func(t *testing.T) {
		var nilSlice []int
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nilSlice),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("array_type", func(t *testing.T) {
		arr := [4]int{1, 2, 3, 4}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(arr),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 4, result)
	})

	t.Run("map_type", func(t *testing.T) {
		m := map[string]int{"a": 1, "b": 2}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(m),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 2, result)
	})

	t.Run("nil_map_returns_zero", func(t *testing.T) {
		var nilMap map[string]int
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nilMap),
			expr.NewLiteral("len"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result)
	})

	t.Run("unsupported_type_errors", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(struct{ X int }{X: 1}),
			expr.NewLiteral("len"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unsupported")
	})
}

func TestEvaluator_StringBuiltins(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("upper", func(t *testing.T) {
		e := expr.SExpr{expr.Op("."), expr.NewLiteral("hello"), expr.NewLiteral("upper")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "HELLO", result)
	})

	t.Run("lower", func(t *testing.T) {
		e := expr.SExpr{expr.Op("."), expr.NewLiteral("HELLO"), expr.NewLiteral("lower")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("trim", func(t *testing.T) {
		e := expr.SExpr{expr.Op("."), expr.NewLiteral("  hello  "), expr.NewLiteral("trim")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("trim_prefix", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("trimprefix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello ")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "world", result)
	})

	t.Run("trim_suffix", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("trimsuffix"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(" world")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})

	t.Run("starts_with", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("startswith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("ends_with", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("endswith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("world")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("replace", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("replace"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("world"), expr.NewLiteral("there")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello there", result)
	})

	t.Run("substring", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello"),
			expr.NewLiteral("substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(1)), expr.NewLiteral(int64(4))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "ell", result)
	})

	t.Run("split", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("a,b,c"),
			expr.NewLiteral("split"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{"a", "b", "c"}, result)
	})

	t.Run("join", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral("a"),
			expr.NewLiteral("b"),
			expr.NewLiteral("c"),
		}
		e := expr.SExpr{
			expr.Op("."),
			arr,
			expr.NewLiteral("join"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(",")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "a,b,c", result)
	})
}

func TestEvaluator_FlattenBuiltin(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("flatten_nested_any_slice", func(t *testing.T) {
		// [[1,2], [3,4]] -> [1,2,3,4]
		inner1 := expr.SExpr{expr.Op("[]"), expr.NewLiteral(int64(1)), expr.NewLiteral(int64(2))}
		inner2 := expr.SExpr{expr.Op("[]"), expr.NewLiteral(int64(3)), expr.NewLiteral(int64(4))}
		outer := expr.SExpr{expr.Op("[]"), inner1, inner2}

		e := expr.SExpr{expr.Op("."), outer, expr.NewLiteral("flatten")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(1), int64(2), int64(3), int64(4)}, result)
	})

	t.Run("flatten_typed_nested_slice", func(t *testing.T) {
		// Typed nested slice to hit reflect path
		nested := [][]int{{1, 2}, {3, 4}}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nested),
			expr.NewLiteral("flatten"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Len(t, result.([]any), 4)
	})

	t.Run("flatten_non_nested", func(t *testing.T) {
		// [1, 2, 3] stays as [1, 2, 3]
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(1)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(3)),
		}
		e := expr.SExpr{expr.Op("."), arr, expr.NewLiteral("flatten")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{int64(1), int64(2), int64(3)}, result)
	})

	t.Run("flatten_empty", func(t *testing.T) {
		arr := expr.SExpr{expr.Op("[]")}
		e := expr.SExpr{expr.Op("."), arr, expr.NewLiteral("flatten")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, []any{}, result)
	})
}

func TestEvaluator_SliceConversion(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Test asSlice with various typed slices
	t.Run("sum_typed_slice_int", func(t *testing.T) {
		typedSlice := []int{1, 2, 3, 4, 5}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(typedSlice),
			expr.NewLiteral("sum"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(15), result)
	})

	t.Run("sum_typed_slice_float", func(t *testing.T) {
		typedSlice := []float64{1.5, 2.5, 3.0}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(typedSlice),
			expr.NewLiteral("sum"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 7.0, result)
	})

	t.Run("asSlice_non_slice_error", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(42)),
			expr.NewLiteral("sum"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "slice or array")
	})
}

func TestEvaluator_ComparisonEdgeCases(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("equal_nil_nil", func(t *testing.T) {
		e := expr.SExpr{expr.Op("=="), expr.NewLiteral(nil), expr.NewLiteral(nil)}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("not_equal_nil_value", func(t *testing.T) {
		e := expr.SExpr{expr.Op("!="), expr.NewLiteral(nil), expr.NewLiteral(int64(5))}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("less_than_strings", func(t *testing.T) {
		// String comparison
		e := expr.SExpr{expr.Op("<"), expr.NewLiteral("abc"), expr.NewLiteral("xyz")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})
}

func TestEvaluator_MinMaxCompare(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("min_multiple_values", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(8)),
			expr.NewLiteral(int64(1)),
		}
		e := expr.SExpr{expr.Op("."), arr, expr.NewLiteral("min")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(1), result)
	})

	t.Run("max_multiple_values", func(t *testing.T) {
		arr := expr.SExpr{
			expr.Op("[]"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral(int64(2)),
			expr.NewLiteral(int64(8)),
			expr.NewLiteral(int64(1)),
		}
		e := expr.SExpr{expr.Op("."), arr, expr.NewLiteral("max")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, int64(8), result)
	})

	t.Run("compare_integers", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("compare"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(10))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, -1, result) // 5 < 10
	})

	t.Run("compare_equal", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("compare"),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(5))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, 0, result)
	})
}

func TestEvaluator_DivisionErrors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("div_by_zero_int", func(t *testing.T) {
		e := expr.SExpr{expr.Op("/"), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(0))}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zero")
	})

	t.Run("mod_by_zero", func(t *testing.T) {
		e := expr.SExpr{expr.Op("%"), expr.NewLiteral(int64(10)), expr.NewLiteral(int64(0))}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "zero")
	})
}

func TestEvaluator_TernaryErrors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("ternary_too_few_args", func(t *testing.T) {
		// Ternary with only 2 args instead of 3
		e := expr.SExpr{
			expr.Op("?"),
			expr.NewLiteral(true),
			expr.NewLiteral("yes"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})

	t.Run("ternary_condition_eval_error", func(t *testing.T) {
		// Condition that causes an error
		e := expr.SExpr{
			expr.Op("?"),
			expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
			expr.NewLiteral("yes"),
			expr.NewLiteral("no"),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})
}

func TestEvaluator_AndOrErrors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("and_first_eval_error", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("&&"),
			expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
			expr.NewLiteral(true),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})

	t.Run("or_first_eval_error", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("||"),
			expr.SExpr{expr.Op("$"), expr.NewLiteral("undefined")},
			expr.NewLiteral(false),
		}
		_, err := ev.Evaluate(e, scope)
		assert.Error(t, err)
	})
}

func TestEvaluator_PropertyNil(t *testing.T) {
	ev := eval.NewEvaluator()

	// Test evalProperty with scope that returns nil for properties
	scope := eval.EmptyScope()

	e := expr.SExpr{expr.Op("p"), expr.NewLiteral("any_property")}
	result, err := ev.Evaluate(e, scope)
	require.NoError(t, err)
	assert.Nil(t, result) // Property not found returns nil
}

func TestEvaluator_EvalMemberWithCall(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	// Test member access that triggers method call path
	t.Run("startswith_call_with_args", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("startswith"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral("hello")}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.True(t, result.(bool))
	})

	t.Run("substring_with_args", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral("hello world"),
			expr.NewLiteral("substring"),
			expr.NewLiteral([]expr.Expression{expr.NewLiteral(int64(0)), expr.NewLiteral(int64(5))}),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "hello", result)
	})
}

func TestEvaluator_NumericVarSet(t *testing.T) {
	ev := eval.NewEvaluator()

	// Test numeric vars with set values
	scope := eval.EmptyScope().WithVar("0", "first").WithVar("1", "second")

	t.Run("numeric_var_zero", func(t *testing.T) {
		e := expr.SExpr{expr.Op("$"), expr.NewLiteral("0")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "first", result)
	})

	t.Run("numeric_var_one", func(t *testing.T) {
		e := expr.SExpr{expr.Op("$"), expr.NewLiteral("1")}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "second", result)
	})
}

func TestEvaluator_MemberAccess_EdgeCases(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("access_nil_returns_nil", func(t *testing.T) {
		// Member access on nil should return nil, not error
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(nil),
			expr.NewLiteral("field"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("access_map_exact_match", func(t *testing.T) {
		m := map[string]any{"name": "Alice", "age": int64(30)}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(m),
			expr.NewLiteral("name"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("access_map_case_insensitive", func(t *testing.T) {
		m := map[string]any{"Name": "Alice"}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(m),
			expr.NewLiteral("name"), // lowercase
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "Alice", result)
	})

	t.Run("access_map_missing_key_returns_nil", func(t *testing.T) {
		m := map[string]any{"name": "Alice"}
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(m),
			expr.NewLiteral("nonexistent"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("access_non_map_errors", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("."),
			expr.NewLiteral(int64(42)), // not a map
			expr.NewLiteral("field"),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot access member")
	})
}

func TestEvaluator_SliceAccess_EdgeCases(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("index_slice", func(t *testing.T) {
		slice := []any{"a", "b", "c"}
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(slice),
			expr.NewLiteral(int64(1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "b", result)
	})

	t.Run("index_string", func(t *testing.T) {
		// String indexing returns single character
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral("hello"),
			expr.NewLiteral(int64(1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "e", result)
	})

	t.Run("index_string_unicode", func(t *testing.T) {
		// String indexing should work with unicode (rune-based)
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral("日本語"),
			expr.NewLiteral(int64(1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, "本", result)
	})

	t.Run("index_nil_returns_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(nil),
			expr.NewLiteral(int64(0)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("index_out_of_bounds_returns_nil", func(t *testing.T) {
		slice := []any{"a", "b"}
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(slice),
			expr.NewLiteral(int64(100)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("index_negative_returns_nil", func(t *testing.T) {
		slice := []any{"a", "b"}
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(slice),
			expr.NewLiteral(int64(-1)),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Nil(t, result)
	})

	t.Run("index_non_integer_errors", func(t *testing.T) {
		slice := []any{"a", "b"}
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(slice),
			expr.NewLiteral("not an index"),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "slice index must be integer")
	})

	t.Run("index_non_indexable_errors", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("@"),
			expr.NewLiteral(int64(42)), // not indexable
			expr.NewLiteral(int64(0)),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "cannot index")
	})
}

func TestEvaluator_NilComparisons(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("equal_with_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=="),
			expr.NewLiteral(nil),
			expr.NewLiteral(nil),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("notequal_with_nil", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("!="),
			expr.NewLiteral(int64(5)),
			expr.NewLiteral(nil),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("equal_strings", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("=="),
			expr.NewLiteral("hello"),
			expr.NewLiteral("hello"),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("lt_floats", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("<"),
			expr.NewLiteral(3.14),
			expr.NewLiteral(3.15),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})

	t.Run("gte_floats", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op(">="),
			expr.NewLiteral(3.15),
			expr.NewLiteral(3.14),
		}
		result, err := ev.Evaluate(e, scope)
		require.NoError(t, err)
		assert.Equal(t, true, result)
	})
}

func TestEvaluator_ArithmeticErrors(t *testing.T) {
	ev := eval.NewEvaluator()
	scope := eval.EmptyScope()

	t.Run("divide_by_zero_int", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("/"),
			expr.NewLiteral(int64(10)),
			expr.NewLiteral(int64(0)),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "division by zero")
	})

	t.Run("mod_by_zero", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("%"),
			expr.NewLiteral(int64(10)),
			expr.NewLiteral(int64(0)),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("negate_non_numeric", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("u-"),
			expr.NewLiteral("not a number"),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("sub_non_numeric", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("-"),
			expr.NewLiteral("a"),
			expr.NewLiteral("b"),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})

	t.Run("mul_non_numeric", func(t *testing.T) {
		e := expr.SExpr{
			expr.Op("*"),
			expr.NewLiteral("a"),
			expr.NewLiteral("b"),
		}
		_, err := ev.Evaluate(e, scope)
		require.Error(t, err)
	})
}
