package graph_test

import (
	"math"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/graph"
)

func TestParseKey_RoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		values []any
	}{
		{"single string", []any{"ABC123"}},
		{"string and int", []any{"us", int64(12345)}},
		{"empty key", []any{}},
		{"quotes and brackets", []any{`he said "hi"`, "[bracketed]"}},
		{"unicode", []any{"café — 日本語 🙂"}},
		{"beyond 2^53", []any{int64(9007199254740993)}},
		{"int64 bounds", []any{int64(math.MaxInt64), int64(math.MinInt64)}},
		{"negative int", []any{int64(-42)}},
		{"fractional float", []any{3.25, -0.5}},
		{"bools", []any{true, false}},
		{"nil component", []any{nil}},
		{"mixed multi-component", []any{"region", int64(100), true, nil, 1.5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := graph.FormatKey(tt.values...)
			got, err := graph.ParseKey(s)
			if err != nil {
				t.Fatalf("graph.ParseKey(%q) error: %v", s, err)
			}
			if len(got) != len(tt.values) {
				t.Fatalf("graph.ParseKey(%q) = %#v, want %#v", s, got, tt.values)
			}
			for i := range got {
				if got[i] != tt.values[i] {
					t.Errorf("component %d = %#v (%T), want %#v (%T)",
						i, got[i], got[i], tt.values[i], tt.values[i])
				}
			}
			if back := graph.FormatKey(got...); back != s {
				t.Errorf("graph.FormatKey(graph.ParseKey(%q)...) = %q, want %q", s, back, s)
			}
		})
	}
}

// A component's Go type comes from the literal's lexical form, never from a
// schema — the rule the .ys reader applies, so a parsed key and a loaded
// snapshot agree on types.
func TestParseKey_ClassifiesNumbersByLexicalForm(t *testing.T) {
	t.Parallel()
	tests := []struct {
		key  string
		want any
	}{
		{`[5]`, int64(5)},
		{`[-5]`, int64(-5)},
		{`[5.0]`, float64(5)},
		{`[5.5]`, float64(5.5)},
		{`[5e2]`, float64(500)},
		{`[5E2]`, float64(500)},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			t.Parallel()
			got, err := graph.ParseKey(tt.key)
			if err != nil {
				t.Fatalf("graph.ParseKey(%q) error: %v", tt.key, err)
			}
			if len(got) != 1 || got[0] != tt.want {
				t.Fatalf("graph.ParseKey(%q) = %#v, want [%#v (%T)]", tt.key, got, tt.want, tt.want)
			}
		})
	}
}

// The one documented carve-out in the round-trip law: FormatKey renders a whole
// float as an int-shaped literal, which reads back as int64.
func TestParseKey_WholeFloatReturnsAsInt64(t *testing.T) {
	t.Parallel()
	s := graph.FormatKey(float64(5))
	if s != "[5]" {
		t.Fatalf("graph.FormatKey(float64(5)) = %q, want %q", s, "[5]")
	}
	got, err := graph.ParseKey(s)
	if err != nil {
		t.Fatalf("graph.ParseKey(%q) error: %v", s, err)
	}
	if _, ok := got[0].(int64); !ok {
		t.Errorf("component 0 is %T, want int64", got[0])
	}
}

// An int-shaped literal past the int64 range falls back to float64 with the
// precision loss that implies, rather than failing.
func TestParseKey_IntegerBeyondInt64ReturnsFloat(t *testing.T) {
	t.Parallel()
	got, err := graph.ParseKey(`[99999999999999999999]`)
	if err != nil {
		t.Fatalf("graph.ParseKey error: %v", err)
	}
	f, ok := got[0].(float64)
	if !ok {
		t.Fatalf("component 0 is %T, want float64", got[0])
	}
	if f != 1e20 {
		t.Errorf("component 0 = %v, want 1e20", f)
	}
}

func TestParseKey_Errors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		key     string
		wantSub string
	}{
		{"empty string", ``, "parsing key"},
		{"json null", `null`, "want a JSON array"},
		{"json object", `{"a":1}`, "parsing key"},
		{"json string", `"abc"`, "parsing key"},
		{"json number", `5`, "parsing key"},
		{"malformed", `["a"`, "parsing key"},
		{"trailing content", `["a"] junk`, "trailing content"},
		{"trailing array", `["a"]["b"]`, "trailing content"},
		{"nested array component", `[["a"]]`, "component 0 is not a scalar"},
		{"nested object component", `[{"a":1}]`, "component 0 is not a scalar"},
		{"nested at index 1", `["a",[1]]`, "component 1 is not a scalar"},
		{"unrepresentable number", `[1e999]`, "component 0 is not a representable number"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := graph.ParseKey(tt.key)
			if err == nil {
				t.Fatalf("graph.ParseKey(%q) = %#v, want an error", tt.key, got)
			}
			if !strings.Contains(err.Error(), tt.wantSub) {
				t.Errorf("graph.ParseKey(%q) error = %q, want it to mention %q", tt.key, err, tt.wantSub)
			}
		})
	}
}

// Whitespace around the array is ordinary JSON, so it is tolerated; content
// after it is not.
func TestParseKey_ToleratesSurroundingWhitespace(t *testing.T) {
	t.Parallel()
	got, err := graph.ParseKey("  [\"a\"] \n\t")
	if err != nil {
		t.Fatalf("graph.ParseKey error: %v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Errorf("graph.ParseKey = %#v, want [\"a\"]", got)
	}
}

// "[]" is a key with no components, which is distinct from a parse failure.
func TestParseKey_EmptyArrayIsEmptyNonNil(t *testing.T) {
	t.Parallel()
	got, err := graph.ParseKey(`[]`)
	if err != nil {
		t.Fatalf("graph.ParseKey error: %v", err)
	}
	if got == nil {
		t.Fatal("graph.ParseKey(`[]`) = nil, want an empty non-nil slice")
	}
	if len(got) != 0 {
		t.Errorf("graph.ParseKey(`[]`) = %#v, want empty", got)
	}
}

func TestParseKeyStrings(t *testing.T) {
	t.Parallel()
	got, err := graph.ParseKeyStrings(`["us","ca","tx"]`)
	if err != nil {
		t.Fatalf("graph.ParseKeyStrings error: %v", err)
	}
	want := []string{"us", "ca", "tx"}
	if len(got) != len(want) {
		t.Fatalf("graph.ParseKeyStrings = %#v, want %#v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Errorf("component %d = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestParseKeyStrings_RejectsEveryNonStringComponent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  string
	}{
		{"integer", `["a",1]`},
		{"float", `["a",1.5]`},
		{"bool", `["a",true]`},
		{"null", `["a",null]`},
		{"nested array", `["a",["b"]]`},
		{"nested object", `["a",{"b":1}]`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := graph.ParseKeyStrings(tt.key)
			if err == nil {
				t.Fatalf("graph.ParseKeyStrings(%q) = %#v, want an error", tt.key, got)
			}
			if !strings.Contains(err.Error(), "component 1") {
				t.Errorf("error = %q, want it to name component 1", err)
			}
		})
	}
}

func TestParseKeyStrings_EmptyArray(t *testing.T) {
	t.Parallel()
	got, err := graph.ParseKeyStrings(`[]`)
	if err != nil {
		t.Fatalf("graph.ParseKeyStrings error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("graph.ParseKeyStrings(`[]`) = %#v, want empty", got)
	}
}
