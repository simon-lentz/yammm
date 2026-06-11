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

// TestCompileString_Ops drives every operator and expression form through
// one table: each source compiles without diagnostics and roots at the
// expected op (with child counts where the arity is part of the contract).
// A chained ->call roots at the FIRST call in the chain.
func TestCompileString_Ops(t *testing.T) {
	tests := []struct {
		name         string
		src          string
		wantOp       string
		wantChildren int // -1: don't check
	}{
		{"addition", "1 + 2", "+", 2},
		{"comparison", "x > 0", ">", -1},
		{"logical and", "a && b", "&&", -1},
		{"logical or", "a || b", "||", -1},
		{"complex boolean", "(a && b) || (c && d)", "||", -1},
		{"property access", "name", "p", -1},
		{"variable", "$x", "$", -1},
		{"self reference", "$self", "$", -1},
		{"variable arithmetic", "$x + $y", "+", -1},
		{"list literal", "[1, 2, 3]", "[]", 3},
		{"empty list literal", "[]", "[]", 0},
		{"nested list literal", "[[1, 2], [3, 4]]", "[]", 2},
		{"function call", "items->Count()", "Count", -1},
		// Unknown functions compile successfully — validation is deferred
		// to the eval layer, supporting runtime builtin registration.
		{"unknown function", "items->UnknownFunc()", "UnknownFunc", -1},
		{"lambda parameter", "items->Filter |$x| { $x > 0 }", "Filter", -1},
		{"function arguments", "str->Substring(0, 5)", "Substring", -1},
		{"multiple arguments", `str->Replace("old", "new")`, "Replace", -1},
		// The pipeline operator is left-associative, so a chain roots at
		// the LAST call: items->Filter…->Count() is Count(Filter(items)).
		{"chained calls root at last", "items->Filter |$x| { $x > 0 }->Count()", "Count", -1},
		{"chained map sum roots at last", "items->Map |$x| { $x * 2 }->Sum()", "Sum", -1},
		// Ternary branches are braced: cond ? { then : else }.
		{"ternary", "a ? { b : c }", "?", 3},
		{"nested ternary", "a ? { b : c ? { d : e } }", "?", -1},
		{"unary minus", "-x", "-x", -1},
		{"negative number", "-42", "-x", -1},
		{"not", "!done", "!", -1},
		{"member access", "obj.prop", ".", -1},
		{"nested member access", "a.b.c", ".", -1},
		{"in operator", "x in [1, 2, 3]", "in", -1},
		{"regexp match", "name =~ /^A/", "=~", -1},
		{"multiplication", "2 * 3", "*", -1},
		{"division", "10 / 2", "/", -1},
		{"modulo", "7 % 3", "%", -1},
		{"group precedence", "(1 + 2) * 3", "*", -1},
		{"complex arithmetic", "(a + b) * (c - d) / e", "/", 2},
		{"array index", "items[0]", "@", -1},
		{"equals", "x == y", "==", -1},
		{"not equals", "x != y", "!=", -1},
		{"less", "a < b", "<", -1},
		{"less or equal", "a <= b", "<=", -1},
		{"greater", "a > b", ">", -1},
		{"greater or equal", "a >= b", ">=", -1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceID := location.MustNewSourceID("test://ops.yammm")
			collector := diag.NewCollector(0)

			result := exprcomp.CompileString(tt.src, collector, sourceID)
			require.False(t, collector.HasErrors(), "compile %q: %s", tt.src, collector.Result().String())
			require.NotNil(t, result)
			assert.Equal(t, tt.wantOp, result.Op(), "op for %q", tt.src)
			if tt.wantChildren >= 0 {
				assert.Len(t, result.Children(), tt.wantChildren, "children for %q", tt.src)
			}
		})
	}
}

// TestCompileString_Literals pins the literal value (and Go type) each
// literal form compiles to.
func TestCompileString_Literals(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want any
	}{
		{"string", `"hello"`, "hello"},
		{"integer", "42", int64(42)},
		{"float", "3.14", 3.14},
		{"float signless exponent", "2.5e10", 2.5e10},
		{"float signed exponent", "1.0e-5", 1.0e-5},
		{"boolean true", "true", true},
		{"boolean false", "false", false},
		{"nil", "nil", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceID := location.MustNewSourceID("test://literals.yammm")
			collector := diag.NewCollector(0)

			result := exprcomp.CompileString(tt.src, collector, sourceID)
			require.False(t, collector.HasErrors(), "compile %q: %s", tt.src, collector.Result().String())
			require.NotNil(t, result)
			assert.Equal(t, tt.want, result.Literal())
		})
	}
}

// TestCompileString_RejectsInvalidSource pins that an expression source
// that does not parse cleanly fails compilation instead of silently
// compiling whatever prefix the parser recovered: syntax errors land in
// the collector and the result is nil. Hexadecimal is the canonical case —
// numeric literals are decimal, so the lexer marks "0x10" malformed rather
// than taking the leading zero and dropping the rest.
func TestCompileString_RejectsInvalidSource(t *testing.T) {
	tests := []struct {
		name      string
		src       string
		wantInMsg string
	}{
		{"hex literal", "0x10", "malformed numeric literal"},
		{"integer with suffix", "42abc", "malformed numeric literal"},
		{"trailing garbage after expression", "1 + 2 )", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sourceID := location.MustNewSourceID("test://invalid.yammm")
			collector := diag.NewCollector(0)

			result := exprcomp.CompileString(tt.src, collector, sourceID)
			assert.Nil(t, result, "invalid source %q must not compile", tt.src)
			require.True(t, collector.HasErrors(), "invalid source %q must collect a syntax error", tt.src)
			if tt.wantInMsg != "" {
				assert.Contains(t, collector.Result().Err().Error(), tt.wantInMsg)
			}
		})
	}
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

	require.NoError(t, err)
	assert.True(t, reg.Has("CustomFunc"))
}

func TestBuiltinRegistry_Register_ErrorOnDuplicate(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	err := reg.Register("Count") // Already exists in defaults

	require.Error(t, err)
	assert.Contains(t, err.Error(), "already registered")
}

func TestBuiltinRegistry_Register_ErrorOnEmpty(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	err := reg.Register("")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestBuiltinRegistry_Register_ErrorOnNilReceiver(t *testing.T) {
	var reg *exprcomp.BuiltinRegistry

	err := reg.Register("Anything")

	require.Error(t, err)
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

			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMatch)
		})
	}
}

func TestBuiltinRegistry_Register_ErrorMessageQuoted(t *testing.T) {
	reg := exprcomp.NewBuiltinRegistry()

	// Register duplicate should use %q format (quoted)
	err := reg.Register("Count")

	require.Error(t, err)
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
	assert.NotEmpty(t, names)

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
		require.NoError(t, err)
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
