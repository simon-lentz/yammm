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
// [TokenStream] is the primary entry point. It accepts raw schema text and
// returns the formatted result:
//
//	formatted, err := format.TokenStream(input)
//	if err != nil {
//	    // the input does not parse, or the output would not preserve it
//	}
//
// # Preservation
//
// Whitespace, and a trailing comma before "]" or "{", are the only things the
// formatter may change. After phase 5, when the output differs from the input,
// [TokenStream] compares the two token sequences with those set aside, and every
// comment's text line by line. On a mismatch it returns an error wrapping
// [ErrNotPreserved] and no output: the defect is the formatter's, never the
// input's, so a caller writes nothing. An input already formatted is returned
// unchanged and costs no comparison.
//
// # Formatting Pipeline
//
// The formatter applies five phases in order:
//
//  1. Token-stream rewriting: canonical spacing between tokens, indentation
//     normalization, expression region preservation.
//  2. Blank line collapsing: removes excess blank lines while preserving
//     intentional section breaks.
//  3. Line wrapping: wraps long lines (enums, extends clauses, invariants)
//     at the [LineWidthThreshold] (100 display cells).
//  4. Column alignment: aligns property types and modifiers within type blocks.
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
// [WrapLongLines] and [AlignColumns] lex the text they are given. A caller that
// enters at one of these phases has no record to inherit.
//
// # Annotation Spacing
//
// An annotation is written tight: no space between the @ / @@ sigil and the
// name that follows it, and none between the name and its argument list. The
// second rule is decided from token-stream state rather than from the token
// pair alone, because a name followed by "(" is also a relation multiplicity
// (`worksAt (one)`), which keeps its space.
//
// A line beginning with a sigil is a declaration in its own right, so line
// wrapping never folds it into the line above as a continuation, and a
// type-level @@ member keeps its own indented line inside the type body.
//
// Annotations are not aligned into a column of their own: each trails the
// property it decorates by a single space, whatever the width of the
// surrounding declarations. A property that carries an annotation is never
// wrapped, because the annotation has no legal place on a continuation line.
//
// # Additional Functions
//
// Two pipeline phases and two helpers are exported. Nothing outside this
// package calls them, apart from the gate that pins these signatures:
//
//   - [WrapLongLines]: phase 3, wrap lines exceeding [LineWidthThreshold]
//   - [AlignColumns]: phase 4, align property types and modifiers within type blocks
//   - [NormalizeIndentation]: a phase-1 helper over one line, convert spaces to tabs
//   - [DisplayWidth]: the one width function, counting display cells
//
// [TokenStream] is what the LSP server calls for textDocument/formatting, and
// what the CLI calls for the "yammm fmt" command.
//
// # Thread Safety
//
// All functions are stateless and safe for concurrent use.
//
// # Dependencies
//
//	format  ──imports──▶  (stdlib + diag + location + internal/parse),
//	                      golang.org/x/text/width
package format
