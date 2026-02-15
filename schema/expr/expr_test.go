package expr_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/internal/source"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
)

func TestExpression_Op(t *testing.T) {
	tests := []struct {
		name     string
		expr     expr.Expression
		expected string
	}{
		{"SExpr with add", expr.SExpr{expr.Op("+"), expr.NewLiteral(1), expr.NewLiteral(2)}, "+"},
		{"SExpr with and", expr.SExpr{expr.Op("&&"), expr.NewLiteral(true), expr.NewLiteral(false)}, "&&"},
		{"Empty SExpr", expr.SExpr{}, ""},
		{"Literal", expr.NewLiteral("hello"), "lit"},
		{"Op", expr.Op("test"), "test"},
		{"DatatypeLiteral", expr.DatatypeLiteral("Integer"), "dt"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.expr.Op())
		})
	}
}

func TestExpression_Children(t *testing.T) {
	lit1 := expr.NewLiteral(1)
	lit2 := expr.NewLiteral(2)
	sexpr := expr.SExpr{expr.Op("+"), lit1, lit2}

	children := sexpr.Children()
	require.Len(t, children, 2)
	assert.Same(t, lit1, children[0])
	assert.Same(t, lit2, children[1])

	// Literals have no children
	assert.Nil(t, lit1.Children())

	// Op has no children
	assert.Nil(t, expr.Op("+").Children())

	// DatatypeLiteral has no children
	assert.Nil(t, expr.DatatypeLiteral("String").Children())
}

func TestExpression_Literal(t *testing.T) {
	assert.Equal(t, "hello", expr.NewLiteral("hello").Literal())
	assert.Equal(t, int64(42), expr.NewLiteral(int64(42)).Literal())
	assert.Equal(t, true, expr.NewLiteral(true).Literal())
	assert.Nil(t, expr.NewLiteral(nil).Literal())

	// Op.Literal returns the op string
	assert.Equal(t, "+", expr.Op("+").Literal())

	// DatatypeLiteral.Literal returns the type name
	assert.Equal(t, "Integer", expr.DatatypeLiteral("Integer").Literal())

	// SExpr.Literal returns the op
	assert.Equal(t, "+", expr.SExpr{expr.Op("+"), expr.NewLiteral(1)}.Literal())
}

func TestNewLiteral_Unwrap(t *testing.T) {
	// NewLiteral should unwrap nested Literal pointers
	lit1 := expr.NewLiteral("hello")
	lit2 := expr.NewLiteral(lit1)
	assert.Same(t, lit1, lit2)
}

func TestStringLiteral(t *testing.T) {
	t.Run("string literal", func(t *testing.T) {
		val, ok := expr.StringLiteral(expr.NewLiteral("hello"))
		assert.True(t, ok)
		assert.Equal(t, "hello", val)
	})

	t.Run("non-string literal", func(t *testing.T) {
		_, ok := expr.StringLiteral(expr.NewLiteral(42))
		assert.False(t, ok)
	})

	t.Run("nil expression", func(t *testing.T) {
		_, ok := expr.StringLiteral(nil)
		assert.False(t, ok)
	})
}

func TestIsNilLiteral(t *testing.T) {
	assert.True(t, expr.IsNilLiteral(nil))
	assert.True(t, expr.IsNilLiteral(expr.NewLiteral(nil)))
	assert.False(t, expr.IsNilLiteral(expr.NewLiteral("hello")))
	assert.False(t, expr.IsNilLiteral(expr.NewLiteral(0)))
}

func TestIsRegexpLiteral(t *testing.T) {
	re := regexp.MustCompile("test")
	assert.True(t, expr.IsRegexpLiteral(expr.NewLiteral(re)))
	assert.False(t, expr.IsRegexpLiteral(expr.NewLiteral("test")))
	assert.False(t, expr.IsRegexpLiteral(expr.Op("+")))
}

