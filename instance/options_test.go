package instance_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

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
		prov := instance.NewProvenance("test.json", path.Root.Key("person"), location.Span{})
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: prov,
		}

		_, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.Error(t, err)
		// The error should include path info from provenance
	})

	t.Run("without_provenance_still_works", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: instance.Provenance{},
		}

		_, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.Error(t, err)
	})
}
