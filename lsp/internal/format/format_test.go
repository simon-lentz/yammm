package format

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/simon-lentz/yammm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTokenStream_NoChanges(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String required
}
`
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, input, tsResult, "formatTokenStream: expected no changes")
}

func TestTokenStream_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"   \n\ntype Person {   \n\tname String required   \n}\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String required\n}\n"

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream()")
}

func TestTokenStream_NormalizeCRLF(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\n\r\ntype Person {\r\n\tname String\r\n}\r\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream()")
}

func TestTokenStream_NormalizeCR(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\rtype Person {\r\tname String\r}\r"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream()")
}

func TestTokenStream_PreservesBlankLines(t *testing.T) {
	t.Parallel()

	input, tsExpected := loadTestPair(t, "preserves_blank_lines")

	// TokenStream collapses blank lines (max 1 blank between declarations)
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, tsExpected, tsResult, "TokenStream()")
}

func TestTokenStream_RemoveTrailingBlankLines(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}\n\n\n\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream()")
}

func TestTokenStream_EnsureTrailingNewline(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream()")
}

func TestTokenStream_PreservesComments(t *testing.T) {
	t.Parallel()

	input := `schema "test"

// This is a type
type Person {
	name String // inline comment
}
`
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, input, tsResult, "formatTokenStream: comments should be preserved")
}

func TestTokenStream_PreservesIndentation(t *testing.T) {
	t.Parallel()

	input, tsExpected := loadTestPair(t, "preserves_indentation")

	// TokenStream aligns name column within same-kind groups
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, tsExpected, tsResult, "TokenStream()")
}

func TestTokenStream_IdempotentInline(t *testing.T) {
	t.Parallel()

	input := `schema "test"


type Person {
	name String

	age Integer
}


