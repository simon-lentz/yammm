// Package format provides canonical formatting for .yammm schema files.
//
// The formatter applies parse-tree-assisted token-stream formatting: it lexes
// and parses the input, then rewrites the token stream with canonical spacing,
// indentation, column alignment, and line wrapping. The output is deterministic
// and idempotent (formatting an already-formatted file produces the same
// output).
//
// It reads the source twice, because neither view alone carries what phase 1
// needs: the token stream holds the whitespace and comments to preserve, and
// the node tree holds the invariant expression extents and the syntax verdict.
//
// # Entry Point
//
// [TokenStream] is the primary entry point. It accepts raw schema text and
// returns the formatted result:
//
//	formatted, err := format.TokenStream(input)
//	if err != nil {
//	    // input has parse errors; formatting cannot proceed
//	}
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
//     at the [LineWidthThreshold] (100 columns).
//  4. Column alignment: aligns property types and modifiers within type blocks.
//  5. Text finalization: trims trailing whitespace from each line, removes
//     trailing blank lines, and ensures the file ends with a newline.
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
// surrounding declarations.
//
// # Additional Functions
//
// Two pipeline phases and two helpers are exported, and none of them has a
// consumer outside this package today:
//
//   - [WrapLongLines]: phase 3, wrap lines exceeding [LineWidthThreshold]
//   - [AlignColumns]: phase 4, align property types and modifiers within type blocks
//   - [NormalizeIndentation]: a phase-1 helper over one line, convert spaces to tabs
//   - [DisplayWidth]: compute visual width of a line (tab-aware)
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
//	format  ──imports──▶  (stdlib + diag + location + internal/parse)
package format
