package immutable

import (
	"reflect"
	"strings"
	"testing"
)

// wantCyclePanic runs call and asserts it panicked with this package's cycle
// message. A cyclic value used to exhaust the stack, which is a fatal runtime
// error: nothing a caller defers can run after it.
func wantCyclePanic(t *testing.T, call func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Error("no panic; a cyclic value must be refused")
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("panic value is %T; want a string", r)
			return
		}
		if !strings.Contains(msg, "cycle detected") {
			t.Errorf("panic message %q does not name the cycle", msg)
		}
	}()
	call()
}

// TestWrap_RefusesACyclicValue holds every constructor to refusing a value that
// refers to itself, through the door the caller used.
func TestWrap_RefusesACyclicValue(t *testing.T) {
	t.Parallel()

	rows := []struct {
		name string
		call func()
	}{
		{"Wrap", func() { _ = Wrap(selfReferencingMap()) }},
		{"WrapProperties", func() { _ = WrapProperties(selfReferencingMap()) }},
		{"WrapSlice", func() { _ = WrapSlice([]any{selfReferencingMap()}) }},
		{"WrapKey", func() { _ = WrapKey([]any{selfReferencingMap()}) }},
		{"a cyclic slice", func() { _ = Wrap(selfReferencingSlice()) }},
		{"a non-string-keyed cyclic map", func() {
			m := map[int]any{}
			m[1] = m
			_ = Wrap(m)
		}},
		{"a cyclic slice under a non-string-keyed map", func() {
			_ = WrapProperties(map[string]any{"a": map[int]any{1: selfReferencingSlice()}})
		}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			wantCyclePanic(t, row.call)
		})
	}
}

// TestWrap_DeepValueIsNotACycle holds the guard to refusing cycles only: a
// value nested past the threshold where the walk starts tracking pointers is
// wrapped, not refused.
func TestWrap_DeepValueIsNotACycle(t *testing.T) {
	t.Parallel()

	const depth = startDetectingCyclesAfter + 200
	leaf := map[string]any{"leaf": "value"}
	current := leaf
	for range depth {
		current = map[string]any{"next": current}
	}

	v := Wrap(current)
	m, ok := v.Any().(Properties)
	if !ok {
		t.Fatalf("Wrap stored %T; want Properties", v.Any())
	}
	for range depth {
		next, ok := m.Get("next")
		if !ok {
			t.Fatal("the wrapped value is shallower than it was built")
		}
		m, ok = next.Any().(Properties)
		if !ok {
			t.Fatal("a level of the wrapped value is not a Properties")
		}
	}
	if _, ok := m.Get("leaf"); !ok {
		t.Error("the leaf did not survive the wrap")
	}
}

// TestWrap_SharedValueIsNotACycle holds each walk that tracks its path to
// leaving the path as it found it: one container reached twice from different
// places is a diamond, not a cycle, and it must be copied even when the walk is
// deep enough to be tracking.
func TestWrap_SharedValueIsNotACycle(t *testing.T) {
	t.Parallel()

	const depth = startDetectingCyclesAfter + 200
	stringKeyedDiamond := func() any {
		shared := map[string]any{"leaf": "value"}
		current := map[string]any{"left": shared, "right": shared}
		for range depth {
			current = map[string]any{"next": current}
		}
		return current
	}
	intKeyedDiamond := func() any {
		shared := map[int]any{0: "value"}
		current := map[int]any{0: shared, 1: shared}
		for range depth {
			current = map[int]any{0: current}
		}
		return current
	}
	sliceDiamond := func() any {
		shared := []any{"value"}
		current := []any{shared, shared}
		for range depth {
			current = []any{current}
		}
		return current
	}

	rows := []struct {
		name  string
		value any
		walk  func(v any) any
	}{
		{"a string-keyed map, wrapped", stringKeyedDiamond(), func(v any) any { return Wrap(v).Clone() }},
		{"a slice, wrapped", sliceDiamond(), func(v any) any { return Wrap(v).Clone() }},
		{"a non-string-keyed map, wrapped", intKeyedDiamond(), func(v any) any { return Wrap(v).Any() }},
		{"a slice under a non-string-keyed map, wrapped", map[int]any{0: sliceDiamond()}, func(v any) any {
			return Wrap(v).Clone()
		}},
	}

	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("a container reached twice was refused: %v", r)
				}
			}()
			if got := row.walk(row.value); !reflect.DeepEqual(got, row.value) {
				t.Error("the copy does not hold the content it was made from")
			}
		})
	}
}

// selfReferencingMap returns a map holding itself.
func selfReferencingMap() map[string]any {
	m := map[string]any{"ok": 1}
	m["self"] = m
	return m
}

// selfReferencingSlice returns a slice holding itself.
func selfReferencingSlice() []any {
	s := make([]any, 1)
	s[0] = s
	return s
}
