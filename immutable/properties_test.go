package immutable

import (
	"slices"
	"sync"
	"testing"
)

func TestProperties_WrapProperties(t *testing.T) {
	p := WrapProperties(map[string]any{
		"name":  "Alice",
		"age":   int64(30),
		"email": "alice@example.com",
	})

	if p.Len() != 3 {
		t.Errorf("Len() = %d, want 3", p.Len())
	}
	name, ok := p.Get("name")
	if !ok {
		t.Fatal("Get(name) = _, false")
	}
	wantString(t, name, "Alice")
	age, ok := p.Get("age")
	if !ok {
		t.Fatal("Get(age) = _, false")
	}
	wantInt(t, age, 30)
	if _, ok := p.Get("Name"); ok {
		t.Error("Get is case-sensitive, but Get(Name) found name")
	}
}

// A nil map gives the zero Properties, which is a nil container; an empty map
// gives an empty one, which is not.
func TestProperties_NilAndEmpty(t *testing.T) {
	for name, tt := range map[string]struct {
		p       Properties
		wantNil bool
	}{
		"a nil map":          {WrapProperties(nil), true},
		"the zero value":     {Properties{}, true},
		"an empty map":       {WrapProperties(map[string]any{}), false},
		"a nil map, Wrap":    {Wrap(map[string]any(nil)).Any().(Properties), true},
		"an empty map, Wrap": {Wrap(map[string]any{}).Any().(Properties), false},
	} {
		if tt.p.Len() != 0 {
			t.Errorf("%s: Len() = %d, want 0", name, tt.p.Len())
		}
		if _, ok := tt.p.Get("anything"); ok {
			t.Errorf("%s: Get found a value", name)
		}
		if got := IsNilContainer(tt.p); got != tt.wantNil {
			t.Errorf("%s: IsNilContainer = %v, want %v", name, got, tt.wantNil)
		}
		if got := tt.p.Clone() == nil; got != tt.wantNil {
			t.Errorf("%s: Clone() == nil is %v, want %v", name, got, tt.wantNil)
		}
		for range tt.p.Keys() {
			t.Errorf("%s: Keys yielded a name", name)
		}
		for range tt.p.All() {
			t.Errorf("%s: All yielded a pair", name)
		}
	}
}

// Keys and All visit the names in sorted order, every time.
func TestProperties_IterateInSortedOrder(t *testing.T) {
	p := WrapProperties(map[string]any{"z": int64(26), "a": int64(1), "m": int64(13), "b": int64(2)})

	for pass := range 2 {
		if got := slices.Collect(p.Keys()); !slices.Equal(got, []string{"a", "b", "m", "z"}) {
			t.Errorf("pass %d: Keys() = %v, want sorted", pass, got)
		}
		var keys []string
		var values []int64
		for k, v := range p.All() {
			keys = append(keys, k)
			n, _ := v.AsInt()
			values = append(values, n)
		}
		if !slices.Equal(keys, []string{"a", "b", "m", "z"}) || !slices.Equal(values, []int64{1, 2, 13, 26}) {
			t.Errorf("pass %d: All() = %v %v, want sorted pairs", pass, keys, values)
		}
	}
}

func TestProperties_IteratorsStopEarly(t *testing.T) {
	p := WrapProperties(map[string]any{"a": 1, "b": 2, "c": 3, "d": 4})

	count := 0
	for range p.Keys() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("Keys: %d names after a break at 2", count)
	}
	count = 0
	for range p.All() {
		count++
		if count == 2 {
			break
		}
	}
	if count != 2 {
		t.Errorf("All: %d pairs after a break at 2", count)
	}
}

func TestProperties_NestedProperties(t *testing.T) {
	p := WrapProperties(map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{"value": "deep"},
		},
	})

	level1, _ := p.Get("level1")
	m1, ok := level1.Any().(Properties)
	if !ok {
		t.Fatalf("level1 holds %T, want Properties", level1.Any())
	}
	level2, _ := m1.Get("level2")
	m2, ok := level2.Any().(Properties)
	if !ok {
		t.Fatalf("level2 holds %T, want Properties", level2.Any())
	}
	value, ok := m2.Get("value")
	if !ok {
		t.Fatal("the leaf did not survive the wrap")
	}
	wantString(t, value, "deep")
}

func TestProperties_Clone(t *testing.T) {
	p := WrapProperties(map[string]any{
		"nested": map[string]any{"value": "original"},
		"items":  []any{"a", "b"},
		"byID":   map[int]any{1: "one"},
	})
	cloned := p.Clone()

	nested, ok := cloned["nested"].(map[string]any)
	if !ok || nested["value"] != "original" {
		t.Fatalf("nested clones to %#v, want map[string]any{value: original}", cloned["nested"])
	}
	items, ok := cloned["items"].([]any)
	if !ok || !slices.Equal(items, []any{"a", "b"}) {
		t.Fatalf("items clones to %#v, want []any{a, b}", cloned["items"])
	}
	byID, ok := cloned["byID"].(map[int]any)
	if !ok || byID[1] != "one" {
		t.Fatalf("byID clones to %#v, want map[int]any{1: one}", cloned["byID"])
	}

	nested["value"] = "modified"
	items[0] = "modified"
	byID[1] = "modified"
	cloned["added"] = true

	if again := p.Clone(); again["nested"].(map[string]any)["value"] != "original" ||
		again["items"].([]any)[0] != "a" || again["byID"].(map[int]any)[1] != "one" {
		t.Errorf("a change to a clone reached the Properties: %#v", again)
	}
	if _, ok := p.Get("added"); ok {
		t.Error("a name added to a clone reached the Properties")
	}
}

func TestProperties_CloneOfAnEmptyMapIsMutable(t *testing.T) {
	cloned := WrapProperties(map[string]any{}).Clone()
	if cloned == nil {
		t.Fatal("Clone of an empty map = nil, want a map")
	}
	cloned["x"] = 1
}

// The first computation may race between goroutines; every caller must see
// one complete view. Run under -race.
func TestProperties_FirstUseIsSafeConcurrently(t *testing.T) {
	p := WrapProperties(map[string]any{"Alpha": int64(1), "beta": int64(2), "GAMMA": int64(3)})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if got := slices.Collect(p.Keys()); !slices.Equal(got, []string{"Alpha", "GAMMA", "beta"}) {
				t.Errorf("Keys() = %v, want sorted", got)
			}
		})
	}
	wg.Wait()
}
