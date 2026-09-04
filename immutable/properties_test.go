package immutable

import (
	"slices"
	"sync"
	"testing"
)

func TestProperties_WrapProperties(t *testing.T) {
	input := map[string]any{
		"name":  "Alice",
		"age":   30,
		"email": "alice@example.com",
	}

	p := WrapProperties(input)

	if p.Len() != 3 {
		t.Errorf("expected Len() to be 3, got %d", p.Len())
	}

	name, ok := p.Get("name")
	if !ok {
		t.Fatal("expected Get('name') ok to be true")
	}
	wantString(t, name, "Alice")
}

func TestProperties_WrapNil(t *testing.T) {
	p := WrapProperties(nil)

	if p.Len() != 0 {
		t.Errorf("expected Len() to be 0 for nil, got %d", p.Len())
	}

	if _, ok := p.Get("anything"); ok {
		t.Error("expected Get() on nil to return false")
	}
}

func TestProperties_GetFold_CaseInsensitive(t *testing.T) {
	input := map[string]any{
		"UserName": "Alice",
	}

	p := WrapProperties(input)

	tests := []string{
		"UserName",
		"username",
		"USERNAME",
		"uSeRnAmE",
	}

	for _, name := range tests {
		t.Run(name, func(t *testing.T) {
			v, ok := p.GetFold(name)
			if !ok {
				t.Errorf("expected GetFold(%q) to find value", name)
			}
			wantString(t, v, "Alice")
		})
	}
}

func TestProperties_GetFold_ExactMatchFirst(t *testing.T) {
	// Test that exact match is tried first
	input := map[string]any{
		"name": "exact",
	}

	p := WrapProperties(input)

	v, ok := p.GetFold("name")
	if !ok {
		t.Fatal("expected GetFold to find value")
	}
	wantString(t, v, "exact")
}

func TestProperties_GetFold_ASCIIOnly(t *testing.T) {
	// ASCII case folding only applies to a-z/A-Z
	input := map[string]any{
		"café": "value",
	}

	p := WrapProperties(input)

	// These should not match because é is not ASCII
	if _, ok := p.GetFold("CAFÉ"); ok {
		t.Error("expected non-ASCII characters to not be folded")
	}

	// Exact match should still work
	v, ok := p.GetFold("café")
	if !ok {
		t.Fatal("expected exact match to work")
	}
	wantString(t, v, "value")
}

func TestProperties_GetFold_NotFound(t *testing.T) {
	input := map[string]any{
		"name": "value",
	}

	p := WrapProperties(input)

	if _, ok := p.GetFold("nonexistent"); ok {
		t.Error("expected GetFold to return false for nonexistent key")
	}
}