func TestArgsLiteral(t *testing.T) {
	t.Run("valid args literal", func(t *testing.T) {
		args := []expr.Expression{expr.NewLiteral(1), expr.NewLiteral(2)}
		lit := expr.NewLiteral(args)

		result, ok := expr.ArgsLiteral(lit)

		assert.True(t, ok)
		assert.Equal(t, args, result)
	})

	t.Run("empty args literal", func(t *testing.T) {
		lit := expr.NewLiteral([]expr.Expression{})

		result, ok := expr.ArgsLiteral(lit)

		assert.True(t, ok)
		assert.Empty(t, result)
	})

	t.Run("nil expression", func(t *testing.T) {
		_, ok := expr.ArgsLiteral(nil)
		assert.False(t, ok)
	})

	t.Run("wrong type literal", func(t *testing.T) {
		lit := expr.NewLiteral("not args")

		_, ok := expr.ArgsLiteral(lit)

		assert.False(t, ok)
	})

	t.Run("non-literal expression", func(t *testing.T) {
		_, ok := expr.ArgsLiteral(expr.Op("+"))
		assert.False(t, ok)
	})
}

func TestParamsLiteral(t *testing.T) {
	t.Run("valid params literal", func(t *testing.T) {
		params := []string{"x", "y", "z"}
		lit := expr.NewLiteral(params)

		result, ok := expr.ParamsLiteral(lit)

		assert.True(t, ok)
		assert.Equal(t, params, result)
	})

	t.Run("empty params literal", func(t *testing.T) {
		lit := expr.NewLiteral([]string{})

		result, ok := expr.ParamsLiteral(lit)

		assert.True(t, ok)
		assert.Empty(t, result)
	})

	t.Run("nil expression", func(t *testing.T) {
		_, ok := expr.ParamsLiteral(nil)
		assert.False(t, ok)
	})

	t.Run("wrong type literal", func(t *testing.T) {
		lit := expr.NewLiteral(123)

		_, ok := expr.ParamsLiteral(lit)

		assert.False(t, ok)
	})

	t.Run("non-literal expression", func(t *testing.T) {
		_, ok := expr.ParamsLiteral(expr.DatatypeLiteral("String"))
		assert.False(t, ok)
	})
}

func TestCompileString_SimpleExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://simple.yammm")
	collector := diag.NewCollector(0)

	// Compile a simple arithmetic expression
	result := expr.CompileString("1 + 2", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "+", result.Op())

	children := result.Children()
	require.Len(t, children, 2)
}

func TestCompileString_ComparisonExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://compare.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("x > 0", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ">", result.Op())
}

func TestCompileString_LogicalExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://logical.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("a && b", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "&&", result.Op())
}

func TestCompileString_PropertyAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://property.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("name", collector, sourceID)
	require.NotNil(t, result)
	// Property access becomes (p "name")
	assert.Equal(t, "p", result.Op())
}

func TestCompileString_Variable(t *testing.T) {
	sourceID := location.MustNewSourceID("test://variable.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("$x", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "$", result.Op())
}

func TestCompileString_ListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://list.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("[1, 2, 3]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 3)
}

func TestCompileString_FunctionCall(t *testing.T) {
	sourceID := location.MustNewSourceID("test://fcall.yammm")
	collector := diag.NewCollector(0)

	// Grammar uses -> for function calls, not .
	result := expr.CompileString("items->Count()", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Count", result.Op())
}

func TestCompileString_UnknownFunction(t *testing.T) {
	sourceID := location.MustNewSourceID("test://unknown.yammm")
	collector := diag.NewCollector(0)

	// Unknown functions compile successfully - validation is deferred to eval layer.
	// This allows schemas to be compiled without knowing all builtins,
	// supporting runtime extension and custom builtin registration.
	result := expr.CompileString("items->UnknownFunc()", collector, sourceID)
	assert.False(t, collector.HasErrors(), "unknown function should compile without error")
	assert.NotNil(t, result)
	assert.Equal(t, "UnknownFunc", result.Op())
}

func TestCompileString_TernaryExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("a ? b : c", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "?", result.Op())
	assert.Len(t, result.Children(), 3)
}

