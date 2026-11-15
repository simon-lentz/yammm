package immutable

import (
	"fmt"
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/internal/yammmtest"
)

func TestValue_Wrap_Scalars(t *testing.T) {
	type check func(t *testing.T, v Value)
	tests := []struct {
		name  string
		input any
		check check
	}{
		{"nil", nil, func(t *testing.T, v Value) { t.Helper(); wantNil(t, v) }},
		{"bool true", true, func(t *testing.T, v Value) { t.Helper(); wantBool(t, v, true) }},
		{"bool false", false, func(t *testing.T, v Value) { t.Helper(); wantBool(t, v, false) }},
		{"string", "hello", func(t *testing.T, v Value) { t.Helper(); wantString(t, v, "hello") }},
		{"empty string", "", func(t *testing.T, v Value) { t.Helper(); wantString(t, v, "") }},
		{"int64", int64(9999999999), func(t *testing.T, v Value) { t.Helper(); wantInt(t, v, 9999999999) }},
		{"float64", 3.14, func(t *testing.T, v Value) {
			t.Helper()
			if f, ok := v.AsFloat(); !ok || f != 3.14 {
				t.Errorf("AsFloat() = %v, %v; want 3.14, true", f, ok)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			tt.check(t, v)
			if v.Any() != tt.input {
				t.Errorf("Any() = %#v, want %#v as given", v.Any(), tt.input)
			}
		})
	}
}

// Each As method reads its one stored form and nothing else: an Integer is
// stored as an int64 and a Float as a float64, so AsInt and AsFloat neither
// widen another integer type nor convert between the two.
func TestValue_AsReadsItsOneFormExactly(t *testing.T) {
	type reads struct{ boolean, integer, float, str, slice bool }
	tests := []struct {
		name  string
		input any
		want  reads
	}{
		{"bool", true, reads{boolean: true}},
		{"int64", int64(1), reads{integer: true}},
		{"int64 minimum", int64(math.MinInt64), reads{integer: true}},
		{"float64", 1.5, reads{float: true}},
		{"a whole float64", float64(42), reads{float: true}},
		{"string", "s", reads{str: true}},
		{"slice", []any{1}, reads{slice: true}},
		{"a typed slice", []string{"a"}, reads{slice: true}},
		{"int", 1, reads{}},
		{"int8", int8(1), reads{}},
		{"int16", int16(1), reads{}},
		{"int32", int32(1), reads{}},
		{"uint", uint(1), reads{}},
		{"uint8", uint8(1), reads{}},
		{"uint16", uint16(1), reads{}},
		{"uint32", uint32(1), reads{}},
		{"uint64", uint64(1), reads{}},
		{"float32", float32(1), reads{}},
		{"a named string", label("s"), reads{}},
		{"nil", nil, reads{}},
		{"a string-keyed map", map[string]any{"a": 1}, reads{}},
		{"an array", [1]int{1}, reads{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			var got reads
			_, got.boolean = v.AsBool()
			_, got.integer = v.AsInt()
			_, got.float = v.AsFloat()
			_, got.str = v.AsString()
			_, got.slice = v.AsSlice()
			if got != tt.want {
				t.Errorf("reads %+v, want %+v", got, tt.want)
			}
		})
	}
}

// label is a named string type, which AsString does not read.
type label string

// A miss returns the zero value with false.
func TestValue_AsMissReturnsZero(t *testing.T) {
	v := Wrap(map[string]any{"a": 1})
	if b, ok := v.AsBool(); b || ok {
		t.Errorf("AsBool() = %v, %v", b, ok)
	}
	if n, ok := v.AsInt(); n != 0 || ok {
		t.Errorf("AsInt() = %v, %v", n, ok)
	}
	if f, ok := v.AsFloat(); f != 0 || ok {
		t.Errorf("AsFloat() = %v, %v", f, ok)
	}
	if s, ok := v.AsString(); s != "" || ok {
		t.Errorf("AsString() = %q, %v", s, ok)
	}
	if s, ok := v.AsSlice(); !IsNilContainer(s) || ok {
		t.Errorf("AsSlice() = %v, %v; want the zero Slice, false", s, ok)
	}
}

// String formats what Clone returns, as fmt.Sprint does, so Value is a
// fmt.Stringer whose output shows a container as Go data.
func TestValue_String(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{"a string", "x", "x"},
		{"an int64", int64(7), "7"},
		{"nil", nil, "<nil>"},
		{"a map", map[string]any{"b": int64(2), "a": "x"}, "map[a:x b:2]"},
		{"a slice", []any{"a", int64(1)}, "[a 1]"},
		{"a nil slice", []any(nil), "[]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := Wrap(tt.input)
			if got := v.String(); got != tt.want {
				t.Errorf("String() = %q, want %q", got, tt.want)
			}
			if got := fmt.Sprint(v); got != tt.want {
				t.Errorf("fmt.Sprint(v) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestValue_Wrap_Map(t *testing.T) {
	v := Wrap(map[string]any{"name": "Alice", "age": int64(30)})

	m, ok := v.Any().(Properties)
	if !ok {
		t.Fatalf("Wrap stored %T, want Properties", v.Any())
	}
	if m.Len() != 2 {
		t.Errorf("Len() = %d, want 2", m.Len())
	}
	name, _ := m.Get("name")
	wantString(t, name, "Alice")
	age, _ := m.Get("age")
	wantInt(t, age, 30)
}

// A map with any string key kind is a Properties: a named string key type too.
func TestValue_Wrap_NamedStringKeyIsProperties(t *testing.T) {
	v := Wrap(map[label]int{"a": 1})
	m, ok := v.Any().(Properties)
	if !ok {
		t.Fatalf("Wrap stored %T, want Properties", v.Any())
	}
	if got, _ := m.Get("a"); got.Any() != 1 {
		t.Errorf("Get(a) = %#v, want 1", got.Any())
	}
}

func TestValue_Wrap_Slice(t *testing.T) {
	v := Wrap([]string{"a", "b", "c"})

	s, ok := v.AsSlice()
	if !ok {
		t.Fatalf("Wrap stored %T, want Slice", v.Any())
	}
	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}
	wantString(t, s.At(0), "a")
}

func TestValue_NestedStructures(t *testing.T) {
	v := Wrap(map[string]any{
		"level1": map[string]any{
			"level2": map[string]any{"level3": []any{"deep", "value"}},
		},
	})

	m1 := v.Any().(Properties)
	l1, _ := m1.Get("level1")
	m2, ok := l1.Any().(Properties)
	if !ok {
		t.Fatalf("level1 holds %T, want Properties", l1.Any())
	}
	l2, _ := m2.Get("level2")
	m3, ok := l2.Any().(Properties)
	if !ok {
		t.Fatalf("level2 holds %T, want Properties", l2.Any())
	}
	l3, _ := m3.Get("level3")
	s, ok := l3.AsSlice()
	if !ok {
		t.Fatalf("level3 holds %T, want Slice", l3.Any())
	}
	if s.Len() != 2 {
		t.Errorf("Len() = %d, want 2", s.Len())
	}
	wantString(t, s.At(0), "deep")
}

// A map with a non-string key is deep-copied as its own type.
func TestWrap_NonStringKeyedMapIsDeepCopied(t *testing.T) {
	inner := []any{"x"}
	in := map[int]any{1: "one", 2: inner, 3: map[string]any{"k": "v"}}
	v := Wrap(in)

	got, ok := v.Any().(map[int]any)
	if !ok {
		t.Fatalf("Wrap stored %T, want map[int]any", v.Any())
	}
	if !reflect.DeepEqual(got, in) {
		t.Fatalf("Any() = %#v, want %#v", got, in)
	}
	if reflect.ValueOf(got).Pointer() == reflect.ValueOf(in).Pointer() {
		t.Error("Wrap stored the caller's map")
	}
	if reflect.ValueOf(got[2]).Pointer() == reflect.ValueOf(inner).Pointer() {
		t.Error("Wrap stored the caller's slice inside the map")
	}
}

// A deep copy keeps the element types it was given: a nil map or slice element
// stays that typed nil, and a nil interface element stays nil.
func TestWrap_NonStringKeyedMapKeepsNilElements(t *testing.T) {
	m, ok := Wrap(map[int]any{
		1: map[string]any(nil),
		2: []any(nil),
		3: nil,
		4: []any{nil, "hello", nil},
	}).Any().(map[int]any)
	if !ok {
		t.Fatal("Wrap did not store a map[int]any")
	}
	if len(m) != 4 {
		t.Fatalf("len = %d, want 4: a nil element was dropped", len(m))
	}
	if v, ok := m[1].(map[string]any); !ok || v != nil {
		t.Errorf("element 1 = %#v, want the typed nil map[string]any", m[1])
	}
	if v, ok := m[2].([]any); !ok || v != nil {
		t.Errorf("element 2 = %#v, want the typed nil []any", m[2])
	}
	if v, ok := m[3]; !ok || v != nil {
		t.Errorf("element 3 = %#v, %v; want a present nil", v, ok)
	}
	if s, ok := m[4].([]any); !ok || !reflect.DeepEqual(s, []any{nil, "hello", nil}) {
		t.Errorf("element 4 = %#v, want []any{nil, hello, nil}", m[4])
	}
}

func TestWrap_NilInStringKeyedMap(t *testing.T) {
	m := Wrap(map[string]any{"a": nil, "b": "value", "c": nil}).Any().(Properties)

	for _, k := range []string{"a", "c"} {
		v, ok := m.Get(k)
		if !ok {
			t.Errorf("key %s was dropped", k)
		}
		wantNil(t, v)
	}
	b, _ := m.Get("b")
	wantString(t, b, "value")
}

func TestWrap_NilInSlice(t *testing.T) {
	s, ok := Wrap([]any{nil, "hello", nil, int64(42), nil}).AsSlice()
	if !ok || s.Len() != 5 {
		t.Fatalf("AsSlice() = %v, %v; want five elements", s, ok)
	}
	for _, i := range []int{0, 2, 4} {
		wantNil(t, s.At(i))
	}
	wantString(t, s.At(1), "hello")
	wantInt(t, s.At(3), 42)
}

// A nil map or slice is wrapped as a nil container, so it stays apart both from
// literal nil, which is no container, and from an empty one. A nil pointer, a
// nil channel and a nil function are stored as given.
func TestWrap_NilForms(t *testing.T) {
	var nilPtr *int
	var nilChan chan int
	var nilFunc func()

	for name, tt := range map[string]struct {
		input        any
		literal      bool
		nilContainer bool
	}{
		"literal nil":       {nil, true, false},
		"a nil map":         {map[string]any(nil), false, true},
		"a nil slice":       {[]any(nil), false, true},
		"an empty map":      {map[string]any{}, false, false},
		"an empty slice":    {[]any{}, false, false},
		"a nil pointer":     {nilPtr, false, false},
		"a nil channel":     {nilChan, false, false},
		"a nil function":    {nilFunc, false, false},
		"a non-nil pointer": {new(int), false, false},
	} {
		v := Wrap(tt.input)
		if got := v.Any() == nil; got != tt.literal {
			t.Errorf("%s: Any() == nil is %v, want %v", name, got, tt.literal)
		}
		if got := IsNilContainer(v.Any()); got != tt.nilContainer {
			t.Errorf("%s: IsNilContainer(Any()) = %v, want %v", name, got, tt.nilContainer)
		}
	}

	if _, ok := Wrap(map[string]any(nil)).Any().(Properties); !ok {
		t.Error("a nil map is not a Properties")
	}
	if _, ok := Wrap([]any(nil)).AsSlice(); !ok {
		t.Error("a nil slice is not a Slice")
	}
	if _, ok := Wrap(nil).AsSlice(); ok {
		t.Error("literal nil reads as a Slice")
	}
	if got := Wrap(nilFunc).Any(); reflect.ValueOf(got).Kind() != reflect.Func || !reflect.ValueOf(got).IsNil() {
		t.Errorf("a nil function is stored as %#v, want the typed nil", got)
	}
}

// Wrap copies every map and slice it reaches, at every depth, so the caller
// can change its value afterwards.
func TestWrap_CopiesNestedContainers(t *testing.T) {
	level5 := map[string]any{"deepValue": "leaf"}
	level4 := map[string]any{"level5": level5}
	level3 := []any{level4}
	level2 := map[string]any{"level3": level3}
	level1 := []any{level2}
	root := map[string]any{"level1": level1}

	p := WrapProperties(root)

	level5["deepValue"] = "mutated"
	level4["new4"] = "added"
	level3[0] = "replaced"
	level2["new2"] = "added"
	level1[0] = "replaced"
	root["new0"] = "added"

	want := map[string]any{"level1": []any{map[string]any{"level3": []any{map[string]any{"level5": map[string]any{"deepValue": "leaf"}}}}}}
	if got := p.Clone(); !reflect.DeepEqual(got, want) {
		t.Errorf("Clone() = %#v after the caller's changes, want %#v", got, want)
	}
}

// A struct, a pointer or an array is stored as given, so what it refers to
// stays shared with the caller.
func TestWrap_StoresAPointerAsGiven(t *testing.T) {
	type holder struct{ M map[string]any }
	h := &holder{M: map[string]any{"k": "v"}}
	v := Wrap(map[string]any{"h": h})

	h.M["k"] = "changed"

	got, _ := v.Any().(Properties).Get("h")
	if got.Any().(*holder).M["k"] != "changed" {
		t.Error("Wrap copied what a pointer refers to")
	}
}

func TestValue_Clone_Independence(t *testing.T) {
	tests := []struct {
		name   string
		clone  func() any
		mutate func(any)
	}{
		{"Value", func() any { return Wrap(map[string]any{"n": map[string]any{"k": "v"}}).Clone() }, func(c any) {
			c.(map[string]any)["n"].(map[string]any)["k"] = "modified"
		}},
		{"Properties", func() any { return WrapProperties(map[string]any{"n": map[string]any{"k": "v"}}).Clone() }, func(c any) {
			c.(map[string]any)["n"].(map[string]any)["k"] = "modified"
		}},
		{"Slice", func() any { return WrapSlice([]any{map[string]any{"k": "v"}}).Clone() }, func(c any) {
			c.([]any)[0].(map[string]any)["k"] = "modified"
		}},
		{"Key", func() any { return WrapKey([]any{"component", int64(123)}).Clone() }, func(c any) {
			c.([]any)[0] = "modified"
		}},
		{"a map with an int key", func() any { return Wrap(map[int]any{1: []any{"x"}}).Clone() }, func(c any) {
			c.(map[int]any)[1].([]any)[0] = "modified"
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := tt.clone()
			cloned := tt.clone()
			tt.mutate(cloned)
			if reflect.DeepEqual(cloned, want) {
				t.Fatal("the mutation changed nothing; the test is vacuous")
			}
			if again := tt.clone(); !reflect.DeepEqual(again, want) {
				t.Errorf("a change to one clone reached the next: %#v", again)
			}
		})
	}

	v := Wrap(map[int]any{1: []any{"x"}})
	v.Clone().(map[int]any)[1].([]any)[0] = "modified"
	if got := v.Clone().(map[int]any)[1].([]any)[0]; got != "x" {
		t.Errorf("a change to a clone reached the Value: %v", got)
	}
}

func TestConcurrent_Value_Read(t *testing.T) {
	v := Wrap(map[string]any{"name": "Alice", "age": int64(30), "items": []any{1, 2, 3}})

	yammmtest.RunConcurrent(100, 1000, func() {
		_ = v.Any()
		_ = v.String()
		_, _ = v.AsBool()
		_, _ = v.AsInt()
		_, _ = v.AsFloat()
		_, _ = v.AsString()
		_, _ = v.AsSlice()
		_ = v.Clone()
	})
}

func TestConcurrent_Slice_Read(t *testing.T) {
	s := WrapSlice([]any{1, 2, 3, map[string]any{"key": "value"}, []any{"a", "b"}})

	yammmtest.RunConcurrent(100, 1000, func() {
		_ = s.Len()
		_ = s.At(0)
		for v := range s.Values() {
			_ = v
		}
		_ = s.Clone()
	})
}

func TestConcurrent_Properties_Read(t *testing.T) {
	p := WrapProperties(map[string]any{
		"Name":    "Alice",
		"AGE":     int64(30),
		"email":   "alice@example.com",
		"Address": map[string]any{"city": "NYC"},
	})

	yammmtest.RunConcurrent(100, 1000, func() {
		_ = p.Len()
		_, _ = p.Get("Name")
		_, _ = p.GetFold("name")
		_, _ = p.GetFold("nonexistent")
		for k := range p.Keys() {
			_ = k
		}
		for k, v := range p.All() {
			_, _ = k, v
		}
		_ = p.Clone()
	})
}

func TestConcurrent_Key_Read(t *testing.T) {
	k := WrapKey([]any{"us", int64(12345), "suffix"})

	yammmtest.RunConcurrent(100, 1000, func() {
		_ = k.Len()
		_ = k.At(0)
		_ = k.String()
		for v := range k.Values() {
			_ = v
		}
		_ = k.Clone()
	})
}

// Every goroutine's iteration sees the whole, sorted name list.
func TestConcurrent_IteratorConsistency(t *testing.T) {
	p := WrapProperties(map[string]any{"c": 3, "a": 1, "b": 2})

	results := make(chan string, 100)
	yammmtest.RunConcurrent(100, 1, func() {
		results <- strings.Join(slices.Collect(p.Keys()), "")
	})
	close(results)

	for got := range results {
		if got != "abc" {
			t.Errorf("Keys() = %q, want abc", got)
		}
	}
}

// A range over any of the iterators allocates nothing once a Properties has
// computed its sorted names.
func TestIterators_RangeAllocatesNothing(t *testing.T) {
	p := WrapProperties(map[string]any{"b": int64(2), "a": int64(1)})
	s := WrapSlice([]any{"x", "y"})
	k := WrapKey([]any{"x", int64(1)})
	_ = slices.Collect(p.Keys())

	for name, f := range map[string]func(){
		"Properties.Keys": func() {
			for name := range p.Keys() {
				_ = name
			}
		},
		"Properties.All": func() {
			for name, v := range p.All() {
				_, _ = name, v
			}
		},
		"Slice.Values": func() {
			for v := range s.Values() {
				_ = v
			}
		},
		"Key.Values": func() {
			for v := range k.Values() {
				_ = v
			}
		},
	} {
		if n := testing.AllocsPerRun(100, f); n != 0 {
			t.Errorf("a range over %s allocates %v times, want 0", name, n)
		}
	}
}
