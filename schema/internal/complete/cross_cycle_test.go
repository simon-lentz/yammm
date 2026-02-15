package complete_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/simon-lentz/yammm/schema/build"
	"github.com/simon-lentz/yammm/schema/internal/complete"
)

func TestDetectCrossSchemaInheritanceCycles_NilRegistry(t *testing.T) {
	issues := complete.DetectCrossSchemaInheritanceCycles(nil)
	assert.Nil(t, issues)
}

func TestDetectCrossSchemaInheritanceCycles_EmptyRegistry(t *testing.T) {
	registry := schema.NewRegistry()
	issues := complete.DetectCrossSchemaInheritanceCycles(registry)
	assert.Empty(t, issues)
}

func TestDetectCrossSchemaInheritanceCycles_SingleSchema_NoCycle(t *testing.T) {
	// Single schema with no cycles
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Person").
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	require.False(t, result.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(s))

	issues := complete.DetectCrossSchemaInheritanceCycles(registry)
	assert.Empty(t, issues)
}

func TestDetectCrossSchemaInheritanceCycles_LocalInheritance_NoCycle(t *testing.T) {
	// Single schema with local inheritance (no cycles)
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("Entity").
		AsAbstract().
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		AddType("Person").
		Extends(schema.LocalTypeRef("Entity", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	require.False(t, result.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(s))

	issues := complete.DetectCrossSchemaInheritanceCycles(registry)
	assert.Empty(t, issues)
}

func TestDetectCrossSchemaInheritanceCycles_CrossSchema_NoCycle(t *testing.T) {
	// Two schemas: derived extends base - no cycle
	baseSchema, baseResult := build.NewBuilder().
		WithName("base").
		WithSourceID(location.MustNewSourceID("test://base.yammm")).
		AddType("Entity").
		AsAbstract().
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		Build()

	require.NotNil(t, baseSchema)
	require.False(t, baseResult.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(baseSchema))

	derivedSchema, derivedResult := build.NewBuilder().
		WithName("derived").
		WithSourceID(location.MustNewSourceID("test://derived.yammm")).
		WithRegistry(registry).
		AddImport("base", "base").
		AddType("Person").
		Extends(schema.NewTypeRef("base", "Entity", location.Span{})).
		WithProperty("name", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, derivedSchema)
	require.False(t, derivedResult.HasErrors())
	require.NoError(t, registry.Register(derivedSchema))

	issues := complete.DetectCrossSchemaInheritanceCycles(registry)
	assert.Empty(t, issues)
}

func TestDetectCrossSchemaInheritanceCycles_Diamond_NoCycle(t *testing.T) {
	// Diamond inheritance: D extends B, C; B extends A; C extends A
	// This is NOT a cycle - it's a valid diamond pattern
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("A").
		AsAbstract().
		WithProperty("id", schema.NewUUIDConstraint()).
		Done().
		AddType("B").
		AsAbstract().
		Extends(schema.LocalTypeRef("A", location.Span{})).
		WithProperty("b_prop", schema.NewStringConstraint()).
		Done().
		AddType("C").
		AsAbstract().
		Extends(schema.LocalTypeRef("A", location.Span{})).
		WithProperty("c_prop", schema.NewStringConstraint()).
		Done().
		AddType("D").
		Extends(schema.LocalTypeRef("B", location.Span{})).
		Extends(schema.LocalTypeRef("C", location.Span{})).
		WithProperty("d_prop", schema.NewStringConstraint()).
		Done().
		Build()

	require.NotNil(t, s)
	require.False(t, result.HasErrors())

	registry := schema.NewRegistry()
	require.NoError(t, registry.Register(s))

	issues := complete.DetectCrossSchemaInheritanceCycles(registry)
	assert.Empty(t, issues, "Diamond inheritance should not be detected as a cycle")
}

func TestDetectCrossSchemaInheritanceCycles_SimpleCycle(t *testing.T) {
	// Local cycle: A extends B, B extends A
	// Note: This would be caught by the existing local cycle detection,
	// but the cross-schema detector should also catch it.
	s, result := build.NewBuilder().
		WithName("test").
		WithSourceID(location.MustNewSourceID("test://test.yammm")).
		AddType("A").
		Extends(schema.LocalTypeRef("B", location.Span{})).
		WithProperty("a_prop", schema.NewStringConstraint()).
		Done().
		AddType("B").
		Extends(schema.LocalTypeRef("A", location.Span{})).
		WithProperty("b_prop", schema.NewStringConstraint()).
		Done().
		Build()

	// The builder/completion phase already catches this cycle
	// so we can't test the cross-schema detector on it directly
	require.Nil(t, s, "Schema with local cycle should not build")
	require.True(t, result.HasErrors())

	// Verify it contains E_INHERIT_CYCLE
	hasInheritCycle := false
	for issue := range result.Issues() {
		if issue.Code() == diag.E_INHERIT_CYCLE {
			hasInheritCycle = true
			break
		}
	}
	assert.True(t, hasInheritCycle, "Should detect inheritance cycle")
}
