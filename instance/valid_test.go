package instance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/immutable"
	"github.com/simon-lentz/yammm/instance"
	"github.com/simon-lentz/yammm/internal/instancetest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/location/path"
	"github.com/simon-lentz/yammm/schema"
)

func TestValidInstance_TypeName(t *testing.T) {
	vi := instancetest.VI(
		"Person",
		instancetest.PK(int64(1)),
	)

	assert.Equal(t, "Person", vi.TypeName())
}

func TestValidInstance_TypeID(t *testing.T) {
	typeID := schema.NewTypeID(location.SourceID{}, "Person")
	vi := instancetest.VI(
		"Person",
		instancetest.TypeID(typeID),
		instancetest.PK(),
	)

	assert.Equal(t, typeID, vi.TypeID())
}

func TestValidInstance_PrimaryKey(t *testing.T) {
	t.Run("single_key", func(t *testing.T) {
		vi := instancetest.VI(
			"Person",
			instancetest.PK(int64(42)),
		)

		assert.Equal(t, immutable.WrapKey([]any{int64(42)}), vi.PrimaryKey())
		assert.Equal(t, "[42]", vi.PrimaryKey().String())
	})

	t.Run("composite_key", func(t *testing.T) {
		vi := instancetest.VI(
			"RegionalOrder",
			instancetest.PK("us-east", int64(123)),
		)

		assert.Equal(t, `["us-east",123]`, vi.PrimaryKey().String())
	})
}

func TestValidInstance_Property(t *testing.T) {
	vi := instancetest.VI(
		"Person",
		instancetest.PK(int64(1)),
		instancetest.Props(map[string]any{
			"name": "Alice",
			"age":  int64(30),
		}),
	)

	t.Run("existing_property", func(t *testing.T) {
		val, ok := vi.Property("name")
		require.True(t, ok)
		s, ok := val.String()
		require.True(t, ok)
		assert.Equal(t, "Alice", s)
	})

	t.Run("another_property", func(t *testing.T) {
		val, ok := vi.Property("age")
		require.True(t, ok)
		i, ok := val.Int()
		require.True(t, ok)
		assert.Equal(t, int64(30), i)
	})

	t.Run("missing_property", func(t *testing.T) {
		_, ok := vi.Property("nonexistent")
		assert.False(t, ok)
	})
}

func TestValidInstance_Properties(t *testing.T) {
	vi := instancetest.VI(
		"User",
		instancetest.PK(int64(1)),
		instancetest.Props(map[string]any{
			"name":  "Bob",
			"email": "bob@example.com",
		}),
	)

	allProps := vi.Properties()

	nameVal, ok := allProps.Get("name")
	require.True(t, ok)
	name, _ := nameVal.String()
	assert.Equal(t, "Bob", name)

	emailVal, ok := allProps.Get("email")
	require.True(t, ok)
	email, _ := emailVal.String()
	assert.Equal(t, "bob@example.com", email)
}

func TestValidInstance_Edge(t *testing.T) {
	t.Run("existing_edge", func(t *testing.T) {
		targets := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(
				immutable.WrapKey([]any{int64(100)}),
				immutable.WrapProperties(nil),
			),
		}
		edges := map[string]*instance.ValidEdgeData{
			"MANAGER": instance.NewValidEdgeData(targets),
		}
		vi := instancetest.VI(
			"Employee",
			instancetest.PK(int64(1)),
			instancetest.Edges(edges),
		)

		edge, ok := vi.Edge("MANAGER")
		require.True(t, ok)
		require.NotNil(t, edge)
		assert.Len(t, edge.Targets(), 1)
	})

	t.Run("missing_edge", func(t *testing.T) {
		vi := instancetest.VI(
			"Employee",
			instancetest.PK(int64(1)),
		)

		edge, ok := vi.Edge("MANAGER")
		assert.False(t, ok)
		assert.Nil(t, edge)
	})

	t.Run("nonexistent_relation", func(t *testing.T) {
		edges := map[string]*instance.ValidEdgeData{
			"MANAGER": instance.NewValidEdgeData(nil),
		}
		vi := instancetest.VI(
			"Employee",
			instancetest.PK(int64(1)),
			instancetest.Edges(edges),
		)

		edge, ok := vi.Edge("nonexistent")
		assert.False(t, ok)
		assert.Nil(t, edge)
	})
}

