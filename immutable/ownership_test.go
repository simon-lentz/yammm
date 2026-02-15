package immutable

import (
	"testing"
)

// This file contains cross-cutting tests for ownership semantics.
//
// The Wrap family implements whole-graph ownership transfer:
// - After calling Wrap(v), the caller MUST NOT retain or use any reference
//   to v or any mutable value reachable from v
// - Mutation after Wrap is undefined behavior (not asserted in tests)
//
// The WrapClone family performs a deep clone before wrapping:
// - Safe with shared references
// - Caller may freely retain and mutate the original value after cloning

func TestOwnership_WrapClone_IsolatesNestedMaps(t *testing.T) {
	// Create a deeply nested structure
	level3 := map[string]any{"key": "original"}
	level2 := map[string]any{"level3": level3}
	level1 := map[string]any{"level2": level2}
	outer := map[string]any{"level1": level1}

	// Clone wrap the structure
	wrapped := WrapClone(outer)

	// Mutate all levels of the original
	level3["key"] = "mutated"
	level3["new"] = "added"
	level2["newKey"] = "newValue"
	level1["another"] = "test"
	outer["topNew"] = "topValue"

	// Verify wrapped structure is completely isolated
	m, ok := wrapped.Map()
	if !ok {
		t.Fatal("expected Map")
	}

	// Check outer level
	if _, ok := m.Get("topNew"); ok {
		t.Error("outer mutation leaked to wrapped")
	}

	// Navigate to level3
	l1Val, _ := m.Get("level1")
	l1Map, _ := l1Val.Map()
	l2Val, _ := l1Map.Get("level2")
	l2Map, _ := l2Val.Map()
	l3Val, _ := l2Map.Get("level3")
	l3Map, _ := l3Val.Map()

	// Check level3
	keyVal, ok := l3Map.Get("key")
	if !ok {
		t.Fatal("expected key in level3")
	}
	if s, _ := keyVal.String(); s != "original" {
		t.Errorf("expected 'original', got %q", s)
	}
	if _, ok := l3Map.Get("new"); ok {
		t.Error("level3 mutation leaked to wrapped")
	}
}

func TestOwnership_WrapClone_IsolatesNestedSlices(t *testing.T) {
	// Create a nested slice structure
	inner := []any{"a", "b", "c"}
	outer := []any{inner, "other"}

	// Clone wrap the structure
	wrapped := WrapSliceClone(outer)

	// Mutate original
	inner[0] = "mutated"
	outer[1] = "changed"

	// Verify wrapped structure is isolated
	if wrapped.Len() != 2 {
		t.Errorf("expected length 2, got %d", wrapped.Len())
	}

	// Check first element (the inner slice)
	firstVal := wrapped.Get(0)
	innerSlice, ok := firstVal.Slice()
	if !ok {
		t.Fatal("expected first element to be Slice")
	}

	firstElem := innerSlice.Get(0)
	if s, _ := firstElem.String(); s != "a" {
		t.Errorf("expected 'a', got %q", s)
	}

	// Check second element
	secondVal := wrapped.Get(1)
	if s, _ := secondVal.String(); s != "other" {
		t.Errorf("expected 'other', got %q", s)
	}
}

func TestOwnership_WrapClone_IsolatesMixedStructures(t *testing.T) {
	// Create a mixed structure with maps and slices
	inner := map[string]any{
		"list":  []any{1, 2, 3},
		"value": "test",
	}
	outer := []any{inner, map[string]any{"other": "data"}}

	// Clone wrap the structure
	wrapped := WrapSliceClone(outer)

	// Mutate original structures
	inner["value"] = "mutated"
	innerList := inner["list"].([]any)
	innerList[0] = 999

	// Verify wrapped structure is isolated
	firstVal := wrapped.Get(0)
	firstMap, ok := firstVal.Map()
	if !ok {
		t.Fatal("expected first element to be Map")
	}

	// Check value
	valVal, _ := firstMap.Get("value")
	if s, _ := valVal.String(); s != "test" {
		t.Errorf("expected 'test', got %q", s)
	}

	// Check list
	listVal, _ := firstMap.Get("list")
	listSlice, _ := listVal.Slice()
	firstNum := listSlice.Get(0)
	if n, _ := firstNum.Int(); n != 1 {
		t.Errorf("expected 1, got %d", n)
	}
}

func TestOwnership_MapClone_Independence(t *testing.T) {
	// Test that Clone() returns an independent mutable copy
	original := map[string]any{
		"nested": map[string]any{"key": "value"},
	}

	wrapped := WrapMap(original)
	cloned := wrapped.Clone()

	// Mutate the clone
	nested := cloned["nested"].(map[string]any)
	nested["key"] = "modified"
	cloned["newKey"] = "newValue"

	// Verify original wrapped structure is unchanged
	nestedVal, _ := wrapped.Get("nested")
	nestedMap, _ := nestedVal.Map()
	keyVal, _ := nestedMap.Get("key")
	if s, _ := keyVal.String(); s != "value" {
		t.Error("clone modification affected wrapped structure")
	}
	if _, ok := wrapped.Get("newKey"); ok {
		t.Error("clone addition affected wrapped structure")
	}
}