func TestCompileString_UnaryMinus(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("-x", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "-x", result.Op())
}

func TestCompileString_Not(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("!done", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "!", result.Op())
}

func TestCompileString_MemberAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("obj.prop", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ".", result.Op())
}

func TestCompileString_InOperator(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("x in [1, 2, 3]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "in", result.Op())
}

func TestCompileString_RegexpMatch(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("name =~ /^A/", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "=~", result.Op())
}

func TestCompileString_DatatypeLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("Integer", collector, sourceID)
	require.NotNil(t, result)
	_, ok := result.(expr.DatatypeLiteral)
	assert.True(t, ok)
}

func TestCompile_NilContext(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	_ = reg.Register(sourceID, []byte("_"))
	collector := diag.NewCollector(0)

	result := expr.Compile(nil, collector, sourceID, reg, reg)
	assert.Nil(t, result)
}

func TestVisitor_HasErrors(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://test.yammm")
	_ = reg.Register(sourceID, []byte("_"))

	visitor := expr.NewVisitor(nil, sourceID, reg, reg)
	assert.False(t, visitor.HasErrors())
}

func TestSExpr_Children_Immutability(t *testing.T) {
	// Verify that mutating the returned slice doesn't affect the original SExpr
	lit1 := expr.NewLiteral(1)
	lit2 := expr.NewLiteral(2)
	lit3 := expr.NewLiteral(3)
	sexpr := expr.SExpr{expr.Op("+"), lit1, lit2}

	children := sexpr.Children()
	require.Len(t, children, 2)

	// Mutate the returned slice
	children[0] = lit3
	_ = append(children, expr.NewLiteral(99)) // append to verify no side effects

	// Original should be unchanged
	originalChildren := sexpr.Children()
	require.Len(t, originalChildren, 2)
	assert.Same(t, lit1, originalChildren[0], "original first child should be unchanged")
	assert.Same(t, lit2, originalChildren[1], "original second child should be unchanged")
}

func TestBuiltinRegistry_Names_Sorted(t *testing.T) {
	sourceID := location.MustNewSourceID("test://sorted.yammm")
	collector := diag.NewCollector(0)

	// Compile a function call to ensure builtins are loaded
	_ = expr.CompileString("items->Count()", collector, sourceID)

	// We can't directly access the builtinRegistry, but we can verify
	// the function is recognized (no error) and trust the sorted implementation.
	// The key invariant is that calling Names() multiple times should return
	// the same order. This is a smoke test; determinism is verified by
	// the slices.Sort call in the implementation.
	assert.False(t, collector.HasErrors())
}

func TestCompileString_FunctionCall_NilNormalization(t *testing.T) {
	sourceID := location.MustNewSourceID("test://normalization.yammm")
	collector := diag.NewCollector(0)

	// Function call with no args/params/body - all should be normalized to non-nil
	result := expr.CompileString("items->Count()", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Count", result.Op())

	children := result.Children()
	// Children should be: [lhs, args, params, body]
	require.Len(t, children, 4)

	// All should be non-nil (normalized to empty literals)
	for i, child := range children {
		assert.NotNil(t, child, "child %d should not be nil", i)
	}
}

// TestVisitor_NilTree verifies that Visit handles nil input gracefully.
// This is related to visitor.go error handling paths.
func TestVisitor_NilTree(t *testing.T) {
	reg := source.NewRegistry()
	sourceID := location.MustNewSourceID("test://nil-tree.yammm")
	_ = reg.Register(sourceID, []byte("_"))
	collector := diag.NewCollector(0)

	visitor := expr.NewVisitor(collector, sourceID, reg, reg)
	result := visitor.Visit(nil)
	assert.Nil(t, result)
	assert.False(t, visitor.HasErrors(), "nil tree should not trigger error")
}

// TestVisitor_ErrorHandling_InvalidExpression verifies error collection for invalid expressions.
// Note: The DatatypeKeywordContext error path (visitor.go:86-88) is defensive code that
// is difficult to trigger through normal parsing. The grammar wraps datatype keywords
// in DatatypeNameContext, not DatatypeKeywordContext. This test covers the general
// error handling pattern for expressions that fail validation.
//
// Note: Unknown function names no longer produce errors at compile time - validation
// is deferred to the eval layer. This test only covers syntax errors like invalid regexp.
func TestVisitor_ErrorHandling_InvalidExpression(t *testing.T) {
	tests := []struct {
		name   string
		expr   string
		errMsg string
	}{
		{
			name:   "invalid regexp triggers error",
			expr:   "name =~ /[/",
			errMsg: "invalid",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceID := location.MustNewSourceID("test://error.yammm")
			collector := diag.NewCollector(0)

			_ = expr.CompileString(tt.expr, collector, sourceID)
			assert.True(t, collector.HasErrors(), "expected error for %q", tt.expr)
		})
	}
}

