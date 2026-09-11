package immutable

import (
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
		{"Wrap with clone", func() { _ = Wrap(selfReferencingMap(), WithClone(true)) }},
		{"WrapMap", func() { _ = WrapMap(selfReferencingMap()) }},
		{"WrapProperties", func() { _ = WrapProperties(selfReferencingMap()) }},
		{"WrapSlice", func() { _ = WrapSlice([]any{selfReferencingMap()}) }},
		{"WrapKey", func() { _ = WrapKey([]any{selfReferencingMap()}) }},
		{"a cyclic slice", func() { _ = Wrap(selfReferencingSlice()) }},
		{"a non-string-keyed cyclic map, cloned", func() {
			m := map[int]any{}
			m[1] = m
			_ = Wrap(m, WithClone(true))
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
	m, ok := v.Map()
	if !ok {
		t.Fatalf("Map() = _, false; want the wrapped map")
	}
	for range depth {
		next, ok := m.Get("next")
		if !ok {
			t.Fatal("the wrapped value is shallower than it was built")
		}
		m, ok = next.Map()
		if !ok {
			t.Fatal("a level of the wrapped value is not a map")
		}
	}
	if _, ok := m.Get("leaf"); !ok {
		t.Error("the leaf did not survive the wrap")
	}
}

// TestWrap_SharedValueIsNotACycle holds the guard to leaving the path as it
// found it: one map reached twice from different places is a diamond, not a
// cycle, and it must wrap even when the walk is deep enough to be tracking.
func TestWrap_SharedValueIsNotACycle(t *testing.T) {
	t.Parallel()

	shared := map[string]any{"leaf": "value"}
	const depth = startDetectingCyclesAfter + 200
	current := map[string]any{"left": shared, "right": shared}
	for range depth {
		current = map[string]any{"next": current}
	}

	v := Wrap(current)
	m, ok := v.Map()
	if !ok {
		t.Fatalf("Map() = _, false; want the wrapped map")
	}
	for range depth {
		next, _ := m.Get("next")
		m, ok = next.Map()
		if !ok {
			t.Fatal("a level of the wrapped value is not a map")
		}
	}
	for _, side := range []string{"left", "right"} {
		branch, ok := m.Get(side)
		if !ok {
			t.Fatalf("%s: the shared value did not survive the wrap", side)
		}
		leaf, ok := branch.Map()
		if !ok {
			t.Fatalf("%s: the shared value is not a map", side)
		}
		if _, ok := leaf.Get("leaf"); !ok {
			t.Errorf("%s: the shared value is empty", side)
		}
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