func TestValidInstance_Provenance(t *testing.T) {
	t.Run("with_provenance", func(t *testing.T) {
		prov := location.NewProvenance(
			"test.json",
			path.Root().Key("users").Index(0),
			location.Range(location.SourceID{}, 10, 1, 20, 100),
		)
		vi := instancetest.VI(
			"User",
			instancetest.PK(int64(1)),
			instancetest.Provenance(prov),
		)

		result := vi.Provenance()
		require.NotNil(t, result)
		assert.Equal(t, "test.json", result.SourceName())
		assert.Equal(t, "$.users[0]", result.Path().String())
	})

	t.Run("without_provenance", func(t *testing.T) {
		vi := instancetest.VI(
			"User",
			instancetest.PK(int64(1)),
		)

		assert.Nil(t, vi.Provenance())
	})
}

func TestValidEdgeData_Targets(t *testing.T) {
	t.Run("non_nil_with_targets", func(t *testing.T) {
		targets := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(
				immutable.WrapKey([]any{int64(1)}),
				immutable.WrapProperties(nil),
			),
			instance.NewValidEdgeTarget(
				immutable.WrapKey([]any{int64(2)}),
				immutable.WrapProperties(nil),
			),
		}
		edge := instance.NewValidEdgeData(targets)

		result := edge.Targets()
		assert.Len(t, result, 2)
	})

	t.Run("nil_edge", func(t *testing.T) {
		var edge *instance.ValidEdgeData
		assert.Nil(t, edge.Targets())
	})

	t.Run("empty_targets", func(t *testing.T) {
		edge := instance.NewValidEdgeData(nil)
		assert.Nil(t, edge.Targets())
	})
}

func TestValidEdgeData_IsEmpty(t *testing.T) {
	t.Run("with_targets", func(t *testing.T) {
		targets := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(1)}), immutable.WrapProperties(nil)),
		}
		edge := instance.NewValidEdgeData(targets)

		assert.False(t, edge.IsEmpty())
	})

	t.Run("nil_edge", func(t *testing.T) {
		var edge *instance.ValidEdgeData
		assert.True(t, edge.IsEmpty())
	})

	t.Run("empty_targets", func(t *testing.T) {
		edge := instance.NewValidEdgeData([]instance.ValidEdgeTarget{})
		assert.True(t, edge.IsEmpty())
	})
}

func TestValidEdgeTarget_TargetKey(t *testing.T) {
	pk := immutable.WrapKey([]any{"us-east", int64(456)})
	target := instance.NewValidEdgeTarget(pk, immutable.WrapProperties(nil))

	assert.Equal(t, pk, target.TargetKey())
	assert.Equal(t, `["us-east",456]`, target.TargetKey().String())
}

func TestValidEdgeTarget_Properties(t *testing.T) {
	t.Run("with_properties", func(t *testing.T) {
		props := immutable.WrapProperties(map[string]any{
			"since":    "2024-01-01",
			"priority": int64(5),
		})
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			props,
		)

		result := target.Properties()
		sinceVal, ok := result.Get("since")
		require.True(t, ok)
		since, _ := sinceVal.String()
		assert.Equal(t, "2024-01-01", since)
	})

	t.Run("without_properties", func(t *testing.T) {
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			immutable.WrapProperties(nil),
		)

		result := target.Properties()
		_, ok := result.Get("anything")
		assert.False(t, ok)
	})
}

