package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

func TestNewImport(t *testing.T) {
	sourceID := location.MustNewSourceID("test://types")
	span := location.Span{
		Source: location.MustNewSourceID("test://schema"),
		Start:  location.Position{Line: 2, Column: 1, Byte: 10},
		End:    location.Position{Line: 2, Column: 25, Byte: 35},
	}

	imp := schema.TestNewImport("./types.yammm", "types", sourceID, span)

	assert.NotNil(t, imp)
	assert.Equal(t, "./types.yammm", imp.Path())
	assert.Equal(t, "types", imp.Alias())
	assert.Equal(t, sourceID, imp.ResolvedSourceID())
	assert.Equal(t, span, imp.Span())
}

func TestImport_Path(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"relative path", "./types.yammm", "./types.yammm"},
		{"parent path", "../common/types.yammm", "../common/types.yammm"},
		{"simple name", "types.yammm", "types.yammm"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp := schema.TestNewImport(tt.path, "alias", location.SourceID{}, location.Span{})
			assert.Equal(t, tt.expected, imp.Path())
		})
	}
}

func TestImport_Alias(t *testing.T) {
	tests := []struct {
		name     string
		alias    string
		expected string
	}{
		{"simple alias", "types", "types"},
		{"short alias", "t", "t"},
		{"underscore alias", "common_types", "common_types"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			imp := schema.TestNewImport("./file.yammm", tt.alias, location.SourceID{}, location.Span{})
			assert.Equal(t, tt.expected, imp.Alias())
		})
	}
}

func TestImport_ResolvedSourceID(t *testing.T) {
	sourceID := location.MustNewSourceID("file:///absolute/path/types.yammm")

	imp := schema.TestNewImport("./types.yammm", "types", sourceID, location.Span{})

	assert.Equal(t, sourceID, imp.ResolvedSourceID())
}

func TestImport_ResolvedSourceID_Zero(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	assert.True(t, imp.ResolvedSourceID().IsZero())
}

func TestImport_ResolvedPath_WithSourceID(t *testing.T) {
	sourceID := location.MustNewSourceID("test://resolved/path")

	imp := schema.TestNewImport("./types.yammm", "types", sourceID, location.Span{})

	// ResolvedPath returns the string representation of the resolved source ID
	result := imp.ResolvedPath()
	assert.Contains(t, result, "resolved/path")
}

func TestImport_ResolvedPath_ZeroSourceID(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	// Returns empty string for zero SourceID
	result := imp.ResolvedPath()
	assert.Empty(t, result)
}

func TestImport_Schema_Nil(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	assert.Nil(t, imp.Schema())
}

func TestImport_Schema_Resolved(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	s := schema.TestNewSchema("types", location.SourceID{}, location.Span{}, "")

	schema.TestSetImportSchema(imp, s)

	assert.Same(t, s, imp.Schema())
}

func TestImport_Span(t *testing.T) {
	span := location.Span{
		Source: location.MustNewSourceID("test://span"),
		Start:  location.Position{Line: 3, Column: 1, Byte: 20},
		End:    location.Position{Line: 3, Column: 30, Byte: 50},
	}

	imp := schema.TestNewImport("./file.yammm", "file", location.SourceID{}, span)

	result := imp.Span()
	assert.Equal(t, span.Source, result.Source)
	assert.Equal(t, 3, result.Start.Line)
	assert.Equal(t, 20, result.Start.Byte)
}

func TestImport_Seal_PreventsSetResolvedSourceID(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	schema.TestSealImport(imp)

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetResolvedSourceID after Seal, but no panic occurred")
		}
	}()

	schema.TestSetImportResolvedSourceID(imp, location.MustNewSourceID("test://new"))
}

func TestImport_Seal_PreventsSetSchema(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	schema.TestSealImport(imp)

	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic on SetSchema after Seal, but no panic occurred")
		}
	}()

	schema.TestSetImportSchema(imp, schema.TestNewSchema("test", location.SourceID{}, location.Span{}, ""))
}

func TestImport_SettersWorkBeforeSeal(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	// These should not panic before sealing
	newSourceID := location.MustNewSourceID("test://updated")
	schema.TestSetImportResolvedSourceID(imp, newSourceID)
	assert.Equal(t, newSourceID, imp.ResolvedSourceID())

	s := schema.TestNewSchema("types", location.SourceID{}, location.Span{}, "")
	schema.TestSetImportSchema(imp, s)
	assert.Same(t, s, imp.Schema())
}

func TestImport_SetResolvedSourceID_UpdatesValue(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	newID := location.MustNewSourceID("test://updated")
	schema.TestSetImportResolvedSourceID(imp, newID)

	assert.Equal(t, newID, imp.ResolvedSourceID())
}

func TestImport_SetSchema_UpdatesValue(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})
	s := schema.TestNewSchema("imported", location.SourceID{}, location.Span{}, "Imported schema")

	schema.TestSetImportSchema(imp, s)

	assert.Same(t, s, imp.Schema())
	assert.Equal(t, "imported", imp.Schema().Name())
}

func TestImport_IsSealed(t *testing.T) {
	imp := schema.TestNewImport("./types.yammm", "types", location.SourceID{}, location.Span{})

	// New import should not be sealed
	assert.False(t, schema.TestIsSealedImport(imp), "new import should not be sealed")

	// After sealing, IsSealed should return true
	schema.TestSealImport(imp)
	assert.True(t, schema.TestIsSealedImport(imp), "sealed import should report IsSealed() == true")
}
