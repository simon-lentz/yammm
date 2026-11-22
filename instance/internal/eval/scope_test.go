package eval_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance/internal/eval"
	"github.com/stretchr/testify/assert"
)

func TestEmptyScope(t *testing.T) {
	scope := eval.EmptyScope()

	t.Run("lookup_returns_false", func(t *testing.T) {
		_, found := scope.Lookup("anything")
		assert.False(t, found)
	})
}

func TestMapScope_WithVar(t *testing.T) {
	scope := eval.EmptyScope()

	t.Run("add_and_lookup", func(t *testing.T) {
		newScope := scope.WithVar("x", 42)
		val, found := newScope.Lookup("x")
		assert.True(t, found)
		assert.Equal(t, 42, val.Any())
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

		assert.Equal(t, 1, v1.Any())
		assert.Equal(t, 2, v2.Any())
	})
}

func TestPropertyScope(t *testing.T) {
	props := map[string]any{
		"name": "Alice",
		"age":  30,
	}
	scope := eval.PropertyScopeFromMap(props)

	t.Run("lookup_property", func(t *testing.T) {
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Alice", val.Any())
	})

	t.Run("lookup_missing", func(t *testing.T) {
		_, found := scope.Lookup("unknown")
		assert.False(t, found)
	})
}

func TestPropertyScope_VariablePrecedence(t *testing.T) {
	props := map[string]any{
		"x": "from_props",
	}
	scope := eval.PropertyScopeFromMap(props).WithVar("x", "from_var")

	val, found := scope.Lookup("x")
	assert.True(t, found)
	assert.Equal(t, "from_var", val.Any()) // variable takes precedence
}

func TestPropertyScopeFromMap(t *testing.T) {
	props := map[string]any{
		"name": "Bob",
	}
	scope := eval.PropertyScopeFromMap(props)

	t.Run("lookup", func(t *testing.T) {
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Bob", val.Any())
	})

	t.Run("isolation", func(t *testing.T) {
		// Mutating original map shouldn't affect scope
		props["name"] = "Changed"
		val, found := scope.Lookup("name")
		assert.True(t, found)
		assert.Equal(t, "Bob", val.Any())
	})
}

// A $ variable names a member exactly: a relation by its declared name, and
// neither its field name nor a property in another casing.
func TestPropertyScope_LookupReadsAMemberByItsDeclaredName(t *testing.T) {
	scope := eval.PropertyScopeFromMap(map[string]any{"name": "Alice", "WORKS_AT": "k"})
	for lookup, want := range map[string]any{"name": "Alice", "WORKS_AT": "k", "Name": nil, "works_at": nil, "Works_At": nil} {
		val, found := scope.Lookup(lookup)
		if found != (want != nil) || (found && val.Any() != want) {
			t.Errorf("Lookup(%q) = %v, %v; want %v", lookup, val.Any(), found, want)
		}
	}
}
