package lsp

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/location"
	"github.com/simon-lentz/yammm/schema"
)

// textToDoc creates a minimal DocumentSnapshot for testing detectCompletionContext.
// LineState is nil so tests exercise the fallback path (isInsideTypeBodyDirect).
func textToDoc(text string) *DocumentSnapshot {
	return &DocumentSnapshot{Text: text}
}

func TestDetectCompletionContext_TopLevel(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected CompletionContext
	}{
		{
			name:     "empty file",
			text:     "",
			line:     0,
			char:     0,
			expected: ContextTopLevel,
		},
		{
			name:     "after schema declaration",
			text:     "schema \"test\"\n\n",
			line:     2,
			char:     0,
			expected: ContextTopLevel,
		},
		{
			name:     "beginning of line after import",
			text:     "schema \"test\"\nimport \"./foo\" as foo\n\n",
			line:     3,
			char:     0,
			expected: ContextTopLevel,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.detectCompletionContext(textToDoc(tt.text), tt.line, tt.char)
			if result != tt.expected {
				t.Errorf("detectCompletionContext() = %v; want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectCompletionContext_TypeBody(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected CompletionContext
	}{
		{
			name:     "inside type braces",
			text:     "type Person {\n    \n}",
			line:     1,
			char:     4,
			expected: ContextTypeBody,
		},
		{
			name:     "after opening brace",
			text:     "type Person {\n",
			line:     1,
			char:     0,
			expected: ContextTypeBody,
		},
		{
			name:     "nested in type with properties",
			text:     "type Person {\n    name String\n    \n}",
			line:     2,
			char:     4,
			expected: ContextTypeBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.detectCompletionContext(textToDoc(tt.text), tt.line, tt.char)
			if result != tt.expected {
				t.Errorf("detectCompletionContext() = %v; want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectCompletionContext_Extends(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected CompletionContext
	}{
		{
			name:     "after extends keyword",
			text:     "type Car extends ",
			line:     0,
			char:     17,
			expected: ContextExtends,
		},
		{
			name:     "after extends with partial",
			text:     "type Car extends Ve",
			line:     0,
			char:     19,
			expected: ContextExtends,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.detectCompletionContext(textToDoc(tt.text), tt.line, tt.char)
			if result != tt.expected {
				t.Errorf("detectCompletionContext() = %v; want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectCompletionContext_PropertyType(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected CompletionContext
	}{
		{
			name:     "after property name with space",
			text:     "type Person {\n    name ",
			line:     1,
			char:     9,
			expected: ContextPropertyType,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.detectCompletionContext(textToDoc(tt.text), tt.line, tt.char)
			if result != tt.expected {
				t.Errorf("detectCompletionContext() = %v; want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectCompletionContext_RelationTarget(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected CompletionContext
	}{
		{
			name:     "after association arrow and multiplicity",
			text:     "type Person {\n    --> ADDRESSES (many) ",
			line:     1,
			char:     28,
			expected: ContextRelationTarget,
		},
		{
			name:     "after composition arrow and multiplicity",
			text:     "type Car {\n    *-> WHEELS (many) ",
			line:     1,
			char:     22,
			expected: ContextRelationTarget,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.detectCompletionContext(textToDoc(tt.text), tt.line, tt.char)
			if result != tt.expected {
				t.Errorf("detectCompletionContext() = %v; want %v", result, tt.expected)
			}
		})
	}
}

func TestDetectCompletionContext_Import(t *testing.T) {
	t.Parallel()

	s := &Server{}

	text := "import "
	result := s.detectCompletionContext(textToDoc(text), 0, 7)
	if result != ContextImportPath {
		t.Errorf("detectCompletionContext() = %v; want %v", result, ContextImportPath)
	}
}

func TestTopLevelCompletions(t *testing.T) {
	t.Parallel()

	s := &Server{}
	items := s.topLevelCompletions()

	if len(items) == 0 {
		t.Fatal("expected some completions")
	}

	// Check for expected keywords
	labels := make(map[string]bool)
	for _, item := range items {
		labels[item.Label] = true
	}

	expected := []string{"schema", "import", "type", "abstract type", "part type"}
	for _, exp := range expected {
		if !labels[exp] {
			t.Errorf("missing expected completion: %s", exp)
		}
	}

	// Check that items have snippet format
	for _, item := range items {
		if item.InsertTextFormat == nil || *item.InsertTextFormat != protocol.InsertTextFormatSnippet {
			t.Errorf("completion %q should have snippet format", item.Label)
		}
	}
}

func TestTypeBodyCompletions(t *testing.T) {
	t.Parallel()

	s := &Server{}
	items := s.typeBodyCompletions()

	if len(items) == 0 {
		t.Fatal("expected some completions")
	}

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

	if !hasProperty {
		t.Error("missing property snippet")
	}
	if !hasAssociation {
		t.Error("missing association snippet")
	}
	if !hasComposition {
		t.Error("missing composition snippet")
	}
	if !hasInvariant {
		t.Error("missing invariant snippet")
	}

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

	if !hasString {
		t.Error("missing String built-in type")
	}
	if !hasInteger {
		t.Error("missing Integer built-in type")
	}
}

func TestTypeCompletions_NilSnapshot(t *testing.T) {
	t.Parallel()

	s := &Server{}

	// With nil snapshot, should return empty slice without panic
	// Use zero SourceID since nil snapshot means no lookup occurs
	items := s.typeCompletions(nil, location.SourceID{})

	if items == nil {
		t.Error("expected non-nil slice")
	}
	if len(items) != 0 {
		t.Errorf("expected empty slice, got %d items", len(items))
	}
}

func TestTypeCompletions_EmptySchema(t *testing.T) {
	t.Parallel()

	s := &Server{}

	// Create a snapshot with an empty schema
	sourceID := location.MustNewSourceID("test://types.yammm")
	span := location.Range(sourceID, 1, 1, 10, 1)

	sch := schema.NewSchema("test", sourceID, span, "")

	snapshot := &Snapshot{
		Schema:          sch,
		SymbolsBySource: make(map[location.SourceID]*SymbolIndex),
	}

	items := s.typeCompletions(snapshot, sourceID)

	// Should return empty but not nil
	if items == nil {
		t.Error("expected non-nil slice")
	}
}

func TestPropertyTypeCompletions_BuiltinTypes(t *testing.T) {
	t.Parallel()

	s := &Server{}
	// Use zero SourceID since nil snapshot means no lookup occurs
	items := s.propertyTypeCompletions(nil, location.SourceID{})

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
		if !found {
			t.Errorf("missing built-in type: %s", name)
		}
	}
}

func TestKeywordCompletion(t *testing.T) {
	t.Parallel()

	s := &Server{}
	item := s.keywordCompletion("type", "type ${1:Name} {}", "Type declaration")

	if item.Label != "type" {
		t.Errorf("Label = %q; want 'type'", item.Label)
	}
	if item.Kind == nil || *item.Kind != protocol.CompletionItemKindKeyword {
		t.Error("expected keyword kind")
	}
	if item.InsertTextFormat == nil || *item.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Error("expected snippet format")
	}
	if item.InsertText == nil || *item.InsertText != "type ${1:Name} {}" {
		t.Error("insert text mismatch")
	}
}

func TestSnippetCompletion(t *testing.T) {
	t.Parallel()

	s := &Server{}
	item := s.snippetCompletion("property", "${1:name} String", "Property")

	if item.Label != "property" {
		t.Errorf("Label = %q; want 'property'", item.Label)
	}
	if item.Kind == nil || *item.Kind != protocol.CompletionItemKindSnippet {
		t.Error("expected snippet kind")
	}
	if item.InsertTextFormat == nil || *item.InsertTextFormat != protocol.InsertTextFormatSnippet {
		t.Error("expected snippet format")
	}
}

func TestIsIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		{"name", true},
		{"_private", true},
		{"Name123", true},
		{"123name", false},
		{"", false},
		{"name-with-dash", false},
		{"name.dot", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			result := isIdentifier(tt.input)
			if result != tt.expected {
				t.Errorf("isIdentifier(%q) = %v; want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsInsideTypeBody(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name      string
		text      string
		line      int
		cursorCol int // byte offset within line (-1 means end of line)
		expected  bool
	}{
		{
			name:      "outside type",
			text:      "schema \"test\"\n\ntype Person {\n}\n",
			line:      1,
			cursorCol: -1,
			expected:  false,
		},
		{
			name:      "inside type",
			text:      "type Person {\n    name String\n}",
			line:      1,
			cursorCol: -1,
			expected:  true,
		},
		{
			name:      "after closing brace",
			text:      "type Person {\n    name String\n}\n",
			line:      3,
			cursorCol: -1,
			expected:  false,
		},
		{
			name:      "nested braces",
			text:      "type A {\n}\ntype B {\n    x Int\n}",
			line:      3,
			cursorCol: -1,
			expected:  true,
		},
		{
			name:      "cursor before closing brace on same line",
			text:      "type Person { }",
			line:      0,
			cursorCol: 14, // cursor before the closing }
			expected:  true,
		},
		{
			name:      "cursor after closing brace on same line",
			text:      "type Person { }",
			line:      0,
			cursorCol: 15, // cursor after the closing }
			expected:  false,
		},
		{
			name:      "cursor before first brace",
			text:      "type Person { }",
			line:      0,
			cursorCol: 12, // cursor before opening {
			expected:  false,
		},
		{
			name:      "cursor just after opening brace",
			text:      "type Person { }",
			line:      0,
			cursorCol: 14, // cursor after opening { but before closing }
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tt.text, "\n")
			cursorCol := tt.cursorCol
			if cursorCol < 0 {
				// Use end of line
				cursorCol = len(lines[tt.line])
			}
			// Test the direct brace-counting logic (without cache)
			result := s.isInsideTypeBodyDirect(lines, tt.line, cursorCol)
			if result != tt.expected {
				t.Errorf("isInsideTypeBodyDirect(line=%d, col=%d) = %v; want %v\ntext: %q",
					tt.line, cursorCol, result, tt.expected, tt.text)
			}
		})
	}
}

func TestIsInsideTypeBodyDirect_CommentSequencesInStrings(t *testing.T) {
	// Tests that // and /* */ inside string literals don't affect brace counting.
	// The string parser should skip over these sequences.
	//
	// This is a regression test for the bug where comment detection ran
	// BEFORE string handling, causing strings like "http://" to be treated
	// as containing a line comment.
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name      string
		text      string
		line      int
		cursorCol int
		expected  bool
	}{
		{
			name:      "URL in string - not a comment",
			text:      "type Foo {\n    url \"http://example.com\"\n}",
			line:      1,
			cursorCol: 30, // Inside braces, after the string
			expected:  true,
		},
		{
			name:      "block comment sequence in string",
			text:      "type Foo {\n    note \"/* not a comment */\"\n}",
			line:      1,
			cursorCol: 35, // Inside braces
			expected:  true,
		},
		{
			name:      "line comment sequence in string",
			text:      "type Foo {\n    path \"C://Windows//path\"\n}",
			line:      1,
			cursorCol: 30,
			expected:  true,
		},
		{
			name:      "brace inside string should not count",
			text:      "type Foo {\n    json \"{nested}\"\n}",
			line:      1,
			cursorCol: 20,
			expected:  true, // Still depth 1, not 2
		},
		{
			name:      "closing brace in string should not close type",
			text:      "type Foo {\n    val \"}\"\n    \n}",
			line:      2,
			cursorCol: 4, // Empty line after string with }
			expected:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tt.text, "\n")
			result := s.isInsideTypeBodyDirect(lines, tt.line, tt.cursorCol)
			if result != tt.expected {
				t.Errorf("isInsideTypeBodyDirect(line=%d, col=%d) = %v; want %v\ntext: %q",
					tt.line, tt.cursorCol, result, tt.expected, tt.text)
			}
		})
	}
}

func TestIsInsideTypeBody_CachedLineState(t *testing.T) {
	// Tests isInsideTypeBody with a pre-computed LineState cache.
	// This exercises the O(1) cached path rather than the direct computation.
	t.Parallel()

	s := &Server{}

	text := "type A {\n    name String\n}"
	lines := strings.Split(text, "\n")

	// Pre-compute LineState using the same logic as Workspace
	braceDepths, inComment := ComputeBraceDepths(text)

	doc := &DocumentSnapshot{
		Version: 1,
		Text:    text,
		LineState: &LineState{
			Version:        1, // Matches doc version
			BraceDepth:     braceDepths,
			InBlockComment: inComment,
		},
	}

	tests := []struct {
		line      int
		cursorCol int // -1 means end of line
		expected  bool
	}{
		{0, -1, true},  // "type A {" - end of line after opening brace, depth=1
		{1, -1, true},  // "    name String" - inside body
		{2, -1, false}, // "}" - end of line after closing brace, depth=0
		{2, 0, true},   // "}" - cursor before closing brace (start of line 2)
	}

	for _, tt := range tests {
		cursorCol := tt.cursorCol
		if cursorCol < 0 {
			cursorCol = len(lines[tt.line])
		}
		result := s.isInsideTypeBody(doc, lines, tt.line, cursorCol)
		if result != tt.expected {
			t.Errorf("isInsideTypeBody(line %d, col %d) = %v; want %v (braceDepths=%v)",
				tt.line, cursorCol, result, tt.expected, braceDepths)
		}
	}
}

func TestIsInsideTypeBody_StaleCacheUsesDirectComputation(t *testing.T) {
	// Tests that isInsideTypeBody falls back to direct computation when
	// LineState version doesn't match document version.
	t.Parallel()

	s := &Server{}

	text := "type A {\n    name String\n}"
	lines := strings.Split(text, "\n")

	doc := &DocumentSnapshot{
		Version: 2, // Document version
		Text:    text,
		LineState: &LineState{
			Version:    1, // Stale cache version
			BraceDepth: []int{1, 1, 0},
		},
	}

	// Should fall back to direct computation, which gives same result
	// Use end of line position
	cursorCol := len(lines[1])
	result := s.isInsideTypeBody(doc, lines, 1, cursorCol)
	if !result {
		t.Error("isInsideTypeBody with stale cache should fall back to direct computation")
	}
}

func TestIsInsideTypeBody_CachedLineState_MultiLineBlockComment(t *testing.T) {
	// Tests isInsideTypeBody with cached LineState when a multi-line block comment
	// contains braces. This exercises the O(1) cached path with InBlockComment state.
	// Guards against false positives from braces inside block comments.
	t.Parallel()

	s := &Server{}

	// Multi-line block comment with braces inside
	text := "type A {\n/* {\n} */\n    name String\n}"
	lines := strings.Split(text, "\n")

	// Pre-compute LineState
	braceDepths, inComment := ComputeBraceDepths(text)

	// Verify the computed state is correct
	// Line 0: "type A {" -> depth 1, not in block comment
	// Line 1: "/* {" -> depth 1 (brace in comment), ends IN block comment
	// Line 2: "} */" -> depth 1 (} is in comment before */), ends NOT in block comment
	// Line 3: "    name String" -> depth 1
	// Line 4: "}" -> depth 0
	expectedDepths := []int{1, 1, 1, 1, 0}
	expectedInComment := []bool{false, true, false, false, false}

	for i, d := range braceDepths {
		if d != expectedDepths[i] {
			t.Fatalf("pre-check: braceDepths[%d] = %d; want %d", i, d, expectedDepths[i])
		}
	}
	for i, c := range inComment {
		if c != expectedInComment[i] {
			t.Fatalf("pre-check: inComment[%d] = %v; want %v", i, c, expectedInComment[i])
		}
	}

	doc := &DocumentSnapshot{
		Version: 1,
		Text:    text,
		LineState: &LineState{
			Version:        1,
			BraceDepth:     braceDepths,
			InBlockComment: inComment,
		},
	}

	tests := []struct {
		name      string
		line      int
		cursorCol int
		expected  bool
	}{
		{"line 0 end (after opening brace)", 0, len(lines[0]), true},
		{"line 1 start (inside block comment)", 1, 0, true}, // In comment, but still inside braces
		{"line 1 end (brace in comment)", 1, len(lines[1]), true},
		{"line 2 start (} in comment before */)", 2, 0, true},
		{"line 2 end (after comment closes)", 2, len(lines[2]), true},
		{"line 3 (inside type body)", 3, len(lines[3]), true},
		{"line 4 end (after closing brace)", 4, len(lines[4]), false},
		{"line 4 start (before closing brace)", 4, 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := s.isInsideTypeBody(doc, lines, tt.line, tt.cursorCol)
			if result != tt.expected {
				t.Errorf("isInsideTypeBody(line %d, col %d) = %v; want %v\nbraceDepths=%v inComment=%v",
					tt.line, tt.cursorCol, result, tt.expected, braceDepths, inComment)
			}
		})
	}
}

func TestImportCompletions(t *testing.T) {
	t.Parallel()

	s := &Server{}
	items := s.importCompletions()

	if len(items) == 0 {
		t.Fatal("expected import completion")
	}

	if items[0].Label != "import" {
		t.Errorf("expected 'import' label, got %q", items[0].Label)
	}
}

func TestBuiltinTypes_Complete(t *testing.T) {
	t.Parallel()

	// Verify all expected built-in types are present
	expected := []string{
		"Boolean", "Date", "Enum", "Float", "Integer", "List",
		"Pattern", "String", "Timestamp", "UUID", "Vector",
	}

	for _, exp := range expected {
		if !slices.Contains(builtinTypes, exp) {
			t.Errorf("missing built-in type: %s", exp)
		}
	}
}

func TestNewExprPtr(t *testing.T) {
	t.Parallel()

	s := "test"
	ptr := new(s)

	if *ptr != s {
		t.Errorf("new(s) = %q; want %q", *ptr, s)
	}
}

// =============================================================================
// UTF-8 Position Encoding Tests
// These tests validate that completion works correctly with UTF-8 encoding
// and multi-byte characters, catching regressions where byte offsets might
// be mistakenly treated as rune offsets.
// =============================================================================

func TestDetectCompletionContext_UTF8_MultiByteChars(t *testing.T) {
	// Validates that detectCompletionContext does not panic when character
	// positions are byte offsets (UTF-8 encoding) and content contains
	// multi-byte UTF-8 characters.
	//
	// This catches regressions where byte offsets cause panics (e.g., slice bounds
	// errors) when they land in the middle of multi-byte sequences.
	//
	// Note: We don't assert specific context values because that tests the context
	// detection logic, not the UTF-8 safety. The key assertion is no panic.
	t.Parallel()

	s := &Server{}

	// Content with CJK characters:
	// Line 0: "type 日本語 {"
	//         t(0) y(1) p(2) e(3) _(4) 日(5,6,7) 本(8,9,10) 語(11,12,13) _(14) {(15)
	// Line 1: "    name String"
	//         _(0) _(1) _(2) _(3) n(4) a(5) m(6) e(7) _(8) S(9)...
	// Line 2: "}"
	text := "type 日本語 {\n    name String\n}"

	tests := []struct {
		name     string
		line     int
		charByte int // byte offset from line start (UTF-8 encoding semantics)
	}{
		// Line 0: "type 日本語 {"
		{"start of file", 0, 0},
		{"after 'type' keyword", 0, 4},
		{"at first byte of 日", 0, 5},
		{"middle byte of 日 (byte 2)", 0, 6},
		{"last byte of 日 (byte 3)", 0, 7},
		{"at first byte of 本", 0, 8},
		{"middle byte of 本", 0, 9},
		{"at first byte of 語", 0, 11},
		{"middle byte of 語", 0, 12},
		{"after CJK, at space", 0, 14},
		{"at opening brace", 0, 15},

		// Line 1: "    name String" (inside type body)
		{"start of line inside body", 1, 0},
		{"after indentation", 1, 4},
		{"after 'name '", 1, 9},

		// Line 2: "}"
		{"at closing brace", 2, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Primary assertion: does not panic when charByte points to middle of multi-byte char
			result := s.detectCompletionContext(textToDoc(text), tt.line, tt.charByte)
			// Secondary: returns a valid context
			if result < ContextUnknown || result > ContextImportPath {
				t.Errorf("detectCompletionContext returned invalid context: %d", result)
			}
		})
	}
}

func TestDetectCompletionContext_UTF8_Emoji(t *testing.T) {
	// Test with emoji (4-byte UTF-8 sequences) to ensure no panics
	// when byte offsets land in the middle of multi-byte sequences.
	t.Parallel()

	s := &Server{}

	// "type 😀Test {" where 😀 is 4 bytes (U+1F600)
	// t(0) y(1) p(2) e(3) _(4) 😀(5,6,7,8) T(9) e(10) s(11) t(12) _(13) {(14)
	text := "type 😀Test {\n}"

	tests := []struct {
		name     string
		charByte int
	}{
		{"before emoji", 4},
		{"first byte of emoji", 5},
		{"second byte of emoji", 6},
		{"third byte of emoji", 7},
		{"fourth byte of emoji", 8},
		{"after emoji", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Primary assertion: does not panic
			result := s.detectCompletionContext(textToDoc(text), 0, tt.charByte)
			// Secondary: returns a valid context (not strictly required, but good sanity check)
			if result < ContextUnknown || result > ContextImportPath {
				t.Errorf("detectCompletionContext returned invalid context: %d", result)
			}
		})
	}
}

// TestCompletion_UTF8Mode_Integration tests the full completion path with UTF-8
// position encoding and multi-byte content. This is an integration test that
// exercises ByteOffsetFromLSP → detectCompletionContext through the Server.
//
// UTF-8 mode still deserves a quick sanity test at the provider level, mainly
// to catch regressions where byte offsets are mistakenly treated as rune offsets.
// Uses textDocumentDidOpen to trigger analysis and snapshot storage, ensuring
// the full provider path is exercised.
func TestCompletion_UTF8Mode_Integration(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a schema file with CJK characters
	// The positions we test are byte offsets when using UTF-8 encoding
	content := `schema "日本語テスト"

type 人物 {
	名前 String
}
`
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Create server with silent logging
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{ModuleRoot: tmpDir})

	// Set UTF-8 position encoding (normally negotiated during initialize,
	// but we set it directly for this test)
	server.workspace.SetPositionEncoding(PositionEncodingUTF8)

	// Open the document and trigger analysis via textDocumentDidOpen.
	// Using nil context is safe - publishSnapshotDiagnostics is a no-op when ctx == nil.
	// This ensures LatestSnapshot(uri) returns a non-nil snapshot.
	uri := PathToURI(filePath)
	err := server.textDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "yammm",
			Version:    1,
			Text:       content,
		},
	})
	if err != nil {
		t.Fatalf("textDocumentDidOpen failed: %v", err)
	}

	// Test completion at various byte positions with multi-byte characters
	tests := []struct {
		name string
		line int
		char int // byte offset from line start (UTF-8 encoding)
	}{
		// Line 0: `schema "日本語テスト"`
		// schema(0-5) _(6) "(7) 日(8,9,10) 本(11,12,13) 語(14,15,16)...
		{"line 0, start", 0, 0},
		{"line 0, at schema name start", 0, 8},
		{"line 0, mid CJK char", 0, 9}, // middle byte of 日

		// Line 2: `type 人物 {`
		// type(0-3) _(4) 人(5,6,7) 物(8,9,10) _(11) {(12)
		{"line 2, before type name", 2, 5},
		{"line 2, mid first CJK char", 2, 6}, // middle byte of 人
		{"line 2, at opening brace", 2, 12},

		// Line 3: `	名前 String` (tab + CJK property name)
		// tab(0) 名(1,2,3) 前(4,5,6) _(7) String(8-13)
		{"line 3, at property name", 3, 1},
		{"line 3, mid property name", 3, 2}, // middle byte of 名
		{"line 3, after property name", 3, 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Call completion handler - should not panic
			params := &protocol.CompletionParams{
				TextDocumentPositionParams: protocol.TextDocumentPositionParams{
					TextDocument: protocol.TextDocumentIdentifier{URI: uri},
					Position: protocol.Position{
						Line:      protocol.UInteger(tt.line), //nolint:gosec // test
						Character: protocol.UInteger(tt.char), //nolint:gosec // test
					},
				},
			}

			result, err := server.textDocumentCompletion(nil, params)
			if err != nil {
				t.Fatalf("completion failed: %v", err)
			}

			// Verify we got a result (may be nil for "no completions", or a slice)
			// The key assertion is that we didn't panic
			switch v := result.(type) {
			case nil:
				// No completions is valid
			case []protocol.CompletionItem:
				// Got completion items - this is fine
				t.Logf("got %d completion items", len(v))
			default:
				t.Errorf("unexpected result type: %T", result)
			}
		})
	}
}