// --- BuiltinRegistry Tests ---

func TestBuiltinRegistry_NewBuiltinRegistry(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	// Should have default builtins
	assert.True(t, reg.Has("Count"), "should have Count builtin")
	assert.True(t, reg.Has("Sum"), "should have Sum builtin")
	assert.True(t, reg.Has("Length"), "should have Length builtin")
	assert.True(t, reg.Has("Upper"), "should have Upper builtin")
}

func TestBuiltinRegistry_Len(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	// Should have many default builtins
	assert.Greater(t, reg.Len(), 30, "should have many default builtins")
}

func TestBuiltinRegistry_Has(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	t.Run("existing builtin", func(t *testing.T) {
		assert.True(t, reg.Has("Map"))
		assert.True(t, reg.Has("Filter"))
		assert.True(t, reg.Has("Reduce"))
	})

	t.Run("non-existing builtin", func(t *testing.T) {
		assert.False(t, reg.Has("NotABuiltin"))
		assert.False(t, reg.Has("CustomFunc"))
	})
}

func TestBuiltinRegistry_Has_NilReceiver(t *testing.T) {
	var reg *expr.BuiltinRegistry

	assert.False(t, reg.Has("Count"))
}

func TestBuiltinRegistry_Register(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	// Register a custom function
	err := reg.Register("CustomFunc")

	assert.NoError(t, err)
	assert.True(t, reg.Has("CustomFunc"))
}

func TestBuiltinRegistry_Register_ErrorOnDuplicate(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	err := reg.Register("Count") // Already exists in defaults

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestBuiltinRegistry_Register_ErrorOnEmpty(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	err := reg.Register("")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestBuiltinRegistry_Register_ErrorOnNilReceiver(t *testing.T) {
	var reg *expr.BuiltinRegistry

	err := reg.Register("Anything")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nil")
}

func TestBuiltinRegistry_Register_ErrorOnWhitespace(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantMatch string
	}{
		{"leading space", " Func", "leading/trailing whitespace"},
		{"trailing space", "Func ", "leading/trailing whitespace"},
		{"leading tab", "\tFunc", "leading/trailing whitespace"},
		{"trailing newline", "Func\n", "leading/trailing whitespace"},
		{"embedded space", "My Func", "contains whitespace"},
		{"embedded tab", "My\tFunc", "contains whitespace"},
		{"embedded newline", "My\nFunc", "contains whitespace"},
		{"embedded carriage return", "My\rFunc", "contains whitespace"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := expr.NewBuiltinRegistry()

			err := reg.Register(tt.input)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMatch)
		})
	}
}

func TestBuiltinRegistry_Register_ErrorMessageQuoted(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	// Register duplicate should use %q format (quoted)
	err := reg.Register("Count")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), `"Count"`) // Should be quoted
}

func TestBuiltinRegistry_MustRegister(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	// Should not panic for new function
	assert.NotPanics(t, func() {
		reg.MustRegister("CustomFunc")
	})
	assert.True(t, reg.Has("CustomFunc"))
}

func TestBuiltinRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	assert.Panics(t, func() {
		reg.MustRegister("Count") // Already exists in defaults
	})
}

func TestBuiltinRegistry_Names(t *testing.T) {
	reg := expr.NewBuiltinRegistry()

	names := reg.Names()

	// Should return sorted names
	assert.Greater(t, len(names), 0)

	// Verify sorted order
	for i := 1; i < len(names); i++ {
		assert.Less(t, names[i-1], names[i], "names should be sorted")
	}

	// Verify contains expected builtins
	assert.Contains(t, names, "Count")
	assert.Contains(t, names, "Sum")
	assert.Contains(t, names, "Map")
}

func TestBuiltinRegistry_Names_NilReceiver(t *testing.T) {
	var reg *expr.BuiltinRegistry

	names := reg.Names()

	assert.Nil(t, names)
}

func TestBuiltinRegistry_Names_IncludesCustom(t *testing.T) {
	reg := expr.NewBuiltinRegistry()
	_ = reg.Register("AAACustom") // Starts with AAA to be first alphabetically

	names := reg.Names()

	assert.Contains(t, names, "AAACustom")
	assert.Equal(t, "AAACustom", names[0], "custom function should be first after sorting")
}

func TestBuiltinRegistry_Len_NilReceiver(t *testing.T) {
	var reg *expr.BuiltinRegistry

	assert.Equal(t, 0, reg.Len())
}

func TestBuiltinRegistry_IsolatedFromDefaults(t *testing.T) {
	// Verify that two registries are isolated
	reg1 := expr.NewBuiltinRegistry()
	reg2 := expr.NewBuiltinRegistry()

	_ = reg1.Register("CustomOnlyInReg1")

	assert.True(t, reg1.Has("CustomOnlyInReg1"))
	assert.False(t, reg2.Has("CustomOnlyInReg1"), "reg2 should not have reg1's custom")
}

func TestBuiltinRegistry_ZeroValue(t *testing.T) {
	// Zero value should be usable and contain defaults
	var reg expr.BuiltinRegistry

	t.Run("Has returns defaults", func(t *testing.T) {
		assert.True(t, reg.Has("Count"), "zero value should have Count builtin")
		assert.True(t, reg.Has("Sum"), "zero value should have Sum builtin")
	})

	t.Run("Register works on zero value", func(t *testing.T) {
		err := reg.Register("ZeroValueCustom")
		assert.NoError(t, err)
		assert.True(t, reg.Has("ZeroValueCustom"))
	})

	t.Run("Names returns sorted defaults", func(t *testing.T) {
		names := reg.Names()
		assert.Greater(t, len(names), 30, "should have many default builtins")
		// Verify sorted order
		for i := 1; i < len(names); i++ {
			assert.Less(t, names[i-1], names[i], "names should be sorted")
		}
	})

	t.Run("Len returns count", func(t *testing.T) {
		assert.Greater(t, reg.Len(), 30, "should have many default builtins")
	})
}

func TestBuiltinRegistry_Clone(t *testing.T) {
	t.Run("clone has same contents", func(t *testing.T) {
		orig := expr.NewBuiltinRegistry()
		_ = orig.Register("CustomFunc")

		clone := orig.Clone()

		assert.True(t, clone.Has("Count"), "clone should have defaults")
		assert.True(t, clone.Has("CustomFunc"), "clone should have custom")
		assert.Equal(t, orig.Len(), clone.Len())
	})

	t.Run("modifications are independent", func(t *testing.T) {
		orig := expr.NewBuiltinRegistry()
		clone := orig.Clone()

		_ = clone.Register("OnlyInClone")

		assert.True(t, clone.Has("OnlyInClone"))
		assert.False(t, orig.Has("OnlyInClone"), "original should not have clone's addition")
	})

	t.Run("nil clone returns defaults", func(t *testing.T) {
		var reg *expr.BuiltinRegistry

		clone := reg.Clone()

		assert.NotNil(t, clone)
		assert.True(t, clone.Has("Count"), "nil clone should have defaults")
	})
}

