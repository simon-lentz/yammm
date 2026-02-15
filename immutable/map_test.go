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
	if s, ok := name.String(); !ok || s != "Alice" {
		t.Errorf("expected name to be 'Alice', got %v", name.Unwrap())
	}

	age, ok := m.Get("age")
	if !ok {
		t.Fatal("expected Get('age') ok to be true")
	}
	if n, ok := age.Int(); !ok || n != 30 {
		t.Errorf("expected age to be 30, got %v", age.Unwrap())
	}
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

func TestMap_Keys(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
	}

	m := WrapMap(input)

	var keys []string
	for k := range m.Keys() {
		keys = append(keys, k)
	}

	if len(keys) != 3 {
		t.Errorf("expected 3 keys, got %d", len(keys))
	}

	slices.Sort(keys)
	expected := []string{"a", "b", "c"}
	if !slices.Equal(keys, expected) {
		t.Errorf("expected keys %v, got %v", expected, keys)
	}
}

func TestMap_Range(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
	}

	m := WrapMap(input)

	seen := make(map[string]int64)
	for k, v := range m.Range() {
		n, ok := v.Int()
		if !ok {
			t.Errorf("expected Int() for key %q", k)
		}
		seen[k] = n
	}

	if len(seen) != 2 {
		t.Errorf("expected 2 entries, got %d", len(seen))
	}
	if seen["a"] != 1 {
		t.Errorf("expected a=1, got %d", seen["a"])
	}
	if seen["b"] != 2 {
		t.Errorf("expected b=2, got %d", seen["b"])
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

func TestMap_WrapMapClone_Isolation(t *testing.T) {
	nested := map[string]any{"key": "original"}
	outer := map[string]any{"nested": nested}

	m := WrapMapClone(outer)

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
	if str, ok := keyVal.String(); !ok || str != "original" {
		t.Errorf("expected 'original', got %v", keyVal.Unwrap())
	}
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

	if s, ok := value.String(); !ok || s != "deep" {
		t.Errorf("expected 'deep', got %v", value.Unwrap())
	}
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

func TestMap_Keys_ZeroValue(t *testing.T) {
	// A4: Verify iterating over literal zero-value Map[string]{} Keys handles gracefully
	var m Map[string]

	count := 0
	for range m.Keys() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for zero-value Map[string]{} Keys, got %d", count)
	}
}

func TestMap_Range_ZeroValue(t *testing.T) {
	// A4: Verify iterating over literal zero-value Map[string]{} Range handles gracefully
	var m Map[string]

	count := 0
	for range m.Range() {
		count++
	}

	if count != 0 {
		t.Errorf("expected 0 iterations for zero-value Map[string]{} Range, got %d", count)
	}
}
