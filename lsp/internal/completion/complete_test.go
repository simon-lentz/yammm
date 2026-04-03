package completion

import (
	"slices"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/lsp/internal/analysis"
	"github.com/simon-lentz/yammm/lsp/internal/lsputil"
	"github.com/simon-lentz/yammm/lsp/internal/protocol"
	"github.com/simon-lentz/yammm/lsp/internal/symbols"
)

func TestTopLevelCompletions(t *testing.T) {
	t.Parallel()

	items := TopLevelCompletions()

	require.NotEmpty(t, items, "expected some completions")

	// Check for expected keywords
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	expected := []string{"schema", "import", "type", "abstract type", "part type"}
	for _, exp := range expected {
		assert.True(t, labels[exp], "missing expected completion: %s", exp)
	}

	// Check that items have snippet format
	for _, item := range items {
		assert.NotNil(t, item.InsertTextFormat, "completion %q should have snippet format", item.Label)
		if item.InsertTextFormat != nil {
			assert.Equal(t, protocol.InsertTextFormatSnippet, *item.InsertTextFormat, "completion %q should have snippet format", item.Label)
		}
	}
}

func TestTypeBodyCompletions(t *testing.T) {
	t.Parallel()

	items := TypeBodyCompletions()

	require.NotEmpty(t, items, "expected some completions")

	// Check for snippets
	hasProperty := false
	hasAssociation := false
	hasComposition := false
	hasInvariant := false

	for _, item := range items {
		switch {
		case strings.Contains(item.Label, "property"):
			hasProperty = true
		case strings.Contains(item.Label, "association"):
			hasAssociation = true
		case strings.Contains(item.Label, "composition"):
			hasComposition = true
		case strings.Contains(item.Label, "invariant"):
			hasInvariant = true
		}
	}

	assert.True(t, hasProperty, "missing property snippet")
	assert.True(t, hasAssociation, "missing association snippet")
	assert.True(t, hasComposition, "missing composition snippet")
	assert.True(t, hasInvariant, "missing invariant snippet")

	// Check that built-in types are included
	hasString := false
	hasInteger := false
	for _, item := range items {
		if item.Label == "String" {
			hasString = true
		}
		if item.Label == "Integer" {
			hasInteger = true
		}
	}

	assert.True(t, hasString, "missing String built-in type")
	assert.True(t, hasInteger, "missing Integer built-in type")
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
	sourceID := location.MustNewSourceID("test://types.yammm")
	span := location.Range(sourceID, 1, 1, 10, 1)

	sch := schema.NewSchema("test", sourceID, span, "")

	snapshot := &analysis.Snapshot{
		Schema:          sch,
		SymbolsBySource: make(map[location.SourceID]*symbols.SymbolIndex),
	}

	items := TypeCompletions(snapshot, sourceID)

	// Should return empty but not nil
	assert.NotNil(t, items, "expected non-nil slice")
}

func TestPropertyTypeCompletions_BuiltinTypes(t *testing.T) {
	t.Parallel()

	// Use zero SourceID since nil snapshot means no lookup occurs
	items := PropertyTypeCompletions(nil, location.SourceID{})

	// Should have built-in types even without snapshot
	builtins := map[string]bool{
		"String":    false,
		"Integer":   false,
		"Float":     false,
		"Boolean":   false,
		"UUID":      false,
		"Date":      false,
		"Timestamp": false,
	}

	for _, item := range items {
		if _, ok := builtins[item.Label]; ok {
			builtins[item.Label] = true
		}
	}

	for name, found := range builtins {
		assert.True(t, found, "missing built-in type: %s", name)
	}
}

func TestKeywordCompletion(t *testing.T) {
	t.Parallel()

	item := KeywordCompletion("type", "type ${1:Name} {}", "Type declaration")

	assert.Equal(t, "type", item.Label)
	require.NotNil(t, item.Kind, "expected keyword kind")
	assert.Equal(t, protocol.CompletionItemKindKeyword, *item.Kind, "expected keyword kind")
	require.NotNil(t, item.InsertTextFormat, "expected snippet format")
	assert.Equal(t, protocol.InsertTextFormatSnippet, *item.InsertTextFormat, "expected snippet format")
	require.NotNil(t, item.InsertText, "insert text mismatch")
	assert.Equal(t, "type ${1:Name} {}", *item.InsertText, "insert text mismatch")
}