func TestValidInstance_Edges_Iterator(t *testing.T) {
	t.Run("with_edges", func(t *testing.T) {
		targets1 := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(100)}), immutable.WrapProperties(nil)),
		}
		targets2 := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(200)}), immutable.WrapProperties(nil)),
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(201)}), immutable.WrapProperties(nil)),
		}
		edges := map[string]*instance.ValidEdgeData{
			"MANAGER":  instance.NewValidEdgeData(targets1),
			"COWORKER": instance.NewValidEdgeData(targets2),
		}
		vi := instancetest.VI(
			"Employee",
			instancetest.PK(int64(1)),
			instancetest.Edges(edges),
		)

		count := 0
		for name, edge := range vi.Edges() {
			count++
			assert.NotEmpty(t, name)
			assert.NotNil(t, edge)
		}
		assert.Equal(t, 2, count)
	})

	t.Run("no_edges", func(t *testing.T) {
		vi := instancetest.VI(
			"Employee",
			instancetest.PK(int64(1)),
		)

		count := 0
		for range vi.Edges() {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestValidInstance_Compositions_Iterator(t *testing.T) {
	t.Run("with_compositions", func(t *testing.T) {
		composed := map[string]immutable.Value{
			"ADDRESSES": immutable.Wrap([]any{map[string]any{"id": int64(1)}}),
			"PHONES":    immutable.Wrap([]any{map[string]any{"id": int64(2)}}),
		}
		vi := instancetest.VI(
			"Person",
			instancetest.PK(int64(1)),
			instancetest.Composed(composed),
		)

		count := 0
		for name, val := range vi.Compositions() {
			count++
			assert.NotEmpty(t, name)
			assert.False(t, val.IsNil())
		}
		assert.Equal(t, 2, count)
	})

	t.Run("no_compositions", func(t *testing.T) {
		vi := instancetest.VI(
			"Person",
			instancetest.PK(int64(1)),
		)

		count := 0
		for range vi.Compositions() {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestValidEdgeTarget_Property(t *testing.T) {
	t.Run("existing_property", func(t *testing.T) {
		props := immutable.WrapProperties(map[string]any{
			"since": "2024-01-01",
			"role":  "mentor",
		})
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			props,
		)

		val, ok := target.Property("since")
		require.True(t, ok)
		s, ok := val.String()
		require.True(t, ok)
		assert.Equal(t, "2024-01-01", s)
	})

	t.Run("missing_property", func(t *testing.T) {
		props := immutable.WrapProperties(map[string]any{
			"since": "2024-01-01",
		})
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			props,
		)

		_, ok := target.Property("nonexistent")
		assert.False(t, ok)
	})

	t.Run("nil_properties", func(t *testing.T) {
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			immutable.WrapProperties(nil),
		)

		_, ok := target.Property("anything")
		assert.False(t, ok)
	})
}

func TestValidEdgeTarget_HasProperties(t *testing.T) {
	t.Run("with_properties", func(t *testing.T) {
		props := immutable.WrapProperties(map[string]any{
			"since": "2024-01-01",
		})
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			props,
		)

		assert.True(t, target.HasProperties())
	})

	t.Run("empty_properties", func(t *testing.T) {
		props := immutable.WrapProperties(map[string]any{})
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			props,
		)

		assert.False(t, target.HasProperties())
	})

	t.Run("nil_properties", func(t *testing.T) {
		target := instance.NewValidEdgeTarget(
			immutable.WrapKey([]any{int64(1)}),
			immutable.WrapProperties(nil),
		)

		assert.False(t, target.HasProperties())
	})
}

func TestValidEdgeData_TargetsIter(t *testing.T) {
	t.Run("with_targets", func(t *testing.T) {
		targets := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(1)}), immutable.WrapProperties(nil)),
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(2)}), immutable.WrapProperties(nil)),
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(3)}), immutable.WrapProperties(nil)),
		}
		edge := instance.NewValidEdgeData(targets)

		count := 0
		for target := range edge.TargetsIter() {
			count++
			assert.NotNil(t, target.TargetKey())
		}
		assert.Equal(t, 3, count)
	})

	t.Run("nil_edge", func(t *testing.T) {
		var edge *instance.ValidEdgeData

		count := 0
		for range edge.TargetsIter() {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("empty_targets", func(t *testing.T) {
		edge := instance.NewValidEdgeData(nil)

		count := 0
		for range edge.TargetsIter() {
			count++
		}
		assert.Equal(t, 0, count)
	})

	t.Run("early_break", func(t *testing.T) {
		targets := []instance.ValidEdgeTarget{
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(1)}), immutable.WrapProperties(nil)),
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(2)}), immutable.WrapProperties(nil)),
			instance.NewValidEdgeTarget(immutable.WrapKey([]any{int64(3)}), immutable.WrapProperties(nil)),
		}
		edge := instance.NewValidEdgeData(targets)

		// Test early break from iteration
		count := 0
		for range edge.TargetsIter() {
			count++
			if count == 2 {
				break
			}
		}
		assert.Equal(t, 2, count)
	})
}
