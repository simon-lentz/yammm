package instance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/instance/path"
	"github.com/simon-lentz/yammm/location"
)

func TestNewInstance(t *testing.T) {
	raw := NewInstance().Build()

	assert.NotNil(t, raw.Properties)
	assert.Empty(t, raw.Properties)
	assert.Nil(t, raw.Provenance)
}

func TestInstanceBuilder_Prop(t *testing.T) {
	t.Run("single property", func(t *testing.T) {
		raw := NewInstance().
			Prop("name", "Alice").
			Build()

		assert.Equal(t, "Alice", raw.Properties["name"])
	})

	t.Run("multiple properties", func(t *testing.T) {
		raw := NewInstance().
			Prop("id", "p1").
			Prop("name", "Alice").
			Prop("age", 30).
			Build()

		assert.Equal(t, "p1", raw.Properties["id"])
		assert.Equal(t, "Alice", raw.Properties["name"])
		assert.Equal(t, 30, raw.Properties["age"])
	})

	t.Run("overwrite property", func(t *testing.T) {
		raw := NewInstance().
			Prop("name", "Alice").
			Prop("name", "Bob").
			Build()

		assert.Equal(t, "Bob", raw.Properties["name"])
	})

	t.Run("nil value", func(t *testing.T) {
		raw := NewInstance().
			Prop("optional", nil).
			Build()

		val, exists := raw.Properties["optional"]
		assert.True(t, exists)
		assert.Nil(t, val)
	})
}

func TestInstanceBuilder_Edge(t *testing.T) {
	t.Run("simple edge with target", func(t *testing.T) {
		raw := NewInstance().
			Prop("vin", "ABC123").
			Edge("owner").Target("person-1").Done().
			Build()

		edge, ok := raw.Properties["owner"].(map[string]any)
		require.True(t, ok, "edge should be a map")
		assert.Equal(t, "person-1", edge["_target_id"])
	})

	t.Run("edge with composite FK", func(t *testing.T) {
		raw := NewInstance().
			Edge("customer").
			TargetField("firstName", "John").
			TargetField("lastName", "Doe").
			Done().
			Build()

		edge, ok := raw.Properties["customer"].(map[string]any)
		require.True(t, ok, "edge should be a map")
		assert.Equal(t, "John", edge["_target_firstName"])
		assert.Equal(t, "Doe", edge["_target_lastName"])
	})

	t.Run("edge with properties", func(t *testing.T) {
		raw := NewInstance().
			Edge("manager").
			Target("mgr-1").
			Prop("since", "2020-01-01").
			Done().
			Build()

		edge, ok := raw.Properties["manager"].(map[string]any)
		require.True(t, ok, "edge should be a map")
		assert.Equal(t, "mgr-1", edge["_target_id"])
		assert.Equal(t, "2020-01-01", edge["since"])
	})
}

func TestInstanceBuilder_Edges(t *testing.T) {
	t.Run("many association", func(t *testing.T) {
		raw := NewInstance().
			Prop("id", "alice").
			Edges("knows",
				NewEdge().Target("bob").Prop("weight", 0.9),
				NewEdge().Target("carol").Prop("weight", 0.5),
			).
			Build()

		edges, ok := raw.Properties["knows"].([]any)
		require.True(t, ok, "edges should be an array")
		require.Len(t, edges, 2)

		edge0, ok := edges[0].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "bob", edge0["_target_id"])
		assert.Equal(t, 0.9, edge0["weight"])

		edge1, ok := edges[1].(map[string]any)
		require.True(t, ok)
		assert.Equal(t, "carol", edge1["_target_id"])
		assert.Equal(t, 0.5, edge1["weight"])
	})

	t.Run("empty edges array", func(t *testing.T) {
		raw := NewInstance().
			Edges("empty").
			Build()

		edges, ok := raw.Properties["empty"].([]any)
		require.True(t, ok)
		assert.Empty(t, edges)
	})
}

func TestInstanceBuilder_Composed(t *testing.T) {
	raw := NewInstance().
		Prop("vin", "CAR-001").
		Composed("engine",
			NewInstance().Prop("serial", "ENG-001"),
		).
		Build()

	engine, ok := raw.Properties["engine"].(map[string]any)
	require.True(t, ok, "composed should be a map")
	assert.Equal(t, "ENG-001", engine["serial"])
}

func TestInstanceBuilder_ComposedMany(t *testing.T) {
	raw := NewInstance().
		Prop("vin", "CAR-001").
		ComposedMany("wheels",
			NewInstance().Prop("position", "FL"),
			NewInstance().Prop("position", "FR"),
			NewInstance().Prop("position", "RL"),
			NewInstance().Prop("position", "RR"),
		).
		Build()

	wheels, ok := raw.Properties["wheels"].([]any)
	require.True(t, ok, "composed many should be an array")
	require.Len(t, wheels, 4)

	positions := make([]string, 4)
	for i, w := range wheels {
		wheel, ok := w.(map[string]any)
		require.True(t, ok)
		positions[i] = wheel["position"].(string)
	}
	assert.Equal(t, []string{"FL", "FR", "RL", "RR"}, positions)
}

func TestInstanceBuilder_WithProvenance(t *testing.T) {
	t.Run("valid path", func(t *testing.T) {
		raw := NewInstance().
			WithProvenance("test://valid", "$.Person[0]").
			Build()

		require.NotNil(t, raw.Provenance)
		assert.Equal(t, "test://valid", raw.Provenance.SourceName())
		assert.Equal(t, "$.Person[0]", raw.Provenance.Path().String())
	})

	t.Run("invalid path falls back to root", func(t *testing.T) {
		raw := NewInstance().
			WithProvenance("test://invalid", "invalid[[[").
			Build()

		require.NotNil(t, raw.Provenance)
		assert.Equal(t, "test://invalid", raw.Provenance.SourceName())
		assert.Equal(t, "$", raw.Provenance.Path().String())
	})
}

