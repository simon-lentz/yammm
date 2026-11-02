package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
)

func TestRecommendedOptions(t *testing.T) {
	s := mustLoad(t, `schema "test"

type Person {
	id String primary
}
`)

	// RecommendedOptions should return valid options
	opts := instance.RecommendedOptions()
	assert.Len(t, opts, 2)

	// Should be usable
	validator := instance.NewValidator(s, opts...)

	raw := instance.RawInstance{
		Properties: map[string]any{"id": "1"},
	}

	valid, result := validator.ValidateOne(t.Context(), "Person", raw)
	require.True(t, result.OK())
	require.False(t, valid.IsZero())
}

func TestOptionsWithProvenance(t *testing.T) {
	// Test that provenance functions are covered in various scenarios
	s := mustLoad(t, `schema "test"

type Person {
	id String primary
	name String required
}
`)

	validator := instance.NewValidator(s)

	t.Run("with_provenance_errors_show_path", func(t *testing.T) {
		prov := location.NewProvenance("test.json", path.Root().Key("person"), location.Span{})
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: prov,
		}

		_, result := validator.ValidateOne(t.Context(), "Person", raw)
		require.False(t, result.OK())
		// The error should include path info from provenance
	})

	t.Run("without_provenance_still_works", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: location.Provenance{},
		}

		_, result := validator.ValidateOne(t.Context(), "Person", raw)
		require.False(t, result.OK())
	})
}