`

	// formatTokenStream idempotency
	tsFirst, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream first pass returned error")
	tsSecond, err := TokenStream(tsFirst)
	require.NoError(t, err, "formatTokenStream second pass returned error")
	assert.Equal(t, tsFirst, tsSecond, "formatTokenStream should be idempotent")
}

func TestTokenStream_ComplexDocument(t *testing.T) {
	t.Parallel()

	input, tsExpected := loadTestPair(t, "complex_document")

	// TokenStream collapses double blanks to single
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, tsExpected, tsResult, "TokenStream()")
}

func TestTokenStream_DeclarationSpacing(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "declaration_spacing")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")

	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_ExpressionPreservation(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "expression_preservation")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")

	assert.Equal(t, expected, result, "TokenStream()")

	assert.Contains(t, result, `! "must_be_enabled" !disabled && active`, "logical NOT spacing should be preserved")
	assert.Contains(t, result, `{ "adult" : "minor" }`, "ternary brace/colon spacing should be preserved")
}

func TestTokenStream_CommentHandling(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "comment_handling")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")

	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_CollapsesBlankLines(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "collapses_blank_lines")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_BlankLinesAtStartOfFile(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "blank_lines_at_start")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_NoBlankAfterOpenBrace(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "no_blank_after_open_brace")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_NoBlankBeforeCloseBrace(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "no_blank_before_close_brace")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_EnsureBlankAfterSchema(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "ensure_blank_after_schema")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_EnsureBlankAfterImportBlock(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "ensure_blank_after_import_block")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_ImportGroupingPreserved(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "import_grouping_preserved")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_CommentNotCollapsedAsBlank(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "comment_not_collapsed_as_blank")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_DocCommentMultilineNotCollapsed(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "doc_comment_multiline_not_collapsed")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_EdgePropertyBlockBlanks(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "edge_property_block_blanks")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_GoldenFile(t *testing.T) {
	t.Parallel()

	unformatted, err := os.ReadFile("../../testdata/lsp/formatting/unformatted.yammm")
	require.NoError(t, err, "failed to read unformatted fixture")
	golden, err := os.ReadFile("../../testdata/lsp/formatting/formatted.yammm.golden")
	require.NoError(t, err, "failed to read golden fixture")

	result, err := TokenStream(string(unformatted))
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, string(golden), result, "TokenStream(unformatted) != golden")
}

func TestTokenStream_GoldenIdempotent(t *testing.T) {
	t.Parallel()

	golden, err := os.ReadFile("../../testdata/lsp/formatting/formatted.yammm.golden")
	require.NoError(t, err, "failed to read golden fixture")

	result, err := TokenStream(string(golden))
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, string(golden), result, "TokenStream(golden) != golden")
}

func TestTokenStream_GoldenFixtures(t *testing.T) {
	t.Parallel()

	fixtures := []string{
		"alignment",
		"wrapping",
		"expressions",
		"edge_cases",
		"comprehensive",
	}

	for _, name := range fixtures {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			inputPath := filepath.Join("../../testdata", "lsp", "formatting", name+".yammm")
			goldenPath := filepath.Join("../../testdata", "lsp", "formatting", name+".yammm.golden")

			input, err := os.ReadFile(inputPath)
			require.NoError(t, err, "failed to read fixture %s", name)
			golden, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "failed to read golden %s", name)

			result, err := TokenStream(string(input))
			require.NoError(t, err, "formatTokenStream returned error")
			assert.Equal(t, string(golden), result, "TokenStream(%s) != golden", name)
		})
	}
}

func TestTokenStream_GoldenIdempotentAll(t *testing.T) {
	t.Parallel()

	goldenFiles := []string{
		"formatted.yammm.golden",
		"alignment.yammm.golden",
		"wrapping.yammm.golden",
		"expressions.yammm.golden",
		"edge_cases.yammm.golden",
		"comprehensive.yammm.golden",
	}

	for _, name := range goldenFiles {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			goldenPath := filepath.Join("../../testdata", "lsp", "formatting", name)
			golden, err := os.ReadFile(goldenPath)
			require.NoError(t, err, "failed to read golden %s", name)

			result, err := TokenStream(string(golden))
			require.NoError(t, err, "formatTokenStream returned error")
			assert.Equal(t, string(golden), result, "TokenStream(%s) not idempotent", name)
		})
	}
}

func TestTokenStream_BlankLineCollapsingIdempotent(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "blank_line_collapsing_idempotent")

	first, err := TokenStream(input)
	require.NoError(t, err, "TokenStream first pass returned error")
	assert.Equal(t, expected, first, "TokenStream()")

	second, err := TokenStream(first)
	require.NoError(t, err, "TokenStream second pass returned error")
	assert.Equal(t, first, second, "blank line collapsing should be idempotent")
}

func TestTokenStream_Idempotent(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "fts_idempotent")

	first, err := TokenStream(input)
	require.NoError(t, err, "TokenStream first pass returned error")
	assert.Equal(t, expected, first, "TokenStream()")

	second, err := TokenStream(first)
	require.NoError(t, err, "TokenStream second pass returned error")
	assert.Equal(t, first, second, "TokenStream should be idempotent")
}

func TestTokenStream_InvalidInputReturnsError(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
`

	_, err := TokenStream(input)
	require.Error(t, err, "expected formatTokenStream to return error for malformed input")
}

func TestTokenStream_ColonInMultiplicity(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "colon_in_multiplicity")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_QualifiedReferences(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "qualified_references")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_ImportSpacing(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "import_spacing")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_ExtendsMultipleTypes(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "extends_multiple_types")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_AllConstraintBracketTypes(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "all_constraint_bracket_types")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_ListAngleBracketSpacing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name: "basic list",
			input: `schema "test"

type T {
	tags List <String>
}
`,
			expected: `schema "test"

type T {
	tags List<String>
}
`,
		},
		{
			name: "list with element constraint",
			input: `schema "test"

type T {
	tags List <String[_, 6]>
}
`,
			expected: `schema "test"

type T {
	tags List<String[_, 6]>
}
`,
		},
		{
			name: "list with bounds",
			input: `schema "test"

type T {
	tags List <String> [1, 5]
}
`,
			expected: `schema "test"

type T {
	tags List<String>[1, 5]
}
`,
		},
		{
			name: "nested list",
			input: `schema "test"

type T {
	matrix List <List <Integer>>
}
`,
			expected: `schema "test"

type T {
	matrix List<List<Integer>>
}
`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := TokenStream(tt.input)
			require.NoError(t, err, "formatTokenStream returned error")
			assert.Equal(t, tt.expected, result, "TokenStream()")
		})
	}
}