func TestSnippetCompletion(t *testing.T) {
	t.Parallel()

	item := SnippetCompletion("property", "${1:name} String", "Property")

	assert.Equal(t, "property", item.Label)
	require.NotNil(t, item.Kind, "expected snippet kind")
	assert.Equal(t, protocol.CompletionItemKindSnippet, *item.Kind, "expected snippet kind")
	require.NotNil(t, item.InsertTextFormat, "expected snippet format")
	assert.Equal(t, protocol.InsertTextFormatSnippet, *item.InsertTextFormat, "expected snippet format")
}

func TestImportCompletions(t *testing.T) {
	t.Parallel()

	items := ImportCompletions()

	require.NotEmpty(t, items, "expected import completion")

	assert.Equal(t, "import", items[0].Label)
}

func TestBuiltinTypes_Complete(t *testing.T) {
	t.Parallel()

	// Verify all expected built-in types are present
	expected := []string{
		"Boolean", "Date", "Enum", "Float", "Integer", "List",
		"Pattern", "String", "Timestamp", "UUID", "Vector",
	}

	for _, exp := range expected {
		assert.True(t, slices.Contains(BuiltinTypes, exp), "missing built-in type: %s", exp)
	}
}

func TestComputeByteOffsetFromText_UTF8Mode(t *testing.T) {
	// Tests that ComputeByteOffsetFromText respects UTF-8 position encoding.
	// In UTF-8 mode, character offset IS byte offset (no conversion needed).
	t.Parallel()

	// Test with ASCII content - should work the same for both encodings
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
			result := ComputeByteOffsetFromText(text, tt.lspLine, tt.lspChar, lsputil.PositionEncodingUTF8)
			assert.Equal(t, tt.expected, result, "ComputeByteOffsetFromText() = %d; want %d", result, tt.expected)
		})
	}
}

func TestComputeByteOffsetFromText_UTF8Mode_MultiByteChars(t *testing.T) {
	// Tests UTF-8 mode with multi-byte characters.
	// In UTF-8 mode, lspChar is already a byte offset, so no conversion needed.
	// In UTF-16 mode, lspChar would be UTF-16 code units.
	//
	// This test verifies that UTF-8 mode passes through the byte offset directly,
	// while UTF-16 mode performs conversion.
	t.Parallel()

	// Line with emoji: "type 😀Test {"
	// Byte positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6,7,8) T(9) e(10) s(11) t(12) _(13) {(14)
	// UTF-16 positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6) T(7) e(8) s(9) t(10) _(11) {(12)
	text := "type 😀Test {"

	t.Run("UTF-8 mode passes through byte offset", func(t *testing.T) {
		t.Parallel()

		// In UTF-8 mode, lspChar=9 means byte offset 9, which is 'T'
		result := ComputeByteOffsetFromText(text, 0, 9, lsputil.PositionEncodingUTF8)
		assert.Equal(t, 9, result, "UTF-8 mode: ComputeByteOffsetFromText(lspChar=9) = %d; want 9 (passthrough)", result)
	})

	t.Run("UTF-16 mode converts code units to bytes", func(t *testing.T) {
		t.Parallel()

		// In UTF-16 mode, lspChar=7 means UTF-16 position 7, which is 'T' (after emoji)
		// 'T' is at byte offset 9 in the string
		result := ComputeByteOffsetFromText(text, 0, 7, lsputil.PositionEncodingUTF16)
		assert.Equal(t, 9, result, "UTF-16 mode: ComputeByteOffsetFromText(lspChar=7) = %d; want 9 (converted from UTF-16)", result)
	})
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
			assert.False(t, strings.Contains(insertText, pattern),
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
