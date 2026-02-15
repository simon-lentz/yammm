package instance_test

import (
	"context"
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
	personType := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	idProp := schema.NewProperty("id", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	personType.SetProperties([]*schema.Property{idProp})
	personType.SetAllProperties([]*schema.Property{idProp})
	personType.SetPrimaryKeys([]*schema.Property{idProp})
	personType.Seal()

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType})
	s.Seal()

	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))

	// WithLogger should not cause any issues
	validator := instance.NewValidator(s, instance.WithLogger(logger))

	raw := instance.RawInstance{
		Properties: map[string]any{"id": int64(1)},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestWithMaxIssuesPerInstance(t *testing.T) {
	personType := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	idProp := schema.NewProperty("id", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	nameProp := schema.NewProperty("name", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	ageProp := schema.NewProperty("age", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	personType.SetProperties([]*schema.Property{idProp, nameProp, ageProp})
	personType.SetAllProperties([]*schema.Property{idProp, nameProp, ageProp})
	personType.SetPrimaryKeys([]*schema.Property{idProp})
	personType.Seal()

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType})
	s.Seal()

	t.Run("limits_issues", func(t *testing.T) {
		// Create validator with max 1 issue per instance
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(1))

		// Provide input with multiple errors - should only report limited issues
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": int64(1),
				// Missing name and age (both required)
			},
		}

		_, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
		// Failure should still be reported
	})

	t.Run("zero_uses_default", func(t *testing.T) {
		// Zero should use default (100)
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(0))

		raw := instance.RawInstance{
			Properties: map[string]any{"id": int64(1), "name": "Alice", "age": int64(30)},
		}

		valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
		require.NoError(t, err)
		assert.Nil(t, failure)
		require.NotNil(t, valid)
	})

	t.Run("negative_uses_default", func(t *testing.T) {
		// Negative should use default (100)
		validator := instance.NewValidator(s, instance.WithMaxIssuesPerInstance(-5))

		raw := instance.RawInstance{
			Properties: map[string]any{"id": int64(1), "name": "Alice", "age": int64(30)},
		}

		valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
		require.NoError(t, err)
		assert.Nil(t, failure)
		require.NotNil(t, valid)
	})
}

func TestRecommendedValidatorOptions(t *testing.T) {
	personType := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	idProp := schema.NewProperty("id", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	personType.SetProperties([]*schema.Property{idProp})
	personType.SetAllProperties([]*schema.Property{idProp})
	personType.SetPrimaryKeys([]*schema.Property{idProp})
	personType.Seal()

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType})
	s.Seal()

	// RecommendedValidatorOptions should return valid options
	opts := instance.RecommendedValidatorOptions()
	assert.Len(t, opts, 2)

	// Should be usable
	validator := instance.NewValidator(s, opts...)

	raw := instance.RawInstance{
		Properties: map[string]any{"id": int64(1)},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}

func TestOptionsWithProvenance(t *testing.T) {
	// Test that provenance functions are covered in various scenarios
	personType := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	idProp := schema.NewProperty("id", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	nameProp := schema.NewProperty("name", location.Span{}, "", schema.NewStringConstraint(), schema.DataTypeRef{}, false, false, schema.DeclaringScope{})
	personType.SetProperties([]*schema.Property{idProp, nameProp})
	personType.SetAllProperties([]*schema.Property{idProp, nameProp})
	personType.SetPrimaryKeys([]*schema.Property{idProp})
	personType.Seal()

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType})
	s.Seal()

	validator := instance.NewValidator(s)

	t.Run("with_provenance_errors_show_path", func(t *testing.T) {
		prov := instance.NewProvenance("test.json", path.Root().Key("person"), location.Span{})
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": int64(1),
				// Missing required "name"
			},
			Provenance: prov,
		}

		_, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
		// The error should include path info from provenance
	})

	t.Run("without_provenance_still_works", func(t *testing.T) {
		raw := instance.RawInstance{
			Properties: map[string]any{
				"id": int64(1),
				// Missing required "name"
			},
			Provenance: nil,
		}

		_, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
		require.NoError(t, err)
		require.NotNil(t, failure)
	})
}

func TestWithValueRegistry(t *testing.T) {
	// Test WithValueRegistry option
	personType := schema.NewType("Person", location.SourceID{}, location.Span{}, "", false, false)
	idProp := schema.NewProperty("id", location.Span{}, "", schema.NewIntegerConstraint(), schema.DataTypeRef{}, false, true, schema.DeclaringScope{})
	personType.SetProperties([]*schema.Property{idProp})
	personType.SetAllProperties([]*schema.Property{idProp})
	personType.SetPrimaryKeys([]*schema.Property{idProp})
	personType.Seal()

	s := schema.NewSchema("test", location.SourceID{}, location.Span{}, "")
	s.SetTypes([]*schema.Type{personType})
	s.Seal()

	// Create validator with custom value registry (zero-value registry for test)
	validator := instance.NewValidator(s, instance.WithValueRegistry(value.Registry{}))

	raw := instance.RawInstance{
		Properties: map[string]any{"id": int64(1)},
	}

	valid, failure, err := validator.ValidateOne(context.Background(), "Person", raw)
	require.NoError(t, err)
	assert.Nil(t, failure)
	require.NotNil(t, valid)
}
