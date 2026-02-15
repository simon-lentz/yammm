package normalize_test

import (
	"errors"
	"testing"

	"github.com/simon-lentz/yammm/internal/normalize"
)

// textMarshalerSample implements encoding.TextMarshaler for testing.
type textMarshalerSample string

func (t textMarshalerSample) MarshalText() ([]byte, error) {
	return []byte(string(t)), nil
}

// stringerSample implements fmt.Stringer for testing.
type stringerSample struct {
	value string
}

func (s stringerSample) String() string { return s.value }

// bothMarshalerAndStringer implements both interfaces with different outputs
// to verify TextMarshaler takes precedence over Stringer.
type bothMarshalerAndStringer struct{}

func (b bothMarshalerAndStringer) MarshalText() ([]byte, error) {
	return []byte("from-marshaler"), nil
}

func (b bothMarshalerAndStringer) String() string {
	return "from-stringer"
}

// errorMarshalerWithStringer returns error from MarshalText but has Stringer
// to verify fallback behavior when MarshalText fails.
type errorMarshalerWithStringer struct{}

func (e errorMarshalerWithStringer) MarshalText() ([]byte, error) {
	return nil, errors.New("intentional error")
}

func (e errorMarshalerWithStringer) String() string {
	return "fallback-stringer"
}

func TestNormalize_Nil(t *testing.T) {
	result := normalize.Normalize(nil)
	if result != nil {
		t.Errorf("Normalize(nil) = %v; want nil", result)
	}
}

func TestNormalize_Primitives(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"int", 42, 42},
		{"string", "hello", "hello"},
		{"bool", true, true},
		{"float64", 3.14, 3.14},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalize.Normalize(tt.input)
			if got != tt.want {
				t.Errorf("Normalize(%v) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestNormalize_BasicStruct(t *testing.T) {
	type sample struct {
		A int
		B string
	}
	data := sample{A: 5, B: "x"}

	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if obj["a"] != 5 {
		t.Errorf("a = %v; want 5", obj["a"])
	}
	if obj["b"] != "x" {
		t.Errorf("b = %v; want x", obj["b"])
	}
}

func TestNormalize_NestedStruct(t *testing.T) {
	type inner struct {
		B string
	}
	type sample struct {
		A  int
		In inner
	}
	data := sample{A: 5, In: inner{B: "x"}}

	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if obj["a"] != 5 {
		t.Errorf("a = %v; want 5", obj["a"])
	}

	in, ok := obj["in"].(map[string]any)
	if !ok {
		t.Fatalf("in: expected map[string]any, got %T", obj["in"])
	}
	if in["b"] != "x" {
		t.Errorf("in.b = %v; want x", in["b"])
	}
}

func TestNormalize_PointerStruct(t *testing.T) {
	type inner struct {
		B string
	}
	type sample struct {
		A   int
		Ptr *inner
	}

	t.Run("non-nil pointer", func(t *testing.T) {
		data := sample{A: 5, Ptr: &inner{B: "y"}}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}

		ptr, ok := obj["ptr"].(map[string]any)
		if !ok {
			t.Fatalf("ptr: expected map[string]any, got %T", obj["ptr"])
		}
		if ptr["b"] != "y" {
			t.Errorf("ptr.b = %v; want y", ptr["b"])
		}
	})

	t.Run("nil pointer", func(t *testing.T) {
		data := sample{A: 5, Ptr: nil}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}

		if obj["ptr"] != nil {
			t.Errorf("ptr = %v; want nil", obj["ptr"])
		}
	})
}

func TestNormalize_PointerToStruct(t *testing.T) {
	type sample struct {
		A int
	}
	data := &sample{A: 42}

	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if obj["a"] != 42 {
		t.Errorf("a = %v; want 42", obj["a"])
	}
}

func TestNormalize_NilPointer(t *testing.T) {
	var data *struct{ A int }
	out := normalize.Normalize(data)
	if out != nil {
		t.Errorf("Normalize(nil pointer) = %v; want nil", out)
	}
}

