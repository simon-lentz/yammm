package immutable

import (
	"slices"
	"testing"
)

func TestMap_WrapMap(t *testing.T) {
	input := map[string]any{
		"name": "Alice",
		"age":  30,
	}

	m := WrapMap(input)

	if m.Len() != 2 {
		t.Errorf("expected Len() to be 2, got %d", m.Len())
	}

	name, ok := m.Get("name")
	if !ok {
		t.Fatal("expected Get('name') ok to be true")
	}
	wantString(t, name, "Alice")

	age, ok := m.Get("age")
	if !ok {
		t.Fatal("expected Get('age') ok to be true")
	}
	wantInt(t, age, 30)
}

func TestMap_WrapNil(t *testing.T) {
	m := WrapMap[string](nil)

	if m.Len() != 0 {
		t.Errorf("expected Len() to be 0 for nil map, got %d", m.Len())
	}

	if _, ok := m.Get("anything"); ok {
		t.Error("expected Get() on nil map to return false")
	}
}

func TestMap_WrapEmpty(t *testing.T) {
	m := WrapMap(map[string]any{})

	if m.Len() != 0 {
		t.Errorf("expected Len() to be 0 for empty map, got %d", m.Len())
	}
}

// TestMap_Keys covers key iteration for a populated map and the zero-value
// Map[string]{}, whose iteration must yield nothing rather than panic.
func TestMap_Keys(t *testing.T) {
	tests := []struct {
		name string
		m    Map[string]
		want []string // sorted
	}{
		{"populated", WrapMap(map[string]any{"a": 1, "b": 2, "c": 3}), []string{"a", "b", "c"}},
		{"zero value", Map[string]{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var keys []string
			for k := range tt.m.Keys() {
				keys = append(keys, k)
			}
			slices.Sort(keys)
			if !slices.Equal(keys, tt.want) {
				t.Errorf("Keys() = %v, want %v", keys, tt.want)
			}
		})
	}
}

// TestMap_Range covers pair iteration for a populated map and the zero-value
// Map[string]{}, whose iteration must yield nothing rather than panic.
func TestMap_Range(t *testing.T) {
	tests := []struct {
		name string
		m    Map[string]
		want map[string]int64
	}{
		{"populated", WrapMap(map[string]any{"a": 1, "b": 2}), map[string]int64{"a": 1, "b": 2}},
		{"zero value", Map[string]{}, map[string]int64{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			seen := make(map[string]int64)
			for k, v := range tt.m.Range() {
				n, ok := v.Int()
				if !ok {
					t.Errorf("expected Int() for key %q", k)
				}
				seen[k] = n
			}
			if len(seen) != len(tt.want) {
				t.Fatalf("Range() yielded %d entries, want %d", len(seen), len(tt.want))
			}
			for k, want := range tt.want {
				if seen[k] != want {
					t.Errorf("Range() %s = %d, want %d", k, seen[k], want)
				}
			}
		})
	}
}

func TestMap_Clone(t *testing.T) {
	input := map[string]any{
		"nested": map[string]any{
			"value": "original",
		},
	}

	m := WrapMap(input)
	cloned := m.Clone()

	// Verify cloned has correct structure
	if cloned == nil {
		t.Fatal("expected Clone() to return non-nil")
	}

	if len(cloned) != 1 {
		t.Errorf("expected 1 entry in clone, got %d", len(cloned))
	}

	nested, ok := cloned["nested"].(map[string]any)
	if !ok {
		t.Fatal("expected nested to be map[string]any")
	}

	if nested["value"] != "original" {
		t.Errorf("expected value 'original', got %v", nested["value"])
	}

	// Verify clone is independent
	nested["value"] = "modified"

	// Original wrapped map should be unchanged
	origNested, _ := m.Get("nested")
	origNestedMap, _ := origNested.Map()
	origVal, _ := origNestedMap.Get("value")
	if s, _ := origVal.String(); s != "original" {
		t.Error("clone modification affected original wrapped map")
	}
}

func TestMap_CloneNil(t *testing.T) {
	m := WrapMap[string](nil)
	cloned := m.Clone()

	if cloned != nil {
		t.Error("expected Clone() of nil map to return nil")
	}
}

func TestMap_WrapMap_WithClone_Isolation(t *testing.T) {
	nested := map[string]any{"key": "original"}
	outer := map[string]any{"nested": nested}

	m := WrapMap(outer, WithClone(true))

	// Mutate original after cloning
	nested["key"] = "mutated"
	outer["new"] = "added"

	// Wrapped map should be isolated
	if _, ok := m.Get("new"); ok {
		t.Error("wrapped should not have 'new' key added after clone")
	}

	nestedVal, ok := m.Get("nested")
	if !ok {
		t.Fatal("expected nested key")
	}
	nestedMap, ok := nestedVal.Map()
	if !ok {
		t.Fatal("expected nested to be Map")
	}
	keyVal, ok := nestedMap.Get("key")
	if !ok {
		t.Fatal("expected key in nested")
	}
	wantString(t, keyVal, "original")
}

func TestMap_IteratorEarlyExit(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
		"d": 4,
	}

	m := WrapMap(input)

	// Test early exit from Keys()
	count := 0
	for range m.Keys() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("expected early exit after 2, got %d", count)
	}

	// Test early exit from Range()
	count = 0
	for range m.Range() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("expected early exit after 2, got %d", count)
	}
}

func TestMap_NestedMaps(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"value": "deep",
			},
		},
	}

	m := WrapMap(input)

	level1, ok := m.Get("level1")
	if !ok {
		t.Fatal("expected level1")
	}

	level1Map, ok := level1.Map()
	if !ok {
		t.Fatal("expected level1 to be Map")
	}

	level2, ok := level1Map.Get("level2")
	if !ok {
		t.Fatal("expected level2")
	}

	level2Map, ok := level2.Map()
	if !ok {
		t.Fatal("expected level2 to be Map")
	}

	value, ok := level2Map.Get("value")
	if !ok {
		t.Fatal("expected value")
	}

	wantString(t, value, "deep")
}

func TestMap_Clone_WithNestedSlice(t *testing.T) {
	// Test Clone with nested slices to cover cloneValue slice path
	input := map[string]any{
		"items": []any{"a", "b", "c"},
		"nested": map[string]any{
			"more": []any{1, 2, 3},
		},
	}

	m := WrapMap(input)
	cloned := m.Clone()

	// Verify cloned structure
	items, ok := cloned["items"].([]any)
	if !ok {
		t.Fatal("expected items to be []any")
	}
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
	if items[0] != "a" {
		t.Errorf("expected first item 'a', got %v", items[0])
	}

	// Verify nested structure
	nested, ok := cloned["nested"].(map[string]any)
	if !ok {
		t.Fatal("expected nested to be map[string]any")
	}
	more, ok := nested["more"].([]any)
	if !ok {
		t.Fatal("expected more to be []any")
	}
	if len(more) != 3 {
		t.Errorf("expected 3 items in more, got %d", len(more))
	}
}
