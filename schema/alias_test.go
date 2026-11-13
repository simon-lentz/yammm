package schema_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/schema"
)

func TestIsReserved(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// DSL keywords
		{name: "schema", input: "schema", expected: true},
		{name: "import", input: "import", expected: true},
		{name: "as", input: "as", expected: true},
		{name: "type", input: "type", expected: true},
		{name: "datatype", input: "datatype", expected: true},
		{name: "required", input: "required", expected: true},
		{name: "primary", input: "primary", expected: true},
		{name: "extends", input: "extends", expected: true},
		{name: "includes", input: "includes", expected: true},
		{name: "abstract", input: "abstract", expected: true},
		{name: "part", input: "part", expected: true},
		{name: "one", input: "one", expected: true},
		{name: "many", input: "many", expected: true},
		{name: "in", input: "in", expected: true},

		// Datatype keywords
		{name: "Integer", input: "Integer", expected: true},
		{name: "Float", input: "Float", expected: true},
		{name: "Boolean", input: "Boolean", expected: true},
		{name: "String", input: "String", expected: true},
		{name: "Enum", input: "Enum", expected: true},
		{name: "Pattern", input: "Pattern", expected: true},
		{name: "Timestamp", input: "Timestamp", expected: true},
		{name: "Date", input: "Date", expected: true},
		{name: "UUID", input: "UUID", expected: true},
		{name: "Vector", input: "Vector", expected: true},

		// Boolean literals
		{name: "true", input: "true", expected: true},
		{name: "false", input: "false", expected: true},

		// Nil literal keyword
		{name: "nil", input: "nil", expected: true},

		// Non-keywords
		{name: "parts", input: "parts", expected: false},
		{name: "User", input: "User", expected: false},
		{name: "my_alias", input: "my_alias", expected: false},
		{name: "Schema", input: "Schema", expected: false}, // Case-sensitive
		{name: "TYPE", input: "TYPE", expected: false},     // Case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parse.IsReserved(tt.input)
			assert.Equal(t, tt.expected, result, "IsReserved(%q)", tt.input)
		})
	}
}

func TestIsIdentifier(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		// Valid aliases
		{name: "lowercase", input: "parts", expected: true},
		{name: "uppercase", input: "PARTS", expected: true},
		{name: "mixed case", input: "MyAlias", expected: true},
		{name: "with underscore", input: "my_alias", expected: true},
		{name: "with digits", input: "parts2", expected: true},
		{name: "single letter", input: "a", expected: true},
		{name: "single uppercase", input: "A", expected: true},
		{name: "each range end after the first letter", input: "Az09", expected: true},
		{name: "a lower-case z first", input: "zone", expected: true},
		{name: "an upper-case Z first", input: "Zone", expected: true},

		// Invalid aliases
		{name: "starts with digit", input: "2parts", expected: false},
		{name: "starts with underscore", input: "_parts", expected: false},
		{name: "contains hyphen", input: "my-alias", expected: false},
		{name: "contains dot", input: "my.alias", expected: false},
		{name: "empty string", input: "", expected: false},
		{name: "contains space", input: "my alias", expected: false},
		{name: "contains special char", input: "my@alias", expected: false},
		{name: "the character after Z", input: "A[", expected: false},
		{name: "the character after 9", input: "a:", expected: false},
		{name: "the character before a", input: "a`", expected: false},
		{name: "the character after z first", input: "{a", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := parse.IsIdentifier(tt.input)
			assert.Equal(t, tt.expected, result, "IsIdentifier(%q)", tt.input)
		})
	}
}

func TestDeriveAliasFromPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Basic paths
		{name: "simple file", input: "parts.yammm", expected: "parts"},
		{name: "relative path", input: "./parts.yammm", expected: "parts"},
		{name: "nested path", input: "./schemas/parts.yammm", expected: "parts"},
		{name: "deep path", input: "a/b/c/parts.yammm", expected: "parts"},

		// Without extension
		{name: "no extension", input: "parts", expected: "parts"},
		{name: "no extension relative", input: "./parts", expected: "parts"},

		// Trailing slashes
		{name: "trailing slash", input: "parts/", expected: "parts"},
		{name: "multiple trailing slashes", input: "parts///", expected: "parts"},

		// Hyphen handling
		{name: "hyphen to underscore", input: "my-parts.yammm", expected: "my_parts"},
		{name: "multiple hyphens", input: "my-complex-parts.yammm", expected: "my_complex_parts"},

		// Dot handling (other than .yammm)
		{name: "dot in name", input: "my.parts.yammm", expected: "my_parts"},

		// Digit handling - prepend "n" to produce valid identifier
		{name: "starts with digit", input: "2parts.yammm", expected: "n2parts"},
		{name: "digit in middle", input: "parts2.yammm", expected: "parts2"},

		// Edge cases
		{name: "empty after strip", input: ".yammm", expected: "n"},
		{name: "only special chars", input: "@#$.yammm", expected: "n___"}, // underscores, then prepend n
		{name: "underscore preserved", input: "my_parts.yammm", expected: "my_parts"},

		// Uppercase preserved
		{name: "uppercase", input: "Parts.yammm", expected: "Parts"},
		{name: "mixed case", input: "MyParts.yammm", expected: "MyParts"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := schema.TestDeriveAliasFromPath(tt.input)
			assert.Equal(t, tt.expected, result, "DeriveAliasFromPath(%q)", tt.input)
		})
	}
}
