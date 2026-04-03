package immutable

import (
	"sync"
	"testing"
)

// This file contains concurrent read safety tests.
//
// All immutable types are safe for concurrent read access.
// The underlying data structures are never modified after construction.

func TestConcurrent_Value_Read(t *testing.T) {
	input := map[string]any{
		"name":  "Alice",
		"age":   30,
		"items": []any{1, 2, 3},
	}

	v := Wrap(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Read operations
				_ = v.Unwrap()
				_ = v.IsNil()
				_, _ = v.Bool()
				_, _ = v.Int()
				_, _ = v.Float()
				_, _ = v.String()
				_, _ = v.Map()
				_, _ = v.Slice()
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_Map_Read(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
		"c": map[string]any{"nested": "value"},
	}

	m := WrapMap(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Read operations
				_ = m.Len()
				_, _ = m.Get("a")
				_, _ = m.Get("nonexistent")

				// Iterator operations
				for range m.Keys() {
					// Just iterate
				}

				for range m.Range() {
					// Just iterate
				}
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_Slice_Read(t *testing.T) {
	input := []any{1, 2, 3, map[string]any{"key": "value"}, []any{"a", "b"}}

	s := WrapSlice(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Read operations
				_ = s.Len()
				_ = s.Get(0)
				_ = s.Get(1)

				// Iterator operations
				for range s.Iter() {
					// Just iterate
				}
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_Properties_Read(t *testing.T) {
	input := map[string]any{
		"Name":    "Alice",
		"AGE":     30,
		"email":   "alice@example.com",
		"Address": map[string]any{"city": "NYC"},
	}

	p := WrapProperties(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Read operations
				_ = p.Len()
				_, _ = p.Get("Name")
				_, _ = p.GetFold("name")
				_, _ = p.GetFold("NAME")
				_, _ = p.GetFold("nonexistent")

				// Iterator operations
				for range p.Keys() {
					// Just iterate
				}

				for range p.SortedKeys() {
					// Just iterate
				}

				for range p.Range() {
					// Just iterate
				}
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_Key_Read(t *testing.T) {
	input := []any{"us", 12345, "suffix"}

	k := WrapKey(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 1000

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Read operations
				_ = k.Len()
				_ = k.Get(0)
				_ = k.Get(1)
				_ = k.String()
				_, _ = k.SingleString()
				_, _ = k.SingleInt()

				// Iterator operations
				for range k.Iter() {
					// Just iterate
				}
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_Clone_Safety(t *testing.T) {
	// Test that Clone() can be called concurrently
	input := map[string]any{
		"nested": map[string]any{"key": "value"},
		"items":  []any{1, 2, 3},
	}

	m := WrapMap(input)

	var wg sync.WaitGroup
	const goroutines = 100

	for range goroutines {
		wg.Go(func() {
			cloned := m.Clone()
			// Mutate the clone (safe, it's independent)
			nested := cloned["nested"].(map[string]any)
			nested["new"] = "added"
		})
	}

	wg.Wait()

	// Verify original is unchanged
	nested, _ := m.Get("nested")
	nestedMap, _ := nested.Map()
	if _, ok := nestedMap.Get("new"); ok {
		t.Error("clone mutations affected original")
	}
}

func TestConcurrent_NestedAccess(t *testing.T) {
	// Test concurrent access to deeply nested structures
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"level3": []any{"a", "b", "c"},
			},
		},
	}

	m := WrapMap(input)

	var wg sync.WaitGroup
	const goroutines = 100
	const iterations = 100

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Navigate to deeply nested value
				l1Val, _ := m.Get("level1")
				l1Map, _ := l1Val.Map()
				l2Val, _ := l1Map.Get("level2")
				l2Map, _ := l2Val.Map()
				l3Val, _ := l2Map.Get("level3")
				l3Slice, _ := l3Val.Slice()

				// Access elements
				for i := range l3Slice.Len() {
					_ = l3Slice.Get(i)
				}

				// Iterate
				for range l3Slice.Iter() {
					// Just iterate
				}
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_MixedTypes(t *testing.T) {
	// Test concurrent access with mixed immutable types
	mapData := WrapMap(map[string]any{"key": "value"})
	sliceData := WrapSlice([]any{1, 2, 3})
	propsData := WrapProperties(map[string]any{"prop": "data"})
	keyData := WrapKey([]any{"us", 123})
	valueData := Wrap(map[string]any{"nested": []any{"a", "b"}})

	var wg sync.WaitGroup
	const goroutines = 50
	const iterations = 100

	for range goroutines {
		wg.Go(func() {
			for range iterations {
				// Access all types
				_, _ = mapData.Get("key")
				_ = sliceData.Get(0)
				_, _ = propsData.GetFold("prop")
				_ = keyData.String()
				_, _ = valueData.Map()

				// Clone all types
				_ = mapData.Clone()
				_ = sliceData.Clone()
				_ = propsData.Clone()
				_ = keyData.Clone()
			}
		})
	}

	wg.Wait()
}

func TestConcurrent_IteratorConsistency(t *testing.T) {
	// Test that iterators return consistent results under concurrent access
	input := map[string]any{"a": 1, "b": 2, "c": 3}
	m := WrapMap(input)

	results := make(chan int, 100)

	var wg sync.WaitGroup
	const goroutines = 100

	for range goroutines {
		wg.Go(func() {
			count := 0
			for range m.Keys() {
				count++
			}
			results <- count
		})
	}

	wg.Wait()
	close(results)

	// All goroutines should have seen 3 keys
	for count := range results {
		if count != 3 {
			t.Errorf("expected 3 keys, got %d", count)
		}
	}
}
