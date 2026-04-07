package path

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRoot(t *testing.T) {
	b := Root()
	assert.Equal(t, "$", b.String())
	assert.True(t, b.IsRoot())
	assert.Equal(t, 0, b.Len())
	assert.Equal(t, "", b.Last())
}

func TestBuilder_Index(t *testing.T) {
	tests := []struct {
		name     string
		build    func() Builder
		expected string
	}{
		{
			name:     "single index",
			build:    func() Builder { return Root().Index(0) },
			expected: "$[0]",
		},
		{
			name:     "nested index",
			build:    func() Builder { return Root().Index(0).Index(1) },
			expected: "$[0][1]",
		},
		{
			name:     "negative index",
			build:    func() Builder { return Root().Index(-1) },
			expected: "$[-1]",
		},
		{
			name:     "large index",
			build:    func() Builder { return Root().Index(1000000) },
			expected: "$[1000000]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			assert.Equal(t, tt.expected, b.String())
		})
	}
}

func TestBuilder_Key(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Root().Key(tt.key)
			assert.Equal(t, tt.expected, b.String())
		})
	}
}

func TestBuilder_PK(t *testing.T) {
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := Root().Key("Person").PK(tt.fields...)
			assert.Equal(t, tt.expected, b.String())
		})
	}
}

func TestBuilder_Chaining(t *testing.T) {
	tests := []struct {
		name     string
		build    func() Builder
		expected string
	}{
		{
			name: "key then index",
			build: func() Builder {
				return Root().Key("items").Index(0)
			},
			expected: "$.items[0]",
		},
		{
			name: "index then key",
			build: func() Builder {
				return Root().Index(0).Key("name")
			},
			expected: "$[0].name",
		},
		{
			name: "key then PK",
			build: func() Builder {
				return Root().Key("Person").PK(PKField{Name: "id", Value: 1})
			},
			expected: "$.Person[id=1]",
		},
		{
			name: "complex path",
			build: func() Builder {
				return Root().Key("data").Key("users").Index(0).Key("profile").Key("address")
			},
			expected: "$.data.users[0].profile.address",
		},
		{
			name: "nested with PK",
			build: func() Builder {
				return Root().Key("Company").PK(PKField{Name: "id", Value: "acme"}).Key("employees").Index(0).Key("name")
			},
			expected: `$.Company[id="acme"].employees[0].name`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := tt.build()
			assert.Equal(t, tt.expected, b.String())
		})
	}
}

func TestBuilder_Immutability(t *testing.T) {
	// Verify that each method returns a new builder without modifying the original
	root := Root()
	child1 := root.Key("a")
	child2 := root.Key("b")

	assert.Equal(t, "$", root.String())
	assert.Equal(t, "$.a", child1.String())
	assert.Equal(t, "$.b", child2.String())

	// Further verify with Index
	grandchild := child1.Index(0)
	assert.Equal(t, "$.a", child1.String())
	assert.Equal(t, "$.a[0]", grandchild.String())
}

func TestBuilder_Navigation(t *testing.T) {
	t.Run("Parent", func(t *testing.T) {
		b := Root().Key("a").Key("b").Index(0)
		assert.Equal(t, "$.a.b[0]", b.String())

		parent := b.Parent()
		assert.Equal(t, "$.a.b", parent.String())

		grandparent := parent.Parent()
		assert.Equal(t, "$.a", grandparent.String())

		root := grandparent.Parent()
		assert.Equal(t, "$", root.String())

		// Parent of root is root
		rootParent := root.Parent()
		assert.Equal(t, "$", rootParent.String())
	})

	t.Run("Len", func(t *testing.T) {
		assert.Equal(t, 0, Root().Len())
		assert.Equal(t, 1, Root().Key("a").Len())
		assert.Equal(t, 2, Root().Key("a").Index(0).Len())
		assert.Equal(t, 3, Root().Key("a").Index(0).Key("b").Len())
	})

	t.Run("Last", func(t *testing.T) {
		assert.Equal(t, "", Root().Last())
		assert.Equal(t, ".a", Root().Key("a").Last())
		assert.Equal(t, "[0]", Root().Key("a").Index(0).Last())
		assert.Equal(t, "[id=42]", Root().Key("Person").PK(PKField{Name: "id", Value: 42}).Last())
	})

	t.Run("IsRoot", func(t *testing.T) {
		assert.True(t, Root().IsRoot())
		assert.False(t, Root().Key("a").IsRoot())
		assert.False(t, Root().Index(0).IsRoot())
	})
}

func TestBuilder_EdgeCases(t *testing.T) {
	t.Run("control characters", func(t *testing.T) {
		// Test control character escaping
		b := Root().Key("\x00\x01\x1f")
		s := b.String()
		assert.Contains(t, s, `\u0000`)
		assert.Contains(t, s, `\u0001`)
		assert.Contains(t, s, `\u001f`)
	})

	t.Run("carriage return", func(t *testing.T) {
		b := Root().Key("a\rb")
		assert.Equal(t, `$["a\rb"]`, b.String())
	})

	t.Run("backspace escape", func(t *testing.T) {
		b := Root().Key("a\bb")
		assert.Equal(t, `$["a\bb"]`, b.String())
	})

	t.Run("form feed escape", func(t *testing.T) {
		b := Root().Key("a\fb")
		assert.Equal(t, `$["a\fb"]`, b.String())
	})

	t.Run("very long key", func(t *testing.T) {
		longKey := make([]byte, 10000)
		for i := range longKey {
			longKey[i] = 'a'
		}
		b := Root().Key(string(longKey))
		assert.Equal(t, 10002, len(b.String())) // "$." + key
	})
}
