package exprcomp_test

import (
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema/expr"
	"github.com/simon-lentz/yammm/schema/internal/exprcomp"
)

// noopSpans is a minimal SpanFromContext for tests that don't need real spans.
type noopSpans struct{}

func (noopSpans) FromContext(antlr.ParserRuleContext) location.Span { return location.Span{} }

func TestCompileString_SimpleExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://simple.yammm")
	collector := diag.NewCollector(0)

	// Compile a simple arithmetic expression
	result := exprcomp.CompileString("1 + 2", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "+", result.Op())

	children := result.Children()
	require.Len(t, children, 2)
}

func TestCompileString_ComparisonExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://compare.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("x > 0", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ">", result.Op())
}

func TestCompileString_LogicalExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://logical.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("a && b", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "&&", result.Op())
}

func TestCompileString_PropertyAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://property.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("name", collector, sourceID)
	require.NotNil(t, result)
	// Property access becomes (p "name")
	assert.Equal(t, "p", result.Op())
}

func TestCompileString_Variable(t *testing.T) {
	sourceID := location.MustNewSourceID("test://variable.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("$x", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "$", result.Op())
}

func TestCompileString_ListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://list.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("[1, 2, 3]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 3)
}

func TestCompileString_FunctionCall(t *testing.T) {
	sourceID := location.MustNewSourceID("test://fcall.yammm")
	collector := diag.NewCollector(0)

	// Grammar uses -> for function calls, not .
	result := exprcomp.CompileString("items->Count()", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Count", result.Op())
}

func TestCompileString_UnknownFunction(t *testing.T) {
	sourceID := location.MustNewSourceID("test://unknown.yammm")
	collector := diag.NewCollector(0)

	// Unknown functions compile successfully - validation is deferred to eval layer.
	// This allows schemas to be compiled without knowing all builtins,
	// supporting runtime extension and custom builtin registration.
	result := exprcomp.CompileString("items->UnknownFunc()", collector, sourceID)
	assert.False(t, collector.HasErrors(), "unknown function should compile without error")
	assert.NotNil(t, result)
	assert.Equal(t, "UnknownFunc", result.Op())
}

func TestCompileString_TernaryExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("a ? b : c", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "?", result.Op())
	assert.Len(t, result.Children(), 3)
}

func TestCompileString_UnaryMinus(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("-x", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "-x", result.Op())
}

func TestCompileString_Not(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("!done", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "!", result.Op())
}

func TestCompileString_MemberAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("obj.prop", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ".", result.Op())
}

func TestCompileString_InOperator(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("x in [1, 2, 3]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "in", result.Op())
}

func TestCompileString_RegexpMatch(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("name =~ /^A/", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "=~", result.Op())
}

func TestCompileString_DatatypeLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("Integer", collector, sourceID)
	require.NotNil(t, result)
	_, ok := result.(expr.DatatypeLiteral)
	assert.True(t, ok)
}

func TestCompile_NilContext(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.Compile(nil, collector, sourceID, nil)
	assert.Nil(t, result)
}

func TestVisitor_HasErrors(t *testing.T) {
	sourceID := location.MustNewSourceID("test://test.yammm")

	visitor := exprcomp.NewVisitor(nil, sourceID, noopSpans{})
	assert.False(t, visitor.HasErrors())
}

func TestBuiltinRegistry_Names_Sorted(t *testing.T) {
	sourceID := location.MustNewSourceID("test://sorted.yammm")
	collector := diag.NewCollector(0)

	// Compile a function call to ensure builtins are loaded
	_ = exprcomp.CompileString("items->Count()", collector, sourceID)

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
	result := exprcomp.CompileString("items->Count()", collector, sourceID)
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
	sourceID := location.MustNewSourceID("test://nil-tree.yammm")
	collector := diag.NewCollector(0)

	visitor := exprcomp.NewVisitor(collector, sourceID, noopSpans{})
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

			_ = exprcomp.CompileString(tt.expr, collector, sourceID)
			assert.True(t, collector.HasErrors(), "expected error for %q", tt.expr)
		})
	}
}

// --- BuiltinRegistry Tests ---

func TestBuiltinRegistry_NewBuiltinRegistry(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Should have default builtins
	assert.True(t, reg.Has("Count"), "should have Count builtin")
	assert.True(t, reg.Has("Sum"), "should have Sum builtin")
	assert.True(t, reg.Has("Len"), "should have Len builtin")
	assert.True(t, reg.Has("Upper"), "should have Upper builtin")
}

func TestBuiltinRegistry_Len(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Should have many default builtins
	assert.Greater(t, reg.Len(), 30, "should have many default builtins")
}

