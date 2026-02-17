package negative_bounds_test

import (
	"context"
	"os"
	"testing"

	jsonadapter "github.com/simon-lentz/yammm/adapter/json"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/load"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_NegativeBounds tests that negative numeric bounds in Float constraints
// work correctly end-to-end: schema loading, JSON parsing, and instance validation.
func TestE2E_NegativeBounds(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// Load schema with negative float bounds
	s, result, err := load.Load(ctx, "negative_bounds.yammm")
	require.NoError(t, err, "load schema")
	require.True(t, result.OK(), "schema has errors: %v", result.Messages())

	// Verify the constraint bounds were parsed correctly
	geoPointType, ok := s.Type("GeoPoint")
	require.True(t, ok, "GeoPoint type should exist")

	latProp, ok := geoPointType.Property("latitude")
	require.True(t, ok, "latitude property should exist")
	latConstraint, ok := latProp.Constraint().(schema.FloatConstraint)
	require.True(t, ok, "latitude should have FloatConstraint")
	latMin, hasMin := latConstraint.Min()
	assert.True(t, hasMin)
	assert.Equal(t, -90.0, latMin, "latitude min should be -90.0")
	latMax, hasMax := latConstraint.Max()
	assert.True(t, hasMax)
	assert.Equal(t, 90.0, latMax, "latitude max should be 90.0")

	lonProp, ok := geoPointType.Property("longitude")
	require.True(t, ok, "longitude property should exist")
	lonConstraint, ok := lonProp.Constraint().(schema.FloatConstraint)
	require.True(t, ok, "longitude should have FloatConstraint")
	lonMin, hasMin := lonConstraint.Min()
	assert.True(t, hasMin)
	assert.Equal(t, -180.0, lonMin, "longitude min should be -180.0")
	lonMax, hasMax := lonConstraint.Max()
	assert.True(t, hasMax)
	assert.Equal(t, 180.0, lonMax, "longitude max should be 180.0")

	// Load test data
	dataBytes, err := os.ReadFile("data.json")
	require.NoError(t, err, "read test data")

	adapter, err := jsonadapter.NewAdapter(nil)
	require.NoError(t, err, "create JSON adapter")

	sourceID := location.NewSourceID("test://data.json")
	parsed, parseResult := adapter.ParseObject(sourceID, dataBytes)
	require.True(t, parseResult.OK(), "JSON parse failed: %v", parseResult.Messages())

	records := parsed["GeoPoint"]
	require.Len(t, records, 3, "expected 3 GeoPoint records")

	// Validate all records pass
	validator := instance.NewValidator(s)

	testCases := []struct {
		name  string
		index int
	}{
		{name: "valid_positive_coords", index: 0},
		{name: "valid_negative_coords", index: 1},
		{name: "valid_edge_coords", index: 2},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			valid, failure, err := validator.ValidateOne(ctx, "GeoPoint", records[tc.index])
			require.NoError(t, err)
			assert.Nil(t, failure, "expected valid instance, got failure: %v",
				failureMessages(failure))
			assert.NotNil(t, valid)
		})
	}
}

func failureMessages(f *instance.ValidationFailure) []string {
	if f == nil {
		return nil
	}
	return f.Result.Messages()
}
