package expr_test

import (
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

func TestSExpr_Empty(t *testing.T) {
	empty := expr.SExpr{}

	assert.Empty(t, empty.Op())
	// Empty SExpr's Literal returns the op string which is ""
	assert.Empty(t, empty.Literal())
	assert.Nil(t, empty.Children())
}