func TestTokenStream_DOCCommentNewlineAfter(t *testing.T) {
	t.Parallel()

	// Verify DOC_COMMENT always gets a newline before the next declaration token.
	input, expected := loadTestPair(t, "doc_comment_newline_after")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_TrailingCommaInConstraints(t *testing.T) {
	t.Parallel()

	// Trailing comma inside Enum is grammar-legal and should be tight before RBRACK.
	input, expected := loadTestPair(t, "trailing_comma_in_constraints")

	result, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestNormalizeIndentation_NoLeading(t *testing.T) {
	t.Parallel()

	input := "name String"
	result := NormalizeIndentation(input)

	assert.Equal(t, input, result, "NormalizeIndentation(%q)", input)
}

func TestNormalizeIndentation_Tabs(t *testing.T) {
	t.Parallel()

	input := "\tname String"
	result := NormalizeIndentation(input)

	assert.Equal(t, input, result, "tabs should be preserved")
}

func TestNormalizeIndentation_SpacesToTabs(t *testing.T) {
	t.Parallel()

	input := "    name String"  // 4 spaces
	expected := "\tname String" // 1 tab

	result := NormalizeIndentation(input)

	assert.Equal(t, expected, result, "NormalizeIndentation(%q)", input)
}

func TestNormalizeIndentation_MixedSpaces(t *testing.T) {
	t.Parallel()

	input := "      name String"  // 6 spaces
	expected := "\t  name String" // 1 tab + 2 spaces

	result := NormalizeIndentation(input)

	assert.Equal(t, expected, result, "NormalizeIndentation(%q)", input)
}

func TestNormalizeIndentation_Empty(t *testing.T) {
	t.Parallel()

	input := ""
	result := NormalizeIndentation(input)

	assert.Equal(t, "", result, "empty input should return empty")
}

func TestTokenStream_ConvertSpacesToTabs(t *testing.T) {
	t.Parallel()

	input, tsExpected := loadTestPair(t, "convert_spaces_to_tabs")

	// TokenStream: tab indentation + name column alignment
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, tsExpected, tsResult, "TokenStream()")
}

func TestTokenStream_MixedIndentNormalized(t *testing.T) {
	t.Parallel()

	input, tsExpected := loadTestPair(t, "mixed_indent_normalized")

	// TokenStream uses canonical brace-depth indentation
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, tsExpected, tsResult, "TokenStream()")
}

func TestTokenStream_MultibyteCJK(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "multibyte_cjk")

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream() with CJK content")
}

func TestTokenStream_Emoji(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "emoji")

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream() with emoji")
}

func TestTokenStream_MultibyteMixedContent(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "multibyte_mixed_content")

	tsResult, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, tsResult, "TokenStream() with mixed content")
}

func TestTokenStream_MultibyteParseable(t *testing.T) {
	// Test that formatted multibyte content in strings is still parseable
	// YAMMM identifiers are ASCII-only, but string literals can contain Unicode
	t.Parallel()

	input := `schema "CJKテスト"

type JapaneseUser {
	name String required
}
`

	ctx := t.Context()

	// Verify formatTokenStream result is parseable
	tsResult, err := TokenStream(input)
	require.NoError(t, err, "formatTokenStream returned error")
	s, diagResult, err := schema.LoadString(ctx, tsResult, "test")
	require.NoError(t, err, "formatTokenStream output failed to load")
	if !diagResult.OK() {
		for issue := range diagResult.Issues() {
			t.Logf("issue: %v", issue)
		}
		assert.Fail(t, "formatTokenStream: formatted multibyte content should be parseable without errors")
	}
	if s != nil {
		assert.Equal(t, "CJKテスト", s.Name(), "formatTokenStream: schema name")
	}
}

