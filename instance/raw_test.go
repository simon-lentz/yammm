package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

func TestNewProvenance(t *testing.T) {
	sourceName := "test.json"
	p := path.Root().Key("users").Index(0)
	span := location.Range(location.SourceID{}, 10, 1, 20, 50)

	prov := location.NewProvenance(sourceName, p, span)

	assert.Equal(t, sourceName, prov.SourceName())
	assert.Equal(t, p.String(), prov.Path().String())
	assert.Equal(t, span, prov.Span())
}

func TestProvenance_SourceName(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		prov := location.NewProvenance("data.json", path.Root(), location.Span{})
		assert.Equal(t, "data.json", prov.SourceName())
	})

	t.Run("nil_returns_empty", func(t *testing.T) {
		var prov *location.Provenance
		assert.Empty(t, prov.SourceName())
	})
}

func TestProvenance_Path(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		p := path.Root().Key("items").Index(5)
		prov := location.NewProvenance("test.json", p, location.Span{})
		assert.Equal(t, `$.items[5]`, prov.Path().String())
	})

	t.Run("nil_returns_root", func(t *testing.T) {
		var prov *location.Provenance
		assert.Equal(t, "$", prov.Path().String())
	})
}

func TestProvenance_Span(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 1, 1, 10, 100)
		prov := location.NewProvenance("test.json", path.Root(), span)
		assert.Equal(t, span, prov.Span())
	})

	t.Run("nil_returns_zero", func(t *testing.T) {
		var prov *location.Provenance
		assert.Equal(t, location.Span{}, prov.Span())
	})
}

func TestProvenance_AtKey(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Range(location.SourceID{}, 5, 1, 10, 20)
		prov := location.NewProvenance("test.json", path.Root().Key("users"), span)

		extended := prov.AtKey("name")

		assert.Equal(t, "test.json", extended.SourceName())
		assert.Equal(t, `$.users.name`, extended.Path().String())
		assert.Equal(t, span, extended.Span())
		// Original should be unchanged
		assert.Equal(t, `$.users`, prov.Path().String())
	})

	t.Run("nil_creates_new_with_key", func(t *testing.T) {
		var prov *location.Provenance

		extended := prov.AtKey("property")

		assert.Empty(t, extended.SourceName())
		assert.Equal(t, `$.property`, extended.Path().String())
	})

	t.Run("special_characters_escaped", func(t *testing.T) {
		prov := location.NewProvenance("test.json", path.Root(), location.Span{})

		extended := prov.AtKey("my field")

		assert.Equal(t, `$["my field"]`, extended.Path().String())
	})
}

func TestRawInstance(t *testing.T) {
	t.Run("with_provenance", func(t *testing.T) {
		prov := location.NewProvenance("data.json", path.Root().Key("person"), location.Span{})
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