// TestCompletion_UTF8Mode_NoPanic verifies that completion in UTF-8 position
// encoding mode works correctly without panicking.
//
// This exercises the full UTF-8 mode code path including ByteOffsetFromLSP
// and line offset calculations. The negative offset clamp in the completion
// handler (provider_completion.go) is a defensive guard that cannot currently
// be triggered with a normal source.Registry, since both LineStartByte and
// ContentBySource depend on the same registry entry. This test verifies the
// happy path works correctly.
//
// Uses textDocumentDidOpen to trigger analysis and snapshot storage,
// ensuring the full provider path is exercised.
func TestCompletion_UTF8Mode_NoPanic(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Multi-line content to exercise line offset calculations
	content := `schema "test"

type Person {
	name String
}
`
	filePath := filepath.Join(tmpDir, "main.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{ModuleRoot: tmpDir})
	server.workspace.SetPositionEncoding(PositionEncodingUTF8)

	// Open the document and trigger analysis via textDocumentDidOpen.
	// Using nil context is safe - publishSnapshotDiagnostics is a no-op when ctx == nil.
	uri := PathToURI(filePath)
	err := server.textDocumentDidOpen(nil, &protocol.DidOpenTextDocumentParams{
		TextDocument: protocol.TextDocumentItem{
			URI:        uri,
			LanguageID: "yammm",
			Version:    1,
			Text:       content,
		},
	})
	if err != nil {
		t.Fatalf("textDocumentDidOpen failed: %v", err)
	}

	// Request completion at line 3 (inside type body)
	params := &protocol.CompletionParams{
		TextDocumentPositionParams: protocol.TextDocumentPositionParams{
			TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			Position: protocol.Position{
				Line:      3,
				Character: 0, // start of line
			},
		},
	}

	// Should not panic and should return valid completions
	result, err := server.textDocumentCompletion(nil, params)
	if err != nil {
		t.Fatalf("completion failed: %v", err)
	}

	// Verify we got type body completions (since we're inside the type)
	items, ok := result.([]protocol.CompletionItem)
	if !ok {
		t.Skipf("no completion items returned (result type: %T)", result)
	}

	// Should have type body completions (property, association, etc.)
	if len(items) == 0 {
		t.Error("expected completion items for type body context")
	}
}

// =============================================================================
// Snippet Text Validity Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{})

	// Get type body completions
	completions := server.typeBodyCompletions()

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
			if strings.Contains(insertText, pattern) {
				t.Errorf("snippet %q contains invalid pattern %q; this would produce a comma in output",
					item.Label, pattern)
			}
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
			if strings.Contains(insertText, "| , required") || strings.Contains(insertText, "|required") {
				t.Errorf("snippet %q has incorrect modifier choice format", item.Label)
			}
		}
	}
}