// TestFormatting_UTF8PositionEncoding verifies that formatting respects
// the negotiated position encoding (UTF-8 vs UTF-16).
func TestAlignColumns_PropertyNamePadding(t *testing.T) {
	t.Parallel()

	input := "\tname String required\n\tage Integer\n\tscore Float[0.0, 100.0]\n"
	expected := "\tname  String required\n\tage   Integer\n\tscore Float[0.0, 100.0]\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_PropertyInlineCommentAlignment(t *testing.T) {
	t.Parallel()

	input := "\tname String required // the name\n\tage Integer // age\n"
	// name(4), age(3) -> max 4. Comments align to common column.
	// name content: "\tname String required" (21 chars)
	// age content:  "\tage  Integer" (13 chars)
	// comment col = 21 + 1 = 22
	expected := "\tname String required // the name\n\tage  Integer         // age\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_RelationshipNamePadding(t *testing.T) {
	t.Parallel()

	input := "\t--> REL (_:many) Target\n\t--> REL2 (_:one) Target\n\t*-> REL3 (one:many) Target\n"
	// REL(3), REL2(4), REL3(4) -> max 4
	expected := "\t--> REL  (_:many) Target\n\t--> REL2 (_:one) Target\n\t*-> REL3 (one:many) Target\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_AliasNamePadding(t *testing.T) {
	t.Parallel()

	input := "type Email = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n"
	// Email(5), StateFP(7) -> max 7
	expected := "type Email   = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_GroupBreakAtBlankLine(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\n\tage Integer\n"
	// Blank line splits into two singleton groups -> no alignment
	expected := "\tname String\n\n\tage Integer\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_GroupBreakAtComment(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\t// standalone comment\n\tage Integer\n"
	// Comment-only line splits properties into separate groups
	expected := "\tname String\n\t// standalone comment\n\tage Integer\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_GroupBreakAtKindChange(t *testing.T) {
	t.Parallel()

	input := "\tname String\n\tage Integer\n\t--> REL (one) Target\n\t--> REL2 (many) Target\n"
	// Properties: name(4), age(3) -> max 4
	// Then kind change -> relationships: REL(3), REL2(4) -> max 4
	expected := "\tname String\n\tage  Integer\n\t--> REL  (one) Target\n\t--> REL2 (many) Target\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_MultilineBreaksGroup(t *testing.T) {
	t.Parallel()

	// Unbalanced [ on a line -> excluded from alignment, breaks groups
	input := "\tname String\n\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\"\n\t]\n\tage Integer\n"
	// name and age are in separate groups (multiline in between)
	expected := "\tname String\n\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\"\n\t]\n\tage Integer\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_EdgePropertyBlockAlignment(t *testing.T) {
	t.Parallel()

	// Properties inside { } edge blocks aligned at indent level 2
	input := "\t--> REL (one) Target {\n\t\tweight Float required\n\t\tscore Integer\n\t}\n"
	// Edge block: weight(6), score(5) -> max 6
	expected := "\t--> REL (one) Target {\n\t\tweight Float required\n\t\tscore  Integer\n\t}\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_SingleMemberNoChange(t *testing.T) {
	t.Parallel()

	input := "\tname String required\n"
	expected := "\tname String required\n"

	result := AlignColumns(input)
	assert.Equal(t, expected, result, "AlignColumns()")
}

func TestAlignColumns_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"\tname String required\n\tage Integer\n\tscore Float\n",
		"\t--> REL (_:many) Target\n\t--> REL2 (_:one) Target\n",
		"type Email = Pattern[\"^.+@.+$\"]\ntype StateFP = String[2, 2]\n",
		"\tname String // the name\n\tage Integer // age\n",
	}

	for _, input := range inputs {
		first := AlignColumns(input)
		second := AlignColumns(first)
		assert.Equal(t, first, second, "alignColumns not idempotent for input:\n%q", input)
	}
}

func TestAlignColumns_EmptyAndPassthrough(t *testing.T) {
	t.Parallel()

	// Empty string
	assert.Equal(t, "", AlignColumns(""), "empty input should return empty")

	// Non-alignable content passes through unchanged
	nonAlignable := "schema \"test\"\n\n// comment\n! \"msg\" expr\n}\n"
	assert.Equal(t, nonAlignable, AlignColumns(nonAlignable), "non-alignable input should pass through unchanged")
}

// --- displayWidth ---