func TestInstanceBuilder_WithFullProvenance(t *testing.T) {
	prov := NewProvenance(
		"test.json",
		path.Root().Key("users").Index(0),
		location.Range(location.SourceID{}, 10, 1, 20, 100),
	)

	raw := NewInstance().
		WithFullProvenance(prov).
		Build()

	require.NotNil(t, raw.Provenance)
	assert.Equal(t, "test.json", raw.Provenance.SourceName())
	assert.Equal(t, "$.users[0]", raw.Provenance.Path().String())
}

func TestInstanceBuilder_Reusability(t *testing.T) {
	builder := NewInstance().
		Prop("id", "p1").
		Prop("name", "Alice")

	raw1 := builder.Build()
	builder.Prop("name", "Bob")
	raw2 := builder.Build()

	assert.Equal(t, "Alice", raw1.Properties["name"], "first build should be unaffected")
	assert.Equal(t, "Bob", raw2.Properties["name"], "second build should have new value")
}

func TestNewEdge(t *testing.T) {
	edge := NewEdge().Build()

	assert.NotNil(t, edge)
	assert.Empty(t, edge)
}

func TestEdgeBuilder_Target(t *testing.T) {
	edge := NewEdge().Target("person-1").Build()

	assert.Equal(t, "person-1", edge["_target_id"])
}

func TestEdgeBuilder_TargetField(t *testing.T) {
	edge := NewEdge().
		TargetField("firstName", "John").
		TargetField("lastName", "Doe").
		Build()

	assert.Equal(t, "John", edge["_target_firstName"])
	assert.Equal(t, "Doe", edge["_target_lastName"])
}

func TestEdgeBuilder_Prop(t *testing.T) {
	edge := NewEdge().
		Target("target-1").
		Prop("weight", 0.8).
		Prop("since", "2020-01-01").
		Build()

	assert.Equal(t, "target-1", edge["_target_id"])
	assert.Equal(t, 0.8, edge["weight"])
	assert.Equal(t, "2020-01-01", edge["since"])
}

func TestEdgeBuilder_Done_WithParent(t *testing.T) {
	// Verify that Done() returns the parent builder
	builder := NewInstance().Prop("id", "p1")
	returnedBuilder := builder.Edge("relation").Target("target").Done()

	assert.Same(t, builder, returnedBuilder, "Done() should return the parent builder")

	// Verify the edge was attached
	raw := builder.Build()
	edge, ok := raw.Properties["relation"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "target", edge["_target_id"])
}

func TestEdgeBuilder_Done_Standalone_Panics(t *testing.T) {
	standalone := NewEdge().Target("target")

	assert.PanicsWithValue(t,
		"instance.EdgeBuilder.Done: cannot call Done on standalone EdgeBuilder (use Build instead)",
		func() {
			standalone.Done()
		},
	)
}

func TestEdgeBuilder_Build_Independence(t *testing.T) {
	builder := NewEdge().Target("original")
	edge1 := builder.Build()
	builder.Target("modified")
	edge2 := builder.Build()

	assert.Equal(t, "original", edge1["_target_id"], "first build should be unaffected")
	assert.Equal(t, "modified", edge2["_target_id"], "second build should have new value")
}

// Integration test: complex nested structure
func TestInstanceBuilder_ComplexStructure(t *testing.T) {
	raw := NewInstance().
		Prop("id", "car-001").
		Prop("vin", "1HGCM82633A123456").
		Prop("make", "Honda").
		Edge("owner").Target("person-1").Done().
		Edges("previousOwners",
			NewEdge().Target("person-2").Prop("soldDate", "2020-01-15"),
			NewEdge().Target("person-3").Prop("soldDate", "2018-06-01"),
		).
		Composed("engine",
			NewInstance().
				Prop("serial", "ENG-001").
				Prop("horsepower", 200),
		).
		ComposedMany("wheels",
			NewInstance().Prop("position", "FL").Prop("brand", "Michelin"),
			NewInstance().Prop("position", "FR").Prop("brand", "Michelin"),
			NewInstance().Prop("position", "RL").Prop("brand", "Goodyear"),
			NewInstance().Prop("position", "RR").Prop("brand", "Goodyear"),
		).
		WithProvenance("test://complex", "$.Car[0]").
		Build()

	// Verify properties
	assert.Equal(t, "car-001", raw.Properties["id"])
	assert.Equal(t, "1HGCM82633A123456", raw.Properties["vin"])
	assert.Equal(t, "Honda", raw.Properties["make"])

	// Verify one-association
	owner, ok := raw.Properties["owner"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "person-1", owner["_target_id"])

	// Verify many-association
	prevOwners, ok := raw.Properties["previousOwners"].([]any)
	require.True(t, ok)
	require.Len(t, prevOwners, 2)

	// Verify composition
	engine, ok := raw.Properties["engine"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "ENG-001", engine["serial"])
	assert.Equal(t, 200, engine["horsepower"])

	// Verify many-composition
	wheels, ok := raw.Properties["wheels"].([]any)
	require.True(t, ok)
	require.Len(t, wheels, 4)

	// Verify provenance
	require.NotNil(t, raw.Provenance)
	assert.Equal(t, "test://complex", raw.Provenance.SourceName())
	assert.Equal(t, "$.Car[0]", raw.Provenance.Path().String())
}