func TestNormalize_TypedStringMap(t *testing.T) {
	t.Run("map[string]string", func(t *testing.T) {
		input := map[string]string{"alpha": "beta", "gamma": "delta"}
		out := normalize.Normalize(input)
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if m["alpha"] != "beta" {
			t.Errorf("alpha = %v; want beta", m["alpha"])
		}
		if m["gamma"] != "delta" {
			t.Errorf("gamma = %v; want delta", m["gamma"])
		}
	})

	t.Run("map[string]struct", func(t *testing.T) {
		type payload struct {
			Value string
		}
		input := map[string]payload{"item": {Value: "ok"}}
		out := normalize.Normalize(input)
		m, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}

		item, ok := m["item"].(map[string]any)
		if !ok {
			t.Fatalf("item: expected map[string]any, got %T", m["item"])
		}
		if item["value"] != "ok" {
			t.Errorf("item.value = %v; want ok", item["value"])
		}
	})
}

func TestNormalize_NonStringKeyMap(t *testing.T) {
	input := map[int]string{1: "one", 2: "two"}
	out := normalize.Normalize(input)

	// Non-string key maps are returned unchanged
	m, ok := out.(map[int]string)
	if !ok {
		t.Fatalf("expected map[int]string, got %T", out)
	}
	if m[1] != "one" {
		t.Errorf("m[1] = %v; want one", m[1])
	}
}

func TestNormalize_Slice(t *testing.T) {
	t.Run("[]any", func(t *testing.T) {
		input := []any{1, "two", true}
		out := normalize.Normalize(input)
		s, ok := out.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", out)
		}
		if len(s) != 3 {
			t.Errorf("len = %d; want 3", len(s))
		}
		if s[0] != 1 || s[1] != "two" || s[2] != true {
			t.Errorf("slice contents don't match: %v", s)
		}
	})

	t.Run("[]struct", func(t *testing.T) {
		type item struct {
			Name string
		}
		input := []item{{Name: "a"}, {Name: "b"}}
		out := normalize.Normalize(input)
		s, ok := out.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", out)
		}
		if len(s) != 2 {
			t.Errorf("len = %d; want 2", len(s))
		}

		first, ok := s[0].(map[string]any)
		if !ok {
			t.Fatalf("s[0]: expected map[string]any, got %T", s[0])
		}
		if first["name"] != "a" {
			t.Errorf("s[0].name = %v; want a", first["name"])
		}
	})

	t.Run("nested []any", func(t *testing.T) {
		input := []any{
			map[string]any{"x": 1},
			[]any{"nested"},
		}
		out := normalize.Normalize(input)
		s, ok := out.([]any)
		if !ok {
			t.Fatalf("expected []any, got %T", out)
		}

		nested, ok := s[1].([]any)
		if !ok {
			t.Fatalf("s[1]: expected []any, got %T", s[1])
		}
		if nested[0] != "nested" {
			t.Errorf("s[1][0] = %v; want nested", nested[0])
		}
	})
}

func TestNormalize_TextMarshaler(t *testing.T) {
	out := normalize.Normalize(textMarshalerSample("marshaled"))
	if out != "marshaled" {
		t.Errorf("Normalize(textMarshaler) = %v; want marshaled", out)
	}
}

func TestNormalize_Stringer(t *testing.T) {
	out := normalize.Normalize(stringerSample{value: "stringified"})
	if out != "stringified" {
		t.Errorf("Normalize(stringer) = %v; want stringified", out)
	}
}

func TestNormalize_TextMarshalerPrecedence(t *testing.T) {
	// When a type implements both TextMarshaler and Stringer,
	// TextMarshaler should take precedence per doc.go.
	out := normalize.Normalize(bothMarshalerAndStringer{})
	if out != "from-marshaler" {
		t.Errorf("TextMarshaler should take precedence; got %q, want %q", out, "from-marshaler")
	}
}

func TestNormalize_MarshalTextErrorFallback(t *testing.T) {
	// When MarshalText returns an error, should fall back to Stringer.
	out := normalize.Normalize(errorMarshalerWithStringer{})
	if out != "fallback-stringer" {
		t.Errorf("Should fallback to Stringer on MarshalText error; got %q, want %q", out, "fallback-stringer")
	}
}