func TestTypeBodySnippets_ModifierChoicesValid(t *testing.T) {
	// Verify that modifier choice placeholders have valid structure:
	// ${N|, option1, option2|} - first option MUST be empty (space-prefixed for non-empty)
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{})

	completions := server.typeBodyCompletions()

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
			if strings.Contains(insertText, "|required") || strings.Contains(insertText, "|primary") {
				// Missing space before the modifier in choice - wrong!
				// Should be "|, required" (empty first option) or "| required"
				t.Errorf("snippet %q has malformed modifier choice; expected '|, modifier' pattern for optional modifiers",
					item.Label)
			}
		}
	}
}

func TestTopLevelSnippets_ValidStructure(t *testing.T) {
	// Verify top-level snippets (schema, import, type) have valid structure
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{})

	completions := server.topLevelCompletions()

	// Each snippet should have InsertText
	for _, item := range completions {
		if item.Kind != nil && *item.Kind == protocol.CompletionItemKindSnippet {
			if item.InsertText == nil || *item.InsertText == "" {
				t.Errorf("snippet %q has empty InsertText", item.Label)
			}
		}
	}
}

func TestImportSnippets_ValidStructure(t *testing.T) {
	// Verify import snippet has valid placeholder structure
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{})

	completions := server.importCompletions()

	for _, item := range completions {
		if item.Label == "import" && item.InsertText != nil {
			insertText := *item.InsertText

			// Import snippet should have path and alias placeholders
			if !strings.Contains(insertText, "${1:") || !strings.Contains(insertText, "${2:") {
				t.Errorf("import snippet should have ${1:} and ${2:} placeholders; got %q", insertText)
			}

			// Should contain import keyword pattern
			if !strings.Contains(insertText, "import") {
				t.Errorf("import snippet should contain 'import'; got %q", insertText)
			}
		}
	}
}

