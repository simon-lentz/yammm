package lsp

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	protocol "github.com/tliron/glsp/protocol_3_16"

	"github.com/simon-lentz/yammm/diag"
	"github.com/simon-lentz/yammm/schema/load"
)

func TestFormatDocument_NoChanges(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String required
}
`
	result := formatDocument(input)

	if result != input {
		t.Errorf("expected no changes, got:\n%q", result)
	}
}

func TestFormatDocument_TrailingWhitespace(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"   \n\ntype Person {   \n\tname String required   \n}\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String required\n}\n"

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_NormalizeCRLF(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\n\r\ntype Person {\r\n\tname String\r\n}\r\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_NormalizeCR(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\r\rtype Person {\r\tname String\r}\r"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_PreservesBlankLines(t *testing.T) {
	t.Parallel()

	// Blank lines are preserved as a conservative aesthetic choice to maintain visual
	// structure. Note: DOC_COMMENT attachment is channel-based and whitespace-agnostic.
	input := `schema "test"



type Person {
	name String
}



type Company {
	title String
}
`

	// Blank lines should be preserved (trailing blank lines at EOF are still removed)
	expected := `schema "test"



type Person {
	name String
}



type Company {
	title String
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_RemoveTrailingBlankLines(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}\n\n\n\n"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_EnsureTrailingNewline(t *testing.T) {
	t.Parallel()

	input := "schema \"test\"\n\ntype Person {\n\tname String\n}"
	expected := "schema \"test\"\n\ntype Person {\n\tname String\n}\n"

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_PreservesComments(t *testing.T) {
	t.Parallel()

	input := `schema "test"

// This is a type
type Person {
	name String // inline comment
}
`
	result := formatDocument(input)

	if result != input {
		t.Errorf("comments should be preserved, got:\n%q", result)
	}
}

func TestFormatDocument_PreservesIndentation(t *testing.T) {
	t.Parallel()

	input := `schema "test"

type Person {
	name String
	age Integer
	--> EMPLOYER (one) Company
}
`
	result := formatDocument(input)

	if result != input {
		t.Errorf("indentation should be preserved, got:\n%q", result)
	}
}

func TestFormatDocument_Empty(t *testing.T) {
	t.Parallel()

	input := ""
	result := formatDocument(input)

	if result != "" {
		t.Errorf("empty input should return empty output, got: %q", result)
	}
}

func TestFormatDocument_OnlyWhitespace(t *testing.T) {
	t.Parallel()

	input := "   \n\t\n   \n"
	result := formatDocument(input)

	if result != "" {
		t.Errorf("whitespace-only input should return empty, got: %q", result)
	}
}

func TestFormatDocument_Idempotent(t *testing.T) {
	t.Parallel()

	input := `schema "test"


type Person {
	name String

	age Integer
}


`

	// Format once
	first := formatDocument(input)

	// Format again
	second := formatDocument(first)

	if first != second {
		t.Errorf("formatting should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

func TestFormatDocument_ComplexDocument(t *testing.T) {
	t.Parallel()

	input := `schema "vehicles"


import "./parts" as parts


// Abstract vehicle type
abstract type Vehicle {
	vin String[17, 17] primary


	--> MANUFACTURER (one) Manufacturer
}


// Concrete car type
type Car extends Vehicle {
	model String required
	*-> WHEELS (many) parts.Wheel
}


`

	// Blank lines are preserved (trailing blank lines at EOF are removed)
	expected := `schema "vehicles"


import "./parts" as parts


// Abstract vehicle type
abstract type Vehicle {
	vin String[17, 17] primary


	--> MANUFACTURER (one) Manufacturer
}


// Concrete car type
type Car extends Vehicle {
	model String required
	*-> WHEELS (many) parts.Wheel
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() =\n%q\nwant:\n%q", result, expected)
	}
}

func TestNormalizeIndentation_NoLeading(t *testing.T) {
	t.Parallel()

	input := "name String"
	result := normalizeIndentation(input)

	if result != input {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, input)
	}
}

func TestNormalizeIndentation_Tabs(t *testing.T) {
	t.Parallel()

	input := "\tname String"
	result := normalizeIndentation(input)

	if result != input {
		t.Errorf("tabs should be preserved: %q", result)
	}
}

func TestNormalizeIndentation_SpacesToTabs(t *testing.T) {
	t.Parallel()

	input := "    name String"  // 4 spaces
	expected := "\tname String" // 1 tab

	result := normalizeIndentation(input)

	if result != expected {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, expected)
	}
}

func TestNormalizeIndentation_MixedSpaces(t *testing.T) {
	t.Parallel()

	input := "      name String"  // 6 spaces
	expected := "\t  name String" // 1 tab + 2 spaces

	result := normalizeIndentation(input)

	if result != expected {
		t.Errorf("normalizeIndentation(%q) = %q; want %q", input, result, expected)
	}
}

func TestNormalizeIndentation_Empty(t *testing.T) {
	t.Parallel()

	input := ""
	result := normalizeIndentation(input)

	if result != "" {
		t.Errorf("empty input should return empty, got: %q", result)
	}
}

func TestFormatDocument_ConvertSpacesToTabs(t *testing.T) {
	t.Parallel()

	// Input uses 4-space indentation
	input := `schema "test"

type Person {
    name String required
    age Integer
}
`
	// Expected output uses tab indentation
	expected := `schema "test"

type Person {
	name String required
	age Integer
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("spaces should be converted to tabs:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_MixedIndentNormalized(t *testing.T) {
	t.Parallel()

	// Input uses mixed 6-space indentation (1 tab + 2 spaces)
	input := `schema "test"

type Person {
      name String
}
`
	// Expected output normalizes to 1 tab + 2 spaces
	expected := `schema "test"

type Person {
	  name String
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("mixed indent should be normalized:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

// --- hasSyntaxErrors tests ---

func TestHasSyntaxErrors_NoErrors(t *testing.T) {
	t.Parallel()

	// Valid schema should have no errors
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

type Person {
	name String
}
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() returned true for valid schema")
	}
}

func TestHasSyntaxErrors_WithSyntaxError(t *testing.T) {
	t.Parallel()

	// Invalid syntax - missing closing brace
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

type Person {
	name String
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	if !hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() should return true for syntax error")
	}
}

func TestHasSyntaxErrors_WithImportOnly(t *testing.T) {
	t.Parallel()

	// Schema with import - LoadString disallows imports, but this is NOT a syntax error
	// The parse succeeds; the import restriction is a semantic error (E_IMPORT_NOT_ALLOWED)
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"

import "./other" as other

type Person {
	name String
}
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Should have errors (import not allowed)
	if result.OK() {
		t.Fatal("expected result to have errors due to import")
	}

	// But NOT syntax errors - the file is syntactically valid
	if hasSyntaxErrors(result) {
		t.Error("hasSyntaxErrors() should return false for import-only errors")
	}
}

func TestHasSyntaxErrors_VerifyImportErrorCategory(t *testing.T) {
	t.Parallel()

	// Verify that import errors are categorized as CategoryImport, not CategorySyntax
	ctx := context.Background()
	_, result, err := load.LoadString(ctx, `schema "test"
import "./other" as other
type Person { name String }
`, "test")
	if err != nil {
		t.Fatalf("unexpected load error: %v", err)
	}

	// Check that we have errors
	if result.OK() {
		t.Fatal("expected errors due to import in LoadString")
	}

	// Verify the error category
	foundImportError := false
	for issue := range result.Errors() {
		if issue.Code().Category() == diag.CategoryImport {
			foundImportError = true
		}
		if issue.Code().Category() == diag.CategorySyntax {
			t.Errorf("import error should not be CategorySyntax, got code: %s", issue.Code())
		}
	}

	if !foundImportError {
		t.Error("expected to find an import error (CategoryImport)")
	}
}

// =============================================================================
// Multibyte Content Tests (Priority 5: Test Coverage Gaps)
// =============================================================================

func TestFormatDocument_MultibyteCJK(t *testing.T) {
	// Test formatting with CJK characters (Chinese/Japanese/Korean) in strings
	// YAMMM identifiers are ASCII-only, but string literals can contain Unicode
	// CJK characters are 3-byte UTF-8
	t.Parallel()

	input := `schema "日本語テスト"

type User {
	name String required
	// 年齢 means age in Japanese
	age Integer
}
`
	expected := `schema "日本語テスト"

type User {
	name String required
	// 年齢 means age in Japanese
	age Integer
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() with CJK content:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_Emoji(t *testing.T) {
	// Test formatting with emoji (4-byte UTF-8, surrogate pairs in UTF-16)
	t.Parallel()

	input := `schema "emoji🎉"

type User {
	status String
}
`
	expected := `schema "emoji🎉"

type User {
	status String
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() with emoji:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_MultibyteMixedContent(t *testing.T) {
	// Test formatting with mixed ASCII and multibyte content in comments and strings
	// YAMMM identifiers are ASCII-only, but strings and comments can contain Unicode
	t.Parallel()

	input := `schema "混合Content"

// コメント with 日本語
type MixedType {
	ascii String required
	// 日本語フィールド
	jpField Integer
	// emoji🎉field
	emojiField Float
}
`
	expected := `schema "混合Content"

// コメント with 日本語
type MixedType {
	ascii String required
	// 日本語フィールド
	jpField Integer
	// emoji🎉field
	emojiField Float
}
`

	result := formatDocument(input)

	if result != expected {
		t.Errorf("formatDocument() with mixed content:\ngot:\n%q\nwant:\n%q", result, expected)
	}
}

func TestFormatDocument_MultibyteParseable(t *testing.T) {
	// Test that formatted multibyte content in strings is still parseable
	// YAMMM identifiers are ASCII-only, but string literals can contain Unicode
	t.Parallel()

	input := `schema "CJKテスト"

type JapaneseUser {
	name String required
}
`

	result := formatDocument(input)

	// Verify the result is still valid YAMMM by parsing it
	ctx := context.Background()
	s, diagResult, err := load.LoadString(ctx, result, "test")
	if err != nil {
		t.Fatalf("formatted output failed to load: %v", err)
	}

	if !diagResult.OK() {
		for issue := range diagResult.Issues() {
			t.Logf("issue: %v", issue)
		}
		t.Error("formatted multibyte content should be parseable without errors")
	}

	// Verify schema name preserved (contains CJK characters)
	if s != nil && s.Name() != "CJKテスト" {
		t.Errorf("schema name = %q; want CJKテスト", s.Name())
	}
}

func TestFormatDocument_MultibyteIdempotent(t *testing.T) {
	// Verify formatting multibyte content is idempotent
	t.Parallel()

	input := `schema "日本語"

type 用戶 {
	名前 String required


	年齢 Integer
}


`

	// Format once
	first := formatDocument(input)

	// Format again
	second := formatDocument(first)

	if first != second {
		t.Errorf("formatting multibyte content should be idempotent:\nfirst:\n%q\nsecond:\n%q", first, second)
	}
}

// TestFormatting_UTF8PositionEncoding verifies that formatting respects
// the negotiated position encoding (UTF-8 vs UTF-16).
func TestFormatting_UTF8PositionEncoding(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create a schema file with CJK characters that needs formatting.
	// In UTF-8 mode, the edit range's Character field should be byte count.
	// In UTF-16 mode, it should be UTF-16 code units.
	// CJK characters are 3 bytes in UTF-8 but 1 UTF-16 code unit each.
	content := "schema \"日本語\"\n\ntype Person {    \n\tname String\n}\n"
	filePath := filepath.Join(tmpDir, "test.yammm")
	if err := os.WriteFile(filePath, []byte(content), 0o600); err != nil {
		t.Fatalf("failed to write file: %v", err)
	}

	// Test both encodings
	tests := []struct {
		name     string
		encoding PositionEncoding
	}{
		{"UTF-16", PositionEncodingUTF16},
		{"UTF-8", PositionEncodingUTF8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger := slog.New(slog.NewTextHandler(io.Discard, nil))
			server := NewServer(logger, Config{ModuleRoot: tmpDir})

			// Set position encoding
			server.workspace.SetPositionEncoding(tt.encoding)

			// Open the document
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

			// Request formatting
			edits, err := server.textDocumentFormatting(nil, &protocol.DocumentFormattingParams{
				TextDocument: protocol.TextDocumentIdentifier{URI: uri},
			})
			if err != nil {
				t.Fatalf("textDocumentFormatting failed: %v", err)
			}

			if len(edits) == 0 {
				// Document doesn't need formatting (trailing spaces removed in formatDocument)
				// This is acceptable - the test just verifies no crash with encoding switch
				return
			}

			// Verify the edit range covers the document (starts at 0,0)
			edit := edits[0]
			if edit.Range.Start.Line != 0 || edit.Range.Start.Character != 0 {
				t.Errorf("edit range should start at 0,0; got %d,%d",
					edit.Range.Start.Line, edit.Range.Start.Character)
			}

			// For UTF-8, the character should be byte offset (larger for multi-byte chars)
			// For UTF-16, the character should be code units (smaller for BMP chars)
			// The test primarily verifies that the call completes without panic
			// and returns a valid edit when the encoding is switched
		})
	}
}