func TestProperties_Keys(t *testing.T) {
	input := map[string]any{
		"c": 3,
		"a": 1,
		"b": 2,
	}

	p := WrapProperties(input)

	var keys []string
	for k := range p.Keys() {
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

// TestProperties_SortedKeys covers ordering and the empty case; each input
// is iterated twice so the sorted sequence is also proven repeatable.
func TestProperties_SortedKeys(t *testing.T) {
	tests := []struct {
		name  string
		input map[string]any
		want  []string
	}{
		{"sorted order", map[string]any{"z": 26, "a": 1, "m": 13, "b": 2}, []string{"a", "b", "m", "z"}},
		{"empty", map[string]any{}, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := WrapProperties(tt.input)
			for pass := range 2 {
				var keys []string
				for k := range p.SortedKeys() {
					keys = append(keys, k)
				}
				if !slices.Equal(keys, tt.want) {
					t.Errorf("pass %d: SortedKeys() = %v, want %v", pass, keys, tt.want)
				}
			}
		})
	}
}

func TestProperties_Range(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
	}

	p := WrapProperties(input)

	seen := make(map[string]int64)
	for k, v := range p.Range() {
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

func TestProperties_Clone(t *testing.T) {
	input := map[string]any{
		"nested": map[string]any{
			"value": "original",
		},
	}

	p := WrapProperties(input)
	cloned := p.Clone()

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

	origNested, _ := p.Get("nested")
	origNestedMap, _ := origNested.Map()
	origVal, _ := origNestedMap.Get("value")
	if s, _ := origVal.String(); s != "original" {
		t.Error("clone modification affected original")
	}
}

func TestProperties_CloneNil(t *testing.T) {
	p := WrapProperties(nil)
	cloned := p.Clone()

	if cloned != nil {
		t.Error("expected Clone() of nil to return nil")
	}
}

func TestProperties_WrapProperties_WithClone_Isolation(t *testing.T) {
	nested := map[string]any{"key": "original"}
	input := map[string]any{"nested": nested}

	p := WrapProperties(input, WithClone(true))

	// Mutate original
	nested["key"] = "mutated"
	input["new"] = "added"

	// Wrapped properties should be isolated
	if _, ok := p.Get("new"); ok {
		t.Error("wrapped should not have 'new' key")
	}

	nestedVal, ok := p.Get("nested")
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

func TestProperties_NestedProperties(t *testing.T) {
	input := map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{
				"value": "deep",
			},
		},
	}

	p := WrapProperties(input)

	level1, ok := p.Get("level1")
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

// TestProperties_SortedRange covers key-sorted pairs and the empty case;
// each input is iterated twice so the order is also proven repeatable.
func TestProperties_SortedRange(t *testing.T) {
	tests := []struct {
		name       string
		input      map[string]any
		wantKeys   []string
		wantValues []int64
	}{
		{"sorted pairs", map[string]any{"z": 26, "a": 1, "m": 13, "b": 2}, []string{"a", "b", "m", "z"}, []int64{1, 2, 13, 26}},
		{"empty", map[string]any{}, nil, nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := WrapProperties(tt.input)
			for pass := range 2 {
				var keys []string
				var values []int64
				for k, v := range p.SortedRange() {
					keys = append(keys, k)
					n, _ := v.Int()
					values = append(values, n)
				}
				if !slices.Equal(keys, tt.wantKeys) {
					t.Errorf("pass %d: keys = %v, want %v", pass, keys, tt.wantKeys)
				}
				if !slices.Equal(values, tt.wantValues) {
					t.Errorf("pass %d: values = %v, want %v", pass, values, tt.wantValues)
				}
			}
		})
	}
}

func TestProperties_SortedRange_EarlyExit(t *testing.T) {
	input := map[string]any{
		"a": 1,
		"b": 2,
		"c": 3,
		"d": 4,
	}

	p := WrapProperties(input)

	count := 0
	for range p.SortedRange() {
		count++
		if count == 2 {
			break
		}
	}

	if count != 2 {
		t.Errorf("expected early exit after 2 iterations, got %d", count)
	}
}

func TestProperties_GetFold_CollisionDeterminism(t *testing.T) {
	// Tests that GetFold returns deterministic results when multiple keys
	// collide under ASCII case folding. The alphabetically first key should win.
	input := map[string]any{
		"Name": "name-value",
		"NAME": "all-uppercase-value",
		"NaMe": "mixed-case-value",
	}

	p := WrapProperties(input)

	// Query with "nAmE" which has no exact match - falls back to folded index.
	// "NAME" sorts before "NaMe" and "Name" alphabetically, so foldedIndex["name"] = "NAME"
	v, ok := p.GetFold("nAmE")
	if !ok {
		t.Fatal("expected GetFold to find a match via folded index")
	}

	expected := "all-uppercase-value" // "NAME" is alphabetically first
	if str, strOk := v.String(); !strOk || str != expected {
		t.Errorf("expected %q (from alphabetically first key 'NAME'), got %q", expected, str)
	}

	// Verify determinism across multiple calls with various case variations
	queries := []string{"nAmE", "namE", "nAME", "NaME"}
	for i := range queries {
		v2, ok2 := p.GetFold(queries[i])
		if !ok2 {
			t.Fatalf("iteration %d: expected GetFold to find a match for %q", i, queries[i])
		}
		if str, _ := v2.String(); str != expected {
			t.Errorf("iteration %d: expected deterministic result %q for %q, got %q", i, expected, queries[i], str)
		}
	}
}

func TestProperties_GetFold_ExactMatchPriority(t *testing.T) {
	// Tests that exact match takes priority over folded match.
	input := map[string]any{
		"NAME": "uppercase",
		"Name": "mixed",
	}

	p := WrapProperties(input)

	// Exact match for "NAME" should return "uppercase"
	v, ok := p.GetFold("NAME")
	if !ok {
		t.Fatal("expected GetFold to find exact match")
	}
	if str, strOk := v.String(); !strOk || str != "uppercase" {
		t.Errorf("expected exact match 'uppercase', got %q", str)
	}

	// Exact match for "Name" should return "mixed"
	v2, ok2 := p.GetFold("Name")
	if !ok2 {
		t.Fatal("expected GetFold to find exact match")
	}
	if str, strOk := v2.String(); !strOk || str != "mixed" {
		t.Errorf("expected exact match 'mixed', got %q", str)
	}

	// No exact match for "name" - should fall back to folded lookup
	// which returns alphabetically first ("NAME" < "Name")
	v3, ok3 := p.GetFold("name")
	if !ok3 {
		t.Fatal("expected GetFold to find folded match")
	}
	if str, strOk := v3.String(); !strOk || str != "uppercase" {
		t.Errorf("expected folded match 'uppercase' (from 'NAME'), got %q", str)
	}
}

func TestProperties_GetFold_O1Performance(t *testing.T) {
	// Tests that GetFold uses O(1) lookup via foldedIndex rather than O(n) scan.
	// We create a large property map and verify GetFold works correctly.
	// (This doesn't directly test performance, but verifies the index is built correctly)
	input := make(map[string]any, 1000)
	for i := range 1000 {
		key := "Key" + string(rune('A'+i%26)) + string(rune('0'+i%10))
		input[key] = i
	}
	// Add a specific key we'll look for
	input["SpecialKey"] = "found"

	p := WrapProperties(input)

	// Should find via exact match
	v, ok := p.GetFold("SpecialKey")
	if !ok {
		t.Fatal("expected to find SpecialKey")
	}
	if str, strOk := v.String(); !strOk || str != "found" {
		t.Errorf("expected 'found', got %v", v.Unwrap())
	}

	// Should find via case-insensitive match
	v2, ok2 := p.GetFold("specialkey")
	if !ok2 {
		t.Fatal("expected to find specialkey (case-insensitive)")
	}
	if str, strOk := v2.String(); !strOk || str != "found" {
		t.Errorf("expected 'found', got %v", v2.Unwrap())
	}
}

// PropertiesOf computes a wrapped map's sorted keys and folded index once
// and shares the view: the second call allocates nothing. Recomputing both
// on every call made a fold miss in the evaluator 3-4x slower than the scan
// it replaced.
func TestPropertiesOf_ComputesTheFoldedViewOnce(t *testing.T) {
	src := map[string]any{}
	for i := range 40 {
		src[string(rune('A'+i%26))+string(rune('a'+i/26))] = int64(i)
	}
	m := WrapMap(src)
	first := PropertiesOf(m)
	if v, ok := first.GetFold("aa"); !ok || v.Unwrap() != int64(0) {
		t.Fatalf("GetFold(aa) = %v, %v; want 0, true", v.Unwrap(), ok)
	}
	if allocs := testing.AllocsPerRun(100, func() { _ = PropertiesOf(m) }); allocs != 0 {
		t.Errorf("PropertiesOf allocates %v per call after the first, want 0", allocs)
	}
	// A nested map wrapped through Wrap shares the same rule.
	nested := Wrap(map[string]any{"outer": src}).Unwrap().(Map[string])
	inner, _ := nested.Get("outer")
	im, _ := inner.Map()
	_ = PropertiesOf(im)
	if allocs := testing.AllocsPerRun(100, func() { _ = PropertiesOf(im) }); allocs != 0 {
		t.Errorf("PropertiesOf on a nested wrapped map allocates %v per call after the first, want 0", allocs)
	}
	if p := PropertiesOf(Map[string]{}); p.Len() != 0 {
		t.Errorf("PropertiesOf(zero Map).Len() = %d, want 0", p.Len())
	}
}

// The first computation may race between goroutines; every caller must see
// one complete view. Run under -race.
func TestPropertiesOf_FirstUseIsSafeConcurrently(t *testing.T) {
	m := WrapMap(map[string]any{"Alpha": int64(1), "beta": int64(2), "GAMMA": int64(3)})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			p := PropertiesOf(m)
			if v, ok := p.GetFold("gamma"); !ok || v.Unwrap() != int64(3) {
				t.Errorf("GetFold(gamma) = %v, %v; want 3, true", v.Unwrap(), ok)
			}
		})
	}
	wg.Wait()
}
