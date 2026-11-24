package path

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestRoot(t *testing.T) {
	if got, want := Root.String(), "$"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// TestPath_KeepsItsTextAsWritten holds the verbatim rule: a path's String is
// its text, including text the methods would spell another way and the empty
// text, and a method appends to that text as it is.
func TestPath_KeepsItsTextAsWritten(t *testing.T) {
	for _, text := range []string{"", "$", "$.a", `$["a"]`, "$[ 0 ]", "not a path", "$\xff"} {
		if got, want := Path(text).String(), text; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	}
	if got, want := Path(`$["a"]`).Key("b").String(), `$["a"].b`; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := Path("").Key("b").String(), ".b"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPath_Index(t *testing.T) {
	tests := []struct {
		name     string
		build    func() Path
		expected string
	}{
		{
			name:     "single index",
			build:    func() Path { return Root.Index(0) },
			expected: "$[0]",
		},
		{
			name:     "nested index",
			build:    func() Path { return Root.Index(0).Index(1) },
			expected: "$[0][1]",
		},
		{
			name:     "large index",
			build:    func() Path { return Root.Index(1000000) },
			expected: "$[1000000]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			if got, want := b.String(), tt.expected; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestPath_Key(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		expected string
	}{
		{
			name:     "simple identifier",
			key:      "name",
			expected: "$.name",
		},
		{
			name:     "underscore prefix",
			key:      "_private",
			expected: "$._private",
		},
		{
			name:     "with digits",
			key:      "item1",
			expected: "$.item1",
		},
		{
			name:     "camelCase",
			key:      "firstName",
			expected: "$.firstName",
		},
		{
			name:     "with spaces",
			key:      "with spaces",
			expected: `$["with spaces"]`,
		},
		{
			name:     "starts with digit",
			key:      "1st",
			expected: `$["1st"]`,
		},
		{
			name:     "empty key",
			key:      "",
			expected: `$[""]`,
		},
		{
			name:     "special characters",
			key:      "a.b.c",
			expected: `$["a.b.c"]`,
		},
		{
			name:     "with quotes",
			key:      `say "hello"`,
			expected: `$["say \"hello\""]`,
		},
		{
			name:     "with backslash",
			key:      `path\to\file`,
			expected: `$["path\\to\\file"]`,
		},
		{
			name:     "with newline",
			key:      "line1\nline2",
			expected: `$["line1\nline2"]`,
		},
		{
			name:     "with tab",
			key:      "col1\tcol2",
			expected: `$["col1\tcol2"]`,
		},
		{
			name:     "unicode",
			key:      "日本語",
			expected: `$["日本語"]`,
		},
		{
			name:     "hyphen",
			key:      "my-key",
			expected: `$["my-key"]`,
		},
		{
			name:     "invalid UTF-8 is written as U+FFFD, as the grammar reads it",
			key:      "a\xffb",
			expected: "$[\"a\uFFFDb\"]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Root.Key(tt.key)
			if got, want := b.String(), tt.expected; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestPath_PK(t *testing.T) {
	tests := []struct {
		name     string
		fields   []PKField
		expected string
	}{
		{
			name:     "integer PK",
			fields:   []PKField{{Name: "id", Value: 42}},
			expected: "$.Person[id=42]",
		},
		{
			name:     "string PK",
			fields:   []PKField{{Name: "name", Value: "Alice"}},
			expected: `$.Person[name="Alice"]`,
		},
		{
			name:     "boolean PK true",
			fields:   []PKField{{Name: "active", Value: true}},
			expected: "$.Person[active=true]",
		},
		{
			name:     "boolean PK false",
			fields:   []PKField{{Name: "active", Value: false}},
			expected: "$.Person[active=false]",
		},
		{
			name: "composite PK",
			fields: []PKField{
				{Name: "region", Value: "us"},
				{Name: "studentId", Value: 12345},
			},
			expected: `$.Person[region="us",studentId=12345]`,
		},
		{
			name:     "int8 PK",
			fields:   []PKField{{Name: "id", Value: int8(127)}},
			expected: "$.Person[id=127]",
		},
		{
			name:     "int64 PK",
			fields:   []PKField{{Name: "id", Value: int64(9223372036854775807)}},
			expected: "$.Person[id=9223372036854775807]",
		},
		{
			name:     "uint PK",
			fields:   []PKField{{Name: "id", Value: uint(100)}},
			expected: "$.Person[id=100]",
		},
		{
			name: "every other sized integer is written bare",
			fields: []PKField{
				{Name: "a", Value: int16(-5)},
				{Name: "b", Value: int32(6)},
				{Name: "c", Value: uint8(7)},
				{Name: "d", Value: uint16(8)},
				{Name: "e", Value: uint32(9)},
				{Name: "f", Value: uint64(10)},
			},
			expected: "$.Person[a=-5,b=6,c=7,d=8,e=9,f=10]",
		},
		{
			name:     "a uintptr is quoted",
			fields:   []PKField{{Name: "id", Value: uintptr(100)}},
			expected: `$.Person[id="100"]`,
		},
		{
			name:     "a defined integer type is quoted",
			fields:   []PKField{{Name: "id", Value: time.Duration(100)}},
			expected: `$.Person[id="100ns"]`,
		},
		{
			name:     "float64 PK",
			fields:   []PKField{{Name: "score", Value: 3.14159}},
			expected: "$.Person[score=3.14159]",
		},
		{
			name:     "float64 PK integer value",
			fields:   []PKField{{Name: "score", Value: float64(1.0)}},
			expected: "$.Person[score=1.0]",
		},
		{
			name:     "float64 PK zero",
			fields:   []PKField{{Name: "score", Value: float64(0.0)}},
			expected: "$.Person[score=0.0]",
		},
		{
			name:     "float32 PK integer value",
			fields:   []PKField{{Name: "score", Value: float32(2.0)}},
			expected: "$.Person[score=2.0]",
		},
		{
			name:     "string with quotes",
			fields:   []PKField{{Name: "name", Value: `O'Brien`}},
			expected: `$.Person[name="O'Brien"]`,
		},
		{
			name:     "string with escapes",
			fields:   []PKField{{Name: "path", Value: "a\\b"}},
			expected: `$.Person[path="a\\b"]`,
		},
		{
			name:     "empty fields",
			fields:   []PKField{},
			expected: "$.Person",
		},
		{
			name:     "a value of another type is quoted and escaped",
			fields:   []PKField{{Name: "tags", Value: []string{`a"b\c`}}},
			expected: `$.Person[tags="[a\"b\\c]"]`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Root.Key("Person").PK(tt.fields...)
			if got, want := b.String(), tt.expected; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

// Each names the refusal in a Path method's panic message.
const (
	refusesNegativeIndex = "Index: negative index"
	refusesPKName        = "is not an identifier"
	refusesPKFloat       = "which the grammar does not spell"
)

// wantPanic runs call and asserts it panicked with a string message naming what.
func wantPanic(t *testing.T, what string, call func()) {
	t.Helper()
	defer func() {
		r := recover()
		if r == nil {
			t.Errorf("no panic; want a refusal naming %q", what)
			return
		}
		msg, ok := r.(string)
		if !ok {
			t.Errorf("panic value is %T; want a string", r)
			return
		}
		if !strings.Contains(msg, what) {
			t.Errorf("panic message %q does not name %q", msg, what)
		}
	}()
	call()
}

// TestPath_RefusesWhatTheGrammarCannotSpell holds the methods to the grammar
// their own output must satisfy: every input below would write a path the
// grammar does not spell, so each method panics instead.
func TestPath_RefusesWhatTheGrammarCannotSpell(t *testing.T) {
	tests := []struct {
		name    string
		build   func() Path
		refusal string
	}{
		{"negative index", func() Path { return Root.Index(-1) }, refusesNegativeIndex},
		{"PK name with a space", func() Path {
			return Root.PK(PKField{Name: "my field", Value: 1})
		}, refusesPKName},
		{"PK name starting with a digit", func() Path {
			return Root.PK(PKField{Name: "1st", Value: 1})
		}, refusesPKName},
		{"empty PK name", func() Path {
			return Root.PK(PKField{Name: "", Value: 1})
		}, refusesPKName},
		{"a later field of a composite PK with a name that is not an identifier", func() Path {
			return Root.PK(PKField{Name: "region", Value: "us"}, PKField{Name: "my field", Value: 1})
		}, refusesPKName},
		{"NaN float64 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: math.NaN()})
		}, refusesPKFloat},
		{"+Inf float64 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: math.Inf(1)})
		}, refusesPKFloat},
		{"-Inf float64 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: math.Inf(-1)})
		}, refusesPKFloat},
		{"NaN float32 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: float32(math.NaN())})
		}, refusesPKFloat},
		{"+Inf float32 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: float32(math.Inf(1))})
		}, refusesPKFloat},
		{"-Inf float32 PK value", func() Path {
			return Root.PK(PKField{Name: "score", Value: float32(math.Inf(-1))})
		}, refusesPKFloat},
		{"a later field of a composite PK holding NaN", func() Path {
			return Root.PK(PKField{Name: "id", Value: 1}, PKField{Name: "score", Value: math.NaN()})
		}, refusesPKFloat},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wantPanic(t, tt.refusal, func() {
				s := tt.build().String()
				if _, err := parse(s); err != nil {
					t.Errorf("no panic, and the path it wrote does not parse: %q: %v", s, err)
				}
			})
		})
	}
}

func TestPath_Chaining(t *testing.T) {
	tests := []struct {
		name     string
		build    func() Path
		expected string
	}{
		{
			name: "key then index",
			build: func() Path {
				return Root.Key("items").Index(0)
			},
			expected: "$.items[0]",
		},
		{
			name: "index then key",
			build: func() Path {
				return Root.Index(0).Key("name")
			},
			expected: "$[0].name",
		},
		{
			name: "key then PK",
			build: func() Path {
				return Root.Key("Person").PK(PKField{Name: "id", Value: 1})
			},
			expected: "$.Person[id=1]",
		},
		{
			name: "complex path",
			build: func() Path {
				return Root.Key("data").Key("users").Index(0).Key("profile").Key("address")
			},
			expected: "$.data.users[0].profile.address",
		},
		{
			name: "nested with PK",
			build: func() Path {
				return Root.Key("Company").PK(PKField{Name: "id", Value: "acme"}).Key("employees").Index(0).Key("name")
			},
			expected: `$.Company[id="acme"].employees[0].name`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			if got, want := b.String(), tt.expected; got != want {
				t.Errorf("got %v, want %v", got, want)
			}
		})
	}
}

func TestPath_Immutability(t *testing.T) {
	// Verify that each method returns a new path without modifying the original
	root := Root
	child1 := root.Key("a")
	child2 := root.Key("b")

	if got, want := root.String(), "$"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := child1.String(), "$.a"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := child2.String(), "$.b"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}

	// Further verify with Index
	grandchild := child1.Index(0)
	if got, want := child1.String(), "$.a"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
	if got, want := grandchild.String(), "$.a[0]"; got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestPath_EdgeCases(t *testing.T) {
	t.Run("control characters", func(t *testing.T) {
		// Test control character escaping
		b := Root.Key("\x00\x01\x1f")
		s := b.String()
		if !strings.Contains(s, `\u0000`) {
			t.Errorf("%q does not contain %q", s, `\u0000`)
		}
		if !strings.Contains(s, `\u0001`) {
			t.Errorf("%q does not contain %q", s, `\u0001`)
		}
		if !strings.Contains(s, `\u001f`) {
			t.Errorf("%q does not contain %q", s, `\u001f`)
		}
	})

	t.Run("a lone control character", func(t *testing.T) {
		if got, want := Root.Key("\x1f").String(), `$["\u001f"]`; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("carriage return", func(t *testing.T) {
		b := Root.Key("a\rb")
		if got, want := b.String(), `$["a\rb"]`; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("backspace escape", func(t *testing.T) {
		b := Root.Key("a\bb")
		if got, want := b.String(), `$["a\bb"]`; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("form feed escape", func(t *testing.T) {
		b := Root.Key("a\fb")
		if got, want := b.String(), `$["a\fb"]`; got != want {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("very long key", func(t *testing.T) {
		longKey := make([]byte, 10000)
		for i := range longKey {
			longKey[i] = 'a'
		}
		b := Root.Key(string(longKey))
		if got := len(b.String()); got != 10002 {
			t.Errorf("len = %d, want %d", got, 10002)
		}
	})
}