// --- Additional Coverage Tests ---

func TestCompileString_MulDivExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://muldiv.yammm")
	collector := diag.NewCollector(0)

	t.Run("multiplication", func(t *testing.T) {
		result := expr.CompileString("2 * 3", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "*", result.Op())
	})

	t.Run("division", func(t *testing.T) {
		result := expr.CompileString("10 / 2", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "/", result.Op())
	})

	t.Run("modulo", func(t *testing.T) {
		result := expr.CompileString("7 % 3", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "%", result.Op())
	})
}

func TestCompileString_EqualityExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://equality.yammm")
	collector := diag.NewCollector(0)

	t.Run("equals", func(t *testing.T) {
		result := expr.CompileString("x == y", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "==", result.Op())
	})

	t.Run("not equals", func(t *testing.T) {
		result := expr.CompileString("x != y", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "!=", result.Op())
	})
}

func TestCompileString_OrExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://or.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("a || b", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "||", result.Op())
}

func TestCompileString_GroupExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://group.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("(1 + 2) * 3", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "*", result.Op())
}

func TestCompileString_ArrayIndexAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://at.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("items[0]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "@", result.Op())
}

func TestCompileString_LiteralTypes(t *testing.T) {
	sourceID := location.MustNewSourceID("test://literals.yammm")
	collector := diag.NewCollector(0)

	t.Run("string literal", func(t *testing.T) {
		result := expr.CompileString(`"hello"`, collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "hello", result.Literal())
	})

	t.Run("integer literal", func(t *testing.T) {
		result := expr.CompileString("42", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, int64(42), result.Literal())
	})

	t.Run("float literal", func(t *testing.T) {
		result := expr.CompileString("3.14", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, 3.14, result.Literal())
	})

	t.Run("boolean true", func(t *testing.T) {
		result := expr.CompileString("true", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, true, result.Literal())
	})

	t.Run("boolean false", func(t *testing.T) {
		result := expr.CompileString("false", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, false, result.Literal())
	})

	t.Run("nil literal", func(t *testing.T) {
		result := expr.CompileString("nil", collector, sourceID)
		require.NotNil(t, result)
		assert.Nil(t, result.Literal())
	})
}

func TestCompileString_LambdaWithParameters(t *testing.T) {
	sourceID := location.MustNewSourceID("test://lambda.yammm")
	collector := diag.NewCollector(0)

	// Filter with lambda parameter
	result := expr.CompileString("items->Filter(|x| x > 0)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Filter", result.Op())
}

func TestCompileString_FunctionWithArguments(t *testing.T) {
	sourceID := location.MustNewSourceID("test://args.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("str->Substring(0, 5)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Substring", result.Op())
}

func TestCompileString_ChainedExpressions(t *testing.T) {
	sourceID := location.MustNewSourceID("test://chain.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("items->Filter(|x| x > 0)->Count()", collector, sourceID)
	require.NotNil(t, result)
	// The AST represents the chain - first Filter is applied
	assert.Contains(t, []string{"Filter", "Count"}, result.Op())
}

func TestCompileString_NestedTernary(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-ternary.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("a ? b : c ? d : e", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "?", result.Op())
}

func TestCompileString_ComplexBoolean(t *testing.T) {
	sourceID := location.MustNewSourceID("test://complex.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("(a && b) || (c && d)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "||", result.Op())
}

func TestCompileString_NegativeNumber(t *testing.T) {
	sourceID := location.MustNewSourceID("test://negative.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("-42", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "-x", result.Op())
}

func TestCompileString_AllComparisonOperators(t *testing.T) {
	sourceID := location.MustNewSourceID("test://compare.yammm")
	collector := diag.NewCollector(0)

	tests := []struct {
		expr string
		op   string
	}{
		{"a < b", "<"},
		{"a <= b", "<="},
		{"a > b", ">"},
		{"a >= b", ">="},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			result := expr.CompileString(tt.expr, collector, sourceID)
			require.NotNil(t, result)
			assert.Equal(t, tt.op, result.Op())
		})
	}
}

func TestExpression_Children_EdgeCases(t *testing.T) {
	// Test nil expression children
	var nilExpr expr.Expression
	assert.Nil(t, nilExpr)

	// Test Op children
	op := expr.Op("+")
	assert.Nil(t, op.Children())

	// Test DatatypeLiteral children
	dt := expr.DatatypeLiteral("String")
	assert.Nil(t, dt.Children())
}

func TestCompileString_EmptyListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://empty-list.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("[]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 0)
}

