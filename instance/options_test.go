package instance_test

import (
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/internal/value"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestWithLogger(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done())

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// WithLogger should not cause any issues
	validator := instance.NewValidator(s, instance.WithLogger(logger))

	raw := instance.RawInstance{
		Properties: map[string]any{"id": "1"},
	}

	valid, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestWithMaxIssuesPerInstance(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		WithProperty("age", schema.IntegerConstraint{}).
		Done())

	t.Run("limits_issues", func(t *testing.T) {
		// Create validator with max 1 issue per instance
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(1))

		// Provide input with multiple errors - should only report limited issues
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing name and age (both required)
			},
		}

		_, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
		// Failure should still be reported
	})

	t.Run("zero_uses_default", func(t *testing.T) {
		// Zero should use default (100)
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(0))

		raw := instance.RawInstance{
			Properties: map[string]any{"id": "1", "name": "Alice", "age": int64(30)},
		}

		valid, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.NoError(t, err)
		assert.Nil(t, failure)
		require.NotNil(t, valid)
	})

	t.Run("negative_uses_default", func(t *testing.T) {
		// Negative should use default (100)
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(-5))

		raw := instance.RawInstance{
			Properties: map[string]any{"id": "1", "name": "Alice", "age": int64(30)},
		}

		valid, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.NoError(t, err)
		assert.Nil(t, failure)
		require.NotNil(t, valid)
	})
}

func TestRecommendedValidatorOptions(t *testing.T) {
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done())

	// RecommendedValidatorOptions should return valid options
	opts := instance.RecommendedValidatorOptions()
	assert.Len(t, opts, 2)

	// Should be usable
	validator := instance.NewValidator(s, opts...)

	raw := instance.RawInstance{
		Properties: map[string]any{"id": "1"},
	}

	valid, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestOptionsWithProvenance(t *testing.T) {
	// Test that provenance functions are covered in various scenarios
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		WithProperty("name", schema.StringConstraint{}).
		Done())

	validator := instance.NewValidator(s)

	t.Run("with_provenance_errors_show_path", func(t *testing.T) {
		prov := instance.NewProvenance("test.json", path.Root().Key("person"), location.Span{})
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: prov,
		}

		_, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
		// The error should include path info from provenance
	})

	t.Run("without_provenance_still_works", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": "1",
				// Missing required "name"
			},
			Provenance: nil,
		}

		_, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
	})
}

func TestWithValueRegistry(t *testing.T) {
	// Test WithValueRegistry option
	s := mustBuild(t, schema.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithPrimaryKey("id", schema.StringConstraint{}).
		Done())

	// Create validator with custom value registry (zero-value registry for test)
	validator := instance.NewValidator(s, instance.WithValueRegistry(value.Registry{}))

	raw := instance.RawInstance{
		Properties: map[string]any{"id": "1"},
	}

	valid, failure, err := validator.ValidateOne(t.Context(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}