func TestOwnership_SliceClone_Independence(t *testing.T) {
	// Test that Clone() returns an independent mutable copy
	original := []any{
		map[string]any{"key": "value"},
		"string",
	}

	wrapped := WrapSlice(original)
	cloned := wrapped.Clone()

	// Mutate the clone
	nested := cloned[0].(map[string]any)
	nested["key"] = "modified"
	cloned[1] = "changed"

	// Verify original wrapped structure is unchanged
	firstVal := wrapped.Get(0)
	firstMap, _ := firstVal.Map()
	keyVal, _ := firstMap.Get("key")
	if s, _ := keyVal.String(); s != "value" {
		t.Error("clone modification affected wrapped structure")
	}

	secondVal := wrapped.Get(1)
	if s, _ := secondVal.String(); s != "string" {
		t.Error("clone modification affected wrapped structure")
	}
}

func TestOwnership_PropertiesClone_Independence(t *testing.T) {
	original := map[string]any{
		"prop": map[string]any{"nested": "data"},
	}

	wrapped := WrapProperties(original)
	cloned := wrapped.Clone()

	// Mutate the clone
	nested := cloned["prop"].(map[string]any)
	nested["nested"] = "modified"

	// Verify original wrapped structure is unchanged
	propVal, _ := wrapped.Get("prop")
	propMap, _ := propVal.Map()
	nestedVal, _ := propMap.Get("nested")
	if s, _ := nestedVal.String(); s != "data" {
		t.Error("clone modification affected wrapped properties")
	}
}

func TestOwnership_KeyClone_Independence(t *testing.T) {
	original := []any{"component", 123}

	wrapped := WrapKey(original)
	cloned := wrapped.Clone()

	// Mutate the clone
	cloned[0] = "modified"

	// Verify original wrapped structure is unchanged
	firstVal := wrapped.Get(0)
	if s, _ := firstVal.String(); s != "component" {
		t.Error("clone modification affected wrapped key")
	}
}

func TestOwnership_DeeplyNestedStructure(t *testing.T) {
	// Create a 5-level deep structure
	level5 := map[string]any{"deepValue": "leaf"}
	level4 := map[string]any{"level5": level5}
	level3 := []any{level4}
	level2 := map[string]any{"level3": level3}
	level1 := []any{level2}
	root := map[string]any{"level1": level1}

	// Clone wrap
	wrapped := WrapMapClone(root)

	// Mutate all levels
	level5["deepValue"] = "mutated"
	level4["new4"] = "added"
	level3[0] = "replaced"
	level2["new2"] = "added"
	level1[0] = "replaced"
	root["new0"] = "added"

	// Navigate and verify isolation
	l1Val, _ := wrapped.Get("level1")
	l1Slice, _ := l1Val.Slice()
	l2Val := l1Slice.Get(0)
	l2Map, _ := l2Val.Map()
	l3Val, _ := l2Map.Get("level3")
	l3Slice, _ := l3Val.Slice()
	l4Val := l3Slice.Get(0)
	l4Map, _ := l4Val.Map()
	l5Val, _ := l4Map.Get("level5")
	l5Map, _ := l5Val.Map()
	deepVal, _ := l5Map.Get("deepValue")

	if s, _ := deepVal.String(); s != "leaf" {
		t.Errorf("expected 'leaf', got %q", s)
	}
}

// Document: Mutation after Wrap is undefined behavior.
// This test demonstrates the ownership transfer contract but does NOT assert
// on the behavior after mutation, since it is undefined.
func TestOwnership_WrapOwnershipDocumentation(t *testing.T) {
	// This test serves as documentation for the ownership contract.
	//
	// After calling Wrap(v), the caller transfers ownership of v and all
	// transitively reachable mutable values to the immutable wrapper.
	// The caller MUST NOT retain or use any reference to v after this point.
	//
	// If the caller violates this contract by mutating v after Wrap, the
	// behavior is undefined. The wrapped structure may or may not reflect
	// the mutations - this is implementation-dependent and should not be
	// relied upon.
	//
	// To safely work with values that may be shared or mutated after wrapping,
	// use the WrapClone family of functions instead.

	original := map[string]any{"key": "value"}

	// After this call, 'original' ownership is transferred
	_ = WrapMap(original)

	// BAD: Do not do this! Mutating 'original' after Wrap is undefined behavior.
	// original["key"] = "mutated" // UNDEFINED BEHAVIOR

	// GOOD: Use WrapClone if you need to retain and mutate the original
	retained := map[string]any{"key": "value"}
	wrapped := WrapMapClone(retained)
	retained["key"] = "mutated" // Safe: wrapped is isolated

	keyVal, _ := wrapped.Get("key")
	if s, _ := keyVal.String(); s != "value" {
		t.Error("WrapClone should isolate from mutations")
	}
}