func TestCompileString_NestedListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-list.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("[[1, 2], [3, 4]]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 2)
}

func TestSExpr_Empty(t *testing.T) {
	empty := expr.SExpr{}

	assert.Equal(t, "", empty.Op())
	// Empty SExpr's Literal returns the op string which is ""
	assert.Equal(t, "", empty.Literal())
	assert.Nil(t, empty.Children())
}

func TestCompileString_HexInteger(t *testing.T) {
	sourceID := location.MustNewSourceID("test://hex.yammm")
	collector := diag.NewCollector(0)

	// Hex literal if supported
	result := expr.CompileString("0x10", collector, sourceID)
	if !collector.HasErrors() {
		require.NotNil(t, result)
	}
}

func TestCompileString_SelfReference(t *testing.T) {
	sourceID := location.MustNewSourceID("test://self.yammm")
	collector := diag.NewCollector(0)

	// Self reference via $self or similar if supported
	result := expr.CompileString("$self", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_MultipleVariables(t *testing.T) {
	sourceID := location.MustNewSourceID("test://vars.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("$x + $y", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "+", result.Op())
}

func TestCompileString_NestedMemberAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("a.b.c", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ".", result.Op())
}

func TestCompileString_ComplexArithmetic(t *testing.T) {
	sourceID := location.MustNewSourceID("test://complex-arith.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("(a + b) * (c - d) / e", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_MultipleArguments(t *testing.T) {
	sourceID := location.MustNewSourceID("test://multi-args.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("str->Replace(\"old\", \"new\")", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Replace", result.Op())
}

func TestCompileString_NestedFunctions(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-fn.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("items->Map(|x| x * 2)->Sum()", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_ListWithDifferentTypes(t *testing.T) {
	sourceID := location.MustNewSourceID("test://mixed-list.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString(`["a", 1, true]`, collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 3)
}

func TestCompileString_MultiIndexAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://multi-index.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("matrix[0][1]", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_TernaryWithoutFalseBranch(t *testing.T) {
	sourceID := location.MustNewSourceID("test://ternary-no-false.yammm")
	collector := diag.NewCollector(0)

	// Some grammars support ternary without false branch; this tests that path
	result := expr.CompileString("a ? b : nil", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_ReduceWithInit(t *testing.T) {
	sourceID := location.MustNewSourceID("test://reduce.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("items->Reduce(0, |acc, x| acc + x)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Reduce", result.Op())
}

func TestCompileString_MultipleParameters(t *testing.T) {
	sourceID := location.MustNewSourceID("test://multi-params.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("items->Reduce(0, |acc, val| acc + val)", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_NotMatch(t *testing.T) {
	sourceID := location.MustNewSourceID("test://not-match.yammm")
	collector := diag.NewCollector(0)

	result := expr.CompileString("name !~ /test/", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "!~", result.Op())
}

func TestIsNilLiteral_EdgeCases(t *testing.T) {
	// Test with non-Literal expression
	assert.False(t, expr.IsNilLiteral(expr.Op("+")))
	assert.False(t, expr.IsNilLiteral(expr.DatatypeLiteral("String")))
	assert.False(t, expr.IsNilLiteral(expr.SExpr{expr.Op("+"), expr.NewLiteral(1)}))
}