func TestDisplayWidth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"empty", "", 0},
		{"ascii", "hello", 5},
		{"tab", "\t", 4},
		{"tab_and_text", "\tname String", 15},
		{"two_tabs", "\t\tvalue", 13},
		{"mixed", "\t  abc", 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := DisplayWidth(tt.input)
			assert.Equal(t, tt.want, got, "DisplayWidth(%q)", tt.input)
		})
	}
}

// --- Enum wrapping tests ---

func TestWrapLongLines_ShortEnumUnchanged(t *testing.T) {
	t.Parallel()

	// Short Enum (well under 100 chars) should pass through unchanged
	input := "\tstatus Enum[\"active\", \"inactive\"] required\n"
	result := WrapLongLines(input)
	assert.Equal(t, input, result, "short Enum should be unchanged")
}

func TestWrapLongLines_LongEnumWraps(t *testing.T) {
	t.Parallel()

	// Build a long Enum that exceeds 100 chars
	input := "\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\", \"deleted\"] required\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// Should be wrapped: Enum[ on first line, values indented, ] with modifier
	assert.Contains(t, result, "Enum[\n", "expected Enum[ on first line with newline after")
	assert.Contains(t, result, "\t\t\"pending_review\",\n", "expected values indented with trailing commas")
	assert.Contains(t, result, "\t\t\"deleted\",\n", "expected last value with trailing comma")
	assert.Contains(t, result, "\t] required\n", "expected ] with modifier on closing line")
}

func TestWrapLongLines_EnumCollapseToSingleLine(t *testing.T) {
	t.Parallel()

	// Multiline Enum that would fit on a single line
	input := "\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t] required\n"
	result := WrapLongLines(input)

	// Should be collapsed to single line (no trailing comma in single-line form)
	expected := "\tstatus Enum[\"a\", \"b\"] required\n"
	assert.Equal(t, expected, result, "multiline Enum should collapse")
}

func TestWrapLongLines_EnumCollapseStillLong(t *testing.T) {
	t.Parallel()

	// Multiline Enum that's still too long when collapsed -> re-canonicalize
	input := "\tstatus Enum[\n\t\t\"pending_review\",\n\t\t\"approved\",\n\t\t\"rejected\",\n\t\t\"needs_revision\",\n\t\t\"escalated\",\n\t\t\"archived\",\n\t\t\"deleted\",\n\t] required\n"

	result := WrapLongLines(input)

	// Should stay multiline with canonical form
	assert.Contains(t, result, "Enum[\n", "long Enum should stay multiline")
	assert.Contains(t, result, "\t] required\n", "expected ] with modifier")
}

func TestWrapLongLines_EnumEscapedQuotes(t *testing.T) {
	t.Parallel()

	// Values with escaped quotes should not break the parser
	input := "\tval Enum[\"say \\\"hello\\\"\", \"say \\\"bye\\\"\", \"normal\", \"another\", \"more_values\", \"extra_long_value_here\", \"padding_it\"] required\n"
	result := WrapLongLines(input)

	if DisplayWidth(strings.TrimSuffix(input, "\n")) > LineWidthThreshold {
		// Should be wrapped
		assert.Contains(t, result, "Enum[\n", "long Enum with escaped quotes should wrap")
		// Escaped quotes preserved
		assert.Contains(t, result, "\\\"hello\\\"", "escaped quotes should be preserved")
	}
}

func TestWrapLongLines_EnumInlineComment(t *testing.T) {
	t.Parallel()

	// Enum with inline comment — comment should reattach to ] line
	input := "\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\"] required // status field\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// Comment should be on the closing line
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	lastLine := lines[len(lines)-1]
	assert.Contains(t, lastLine, "] required // status field", "inline comment should reattach to ] line")
}

// --- Extends wrapping tests ---

func TestWrapLongLines_ShortExtendsUnchanged(t *testing.T) {
	t.Parallel()

	input := "type Concrete extends Base, Audit {\n"
	result := WrapLongLines(input)
	assert.Equal(t, input, result, "short extends should be unchanged")
}

func TestWrapLongLines_LongExtendsWraps(t *testing.T) {
	t.Parallel()

	input := "type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable, Publishable {\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	assert.Contains(t, result, "type ComplexEntity extends\n", "expected header on first line")
	assert.Contains(t, result, "\tAuditable,\n", "expected types indented with trailing comma")
	assert.Contains(t, result, "\tPublishable,\n", "expected last type with trailing comma")
	// { on own line
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	assert.Equal(t, "{", lastLine, "expected { on own line")
}

