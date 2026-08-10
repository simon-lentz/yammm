package schema_test

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/parse"
	"github.com/simon-lentz/yammm/schema"
)

func TestIsReservedKeyword(t *testing.T) {
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
			result := schema.TestIsReservedKeyword(tt.input)
			assert.Equal(t, tt.expected, result, "IsReservedKeyword(%q)", tt.input)
		})
	}
}

func TestIsValidAlias(t *testing.T) {
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

		// Invalid aliases
		{name: "starts with digit", input: "2parts", expected: false},
		{name: "starts with underscore", input: "_parts", expected: false},
		{name: "contains hyphen", input: "my-alias", expected: false},
		{name: "contains dot", input: "my.alias", expected: false},
		{name: "empty string", input: "", expected: false},
		{name: "contains space", input: "my alias", expected: false},
		{name: "contains special char", input: "my@alias", expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := schema.TestIsValidAlias(tt.input)
			assert.Equal(t, tt.expected, result, "IsValidAlias(%q)", tt.input)
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

// TestGrammarAliasSynchronization verifies that this package's reserved-keyword
// list matches the parser's own vocabulary, so an alias the loader accepts can
// never be a spelling the grammar refuses to lex as a qualifier.
//
// The comparison is against [parse.ReservedKeywords], which the parser composes
// from the maps its own positions test against. Both lists are hand-written, in
// different packages, and only this test couples them.
func TestGrammarAliasSynchronization(t *testing.T) {
	grammarKeywords := parse.ReservedKeywords()
	codeKeywords := schema.TestReservedKeywords()

	// Convert to sorted slices for comparison
	grammarList := mapToSortedSlice(grammarKeywords)
	codeList := mapToSortedSlice(codeKeywords)

	// Check for keywords in grammar but missing from code
	var missingInCode []string
	for kw := range grammarKeywords {
		if !codeKeywords[kw] {
			missingInCode = append(missingInCode, kw)
		}
	}
	slices.Sort(missingInCode)

	// Check for keywords in code but missing from grammar
	var missingInGrammar []string
	for kw := range codeKeywords {
		if !grammarKeywords[kw] {
			missingInGrammar = append(missingInGrammar, kw)
		}
	}
	slices.Sort(missingInGrammar)

	// Report discrepancies
	if len(missingInCode) > 0 {
		t.Errorf("Keywords in the parser but MISSING from alias.reservedKeywords: %v", missingInCode)
	}
	if len(missingInGrammar) > 0 {
		t.Errorf("Keywords in alias.reservedKeywords but MISSING from the parser: %v", missingInGrammar)
	}

	// Log summary for debugging
	t.Logf("Grammar keywords (%d): %v", len(grammarList), grammarList)
	t.Logf("Code keywords (%d): %v", len(codeList), codeList)

	// Verify exact match
	assert.ElementsMatch(t, grammarList, codeList,
		"Reserved keywords must exactly match grammar keywords")
}

func mapToSortedSlice(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	slices.Sort(result)
	return result
}

func TestReservedKeywordsReturnsDefensiveCopy(t *testing.T) {
	kw1 := schema.TestReservedKeywords()
	kw2 := schema.TestReservedKeywords()

	// Modify kw1
	kw1["new_keyword"] = true

	// Verify kw2 is unaffected
	assert.False(t, kw2["new_keyword"], "ReservedKeywords should return a defensive copy")
}
