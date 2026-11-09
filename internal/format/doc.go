// Package format provides canonical formatting for .yammm schema files.
//
// The formatter applies parse-tree-assisted token-stream formatting: it lexes
// and parses the input, then rewrites the token stream with canonical spacing,
// indentation, column alignment, and line wrapping. The output is deterministic
// and idempotent (formatting an already-formatted file produces the same
// output).
//
// It needs two views of one source, and parse.LexAndParse returns both from a
// single lex: the token stream holds the whitespace and comments to preserve,
// and the node tree holds the invariant expression extents and the syntax
// verdict.
//
// # Entry Point
//
// [Source] is the entry point, in go/format's shape. It accepts the bytes of a
// schema file and returns the formatted bytes:
//
//	formatted, err := format.Source(src)
//	if err != nil {
//	    // the input does not parse, or the output would not preserve it
//	}
//
// A source that does not parse returns a [*SyntaxError] carrying the parser's
// positioned diagnostic. A source parses when the grammar matches its tokens
// and every escape in a string outside an invariant expression and an
// annotation argument is one the language has, with well-formed digits and no
// surrogate code point. So a source a load refuses for any other reason still
// formats, such as one with inverted bounds or an unknown annotation.
//
// # Preservation
//
// Whitespace, a trailing comma before "]" or "{", and a leading byte order mark
// are the only things the formatter may change. After phase 5, when the output
// differs from the input with its line ends made LF, [Source] compares the
// two token sequences with those set aside, and every comment's text line by
// line. On a mismatch it returns an error wrapping
// [ErrNotPreserved] and no output: the defect is the formatter's, never the
// input's, so a caller writes nothing. An input already formatted is returned
// unchanged and costs no comparison.
//
// # Formatting Pipeline
//
// The formatter applies five phases in order:
//
//  1. Token-stream rewriting: canonical spacing between tokens, indentation
//     normalization, each file-scope declaration (its doc comment first) and
//     each type-body member on a line of its own, expression region
//     preservation.
//  2. Blank line collapsing: removes excess blank lines while preserving
//     intentional section breaks.
//  3. Line wrapping: wraps long lines (enums, extends clauses, invariants)
//     at 100 display cells.
//  4. Column alignment: pads the name column of each run of properties, of
//     relationships and of datatype declarations, and lines up their trailing
//     comments.
//  5. Text finalization: trims trailing whitespace from each line, removes
//     trailing blank lines, and ensures the file ends with a newline.
//
// Phase 1 records the lexer's view of each line as it emits that line: the
// line's class — blank, comment, or content — the offset where a trailing
// comment starts, and the extent of every string and regex literal. A blank
// line inside a block comment is comment text. Phases 2 and 3 read this record.
// Phase 3 builds lines from pieces of others, so when it changed the text,
// phase 4 reads a record lexed from phase 3's text; when it did not, phase 4
// reads phase 1's. No phase derives the record from the text by hand.
//
// A comma inside a string literal is therefore not a value separator, a bracket
// inside a comment does not open a construct, and a comment line is never
// wrapped, aligned, or read as a value or type name.
//
// # Annotation Spacing
//
// The formatter removes a space between the @ or @@ sigil and its name. A newline
// there stays. It also removes the whitespace between the name and a "(" that
// directly follows it, and a comment between them keeps its line. The
// second rule is decided from token-stream state rather than from the token
// pair alone, because a name followed by "(" is also a relation multiplicity
// (`worksAt (one)`), which keeps its space.
//
// A line beginning with a sigil is a declaration in its own right, so line
// wrapping never folds it into the line above as a continuation, and a
// type-level @@ member keeps its own indented line inside the type body.
//
// Annotations are not aligned into a column of their own: one written on its
// property's line trails the property by a single space, whatever the width of
// the surrounding declarations. Line wrapping leaves a single-line property that
// carries an annotation on its line, because the annotation has no legal place on
// a continuation line. A multi-line Enum with an annotation on its closing line
// stays multi-line when it is too wide to fold, and a load refuses it with
// E_INVALID_ANNOTATION.
//
// # Callers
//
// The package is internal to the module. [Source] is what the language server
// calls for textDocument/formatting, and what the CLI calls for the "yammm fmt"
// command.
//
// # Thread Safety
//
// All functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	internal/format  ──imports──▶  (stdlib + diag + location + internal/parse),
//	                               golang.org/x/text/width
package format