func TestWrapLongLines_ExtendsCollapseToSingleLine(t *testing.T) {
	t.Parallel()

	// Multiline extends that fits on one line
	input := "type Concrete extends\n\tBase,\n\tAudit,\n{\n"
	result := WrapLongLines(input)

	expected := "type Concrete extends Base, Audit {\n"
	assert.Equal(t, expected, result, "multiline extends should collapse")
}

func TestWrapLongLines_ExtendsCollapseStillLong(t *testing.T) {
	t.Parallel()

	// Multiline extends that's still too long when collapsed
	input := "type ComplexEntity extends\n\tAuditable,\n\tTrackable,\n\tValidatable,\n\tSerializable,\n\tCacheable,\n\tObservable,\n\tPublishable,\n{\n"
	result := WrapLongLines(input)

	// Should stay multiline
	assert.Contains(t, result, "type ComplexEntity extends\n", "long extends should stay multiline")
	assert.Contains(t, result, "{\n", "expected { on last line")
}

func TestWrapLongLines_ExtendsQualifiedTypes(t *testing.T) {
	t.Parallel()

	// Extends with qualified types (base.Type)
	input := "type ComplexEntity extends base.Auditable, other.Trackable, third.Validatable, fourth.Serializable, fifth.Cacheable {\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	assert.Contains(t, result, "\tbase.Auditable,\n", "qualified types should be preserved")
	assert.Contains(t, result, "\tother.Trackable,\n", "qualified types should be preserved")
}

func TestWrapLongLines_ExtendsAbstractType(t *testing.T) {
	t.Parallel()

	input := "abstract type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable {\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	assert.Contains(t, result, "abstract type ComplexEntity extends\n", "abstract prefix should be preserved")
}

// --- Datatype alias tests ---

func TestWrapLongLines_ShortDatatypeAliasUnchanged(t *testing.T) {
	t.Parallel()

	input := "type Status = Enum[\"active\", \"inactive\"]\n"
	result := WrapLongLines(input)
	assert.Equal(t, input, result, "short alias should be unchanged")
}