func TestNormalize_YammmTags(t *testing.T) {
	t.Run("renamed field", func(t *testing.T) {
		type sample struct {
			Label string `yammm:"label"`
		}
		data := sample{Label: "ok"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["label"] != "ok" {
			t.Errorf("label = %v; want ok", obj["label"])
		}
		if _, exists := obj["Label"]; exists {
			t.Error("field should be renamed, not original casing")
		}
	})

	t.Run("skipped field", func(t *testing.T) {
		type sample struct {
			Keep string
			Skip string `yammm:"-"`
		}
		data := sample{Keep: "visible", Skip: "hidden"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["keep"] != "visible" {
			t.Errorf("keep = %v; want visible", obj["keep"])
		}
		if _, exists := obj["skip"]; exists {
			t.Error("skip field should be omitted")
		}
	})

	t.Run("blank tag uses default name", func(t *testing.T) {
		type sample struct {
			WithOpts string `yammm:",omitempty"`
		}
		data := sample{WithOpts: "ok"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["withOpts"] != "ok" {
			t.Errorf("withOpts = %v; want ok", obj["withOpts"])
		}
	})
}

func TestNormalize_UnexportedFields(t *testing.T) {
	type sample struct {
		Public  string
		private string //nolint:unused // intentionally unexported to test field filtering
	}
	data := sample{Public: "visible"}
	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if obj["public"] != "visible" {
		t.Errorf("public = %v; want visible", obj["public"])
	}
	if _, exists := obj["private"]; exists {
		t.Error("private field should be omitted")
	}
}

func TestNormalize_EmbeddedStruct(t *testing.T) {
	t.Run("anonymous embedded flattens", func(t *testing.T) {
		type Embedded struct {
			Field string
		}
		type Outer struct {
			Embedded
			Other string
		}
		data := Outer{Embedded: Embedded{Field: "inner"}, Other: "outer"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["field"] != "inner" {
			t.Errorf("field = %v; want inner", obj["field"])
		}
		if obj["other"] != "outer" {
			t.Errorf("other = %v; want outer", obj["other"])
		}
	})

	t.Run("named embedded stays nested", func(t *testing.T) {
		type Inner struct {
			Value string
		}
		type Outer struct {
			Inner `yammm:"inner"`
		}
		data := Outer{Inner: Inner{Value: "nested"}}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}

		inner, ok := obj["inner"].(map[string]any)
		if !ok {
			t.Fatalf("inner: expected map[string]any, got %T", obj["inner"])
		}
		if inner["value"] != "nested" {
			t.Errorf("inner.value = %v; want nested", inner["value"])
		}
	})

	t.Run("field dominance", func(t *testing.T) {
		type Embedded struct {
			Field string
		}
		type Outer struct {
			Field string // shadows embedded.Field
			Embedded
		}
		data := Outer{Field: "outer", Embedded: Embedded{Field: "embedded"}}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["field"] != "outer" {
			t.Errorf("field = %v; want outer", obj["field"])
		}
	})
}

func TestNormalize_NilMap(t *testing.T) {
	var m map[string]any
	out := normalize.Normalize(m)
	if out != nil {
		t.Errorf("Normalize(nil map) = %v; want nil", out)
	}
}

func TestNormalize_EmptyMap(t *testing.T) {
	m := map[string]any{}
	out := normalize.Normalize(m)
	result, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if len(result) != 0 {
		t.Errorf("len = %d; want 0", len(result))
	}
}

func TestNormalize_EmptySlice(t *testing.T) {
	s := []any{}
	out := normalize.Normalize(s)
	result, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(result) != 0 {
		t.Errorf("len = %d; want 0", len(result))
	}
}

func TestNormalize_MapStringAny_PassthroughWithNormalization(t *testing.T) {
	type nested struct {
		Value int
	}
	input := map[string]any{
		"simple": 1,
		"nested": nested{Value: 42},
	}
	out := normalize.Normalize(input)
	m, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if m["simple"] != 1 {
		t.Errorf("simple = %v; want 1", m["simple"])
	}

	// Nested struct should be normalized
	nestedOut, ok := m["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested: expected map[string]any, got %T", m["nested"])
	}
	if nestedOut["value"] != 42 {
		t.Errorf("nested.value = %v; want 42", nestedOut["value"])
	}
}

func TestNormalize_PointerEmbeddedChains(t *testing.T) {
	type Embedded struct {
		Name string
	}
	type Middle struct {
		*Embedded
	}
	type Outer struct {
		*Middle
		Label string
	}

	t.Run("nil embedded chain", func(t *testing.T) {
		data := Outer{Label: "data"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["label"] != "data" {
			t.Errorf("label = %v; want data", obj["label"])
		}
		// name should not be present since embedded is nil
		if val, exists := obj["name"]; exists && val != nil {
			t.Errorf("name should be nil or not present, got %v", val)
		}
	})

	t.Run("populated chain", func(t *testing.T) {
		data := Outer{Middle: &Middle{Embedded: &Embedded{Name: "ok"}}, Label: "data"}
		out := normalize.Normalize(data)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["name"] != "ok" {
			t.Errorf("name = %v; want ok", obj["name"])
		}
	})
}

func TestNormalize_NilSlice(t *testing.T) {
	var s []any
	out := normalize.Normalize(s)
	if out != nil {
		t.Errorf("Normalize(nil slice) = %v; want nil", out)
	}
}

func TestNormalize_TypedSlice(t *testing.T) {
	input := []int{1, 2, 3}
	out := normalize.Normalize(input)
	s, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(s) != 3 {
		t.Errorf("len = %d; want 3", len(s))
	}
	if s[0] != 1 || s[1] != 2 || s[2] != 3 {
		t.Errorf("slice = %v; want [1 2 3]", s)
	}
}

func TestNormalize_Array(t *testing.T) {
	input := [3]string{"a", "b", "c"}
	out := normalize.Normalize(input)
	s, ok := out.([]any)
	if !ok {
		t.Fatalf("expected []any, got %T", out)
	}
	if len(s) != 3 {
		t.Errorf("len = %d; want 3", len(s))
	}
	if s[0] != "a" || s[1] != "b" || s[2] != "c" {
		t.Errorf("array = %v; want [a b c]", s)
	}
}

// pointerStringer implements Stringer only on the pointer receiver.
type pointerStringer struct {
	value string
}

func (a *pointerStringer) String() string { return a.value }

func TestNormalize_PointerStringer(t *testing.T) {
	// Pass by pointer - the pointer receiver is found directly
	data := &pointerStringer{value: "stringified"}
	out := normalize.Normalize(data)
	if out != "stringified" {
		t.Errorf("Normalize(pointer stringer) = %v; want stringified", out)
	}
}

// pointerTextMarshaler implements TextMarshaler only on the pointer receiver.
type pointerTextMarshaler struct {
	value string
}

func (a *pointerTextMarshaler) MarshalText() ([]byte, error) {
	return []byte(a.value), nil
}

func TestNormalize_PointerTextMarshaler(t *testing.T) {
	// Pass by pointer - the pointer receiver is found directly
	data := &pointerTextMarshaler{value: "marshaled-ptr"}
	out := normalize.Normalize(data)
	if out != "marshaled-ptr" {
		t.Errorf("Normalize(pointer text marshaler) = %v; want marshaled-ptr", out)
	}
}

func TestNormalize_FieldConflict_Ambiguous(t *testing.T) {
	// Two embedded structs at same depth with same field name = ambiguous, dropped
	type Embed1 struct {
		Field string
	}
	type Embed2 struct {
		Field string
	}
	type Outer struct {
		Embed1
		Embed2
	}
	data := Outer{
		Embed1: Embed1{Field: "one"},
		Embed2: Embed2{Field: "two"},
	}
	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	// field should be dropped due to ambiguity
	if _, exists := obj["field"]; exists {
		t.Error("ambiguous field should not be present")
	}
}

func TestNormalize_FieldConflict_TaggedWithOptions(t *testing.T) {
	// Tagged field with options (but no name override) should dominate untagged embedded field
	// This tests that `yammm:",omitempty"` is treated as tagged even though name is empty
	type Embedded struct {
		Field string
	}
	type Outer struct {
		Field string `yammm:",omitempty"` // tagged with options, no name override
		Embedded
	}
	data := Outer{
		Field:    "outer-wins",
		Embedded: Embedded{Field: "embedded-loses"},
	}
	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	// The tagged field (even with empty name) should dominate the untagged embedded field
	if obj["field"] != "outer-wins" {
		t.Errorf("field = %v; want outer-wins (tagged field should dominate)", obj["field"])
	}
}

func TestNormalize_NilTypedStringMap(t *testing.T) {
	var m map[string]int
	out := normalize.Normalize(m)
	if out != nil {
		t.Errorf("Normalize(nil typed map) = %v; want nil", out)
	}
}

func TestNormalize_NilTypedSlice(t *testing.T) {
	var s []int
	out := normalize.Normalize(s)
	if out != nil {
		t.Errorf("Normalize(nil typed slice) = %v; want nil", out)
	}
}

func TestNormalize_EmptyString(t *testing.T) {
	out := normalize.Normalize("")
	if out != "" {
		t.Errorf("Normalize(\"\") = %v; want empty string", out)
	}
}

func TestNormalize_InvalidReflectValue(t *testing.T) {
	// Test with a channel - should be returned as-is
	ch := make(chan int)
	out := normalize.Normalize(ch)
	if out != ch {
		t.Errorf("Normalize(chan) should return the channel unchanged")
	}
}

func TestNormalize_EmptyStruct(t *testing.T) {
	type Empty struct{}
	data := Empty{}
	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if len(obj) != 0 {
		t.Errorf("len = %d; want 0", len(obj))
	}
}

func TestNormalize_PointerToNonStruct(t *testing.T) {
	// Test pointer to primitive - pointers to primitives pass through unchanged
	// (Normalize only transforms maps, slices, structs, and marshalers)
	val := 42
	ptr := &val
	out := normalize.Normalize(ptr)
	// Pointer to int is returned unchanged (reflection fallback returns original v)
	if out != ptr {
		t.Errorf("Normalize(*int) should return the pointer unchanged")
	}
}

func TestNormalize_PointerToNil(t *testing.T) {
	var ptr *int
	out := normalize.Normalize(ptr)
	if out != nil {
		t.Errorf("Normalize(nil *int) = %v; want nil", out)
	}
}

func TestNormalize_SingleCharFieldName(t *testing.T) {
	type sample struct {
		A string
	}
	data := sample{A: "val"}
	out := normalize.Normalize(data)
	obj, ok := out.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", out)
	}
	if obj["a"] != "val" {
		t.Errorf("a = %v; want val", obj["a"])
	}
}

// TestNormalize_TriplePointer tests deeply nested pointer chains (***struct).
// The Normalize function handles arbitrary pointer depth via a loop that
// dereferences pointers until reaching a non-pointer value.
func TestNormalize_TriplePointer(t *testing.T) {
	type Sample struct {
		Value string
	}

	t.Run("triple pointer non-nil", func(t *testing.T) {
		inner := &Sample{Value: "deep"}
		middle := &inner
		outer := &middle // ***Sample

		out := normalize.Normalize(outer)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["value"] != "deep" {
			t.Errorf("value = %v; want deep", obj["value"])
		}
	})

	t.Run("triple pointer nil at middle", func(t *testing.T) {
		var inner *Sample
		middle := &inner
		outer := &middle // ***Sample where **Sample is nil

		out := normalize.Normalize(outer)
		if out != nil {
			t.Errorf("expected nil for nil pointer chain, got %v", out)
		}
	})

	t.Run("quadruple pointer", func(t *testing.T) {
		inner := &Sample{Value: "very deep"}
		p2 := &inner
		p3 := &p2
		outer := &p3 // ****Sample

		out := normalize.Normalize(outer)
		obj, ok := out.(map[string]any)
		if !ok {
			t.Fatalf("expected map[string]any, got %T", out)
		}
		if obj["value"] != "very deep" {
			t.Errorf("value = %v; want very deep", obj["value"])
		}
	})
}
