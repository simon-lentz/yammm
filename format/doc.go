// Package format provides canonical formatting for .yammm schema files.
//
// The formatter applies parse-tree-assisted token-stream formatting: it lexes
// and parses the input using the ANTLR grammar, then rewrites the token stream
// with canonical spacing, indentation, column alignment, and line wrapping.
// The output is deterministic and idempotent (formatting an already-formatted
// file produces the same output).
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
// The formatter applies four phases in order:
//
//  1. Token-stream rewriting: canonical spacing between tokens, indentation
//     normalization, expression region preservation.
//  2. Blank line collapsing: removes excess blank lines while preserving
//     intentional section breaks.
//  3. Line wrapping: wraps long lines (enums, extends clauses, invariants)
//     at the [LineWidthThreshold].
//  4. Column alignment: aligns property types and modifiers within type blocks.
//
// The formatter is used by the LSP server for textDocument/formatting and by
// the CLI for the "yammm fmt" command.
package format
