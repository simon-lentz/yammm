package immutable

import (
	"slices"
	"strings"
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
		if _, ok := tt.p.GetFold("ANYTHING"); ok {
			t.Errorf("%s: GetFold found a value", name)
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

func TestProperties_GetFold_CaseInsensitive(t *testing.T) {
	p := WrapProperties(map[string]any{"UserName": "Alice"})

	for _, name := range []string{"UserName", "username", "USERNAME", "uSeRnAmE"} {
		t.Run(name, func(t *testing.T) {
			v, ok := p.GetFold(name)
			if !ok {
				t.Fatalf("GetFold(%q) found nothing", name)
			}
			wantString(t, v, "Alice")
		})
	}
}

func TestProperties_GetFold_ASCIIOnly(t *testing.T) {
	p := WrapProperties(map[string]any{"café": "value"})

	if _, ok := p.GetFold("CAFÉ"); ok {
		t.Error("GetFold folded a non-ASCII letter")
	}
	v, ok := p.GetFold("CAFé")
	if !ok {
		t.Fatal("GetFold(CAFé) found nothing; the ASCII letters fold")
	}
	wantString(t, v, "value")
}

func TestProperties_GetFold_NotFound(t *testing.T) {
	p := WrapProperties(map[string]any{"name": "value"})

	if _, ok := p.GetFold("nonexistent"); ok {
		t.Error("GetFold found a name the map does not hold")
	}
}

// When several names fold to one, the alphabetically first wins, whichever
// spelling asks.
func TestProperties_GetFold_CollisionDeterminism(t *testing.T) {
	p := WrapProperties(map[string]any{
		"Name": "name-value",
		"NAME": "all-uppercase-value",
		"NaMe": "mixed-case-value",
	})

	for _, query := range []string{"nAmE", "namE", "nAME", "name"} {
		v, ok := p.GetFold(query)
		if !ok {
			t.Fatalf("GetFold(%q) found nothing", query)
		}
		wantString(t, v, "all-uppercase-value")
	}
}

// An exact match wins over a folded one.
func TestProperties_GetFold_ExactMatchPriority(t *testing.T) {
	p := WrapProperties(map[string]any{"NAME": "uppercase", "Name": "mixed"})

	for query, want := range map[string]string{"NAME": "uppercase", "Name": "mixed", "name": "uppercase"} {
		v, ok := p.GetFold(query)
		if !ok {
			t.Fatalf("GetFold(%q) found nothing", query)
		}
		wantString(t, v, want)
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

// The sorted names and folded index are computed once and shared by every
// copy: a GetFold after the first allocates nothing, at the top and nested.
func TestProperties_ComputesTheFoldedViewOnce(t *testing.T) {
	src := map[string]any{}
	for i := range 40 {
		src[string(rune('A'+i%26))+string(rune('a'+i/26))] = int64(i)
	}
	p := WrapProperties(src)
	if v, ok := p.GetFold("aa"); !ok || v.Any() != int64(0) {
		t.Fatalf("GetFold(aa) = %v, %v; want 0, true", v.Any(), ok)
	}
	if allocs := testing.AllocsPerRun(100, func() { _, _ = p.GetFold("zz") }); allocs != 0 {
		t.Errorf("GetFold allocates %v per call after the first, want 0", allocs)
	}
	shared := p
	if allocs := testing.AllocsPerRun(100, func() { _, _ = shared.GetFold("zz") }); allocs != 0 {
		t.Errorf("GetFold on a copy allocates %v per call, want 0", allocs)
	}

	outer := WrapProperties(map[string]any{"outer": src})
	inner, _ := outer.Get("outer")
	ip := inner.Any().(Properties)
	_, _ = ip.GetFold("aa")
	if allocs := testing.AllocsPerRun(100, func() { _, _ = ip.GetFold("zz") }); allocs != 0 {
		t.Errorf("GetFold on a nested Properties allocates %v per call after the first, want 0", allocs)
	}
}

// TestProperties_ASortedReadBuildsNoFoldedIndex holds a sorted read to the
// names alone: the folded index waits for a GetFold that misses the exact name.
func TestProperties_ASortedReadBuildsNoFoldedIndex(t *testing.T) {
	p := WrapProperties(map[string]any{"b": int64(2), "A": int64(1)})
	for range p.All() {
	}
	if _, ok := p.GetFold("b"); !ok {
		t.Fatal("GetFold(b) found nothing")
	}
	if p.view.foldedIndex != nil {
		t.Fatalf("after All and an exact GetFold, the folded index is %v, want none", p.view.foldedIndex)
	}
	if _, ok := p.GetFold("a"); !ok || p.view.foldedIndex == nil {
		t.Errorf("GetFold(a) = %v with folded index %v, want a hit that builds it", ok, p.view.foldedIndex)
	}
}

// TestProperties_AFoldedMissAllocatesNothing holds a GetFold that misses the
// exact name to no allocation once the index is built, for a name of up to 64
// bytes, capitals or not.
func TestProperties_AFoldedMissAllocatesNothing(t *testing.T) {
	p := WrapProperties(map[string]any{"Name": int64(1)})
	_, _ = p.GetFold("name")
	for _, q := range []string{"NAME", strings.Repeat("Q", 64), strings.Repeat("q", 64), strings.Repeat("q", 33)} {
		if allocs := testing.AllocsPerRun(100, func() { _, _ = p.GetFold(q) }); allocs != 0 {
			t.Errorf("GetFold(%d bytes) allocates %v per call, want 0", len(q), allocs)
		}
	}
}

// foldSink keeps toLowerASCII's answer live for an allocation test.
var foldSink string

// TestToLowerASCII_ReturnsALowercaseNameAsIs holds the folded index's build to
// one copy per name that holds a capital: a lowercase name is used as given.
func TestToLowerASCII_ReturnsALowercaseNameAsIs(t *testing.T) {
	name := strings.Repeat("q", 40)
	if allocs := testing.AllocsPerRun(100, func() { foldSink = toLowerASCII(name) }); allocs != 0 {
		t.Errorf("toLowerASCII of a lowercase name allocates %v per call, want 0", allocs)
	}
	if got := toLowerASCII("aBc[Z"); got != "abc[z" {
		t.Errorf("toLowerASCII(aBc[Z) = %q, want abc[z", got)
	}
}

// The first computation may race between goroutines; every caller must see
// one complete view. Run under -race.
func TestProperties_FirstUseIsSafeConcurrently(t *testing.T) {
	p := WrapProperties(map[string]any{"Alpha": int64(1), "beta": int64(2), "GAMMA": int64(3)})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			if v, ok := p.GetFold("gamma"); !ok || v.Any() != int64(3) {
				t.Errorf("GetFold(gamma) = %v, %v; want 3, true", v.Any(), ok)
			}
			if got := slices.Collect(p.Keys()); !slices.Equal(got, []string{"Alpha", "GAMMA", "beta"}) {
				t.Errorf("Keys() = %v, want sorted", got)
			}
		})
	}
	wg.Wait()
}