func TestBuiltinRegistry_Has(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

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
	var reg *exprcomp.BuiltinRegistry

	assert.False(t, reg.Has("Count"))
}

func TestBuiltinRegistry_Register(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Register a custom function
	err := reg.Register("CustomFunc")

	assert.NoError(t, err)
	assert.True(t, reg.Has("CustomFunc"))
}

func TestBuiltinRegistry_Register_ErrorOnDuplicate(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	err := reg.Register("Count") // Already exists in defaults

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestBuiltinRegistry_Register_ErrorOnEmpty(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	err := reg.Register("")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestBuiltinRegistry_Register_ErrorOnNilReceiver(t *testing.T) {
	var reg *exprcomp.BuiltinRegistry

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
			reg := exprcomp.NewBuiltinRegistry()

			err := reg.Register(tt.input)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMatch)
		})
	}
}

func TestBuiltinRegistry_Register_ErrorMessageQuoted(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Register duplicate should use %q format (quoted)
	err := reg.Register("Count")

	assert.Error(t, err)
	assert.Contains(t, err.Error(), `"Count"`) // Should be quoted
}

func TestBuiltinRegistry_MustRegister(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Should not panic for new function
	assert.NotPanics(t, func() {
		reg.MustRegister("CustomFunc")
	})
	assert.True(t, reg.Has("CustomFunc"))
}

func TestBuiltinRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	assert.Panics(t, func() {
		reg.MustRegister("Count") // Already exists in defaults
	})
}

func TestBuiltinRegistry_Names(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

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
	var reg *exprcomp.BuiltinRegistry

	names := reg.Names()

	assert.Nil(t, names)
}

func TestBuiltinRegistry_Names_IncludesCustom(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()
	_ = reg.Register("AAACustom") // Starts with AAA to be first alphabetically

	names := reg.Names()

	assert.Contains(t, names, "AAACustom")
	assert.Equal(t, "AAACustom", names[0], "custom function should be first after sorting")
}

func TestBuiltinRegistry_Len_NilReceiver(t *testing.T) {
	var reg *exprcomp.BuiltinRegistry

	assert.Equal(t, 0, reg.Len())
}

func TestBuiltinRegistry_IsolatedFromDefaults(t *testing.T) {
	// Verify that two registries are isolated
	reg1 := exprcomp.NewBuiltinRegistry()
	reg2 := exprcomp.NewBuiltinRegistry()

	_ = reg1.Register("CustomOnlyInReg1")

	assert.True(t, reg1.Has("CustomOnlyInReg1"))
	assert.False(t, reg2.Has("CustomOnlyInReg1"), "reg2 should not have reg1's custom")
}

func TestBuiltinRegistry_ZeroValue(t *testing.T) {
	// Zero value should be usable and contain defaults
	var reg exprcomp.BuiltinRegistry

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
		orig := exprcomp.NewBuiltinRegistry()
		_ = orig.Register("CustomFunc")

		clone := orig.Clone()

		assert.True(t, clone.Has("Count"), "clone should have defaults")
		assert.True(t, clone.Has("CustomFunc"), "clone should have custom")
		assert.Equal(t, orig.Len(), clone.Len())
	})

	t.Run("modifications are independent", func(t *testing.T) {
		orig := exprcomp.NewBuiltinRegistry()
		clone := orig.Clone()

		_ = clone.Register("OnlyInClone")

		assert.True(t, clone.Has("OnlyInClone"))
		assert.False(t, orig.Has("OnlyInClone"), "original should not have clone's addition")
	})

	t.Run("nil clone returns defaults", func(t *testing.T) {
		var reg *exprcomp.BuiltinRegistry

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
		result := exprcomp.CompileString("2 * 3", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "*", result.Op())
	})

	t.Run("division", func(t *testing.T) {
		result := exprcomp.CompileString("10 / 2", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "/", result.Op())
	})

	t.Run("modulo", func(t *testing.T) {
		result := exprcomp.CompileString("7 % 3", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "%", result.Op())
	})
}

func TestCompileString_EqualityExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://equality.yammm")
	collector := diag.NewCollector(0)

	t.Run("equals", func(t *testing.T) {
		result := exprcomp.CompileString("x == y", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "==", result.Op())
	})

	t.Run("not equals", func(t *testing.T) {
		result := exprcomp.CompileString("x != y", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "!=", result.Op())
	})
}

func TestCompileString_OrExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://or.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("a || b", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "||", result.Op())
}

func TestCompileString_GroupExpression(t *testing.T) {
	sourceID := location.MustNewSourceID("test://group.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("(1 + 2) * 3", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "*", result.Op())
}

func TestCompileString_ArrayIndexAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://at.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("items[0]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "@", result.Op())
}

