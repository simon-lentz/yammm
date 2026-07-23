package completion

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/simon-lentz/yammm/lsp/internal/docstate"
)

// textToDoc creates a minimal docstate.Snapshot for testing DetectContext.
// LineState is nil so tests exercise the fallback path (isInsideTypeBodyDirect).
func textToDoc(text string) *docstate.Snapshot {
	return &docstate.Snapshot{Text: text}
}

func TestDetectContext_TopLevel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected Context
	}{
		{"empty file", "", 0, 0, TopLevel},
		{"after schema declaration", "schema \"test\"\n\n", 2, 0, TopLevel},
		{"beginning of line after import", "schema \"test\"\nimport \"./foo\" as foo\n\n", 3, 0, TopLevel},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DetectContext(textToDoc(tt.text), tt.line, tt.char)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDetectContext_TypeBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected Context
	}{
		{"inside type braces", "type Person {\n    \n}", 1, 4, TypeBody},
		{"after opening brace", "type Person {\n", 1, 0, TypeBody},
		{"nested in type with properties", "type Person {\n    name String\n    \n}", 2, 4, TypeBody},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DetectContext(textToDoc(tt.text), tt.line, tt.char)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDetectContext_Extends(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected Context
	}{
		{"after extends keyword", "type Car extends ", 0, 17, Extends},
		{"after extends with partial", "type Car extends Ve", 0, 19, Extends},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DetectContext(textToDoc(tt.text), tt.line, tt.char)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDetectContext_PropertyType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected Context
	}{
		{"after property name with space", "type Person {\n    name ", 1, 9, PropertyType},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DetectContext(textToDoc(tt.text), tt.line, tt.char)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDetectContext_RelationTarget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		line     int
		char     int
		expected Context
	}{
		{"after association arrow and multiplicity", "type Person {\n    --> ADDRESSES (many) ", 1, 28, RelationTarget},
		{"after composition arrow and multiplicity", "type Car {\n    *-> WHEELS (many) ", 1, 22, RelationTarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, _ := DetectContext(textToDoc(tt.text), tt.line, tt.char)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestDetectContext_Import(t *testing.T) {
	t.Parallel()

	got, _ := DetectContext(textToDoc("import "), 0, 7)
	assert.Equal(t, ImportPath, got)
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
			assert.Equal(t, tt.expected, isIdentifier(tt.input))
		})
	}
}

func TestIsInsideTypeBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		line      int
		cursorCol int // byte offset within line (-1 means end of line)
		expected  bool
	}{
		{"outside type", "schema \"test\"\n\ntype Person {\n}\n", 1, -1, false},
		{"inside type", "type Person {\n    name String\n}", 1, -1, true},
		{"after closing brace", "type Person {\n    name String\n}\n", 3, -1, false},
		{"nested braces", "type A {\n}\ntype B {\n    x Int\n}", 3, -1, true},
		{"brace in comment should not count", "type Person {\n    // } this is a comment\n    name String\n}", 2, -1, true},
		{"cursor before closing brace on same line", "type Person { }", 0, 14, true},
		{"cursor after closing brace on same line", "type Person { }", 0, 15, false},
		{"cursor before first brace", "type Person { }", 0, 12, false},
		{"cursor just after opening brace", "type Person { }", 0, 14, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tt.text, "\n")
			cursorCol := tt.cursorCol
			if cursorCol < 0 {
				cursorCol = len(lines[tt.line])
			}
			assert.Equal(t, tt.expected, isInsideTypeBodyDirect(lines, tt.line, cursorCol),
				"text: %q", tt.text)
		})
	}
}

func TestIsInsideTypeBodyDirect_CommentSequencesInStrings(t *testing.T) {
	// Tests that // and /* */ inside string literals don't affect brace counting.
	t.Parallel()

	tests := []struct {
		name      string
		text      string
		line      int
		cursorCol int
		expected  bool
	}{
		{"URL in string - not a comment", "type Foo {\n    url \"http://example.com\"\n}", 1, 30, true},
		{"block comment sequence in string", "type Foo {\n    note \"/* not a comment */\"\n}", 1, 35, true},
		{"line comment sequence in string", "type Foo {\n    path \"C://Windows//path\"\n}", 1, 30, true},
		{"brace inside string should not count", "type Foo {\n    json \"{nested}\"\n}", 1, 20, true},
		{"closing brace in string should not close type", "type Foo {\n    val \"}\"\n    \n}", 2, 4, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			lines := strings.Split(tt.text, "\n")
			assert.Equal(t, tt.expected, isInsideTypeBodyDirect(lines, tt.line, tt.cursorCol),
				"text: %q", tt.text)
		})
	}
}

func TestIsInsideTypeBody_CachedLineState(t *testing.T) {
	// Tests IsInsideTypeBody with a pre-computed lineState cache.
	t.Parallel()

	text := "type A {\n    name String\n}"
	lines := strings.Split(text, "\n")

	braceDepths, inComment := docstate.ComputeBraceDepths(text)

	doc := &docstate.Snapshot{
		Version: 1,
		Text:    text,
		LineState: &docstate.LineState{
			Version:        1,
			BraceDepth:     braceDepths,
			InBlockComment: inComment,
		},
	}

	tests := []struct {
		line      int
		cursorCol int // -1 means end of line
		expected  bool
	}{
		{0, -1, true},  // "type A {" - end of line after opening brace
		{1, -1, true},  // "    name String" - inside body
		{2, -1, false}, // "}" - after closing brace
		{2, 0, true},   // "}" - cursor before closing brace
	}

	for _, tt := range tests {
		cursorCol := tt.cursorCol
		if cursorCol < 0 {
			cursorCol = len(lines[tt.line])
		}
		assert.Equal(t, tt.expected, IsInsideTypeBody(doc, lines, tt.line, cursorCol),
			"line %d, col %d, braceDepths=%v", tt.line, cursorCol, braceDepths)
	}
}

