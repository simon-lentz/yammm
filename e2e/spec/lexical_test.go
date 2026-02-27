package spec_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLexical_SchemaCompilation verifies that all lexical element schemas
// load cleanly, proving that comments, identifiers, and literals are
// lexed correctly per SPEC.md Section 2 (Lexical Elements).
func TestLexical_SchemaCompilation(t *testing.T) {
	t.Parallel()

	schemas := []struct {
		name string
		path string
	}{
		{"line_comments", "testdata/lexical/line_comments.yammm"},
		{"block_comments", "testdata/lexical/block_comments.yammm"},
		{"doc_comment", "testdata/lexical/doc_comment.yammm"},
		{"identifier_forms", "testdata/lexical/identifier_forms.yammm"},
		{"lc_keyword_property", "testdata/lexical/lc_keyword_property.yammm"},
		{"literals", "testdata/lexical/literals.yammm"},
	}

	for _, tc := range schemas {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// loadSchema fails the test if compilation produces any errors.
			loadSchema(t, tc.path)
		})
	}
}

// TestLexical_DocCommentPreserved verifies that a block comment immediately
// preceding a type declaration is captured as documentation on the Type
// object (SPEC.md Section 2.2: "A block comment immediately before a
// declaration is a doc comment").
func TestLexical_DocCommentPreserved(t *testing.T) {
	t.Parallel()

	s, _ := loadSchemaRaw(t, "testdata/lexical/doc_comment.yammm")
	typ, ok := s.Type("Documented")
	require.True(t, ok, "type Documented must exist in schema")
	assert.NotEmpty(t, typ.Documentation(), "doc comment should be preserved on type")
	assert.Contains(t, typ.Documentation(), "documentation",
		"doc comment should contain the expected text")
}

// TestLexical_CommentInsideStringNotComment verifies that comment-like
// character sequences inside string/regex literals are not treated as
// comments. If the schema compiles, the "//" inside the regex pattern
// was correctly lexed as part of the literal, not as a line comment.
func TestLexical_CommentInsideStringNotComment(t *testing.T) {
	t.Parallel()

	// The regex pattern contains "//" which would break compilation
	// if the lexer incorrectly treated it as a comment start.
	src := `schema "CommentInString"

type URLRecord {
    url String required

    // The regex below contains "//" as part of the URL protocol pattern.
    // If the lexer treats "//" inside a regex as a comment, this fails.
    ! "url_has_protocol" url =~ /^https?:\/\/.+/
}
`
	loadSchemaString(t, src, "comment_in_string.yammm")
}

// TestLexical_LcKeywordAsPropertyName verifies that lowercase keyword
// forms (schema, type, required, primary, extends) can be used as
// property names without ambiguity (SPEC.md Section 2.4: "lowercase
// keywords are context-sensitive and may appear as property identifiers").
func TestLexical_LcKeywordAsPropertyName(t *testing.T) {
	t.Parallel()

	v := loadSchema(t, "testdata/lexical/lc_keyword_property.yammm")
	records := loadTestData(t, "testdata/lexical/data.json", "Record")
	for _, raw := range records {
		assertValid(t, v, "Record", raw)
	}
}

// TestLexical_LiteralForms verifies that integer, float, boolean, string,
// and regex literals are correctly recognized in expression contexts
// (SPEC.md Section 2.5: Literals). The schema uses invariants that
// exercise each literal form; validation of a conforming instance proves
// they were all parsed correctly.
func TestLexical_LiteralForms(t *testing.T) {
	t.Parallel()

	v := loadSchema(t, "testdata/lexical/literals.yammm")
	records := loadTestData(t, "testdata/lexical/data.json", "LiteralTest")
	for _, raw := range records {
		assertValid(t, v, "LiteralTest", raw)
	}
}