func TestWrapLongLines_LongDatatypeAliasWraps(t *testing.T) {
	t.Parallel()

	input := "type DeactivatedReason = Enum[\"removed_from_source\", \"matured\", \"merged\", \"manual\", \"superseded\", \"error_corrected\"]\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	assert.Contains(t, result, "= Enum[\n", "expected Enum[ on first line")
	assert.Contains(t, result, "\t\"removed_from_source\",\n", "expected values indented")
}

func TestWrapLongLines_DatatypeAliasCollapses(t *testing.T) {
	t.Parallel()

	// Multiline alias that fits on one line
	input := "type Status = Enum[\n\t\"a\",\n\t\"b\",\n]\n"
	result := WrapLongLines(input)

	expected := "type Status = Enum[\"a\", \"b\"]\n"
	assert.Equal(t, expected, result, "multiline alias should collapse")
}

// --- Invariant wrapping tests ---

func TestWrapLongLines_ShortInvariantUnchanged(t *testing.T) {
	t.Parallel()

	input := "\t! \"check\" a > 0 && b < 100\n"
	result := WrapLongLines(input)
	assert.Equal(t, input, result, "short invariant should be unchanged")
}

func TestWrapLongLines_LongInvariantWrapsAtOr(t *testing.T) {
	t.Parallel()

	input := "\t! \"geo_check\" (geo_type == \"state\" && Len(geoid) == 2) || (geo_type == \"county\" && Len(geoid) == 5) || (geo_type == \"place\" && Len(geoid) == 7)\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// First line should be just the prefix
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected multiple lines")
	assert.Equal(t, "! \"geo_check\"", strings.TrimSpace(lines[0]), "first line should be just the prefix")
	// Operator should be at end of line
	assert.True(t, strings.HasSuffix(strings.TrimSpace(lines[1]), "||"), "operator should be at end of line, got: %q", lines[1])
}

func TestWrapLongLines_LongInvariantWrapsAtAnd(t *testing.T) {
	t.Parallel()

	input := "\t! \"complex_check\" very_long_field_name_one == \"expected_value_one\" && very_long_field_name_two == \"expected_value_two\" && third_field > 0\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 3, "expected at least 3 lines")
	// Check operators at end of continuation lines
	for _, line := range lines[1 : len(lines)-1] {
		trimmed := strings.TrimSpace(line)
		assert.True(t, strings.HasSuffix(trimmed, "&&") || strings.HasSuffix(trimmed, "||"),
			"continuation line should end with operator, got: %q", trimmed)
	}
}

func TestWrapLongLines_InvariantNeverCollapse(t *testing.T) {
	t.Parallel()

	// Multiline invariant should pass through unchanged — never collapse
	input := "\t! \"check\"\n\t\ta > 0 &&\n\t\tb < 100\n"
	result := WrapLongLines(input)
	assert.Equal(t, input, result, "multiline invariant should not be collapsed")
}

func TestWrapLongLines_InvariantNestedOpsSkipped(t *testing.T) {
	t.Parallel()

	// && inside () should NOT be a wrap point — only top-level operators
	input := "\t! \"check\" (very_long_condition_name && another_very_long_condition_name) || (yet_another_long_condition && final_long_condition_name)\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// Should wrap at || but NOT at && inside parens
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")

	// Verify the || is a wrap point
	foundOr := false
	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		if strings.HasSuffix(trimmed, "||") {
			foundOr = true
		}
		// Continuation lines containing && should also contain surrounding parens
		// (meaning the && is nested, not top-level)
		if strings.Contains(trimmed, "&&") && !strings.Contains(trimmed, "(") {
			assert.Fail(t, "& outside parens should not appear on a continuation line", "line: %q", trimmed)
		}
	}
	assert.True(t, foundOr, "expected || as wrap point:\n%s", result)
}

func TestWrapLongLines_InvariantNoTopLevelOps(t *testing.T) {
	t.Parallel()

	// Very long invariant with no top-level && or || -> left as-is
	input := "\t! \"check\" Len(very_long_field_name_that_makes_line_exceed_one_hundred_characters_by_quite_a_bit_actually) > 0\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// Should pass through unchanged (no operators to wrap at)
	assert.Equal(t, input, result, "invariant with no top-level ops should be unchanged")
}

func TestWrapLongLines_InvariantBraceExprPreserved(t *testing.T) {
	t.Parallel()

	// && inside { } braces (lambdas) should not be wrap points
	input := "\t! \"all_valid\" ITEMS -> All |$item| { $item.qty > 0 && $item.price > 0 }\n"
	result := WrapLongLines(input)

	// Under 100 chars, should pass through unchanged
	assert.Equal(t, input, result, "invariant with brace expr should be unchanged")
}

func TestWrapLongLines_InvariantBracketExprPreserved(t *testing.T) {
	t.Parallel()

	// && and || inside [] (list literals) should NOT be top-level wrap points
	input := "\t! \"list_logic\" value in [cond_a && cond_b, cond_c || cond_d] || very_long_field_name_that_pushes_past_the_one_hundred_character_threshold > 0\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input should exceed threshold")

	result := WrapLongLines(input)

	// Should wrap at the top-level || but NOT at && or || inside brackets
	lines := strings.Split(strings.TrimSuffix(result, "\n"), "\n")
	require.GreaterOrEqual(t, len(lines), 2, "expected multiple lines")

	for _, line := range lines[1:] {
		trimmed := strings.TrimSpace(line)
		// No continuation line should contain bracketed operators split out
		assert.False(t, strings.HasPrefix(trimmed, "cond_a &&"),
			"operator inside brackets was treated as top-level wrap point: %q", trimmed)
		assert.False(t, strings.HasPrefix(trimmed, "cond_c ||"),
			"operator inside brackets was treated as top-level wrap point: %q", trimmed)
	}
}