func TestIsInsideTypeBody_StaleCacheUsesDirectComputation(t *testing.T) {
	t.Parallel()

	text := "type A {\n    name String\n}"
	lines := strings.Split(text, "\n")

	doc := &docstate.Snapshot{
		Version: 2,
		Text:    text,
		LineState: &docstate.LineState{
			Version:    1, // Stale cache version
			BraceDepth: []int{1, 1, 0},
		},
	}

	cursorCol := len(lines[1])
	assert.True(t, IsInsideTypeBody(doc, lines, 1, cursorCol),
		"IsInsideTypeBody with stale cache should fall back to direct computation")
}

func TestIsInsideTypeBody_CachedLineState_MultiLineBlockComment(t *testing.T) {
	t.Parallel()

	text := "type A {\n/* {\n} */\n    name String\n}"
	lines := strings.Split(text, "\n")

	braceDepths, inComment := docstate.ComputeBraceDepths(text)

	expectedDepths := []int{1, 1, 1, 1, 0}
	expectedInComment := []bool{false, true, false, false, false}

	require.Equal(t, expectedDepths, braceDepths, "pre-check: braceDepths")
	require.Equal(t, expectedInComment, inComment, "pre-check: inComment")

	doc := &docstate.Snapshot{
		Version: 1,
		Text:    text,
		LineState: &docstate.LineState{
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
		{"line 1 start (inside block comment)", 1, 0, true},
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
			assert.Equal(t, tt.expected, IsInsideTypeBody(doc, lines, tt.line, tt.cursorCol),
				"braceDepths=%v inComment=%v", braceDepths, inComment)
		})
	}
}

func TestIsImportContext_CursorAfterPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		beforeCursor string
		want         bool
	}{
		{"cursor after closing quote", `import "some/path"`, false},
		{"cursor after path with space", `import "some/path" `, false},
		{"cursor inside path (one quote)", `import "some/pa`, true},
		{"cursor at opening quote", `import "`, true},
		{"cursor before any quote", `import `, true},
		{"single-quoted path complete", `import 'some/path'`, false},
		{"single-quoted path complete with space", `import 'some/path' `, false},
		{"single-quoted path incomplete", `import 'some/pa`, true},
		{"single-quoted path at opening", `import '`, true},
		{"escaped double quote inside path", `import "path\"with`, true},
		{"escaped double quote complete", `import "path\"with"`, false},
		{"escaped single quote inside path", `import 'it\'s`, true},
		{"escaped single quote complete", `import 'it\'s'`, false},
		{"double-quoted path starting with as", `import "as`, true},
		{"double-quoted path assets incomplete", `import "assets/foo`, true},
		{"single-quoted path starting with as", `import 'as`, true},
		{"single-quoted path assets incomplete", `import 'assets/foo`, true},
		{"double-quoted path starting with as complete", `import "assets"`, false},
		{"double-quoted path starting with as then alias", `import "assets" as `, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isImportContext(tt.beforeCursor))
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
			assert.Equal(t, tt.want, isQuotedStringComplete(tt.input))
		})
	}
}

func TestIsImportContext_CursorInAlias(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		beforeCursor string
		want         bool
	}{
		{"cursor after 'as' keyword", `import "some/path" as `, false},
		{"cursor typing alias", `import "some/path" as ali`, false},
		{"cursor at 'as' keyword", `import "some/path" as`, false},
		{"cursor with space before as", `import "some/path"  as foo`, false},
		{"cursor with tab before as", "import \"some/path\"\tas foo", false},
		{"cursor with tab after as", "import \"some/path\" as\tfoo", false},
		{"cursor with tabs around as", "import \"some/path\"\tas\tfoo", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, isImportContext(tt.beforeCursor))
		})
	}
}

func TestDetectContext_UTF8_MultiByteChars(t *testing.T) {
	t.Parallel()

	text := "type 日本語 {\n    name String\n}"

	tests := []struct {
		name     string
		line     int
		charByte int
	}{
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
		{"start of line inside body", 1, 0},
		{"after indentation", 1, 4},
		{"after 'name '", 1, 9},
		{"at closing brace", 2, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, _ := DetectContext(textToDoc(text), tt.line, tt.charByte)
			assert.GreaterOrEqual(t, int(result), int(Unknown))
			assert.LessOrEqual(t, int(result), int(ImportPath))
		})
	}
}

func TestDetectContext_UTF8_Emoji(t *testing.T) {
	t.Parallel()

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
			result, _ := DetectContext(textToDoc(text), 0, tt.charByte)
			assert.GreaterOrEqual(t, int(result), int(Unknown))
			assert.LessOrEqual(t, int(result), int(ImportPath))
		})
	}
}
