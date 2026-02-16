package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance/eval"
	"github.com/stretchr/testify/assert"
)

func TestEmptyScope(t *testing.T) {
	scope := eval.EmptyScope()

	t.Run("lookup_returns_false", func(t *testing.T) {
		_, found := scope.Lookup("anything")
		assert.False(t, found)
	})

	t.Run("lookup_fold_returns_false", func(t *testing.T) {
		_, found := scope.LookupFold("ANYTHING")
		assert.False(t, found)
	})
}

func TestMapScope_WithVar(t *testing.T) {
	scope := eval.EmptyScope()

	t.Run("add_and_lookup", func(t *testing.T) {
		newScope := scope.WithVar("x", 42)
		val, found := newScope.Lookup("x")
		assert.True(t, found)
		assert.Equal(t, 42, val.Unwrap())
	})

	t.Run("original_unchanged", func(t *testing.T) {
		scope.WithVar("y", 100)
		_, found := scope.Lookup("y")
		assert.False(t, found) // original scope unaffected
	})

	t.Run("shadowing", func(t *testing.T) {
		s1 := scope.WithVar("x", 1)
		s2 := s1.WithVar("x", 2)

		v1, _ := s1.Lookup("x")
		v2, _ := s2.Lookup("x")

		assert.Equal(t, 1, v1.Unwrap())
		assert.Equal(t, 2, v2.Unwrap())
	})
}

func TestMapScope_WithSelf(t *testing.T) {
	scope := eval.EmptyScope()

	data := map[string]any{"name": "Alice"}
	newScope := scope.WithSelf(data)

	val, found := newScope.Lookup("self")
	assert.True(t, found)
	// The value is wrapped in an immutable type
	assert.NotNil(t, val.Unwrap())
}

func TestMapScope_LookupFold(t *testing.T) {
	scope := eval.EmptyScope().WithVar("UserName", "Alice")

	tests := []struct {
		name     string
		lookup   string
		expected string
		found    bool
	}{
		{"exact", "UserName", "Alice", true},
		{"lowercase", "username", "Alice", true},
		{"uppercase", "USERNAME", "Alice", true},
		{"mixed", "uSeRnAmE", "Alice", true},
		{"not_found", "other", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, found := scope.LookupFold(tt.lookup)
			assert.Equal(t, tt.found, found)
			if found {
				assert.Equal(t, tt.expected, val.Unwrap())
			}
		})
	}
}

func TestPropertyScope(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"name": "Alice",
		"age":  30,
	})
	scope := eval.PropertyScope(props)

	t.Run("lookup_property", func(t *testing.T) {
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Alice", val.Unwrap())
	})

	t.Run("lookup_missing", func(t *testing.T) {
		_, found := scope.Lookup("unknown")
		assert.False(t, found)
	})
}

func TestPropertyScope_VariablePrecedence(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"x": "from_props",
	})
	scope := eval.PropertyScope(props).WithVar("x", "from_var")

	val, found := scope.Lookup("x")
	assert.True(t, found)
	assert.Equal(t, "from_var", val.Unwrap()) // variable takes precedence
}

func TestPropertyScopeFromMap(t *testing.T) {
	props := map[string]any{
		"name": "Bob",
	}
	scope := eval.PropertyScopeFromMap(props)

	t.Run("lookup", func(t *testing.T) {
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Bob", val.Unwrap())
	})

	t.Run("isolation", func(t *testing.T) {
		// Mutating original map shouldn't affect scope
		props["name"] = "Changed"
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Bob", val.Unwrap())
	})
}

func TestPropertyScope_LookupFold(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"UserName": "Alice",
	})
	scope := eval.PropertyScope(props)

	tests := []struct {
		name     string
		lookup   string
		expected string
		found    bool
	}{
		{"exact", "UserName", "Alice", true},
		{"lowercase", "username", "Alice", true},
		{"uppercase", "USERNAME", "Alice", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, found := scope.LookupFold(tt.lookup)
			assert.Equal(t, tt.found, found)
			if found {
				assert.Equal(t, tt.expected, val.Unwrap())
			}
		})
	}
}

func TestPropertyScope_LookupFold_VariablePrecedence(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"Name": "FromProps",
	})
	scope := eval.PropertyScope(props).WithVar("NAME", "FromVar")

	// Variable should take precedence even with case-insensitive lookup
	val, found := scope.LookupFold("name")
	assert.True(t, found)
	assert.Equal(t, "FromVar", val.Unwrap())
}

func TestPropertyScope_WithSelf(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"name": "Alice",
	})
	scope := eval.PropertyScope(props)

	selfData := map[string]any{"id": int64(42)}
	newScope := scope.WithSelf(selfData)

	val, found := newScope.Lookup("self")
	assert.True(t, found)
	assert.NotNil(t, val.Unwrap())
}

func TestPropertyScope_LookupFold_NotFound(t *testing.T) {
	props := immutable.WrapPropertiesClone(map[string]any{
		"name": "Alice",
	})
	scope := eval.PropertyScope(props)

	_, found := scope.LookupFold("nonexistent")
	assert.False(t, found)
}