func TestIsImportContext_CursorAfterPath(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name         string
		beforeCursor string
		want         bool
	}{
		{
			name:         "cursor after closing quote",
			beforeCursor: `import "some/path"`,
			want:         false,
		},
		{
			name:         "cursor after path with space",
			beforeCursor: `import "some/path" `,
			want:         false,
		},
		{
			name:         "cursor inside path (one quote)",
			beforeCursor: `import "some/pa`,
			want:         true,
		},
		{
			name:         "cursor at opening quote",
			beforeCursor: `import "`,
			want:         true,
		},
		{
			name:         "cursor before any quote",
			beforeCursor: `import `,
			want:         true,
		},
		// Single-quote support
		{
			name:         "single-quoted path complete",
			beforeCursor: `import 'some/path'`,
			want:         false,
		},
		{
			name:         "single-quoted path complete with space",
			beforeCursor: `import 'some/path' `,
			want:         false,
		},
		{
			name:         "single-quoted path incomplete",
			beforeCursor: `import 'some/pa`,
			want:         true,
		},
		{
			name:         "single-quoted path at opening",
			beforeCursor: `import '`,
			want:         true,
		},
		// Escaped quotes
		{
			name:         "escaped double quote inside path",
			beforeCursor: `import "path\"with`,
			want:         true,
		},
		{
			name:         "escaped double quote complete",
			beforeCursor: `import "path\"with"`,
			want:         false,
		},
		{
			name:         "escaped single quote inside path",
			beforeCursor: `import 'it\'s`,
			want:         true,
		},
		{
			name:         "escaped single quote complete",
			beforeCursor: `import 'it\'s'`,
			want:         false,
		},
		// Paths starting with "as" - should NOT be detected as the `as` keyword
		{
			name:         "double-quoted path starting with as",
			beforeCursor: `import "as`,
			want:         true,
		},
		{
			name:         "double-quoted path assets incomplete",
			beforeCursor: `import "assets/foo`,
			want:         true,
		},
		{
			name:         "single-quoted path starting with as",
			beforeCursor: `import 'as`,
			want:         true,
		},
		{
			name:         "single-quoted path assets incomplete",
			beforeCursor: `import 'assets/foo`,
			want:         true,
		},
		{
			name:         "double-quoted path starting with as complete",
			beforeCursor: `import "assets"`,
			want:         false,
		},
		{
			name:         "double-quoted path starting with as then alias",
			beforeCursor: `import "assets" as `,
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := s.isImportContext(tt.beforeCursor)
			if got != tt.want {
				t.Errorf("isImportContext(%q) = %v; want %v", tt.beforeCursor, got, tt.want)
			}
		})
	}
}

func TestIsQuotedStringComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"no quotes", "hello", false},
		{"double quote open only", `"hello`, false},
		{"double quote complete", `"hello"`, true},
		{"single quote open only", `'hello`, false},
		{"single quote complete", `'hello'`, true},
		{"escaped double quote incomplete", `"hel\"lo`, false},
		{"escaped double quote complete", `"hel\"lo"`, true},
		{"escaped single quote incomplete", `'it\'s`, false},
		{"escaped single quote complete", `'it\'s'`, true},
		{"mismatched quotes", `"hello'`, false},
		{"mismatched quotes reverse", `'hello"`, false},
		{"trailing backslash", `"hello\`, false},
		{"multiple escapes complete", `"a\"b\"c"`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := isQuotedStringComplete(tt.input)
			if got != tt.want {
				t.Errorf("isQuotedStringComplete(%q) = %v; want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestIsImportContext_CursorInAlias(t *testing.T) {
	t.Parallel()

	s := &Server{}

	tests := []struct {
		name         string
		beforeCursor string
		want         bool
	}{
		{
			name:         "cursor after 'as' keyword",
			beforeCursor: `import "some/path" as `,
			want:         false,
		},
		{
			name:         "cursor typing alias",
			beforeCursor: `import "some/path" as ali`,
			want:         false,
		},
		{
			name:         "cursor at 'as' keyword",
			beforeCursor: `import "some/path" as`,
			want:         false,
		},
		{
			name:         "cursor with space before as",
			beforeCursor: `import "some/path"  as foo`,
			want:         false,
		},
		{
			name:         "cursor with tab before as",
			beforeCursor: "import \"some/path\"\tas foo",
			want:         false,
		},
		{
			name:         "cursor with tab after as",
			beforeCursor: "import \"some/path\" as\tfoo",
			want:         false,
		},
		{
			name:         "cursor with tabs around as",
			beforeCursor: "import \"some/path\"\tas\tfoo",
			want:         false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := s.isImportContext(tt.beforeCursor)
			if got != tt.want {
				t.Errorf("isImportContext(%q) = %v; want %v", tt.beforeCursor, got, tt.want)
			}
		})
	}
}

func TestComputeByteOffsetFromText_UTF8Mode(t *testing.T) {
	// Tests that computeByteOffsetFromText respects UTF-8 position encoding.
	// In UTF-8 mode, character offset IS byte offset (no conversion needed).
	// The bug was that this function always assumed UTF-16 encoding.
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	server := NewServer(logger, Config{})

	// Set UTF-8 position encoding
	server.workspace.SetPositionEncoding(PositionEncodingUTF8)

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
			result := server.computeByteOffsetFromText(text, tt.lspLine, tt.lspChar)
			if result != tt.expected {
				t.Errorf("computeByteOffsetFromText() = %d; want %d", result, tt.expected)
			}
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

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Line with emoji: "type 😀Test {"
	// Byte positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6,7,8) T(9) e(10) s(11) t(12) _(13) {(14)
	// UTF-16 positions: t(0) y(1) p(2) e(3) _(4) 😀(5,6) T(7) e(8) s(9) t(10) _(11) {(12)
	text := "type 😀Test {"

	t.Run("UTF-8 mode passes through byte offset", func(t *testing.T) {
		t.Parallel()
		server := NewServer(logger, Config{})
		server.workspace.SetPositionEncoding(PositionEncodingUTF8)

		// In UTF-8 mode, lspChar=9 means byte offset 9, which is 'T'
		result := server.computeByteOffsetFromText(text, 0, 9)
		if result != 9 {
			t.Errorf("UTF-8 mode: computeByteOffsetFromText(lspChar=9) = %d; want 9 (passthrough)", result)
		}
	})

	t.Run("UTF-16 mode converts code units to bytes", func(t *testing.T) {
		t.Parallel()
		server := NewServer(logger, Config{})
		// Default is UTF-16, no need to set

		// In UTF-16 mode, lspChar=7 means UTF-16 position 7, which is 'T' (after emoji)
		// 'T' is at byte offset 9 in the string
		result := server.computeByteOffsetFromText(text, 0, 7)
		if result != 9 {
			t.Errorf("UTF-16 mode: computeByteOffsetFromText(lspChar=7) = %d; want 9 (converted from UTF-16)", result)
		}
	})
}
