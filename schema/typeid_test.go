package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestTypeID_Contract5_Equality(t *testing.T) {
	// TypeID is (SourceID, name) tuple for semantic equality

	path1 := location.NewSourceID("schema1.yammm")
	path2 := location.NewSourceID("schema2.yammm")

	t.Run("same path and name are equal", func(t *testing.T) {
		id1 := schema.NewTypeID(path1, "User")
		id2 := schema.NewTypeID(path1, "User")
		assert.True(t, id1 == id2, "TypeID should be comparable with ==")
	})

	t.Run("different paths are not equal", func(t *testing.T) {
		id1 := schema.NewTypeID(path1, "User")
		id2 := schema.NewTypeID(path2, "User")
		assert.False(t, id1 == id2, "TypeIDs from different schemas should not be equal")
	})

	t.Run("different names are not equal", func(t *testing.T) {
		id1 := schema.NewTypeID(path1, "User")
		id2 := schema.NewTypeID(path1, "Account")
		assert.False(t, id1 == id2, "TypeIDs with different names should not be equal")
	})

	t.Run("zero value detection", func(t *testing.T) {
		var zero schema.TypeID
		assert.True(t, zero.IsZero())

		id := schema.NewTypeID(path1, "User")
		assert.False(t, id.IsZero())
	})
}

func TestTypeID_String(t *testing.T) {
	path := location.NewSourceID("schema.yammm")

	id := schema.NewTypeID(path, "User")
	s := id.String()

	assert.Contains(t, s, "User")
}

func TestTypeRef_Syntactic(t *testing.T) {
	t.Run("local type ref", func(t *testing.T) {
		ref := schema.LocalTypeRef("User", location.Span{})
		assert.Equal(t, "User", ref.Name())
		assert.Equal(t, "", ref.Qualifier())
		assert.False(t, ref.IsQualified())
		assert.Equal(t, "User", ref.String())
	})

	t.Run("qualified type ref", func(t *testing.T) {
		ref := schema.NewTypeRef("parts", "Wheel", location.Span{})
		assert.Equal(t, "Wheel", ref.Name())
		assert.Equal(t, "parts", ref.Qualifier())
		assert.True(t, ref.IsQualified())
		assert.Equal(t, "parts.Wheel", ref.String())
	})
}
