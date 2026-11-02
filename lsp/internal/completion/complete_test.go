package completion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/simon-lentz/yammm/internal/yammmtest"
	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

// TestCompletionLists_Golden pins every static completion list — labels,
// kinds, snippet bodies, insert-text formats, documentation — as JSON
// goldens. Membership and format assertions live in the golden bytes.
func TestCompletionLists_Golden(t *testing.T) {
	t.Parallel()

	yammmtest.GoldenJSON(t, "completions_top_level", TopLevelCompletions())
	yammmtest.GoldenJSON(t, "completions_type_body", TypeBodyCompletions())
	yammmtest.GoldenJSON(t, "completions_property_type", PropertyTypeCompletions(nil, location.SourceID{}))
	yammmtest.GoldenJSON(t, "completions_import", ImportCompletions())
	yammmtest.GoldenJSON(t, "builtin_types", BuiltinTypes)
}

func TestTypeCompletions_NilSnapshot(t *testing.T) {
	t.Parallel()

	// With nil snapshot, should return empty slice without panic
	// Use zero SourceID since nil snapshot means no lookup occurs
	items := TypeCompletions(nil, location.SourceID{})

	assert.NotNil(t, items, "expected non-nil slice")
	assert.Empty(t, items, "expected empty slice, got %d items", len(items))
}

func TestTypeCompletions_EmptySchema(t *testing.T) {
	t.Parallel()

	// Create a snapshot with an empty schema
	sch, _ := schema.LoadString(t.Context(), `schema "test"
`, "types.yammm")
	sourceID := sch.SourceID()

	snapshot := &analysis.Snapshot{
		Schema:          sch,
		SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex),
	}

	items := TypeCompletions(snapshot, sourceID)

	// Should return empty but not nil
	assert.NotNil(t, items, "expected non-nil slice")
}

func TestComputeByteOffsetFromText_ASCII(t *testing.T) {
	t.Parallel()

	text := "type Person {\n    name String\n}"

	tests := []struct {
		name     string
		lspLine  int
		lspChar  int
		expected int
	}{
		{
			name:     "ASCII - start of line",
			lspLine:  1,
			lspChar:  0,
			expected: 0,
		},
		{
			name:     "ASCII - middle of line",
			lspLine:  1,
			lspChar:  4,
			expected: 4,
		},
		{
			name:     "ASCII - end of indentation",
			lspLine:  1,
			lspChar:  8,
			expected: 8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := ComputeByteOffsetFromText(text, tt.lspLine, tt.lspChar)
			assert.Equal(t, tt.expected, result, "ComputeByteOffsetFromText() = %d; want %d", result, tt.expected)
		})
	}
}

func TestComputeByteOffsetFromText_CountsUTF16Units(t *testing.T) {
	t.Parallel()

	// Line with emoji: "type 😀Test {"
	// Byte positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6,7,8) T(9) e(10) s(11) t(12) _(13) {(14)
	// UTF-16 positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6) T(7) e(8) s(9) t(10) _(11) {(12)
	result := ComputeByteOffsetFromText("type 😀Test {", 0, 7)
	assert.Equal(t, 9, result, "ComputeByteOffsetFromText(lspChar=7) = %d; want 9", result)
}

func TestTypeBodySnippets_NoCommasBeforeModifiers(t *testing.T) {
	// Regression test for Priority 1.3 fix: snippets should NOT have commas
	// before modifiers like "required" or "primary" in the OUTPUT.
	// YAMMM uses space-separated modifiers, not comma-separated.
	//
	// The invalid pattern was: ${N| , required, primary|} with space before comma
	// This would produce " , required" when selected (wrong!).
	//
	// The valid pattern is: ${N|, required, primary|} where:
	// - First option is empty (nothing before first comma)
	// - Second option is " required" (space+word)
	// - Third option is " primary" (space+word)
	// This produces " required" when selected (correct!).
	t.Parallel()

	// Get type body completions
	completions := TypeBodyCompletions()

	// The INVALID pattern is "| ," (space before comma in choice placeholder)
	// which would produce " , modifier" output
	invalidPatterns := []string{
		"| ,", // Space before comma in choice - would produce " ," in output
	}

	for _, item := range completions {
		if item.InsertText == nil {
			continue
		}
		insertText := *item.InsertText

		for _, pattern := range invalidPatterns {
			assert.NotContains(t, insertText, pattern,
				"snippet %q contains invalid pattern %q; this would produce a comma in output",
				item.Label, pattern)
		}
	}

	// Additionally verify that modifier choices have the correct format:
	// ${N|, modifier1, modifier2|} - empty first option, space-prefixed subsequent
	for _, item := range completions {
		if item.InsertText == nil {
			continue
		}
		insertText := *item.InsertText

		// Check that modifier choices like "|, required" are present (correct format)
		if strings.Contains(insertText, "required") {
			// Should have "|," followed by " required" (not "| ," or "|required")
			assert.False(t, strings.Contains(insertText, "| , required") || strings.Contains(insertText, "|required"),
				"snippet %q has incorrect modifier choice format", item.Label)
		}
	}
}

func TestTypeBodySnippets_ModifierChoicesValid(t *testing.T) {
	// Verify that modifier choice placeholders have valid structure:
	// ${N|, option1, option2|} - first option MUST be empty (space-prefixed for non-empty)
	t.Parallel()

	completions := TypeBodyCompletions()

	for _, item := range completions {
		if item.InsertText == nil {
			continue
		}
		insertText := *item.InsertText

		// Find all choice placeholders like ${N|...|} that contain "required" or "primary"
		// These should have format ${N|, required|} or ${N|, required, primary|}
		// where the empty first option allows omitting the modifier
		if strings.Contains(insertText, "required") || strings.Contains(insertText, "primary") {
			// Check for malformed patterns
			assert.False(t, strings.Contains(insertText, "|required") || strings.Contains(insertText, "|primary"),
				"snippet %q has malformed modifier choice; expected '|, modifier' pattern for optional modifiers",
				item.Label)
		}
	}
}

func TestTopLevelSnippets_ValidStructure(t *testing.T) {
	// Verify top-level snippets (schema, import, type) have valid structure
	t.Parallel()

	completions := TopLevelCompletions()

	// Each snippet should have InsertText
	for _, item := range completions {
		if item.Kind != nil && *item.Kind == protocol.CompletionItemKindSnippet {
			assert.True(t, item.InsertText != nil && *item.InsertText != "",
				"snippet %q has empty InsertText", item.Label)
		}
	}
}

func TestImportSnippets_ValidStructure(t *testing.T) {
	// Verify import snippet has valid placeholder structure
	t.Parallel()

	completions := ImportCompletions()

	for _, item := range completions {
		if item.Label == "import" && item.InsertText != nil {
			insertText := *item.InsertText

			// Import snippet should have path and alias placeholders
			assert.True(t, strings.Contains(insertText, "${1:") && strings.Contains(insertText, "${2:"),
				"import snippet should have ${1:} and ${2:} placeholders; got %q", insertText)

			// Should contain import keyword pattern
			assert.Contains(t, insertText, "import",
				"import snippet should contain 'import'; got %q", insertText)
		}
	}
}
