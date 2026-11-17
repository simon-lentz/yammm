package instance_test

import (
	"testing"

	"github.com/simon-lentz/yammm/instance"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

func TestNewProvenance(t *testing.T) {
	sourceName := "test.json"
	p := path.Root.Key("users").Index(0)
	span := location.Span{Source: location.SourceID{}, Start: location.Position{Line: 10, Column: 1}, End: location.Position{Line: 20, Column: 50}}

	prov := instance.NewProvenance(sourceName, p, span)

	assert.Equal(t, sourceName, prov.SourceName())
	assert.Equal(t, p.String(), prov.Path().String())
	assert.Equal(t, span, prov.Span())
}

func TestProvenance_SourceName(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		prov := instance.NewProvenance("data.json", path.Root, location.Span{})
		assert.Equal(t, "data.json", prov.SourceName())
	})

	t.Run("nil_returns_empty", func(t *testing.T) {
		var prov instance.Provenance
		assert.Empty(t, prov.SourceName())
	})
}

func TestProvenance_Path(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		p := path.Root.Key("items").Index(5)
		prov := instance.NewProvenance("test.json", p, location.Span{})
		assert.Equal(t, `$.items[5]`, prov.Path().String())
	})

	t.Run("nil_returns_root", func(t *testing.T) {
		var prov instance.Provenance
		assert.Equal(t, "$", prov.Path().String())
	})
}

func TestProvenance_Span(t *testing.T) {
	t.Run("non_nil", func(t *testing.T) {
		span := location.Span{Source: location.SourceID{}, Start: location.Position{Line: 1, Column: 1}, End: location.Position{Line: 10, Column: 100}}
		prov := instance.NewProvenance("test.json", path.Root, span)
		assert.Equal(t, span, prov.Span())
	})

	t.Run("nil_returns_zero", func(t *testing.T) {
		var prov instance.Provenance
		assert.Equal(t, location.Span{}, prov.Span())
	})
}
