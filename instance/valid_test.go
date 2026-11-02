package instance_test

import (
	"maps"
	"slices"
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

const validRecordsSchema = `schema "valid"

type Person {
	id String primary
	name String
	age Integer
	email String
	*-> ADDRESSES (many) Address
	*-> PHONES (many) Phone
}

part type Address {
	id String primary
}

part type Phone {
	id String primary
}

type RegionalOrder {
	region String primary
	number String primary
}

type Employee {
	id String primary
	--> MANAGER (_:one) Employee {
		since String
		role String
		priority Integer
	}
	--> COWORKER (_:many) Employee
	--> ORDER (_:one) RegionalOrder
}
`

func loadValidRecords(t *testing.T) schema.Schema {
	t.Helper()
	s, res := schema.LoadString(t.Context(), validRecordsSchema, "valid.yammm")
	require.False(t, res.HasErrors(), res.String())
	return s
}

// managerTarget validates an Employee whose MANAGER target holds edgeProps and
// returns that target.
func managerTarget(t *testing.T, edgeProps map[string]any) instance.ValidEdgeTarget {
	t.Helper()
	manager := map[string]any{"_target_id": "1"}
	maps.Copy(manager, edgeProps)
	vi := instancetest.Valid(t, loadValidRecords(t), "Employee", map[string]any{"id": "9", "manager": manager})
	edge, ok := vi.Edge("MANAGER")
	require.True(t, ok)
	targets := slices.Collect(edge.Targets())
	require.Len(t, targets, 1)
	return targets[0]
}

func TestValidInstance_TypeName(t *testing.T) {
	vi := instancetest.Valid(t, loadValidRecords(t), "Person", map[string]any{"id": "1"})

	assert.Equal(t, "Person", vi.TypeName())
}

func TestValidInstance_Type(t *testing.T) {
	s := loadValidRecords(t)
	typ, ok := s.Type("Person")
	require.True(t, ok)
	vi := instancetest.Valid(t, s, "Person", map[string]any{"id": "1"})

	assert.Equal(t, typ, vi.Type())
}

func TestValidInstance_PrimaryKey(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("single_key", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Person", map[string]any{"id": "42"})

		assert.Equal(t, immutable.WrapKey([]any{"42"}), vi.PrimaryKey())
		assert.Equal(t, `["42"]`, vi.PrimaryKey().String())
	})

	t.Run("composite_key", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "RegionalOrder", map[string]any{"region": "us-east", "number": "123"})

		assert.Equal(t, `["us-east","123"]`, vi.PrimaryKey().String())
	})
}

func TestValidInstance_Property(t *testing.T) {
	vi := instancetest.Valid(t, loadValidRecords(t), "Person", map[string]any{
		"id":   "1",
		"name": "Alice",
		"age":  int64(30),
	})

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
	vi := instancetest.Valid(t, loadValidRecords(t), "Person", map[string]any{
		"id":    "1",
		"name":  "Bob",
		"email": "bob@example.com",
	})

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
	s := loadValidRecords(t)
	t.Run("existing_edge", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{
			"id":      "1",
			"manager": map[string]any{"_target_id": "100"},
		})

		edge, ok := vi.Edge("MANAGER")
		require.True(t, ok)
		require.False(t, edge.IsZero())
		assert.Len(t, slices.Collect(edge.Targets()), 1)
	})

	t.Run("missing_edge", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{"id": "1"})

		edge, ok := vi.Edge("MANAGER")
		assert.False(t, ok)
		assert.True(t, edge.IsZero())
	})

	t.Run("nonexistent_relation", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{
			"id":       "1",
			"coworker": []any{},
		})

		edge, ok := vi.Edge("nonexistent")
		assert.False(t, ok)
		assert.True(t, edge.IsZero())
	})
}

func TestValidInstance_Provenance(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("with_provenance", func(t *testing.T) {
		prov := location.NewProvenance(
			"test.json",
			path.Root().Key("users").Index(0),
			location.RangeWithBytes(location.SourceID{}, 10, 1, -1, 20, 100, -1),
		)
		vi := instancetest.ValidRaw(t, s, "Person", instance.RawInstance{
			Properties: map[string]any{"id": "1"},
			Provenance: prov,
		})

		result := vi.Provenance()
		require.False(t, result.IsZero())
		assert.Equal(t, "test.json", result.SourceName())
		assert.Equal(t, "$.users[0]", result.Path().String())
	})

	t.Run("without_provenance", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Person", map[string]any{"id": "1"})

		assert.True(t, vi.Provenance().IsZero())
	})
}

func TestValidEdgeData_Targets(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("non_nil_with_targets", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{
			"id":       "9",
			"coworker": []any{map[string]any{"_target_id": "1"}, map[string]any{"_target_id": "2"}},
		})
		edge, ok := vi.Edge("COWORKER")
		require.True(t, ok)

		result := slices.Collect(edge.Targets())
		assert.Len(t, result, 2)
	})

	t.Run("zero_edge", func(t *testing.T) {
		var edge instance.ValidEdgeData
		assert.Nil(t, slices.Collect(edge.Targets()))
	})

	t.Run("empty_targets", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{"id": "9", "coworker": []any{}})
		edge, ok := vi.Edge("COWORKER")
		require.True(t, ok)
		assert.False(t, edge.IsZero())
		assert.Nil(t, slices.Collect(edge.Targets()))
	})
}