func TestWrapLongLines_InvariantRegexLiteralPreserved(t *testing.T) {
	t.Parallel()

	// || inside /regex/ must NOT be treated as a top-level wrap point
	input := "\t! \"pattern_check\" field =~ /very_long_pattern_foo||bar_baz_qux/ && other_very_long_field_name_exceeding_threshold > 0\n"
	require.Greater(t, DisplayWidth(strings.TrimSuffix(input, "\n")), LineWidthThreshold, "test input must exceed threshold")

	result := WrapLongLines(input)

	for line := range strings.SplitSeq(strings.TrimSuffix(result, "\n"), "\n") {
		trimmed := strings.TrimSpace(line)
		assert.Equal(t, 0, strings.Count(trimmed, "/")%2, "regex literal split across lines: %q", trimmed)
	}
}

// --- Integration / edge case tests ---

func TestWrapLongLines_NonWrappable(t *testing.T) {
	t.Parallel()

	// Long Pattern or long string — not wrappable, left as-is
	input := "\tregex Pattern[\"^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\\\.[a-zA-Z]{2,}$\"] required // this is a very very long line that exceeds\n"

	result := WrapLongLines(input)
	assert.Equal(t, input, result, "non-wrappable line should pass through")
}

func TestWrapLongLines_Idempotent(t *testing.T) {
	t.Parallel()

	inputs := []string{
		// Long Enum
		"\tstatus Enum[\"pending_review\", \"approved\", \"rejected\", \"needs_revision\", \"escalated\", \"archived\", \"deleted\"] required\n",
		// Long extends
		"type ComplexEntity extends Auditable, Trackable, Validatable, Serializable, Cacheable, Observable, Publishable {\n",
		// Long alias
		"type DeactivatedReason = Enum[\"removed_from_source\", \"matured\", \"merged\", \"manual\", \"superseded\", \"error_corrected\"]\n",
		// Long invariant
		"\t! \"geo_check\" (geo_type == \"state\" && Len(geoid) == 2) || (geo_type == \"county\" && Len(geoid) == 5) || (geo_type == \"place\" && Len(geoid) == 7)\n",
		// Multiline Enum (collapsible)
		"\tstatus Enum[\n\t\t\"a\",\n\t\t\"b\",\n\t] required\n",
		// Multiline extends (collapsible)
		"type Concrete extends\n\tBase,\n\tAudit,\n{\n",
		// Short (unchanged)
		"\tname String required\n",
	}

	for _, input := range inputs {
		first := WrapLongLines(input)
		second := WrapLongLines(first)
		assert.Equal(t, first, second, "wrapLongLines not idempotent for input:\n%q", input)
	}
}

func TestWrapLongLines_EmptyAndPassthrough(t *testing.T) {
	t.Parallel()

	// Empty string
	assert.Equal(t, "", WrapLongLines(""), "empty input should return empty")

	// Non-wrappable content passes through unchanged
	input := "schema \"test\"\n\ntype T {\n\tname String\n}\n"
	assert.Equal(t, input, WrapLongLines(input), "short content should pass through")
}

func TestWrapLongLines_ExactlyAtThreshold(t *testing.T) {
	t.Parallel()

	// Build a line that's exactly 100 chars — should NOT be wrapped
	// "\tstatus Enum[...]" — pad to exactly 100 display width
	// Tab = 4, so we need 96 more chars after tab
	line := "\t" + strings.Repeat("x", 96) + "\n"
	require.Equal(t, 100, DisplayWidth(strings.TrimSuffix(line, "\n")), "test line should be exactly 100 chars")

	result := WrapLongLines(line)
	assert.Equal(t, line, result, "line at exactly 100 chars should NOT be wrapped")
}

// --- Full pipeline tests ---

func TestTokenStream_WrapLongEnum(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "wrap_long_enum")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_CollapseShortMultilineEnum(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "collapse_short_multiline_enum")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")
}

func TestTokenStream_WrapAndAlignInteraction(t *testing.T) {
	t.Parallel()

	input, expected := loadTestPair(t, "wrap_and_align_interaction")

	result, err := TokenStream(input)
	require.NoError(t, err, "TokenStream returned error")
	assert.Equal(t, expected, result, "TokenStream()")

	// Verify idempotency of full pipeline
	second, err := TokenStream(result)
	require.NoError(t, err, "TokenStream second pass returned error")
	assert.Equal(t, result, second, "full pipeline should be idempotent")
}
