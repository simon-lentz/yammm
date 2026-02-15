package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

func TestNewProvenance(t *testing.T) {
	sourceName := "test.json"
	p := path.Root().Key("users").Index(0)
	span := location.Range(location.SourceID{}, 10, 1, 20, 50)

	prov := instance.NewProvenance(sourceName, p, span)

	assert.Equal(t, sourceName, prov.SourceName())
	assert.Equal(t, p.String(), prov.Path().String())
	assert.Equal(t, span, prov.Span())
}

func TestProvenance_SourceName(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		prov := instance.NewProvenance("data.json", path.Root(), location.Span{})
		assert.Equal(t, "data.json", prov.SourceName())
	})

	t.Run("nil_returns_empty", func(t *testing.T) {
		var prov *instance.Provenance
		assert.Equal(t, "", prov.SourceName())
	})
}

func TestProvenance_Path(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		p := path.Root().Key("items").Index(5)
		prov := instance.NewProvenance("test.json", p, location.Span{})
		assert.Equal(t, `$.items[5]`, prov.Path().String())
	})

	t.Run("nil_returns_root", func(t *testing.T) {
		var prov *instance.Provenance
		assert.Equal(t, "$", prov.Path().String())
	})
}

func TestProvenance_Span(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 1, 1, 10, 100)
		prov := instance.NewProvenance("test.json", path.Root(), span)
		assert.Equal(t, span, prov.Span())
	})

	t.Run("nil_returns_zero", func(t *testing.T) {
		var prov *instance.Provenance
		assert.Equal(t, location.Span{}, prov.Span())
	})
}

func TestProvenance_WithPath(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 1, 1, 2, 3)
		original := instance.NewProvenance("test.json", path.Root().Key("old"), span)
		newPath := path.Root().Key("new").Index(0)

		updated := original.WithPath(newPath)

		assert.Equal(t, "test.json", updated.SourceName())
		assert.Equal(t, `$.new[0]`, updated.Path().String())
		assert.Equal(t, span, updated.Span())
		// Original should be unchanged
		assert.Equal(t, `$.old`, original.Path().String())
	})

	t.Run("nil_creates_new_with_path", func(t *testing.T) {
		var prov *instance.Provenance
		newPath := path.Root().Key("data")

		updated := prov.WithPath(newPath)

		assert.Equal(t, "", updated.SourceName())
		assert.Equal(t, `$.data`, updated.Path().String())
		assert.Equal(t, location.Span{}, updated.Span())
	})
}

func TestProvenance_AtKey(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 5, 1, 10, 20)
		prov := instance.NewProvenance("test.json", path.Root().Key("users"), span)

		extended := prov.AtKey("name")

		assert.Equal(t, "test.json", extended.SourceName())
		assert.Equal(t, `$.users.name`, extended.Path().String())
		assert.Equal(t, span, extended.Span())
		// Original should be unchanged
		assert.Equal(t, `$.users`, prov.Path().String())
	})

	t.Run("nil_creates_new_with_key", func(t *testing.T) {
		var prov *instance.Provenance

		extended := prov.AtKey("property")

		assert.Equal(t, "", extended.SourceName())
		assert.Equal(t, `$.property`, extended.Path().String())
	})

	t.Run("special_characters_escaped", func(t *testing.T) {
		prov := instance.NewProvenance("test.json", path.Root(), location.Span{})

		extended := prov.AtKey("my field")

		assert.Equal(t, `$["my field"]`, extended.Path().String())
	})
}

func TestProvenance_AtIndex(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 1, 1, 2, 3)
		prov := instance.NewProvenance("test.json", path.Root().Key("items"), span)

		extended := prov.AtIndex(42)

		assert.Equal(t, "test.json", extended.SourceName())
		assert.Equal(t, `$.items[42]`, extended.Path().String())
		assert.Equal(t, span, extended.Span())
		// Original should be unchanged
		assert.Equal(t, `$.items`, prov.Path().String())
	})

	t.Run("nil_creates_new_with_index", func(t *testing.T) {
		var prov *instance.Provenance

		extended := prov.AtIndex(0)

		assert.Equal(t, "", extended.SourceName())
		assert.Equal(t, `$[0]`, extended.Path().String())
	})

	t.Run("multiple_indices", func(t *testing.T) {
		prov := instance.NewProvenance("test.json", path.Root(), location.Span{})

		result := prov.AtIndex(0).AtIndex(1).AtIndex(2)

		assert.Equal(t, `$[0][1][2]`, result.Path().String())
	})
}

func TestProvenance_Chaining(t *testing.T) {
	// Test complex path construction via chaining
	prov := instance.NewProvenance("complex.json", path.Root(), location.Span{})

	result := prov.AtKey("users").AtIndex(0).AtKey("addresses").AtIndex(2).AtKey("city")

	assert.Equal(t, `$.users[0].addresses[2].city`, result.Path().String())
	assert.Equal(t, "complex.json", result.SourceName())
}

func TestRawInstance(t *testing.T) {
	t.Run("with_provenance", func(t *testing.T) {
		prov := instance.NewProvenance("data.json", path.Root().Key("person"), location.Span{})
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id":   int64(1),
				"name": "Alice",
			},
			Provenance: prov,
		}

		assert.Equal(t, int64(1), raw.Properties["id"])
		assert.Equal(t, "Alice", raw.Properties["name"])
		assert.Equal(t, "data.json", raw.Provenance.SourceName())
	})

	t.Run("without_provenance", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"value": "test",
			},
		}

		assert.Equal(t, "test", raw.Properties["value"])
		assert.Nil(t, raw.Provenance)
	})
}