func TestValidEdgeData_IsEmpty(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("with_targets", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{
			"id":       "9",
			"coworker": []any{map[string]any{"_target_id": "1"}},
		})
		edge, ok := vi.Edge("COWORKER")
		require.True(t, ok)

		assert.False(t, edge.IsEmpty())
	})

	t.Run("zero_edge", func(t *testing.T) {
		var edge instance.ValidEdgeData
		assert.True(t, edge.IsEmpty())
	})

	t.Run("empty_targets", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{"id": "9", "coworker": []any{}})
		edge, ok := vi.Edge("COWORKER")
		require.True(t, ok)
		assert.True(t, edge.IsEmpty())
	})
}

func TestValidEdgeTarget_TargetKey(t *testing.T) {
	vi := instancetest.Valid(t, loadValidRecords(t), "Employee", map[string]any{
		"id":    "9",
		"order": map[string]any{"_target_region": "us-east", "_target_number": "456"},
	})
	edge, ok := vi.Edge("ORDER")
	require.True(t, ok)
	target := slices.Collect(edge.Targets())[0]

	assert.Equal(t, immutable.WrapKey([]any{"us-east", "456"}), target.TargetKey())
	assert.Equal(t, `["us-east","456"]`, target.TargetKey().String())
}

func TestValidEdgeTarget_Properties(t *testing.T) {
	t.Run("with_properties", func(t *testing.T) {
		target := managerTarget(t, map[string]any{"since": "2024-01-01", "priority": int64(5)})

		result := target.Properties()
		sinceVal, ok := result.Get("since")
		require.True(t, ok)
		since, _ := sinceVal.String()
		assert.Equal(t, "2024-01-01", since)
	})

	t.Run("without_properties", func(t *testing.T) {
		target := managerTarget(t, nil)

		result := target.Properties()
		_, ok := result.Get("anything")
		assert.False(t, ok)
	})
}

func TestValidInstance_Edges_Iterator(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("with_edges", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{
			"id":       "1",
			"manager":  map[string]any{"_target_id": "100"},
			"coworker": []any{map[string]any{"_target_id": "200"}, map[string]any{"_target_id": "201"}},
		})

		count := 0
		for name, edge := range vi.Edges() {
			count++
			assert.NotEmpty(t, name)
			assert.False(t, edge.IsZero())
		}
		assert.Equal(t, 2, count)
	})

	t.Run("no_edges", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Employee", map[string]any{"id": "1"})

		count := 0
		for range vi.Edges() {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestValidInstance_Compositions_Iterator(t *testing.T) {
	s := loadValidRecords(t)
	t.Run("with_compositions", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Person", map[string]any{
			"id":        "1",
			"addresses": []any{map[string]any{"id": "1"}},
			"phones":    []any{map[string]any{"id": "2"}},
		})

		count := 0
		for name, val := range vi.Compositions() {
			count++
			assert.NotEmpty(t, name)
			assert.False(t, val.IsNil())
			children, ok := val.Slice()
			require.True(t, ok, "a composition holds a slice")
			for child := range children.Iter() {
				held, ok := child.Unwrap().(instance.ValidInstance)
				require.True(t, ok, "a composition holds instances")
				assert.False(t, held.IsZero())
			}
		}
		assert.Equal(t, 2, count)
	})

	t.Run("no_compositions", func(t *testing.T) {
		vi := instancetest.Valid(t, s, "Person", map[string]any{"id": "1"})

		count := 0
		for range vi.Compositions() {
			count++
		}
		assert.Equal(t, 0, count)
	})
}

func TestValidEdgeTarget_Property(t *testing.T) {
	t.Run("existing_property", func(t *testing.T) {
		target := managerTarget(t, map[string]any{"since": "2024-01-01", "role": "mentor"})

		val, ok := target.Property("since")
		require.True(t, ok)
		s, ok := val.String()
		require.True(t, ok)
		assert.Equal(t, "2024-01-01", s)
	})

	t.Run("missing_property", func(t *testing.T) {
		target := managerTarget(t, map[string]any{"since": "2024-01-01"})

		_, ok := target.Property("nonexistent")
		assert.False(t, ok)
	})

	t.Run("no_properties", func(t *testing.T) {
		target := managerTarget(t, nil)

		_, ok := target.Property("anything")
		assert.False(t, ok)
	})
}

func TestValidEdgeTarget_HasProperties(t *testing.T) {
	t.Run("with_properties", func(t *testing.T) {
		target := managerTarget(t, map[string]any{"since": "2024-01-01"})

		assert.True(t, target.HasProperties())
	})

	t.Run("no_properties", func(t *testing.T) {
		target := managerTarget(t, nil)

		assert.False(t, target.HasProperties())
	})
}