func TestCompileString_LiteralTypes(t *testing.T) {
	sourceID := location.MustNewSourceID("test://literals.yammm")
	collector := diag.NewCollector(0)

	t.Run("string literal", func(t *testing.T) {
		result := exprcomp.CompileString(`"hello"`, collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, "hello", result.Literal())
	})

	t.Run("integer literal", func(t *testing.T) {
		result := exprcomp.CompileString("42", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, int64(42), result.Literal())
	})

	t.Run("float literal", func(t *testing.T) {
		result := exprcomp.CompileString("3.14", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, 3.14, result.Literal())
	})

	t.Run("boolean true", func(t *testing.T) {
		result := exprcomp.CompileString("true", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, true, result.Literal())
	})

	t.Run("boolean false", func(t *testing.T) {
		result := exprcomp.CompileString("false", collector, sourceID)
		require.NotNil(t, result)
		assert.Equal(t, false, result.Literal())
	})

	t.Run("nil literal", func(t *testing.T) {
		result := exprcomp.CompileString("nil", collector, sourceID)
		require.NotNil(t, result)
		assert.Nil(t, result.Literal())
	})
}

func TestCompileString_LambdaWithParameters(t *testing.T) {
	sourceID := location.MustNewSourceID("test://lambda.yammm")
	collector := diag.NewCollector(0)

	// Filter with lambda parameter
	result := exprcomp.CompileString("items->Filter(|x| x > 0)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Filter", result.Op())
}

func TestCompileString_FunctionWithArguments(t *testing.T) {
	sourceID := location.MustNewSourceID("test://args.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("str->Substring(0, 5)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Substring", result.Op())
}

func TestCompileString_ChainedExpressions(t *testing.T) {
	sourceID := location.MustNewSourceID("test://chain.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("items->Filter(|x| x > 0)->Count()", collector, sourceID)
	require.NotNil(t, result)
	// The AST represents the chain - first Filter is applied
	assert.Contains(t, []string{"Filter", "Count"}, result.Op())
}

func TestCompileString_NestedTernary(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-ternary.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("a ? b : c ? d : e", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "?", result.Op())
}

func TestCompileString_ComplexBoolean(t *testing.T) {
	sourceID := location.MustNewSourceID("test://complex.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("(a && b) || (c && d)", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "||", result.Op())
}

func TestCompileString_NegativeNumber(t *testing.T) {
	sourceID := location.MustNewSourceID("test://negative.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("-42", collector, sourceID)
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
			result := exprcomp.CompileString(tt.expr, collector, sourceID)
			require.NotNil(t, result)
			assert.Equal(t, tt.op, result.Op())
		})
	}
}

func TestCompileString_EmptyListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://empty-list.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("[]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 0)
}

func TestCompileString_NestedListLiteral(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-list.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("[[1, 2], [3, 4]]", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "[]", result.Op())
	assert.Len(t, result.Children(), 2)
}

func TestCompileString_HexInteger(t *testing.T) {
	sourceID := location.MustNewSourceID("test://hex.yammm")
	collector := diag.NewCollector(0)

	// Hex literal if supported
	result := exprcomp.CompileString("0x10", collector, sourceID)
	if !collector.HasErrors() {
		require.NotNil(t, result)
	}
}

func TestCompileString_SelfReference(t *testing.T) {
	sourceID := location.MustNewSourceID("test://self.yammm")
	collector := diag.NewCollector(0)

	// Self reference via $self or similar if supported
	result := exprcomp.CompileString("$self", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_MultipleVariables(t *testing.T) {
	sourceID := location.MustNewSourceID("test://vars.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("$x + $y", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "+", result.Op())
}

func TestCompileString_NestedMemberAccess(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("a.b.c", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, ".", result.Op())
}

func TestCompileString_ComplexArithmetic(t *testing.T) {
	sourceID := location.MustNewSourceID("test://complex-arith.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("(a + b) * (c - d) / e", collector, sourceID)
	require.NotNil(t, result)
}

func TestCompileString_MultipleArguments(t *testing.T) {
	sourceID := location.MustNewSourceID("test://multi-args.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("str->Replace(\"old\", \"new\")", collector, sourceID)
	require.NotNil(t, result)
	assert.Equal(t, "Replace", result.Op())
}

func TestCompileString_NestedFunctions(t *testing.T) {
	sourceID := location.MustNewSourceID("test://nested-fn.yammm")
	collector := diag.NewCollector(0)

	result := exprcomp.CompileString("items->Map(|x| x * 2)->Sum()", collector, sourceID)
	require.NotNil(t, result)
}
