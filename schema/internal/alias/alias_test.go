package alias_test

import (
	"slices"
	"sort"
	"testing"

	"github.com/antlr4-go/antlr/v4"
	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/grammar"
	"github.com/simon-lentz/yammm/schema/internal/alias"
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

		// Non-keywords
		{name: "parts", input: "parts", expected: false},
		{name: "User", input: "User", expected: false},
		{name: "my_alias", input: "my_alias", expected: false},
		{name: "Schema", input: "Schema", expected: false}, // Case-sensitive
		{name: "TYPE", input: "TYPE", expected: false},     // Case-sensitive
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := alias.IsReservedKeyword(tt.input)
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
			result := alias.IsValidAlias(tt.input)
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
			result := alias.DeriveAliasFromPath(tt.input)
			assert.Equal(t, tt.expected, result, "DeriveAliasFromPath(%q)", tt.input)
		})
	}
}

// TestGrammarAliasSynchronization is the CRITICAL test that verifies the
// reserved keyword list matches the generated lexer. This prevents drift between
// the grammar and the alias validation logic.
//
// It extracts keywords from the generated lexer's LiteralNames, which is
// automatically kept in sync with the grammar file during code generation.
// This approach is filesystem-independent and more robust than parsing .g4 files.
func TestGrammarAliasSynchronization(t *testing.T) {
	grammarKeywords := extractKeywordsFromLexer()
	codeKeywords := alias.ReservedKeywords()

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
	sort.Strings(missingInCode)

	// Check for keywords in code but missing from grammar
	var missingInGrammar []string
	for kw := range codeKeywords {
		if !grammarKeywords[kw] {
			missingInGrammar = append(missingInGrammar, kw)
		}
	}
	sort.Strings(missingInGrammar)

	// Report discrepancies
	if len(missingInCode) > 0 {
		t.Errorf("Keywords in grammar but MISSING from alias.reservedKeywords: %v", missingInCode)
	}
	if len(missingInGrammar) > 0 {
		t.Errorf("Keywords in alias.reservedKeywords but MISSING from grammar: %v", missingInGrammar)
	}

	// Log summary for debugging
	t.Logf("Grammar keywords (%d): %v", len(grammarList), grammarList)
	t.Logf("Code keywords (%d): %v", len(codeList), codeList)

	// Verify exact match
	assert.ElementsMatch(t, grammarList, codeList,
		"Reserved keywords must exactly match grammar keywords")
}

// extractKeywordsFromLexer extracts all keyword literals from the generated
// ANTLR lexer. Keywords are identified by being quoted string literals in
// the LiteralNames array (format: 'keyword').
//
// This includes:
// - DSL keywords (schema, import, type, extends, etc.)
// - Datatype keywords (Integer, Float, Boolean, etc.)
// - Boolean literals (true, false) - added explicitly since BOOLEAN is a rule, not a literal
func extractKeywordsFromLexer() map[string]bool {
	keywords := make(map[string]bool)

	// Create a lexer to access its LiteralNames
	// We use an empty input stream since we only need the static metadata
	lexer := grammar.NewYammmGrammarLexer(antlr.NewInputStream(""))

	// Extract keyword literals from LiteralNames
	// Keywords are quoted strings like 'schema', 'type', 'Integer', etc.
	for _, lit := range lexer.LiteralNames {
		// Skip empty entries and non-quoted strings
		if len(lit) < 3 || lit[0] != '\'' || lit[len(lit)-1] != '\'' {
			continue
		}

		// Extract the keyword (strip quotes)
		kw := lit[1 : len(lit)-1]

		// Only include identifier-like keywords (letters, digits, underscores)
		// Skip operators and punctuation
		if isIdentifierKeyword(kw) {
			keywords[kw] = true
		}
	}

	// The BOOLEAN lexer rule matches 'true' | 'false' but these literals
	// aren't in LiteralNames because they're part of a pattern rule.
	// We check if BOOLEAN is a symbolic name and add the literals explicitly.
	if slices.Contains(lexer.SymbolicNames, "BOOLEAN") {
		keywords["true"] = true
		keywords["false"] = true
	}

	return keywords
}

// isIdentifierKeyword returns true if the string looks like an identifier
// (starts with letter, contains only letters/digits/underscores).
func isIdentifierKeyword(s string) bool {
	if len(s) == 0 {
		return false
	}

	// Must start with a letter
	first := s[0]
	isLetter := (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
	if !isLetter {
		return false
	}

	// Rest must be alphanumeric or underscore
	for i := 1; i < len(s); i++ {
		c := s[i]
		isAlphaNumUnderscore := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_'
		if !isAlphaNumUnderscore {
			return false
		}
	}

	return true
}

func mapToSortedSlice(m map[string]bool) []string {
	result := make([]string, 0, len(m))
	for k := range m {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}

func TestReservedKeywordsReturnsDefensiveCopy(t *testing.T) {
	kw1 := alias.ReservedKeywords()
	kw2 := alias.ReservedKeywords()

	// Modify kw1
	kw1["new_keyword"] = true

	// Verify kw2 is unaffected
	assert.False(t, kw2["new_keyword"], "ReservedKeywords should return a defensive copy")
}
